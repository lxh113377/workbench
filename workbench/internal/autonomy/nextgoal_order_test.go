package autonomy

import (
	"errors"
	"testing"

	"workbench/internal/model"
)

// issue #1 / D6 守卫：闭环「推导下一目标」必须**先写决策台账，再切活跃目标**。
//
// 为什么单独测这个顺序：SetActive 与 AppendDecision 写两个不同文件
// （goals.json / decisions.jsonl），非原子。顺序反了不会报错，只会开出一个
// 时间窗口 —— 观察者看到 active 已切换而台账缺 nextGoal，把「闭环正常」误判成
// 「闭环没推导」。CI 上 TestRunOneDrivesAutonomyClosedLoop 的偶发红就是撞窗口，
// 而时序类偶发在评审里最难防，故把顺序固化成可断言的契约。

// TestWriteLedgerThenActivateOrder 直接断言调用次序（不依赖任何时间/IO）。
func TestWriteLedgerThenActivateOrder(t *testing.T) {
	var calls []string
	err := writeLedgerThenActivate(
		func() error { calls = append(calls, "ledger"); return nil },
		func() error { calls = append(calls, "activate"); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "ledger" || calls[1] != "activate" {
		t.Fatalf("顺序必须是 先留痕后生效，实际 %v", calls)
	}
}

// TestWriteLedgerThenActivateErrorSemantics 钉死两条失效语义：
//   - 留痕失败**不阻断**生效（台账写不进不该让自主线停摆）；
//   - 生效的错误**必须**返回给调用方。
func TestWriteLedgerThenActivateErrorSemantics(t *testing.T) {
	ledgerErr := errors.New("disk full")
	var calls []string
	err := writeLedgerThenActivate(
		func() error { calls = append(calls, "ledger"); return ledgerErr },
		func() error { calls = append(calls, "activate"); return nil },
	)
	if err != nil {
		t.Fatalf("留痕失败不应让整体失败（自主线不得因台账停摆）: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("留痕失败后仍须尝试生效: %v", calls)
	}

	activateErr := errors.New("cannot persist")
	err = writeLedgerThenActivate(
		func() error { return nil },
		func() error { return activateErr },
	)
	if !errors.Is(err, activateErr) {
		t.Fatalf("生效错误必须原样返回，实际 %v", err)
	}
}

// TestGoalCompletionWritesNextGoalLedgerBeforeActivating 钉死完成后的**结果不变量**：
// 目标完成时「活跃目标已切换」与「台账含 nextGoal」两者必须同时成立。
//
// 诚实边界（变异测试实测）：本用例同步读取，即使顺序被反转（先生效后留痕）
// 它也**照样通过** —— 因为两次写在同一临界区内都已跑完。真正抓顺序回归的是
// 上面 TestWriteLedgerThenActivateOrder（把 seam 换成反序后它立刻 FAIL）。
// 本用例的价值是守住另一半：别把 nextGoal 决策写丢、别让 active 没切过去。
// 引擎层 TestRunOneDrivesAutonomyClosedLoop 从别的 goroutine 轮询，才是撞窗口的观测点。
func TestGoalCompletionWritesNextGoalLedgerBeforeActivating(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 3
	m, _ := newTestManager(t, cfg)
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	g.Criteria = []Criterion{{ID: "c-done", Desc: "完成 1 个阶段", Kind: CriterionTaskDone, Threshold: 1}}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}

	out := m.CompleteStage(g.ID, &model.Task{ID: "t1", Title: "阶段一", Critique: &model.Critique{Quality: 9}}, 900, nil, nil)
	if !out.GoalCompleted {
		t.Fatalf("应判定目标完成: %+v", out.Acceptance)
	}
	if out.NextGoalID == "" {
		t.Fatal("必须推导出下一目标")
	}

	// 完成瞬间：两个可观测面必须都已成立（旧顺序下这里会偶发失败）。
	active, ok := m.Active()
	if !ok || active == nil || active.ID != out.NextGoalID {
		t.Fatalf("活跃目标应已切换为新目标: %+v ok=%v", active, ok)
	}
	var hasNextGoal bool
	for _, d := range m.Decisions(200) {
		if d.Kind == DecisionNextGoal && d.GoalID == out.NextGoalID {
			hasNextGoal = true
			break
		}
	}
	if !hasNextGoal {
		t.Fatal("活跃目标已切换时，台账必须已含该目标的 nextGoal 决策（顺序回归守卫）")
	}
}

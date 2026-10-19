package engine

import (
	"testing"

	"workbench/internal/model"
)

// L4 崩溃救援的判据测试。分两层：
//   1) decideRescue 纯函数表驱动（含负面：额度未配/负数、计数恰好到界、超界）
//   2) rescueInterrupted 的端到端行为（累加、重新入队、额度耗尽判死、面板可见的 ErrorKind）

func TestDecideRescueBoundaries(t *testing.T) {
	cases := []struct {
		name         string
		crashRescues int // 累加后的序号：本次是第几轮救援
		budget       int // 允许的救援轮数
		want         rescueAction
	}{
		{"第 1 轮", 1, 3, rescueRequeue},
		{"第 2 轮", 2, 3, rescueRequeue},
		{"最后一轮（3/3）", 3, 3, rescueRequeue},
		{"超出额度（第 4 轮）", 4, 3, rescueCondemn},
		{"远超额度", 7, 3, rescueCondemn},
		{"序号 0（调用方忘了累加）→ 保守放行", 0, 3, rescueRequeue},
		// 负面：额度未配置 / 非法 —— 只救一轮，既不集体判死也不无界
		{"额度 0 + 第 1 轮 → 救", 1, 0, rescueRequeue},
		{"额度 0 + 第 2 轮 → 判死（防无界）", 2, 0, rescueCondemn},
		{"额度负数 + 第 1 轮 → 救", 1, -5, rescueRequeue},
		{"额度负数 + 已救过 → 判死", 3, -5, rescueCondemn},
		// 负面：计数为负（数据被外部改坏）不得 panic，也不得无界
		{"计数负数 + 正常额度 → 救", -1, 3, rescueRequeue},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decideRescue(c.crashRescues, c.budget); got != c.want {
				t.Fatalf("decideRescue(%d, %d) = %s, want %s", c.crashRescues, c.budget, got, c.want)
			}
		})
	}
}

func TestRescueActionString(t *testing.T) {
	if rescueRequeue.String() != "requeue" || rescueCondemn.String() != "condemn" {
		t.Fatal("rescueAction 字符串化用于日志与审计，不能为空")
	}
}

// TestRescueInterruptedConsumesBudget 是核心回归：崩溃恢复必须**消耗额度**，
// 且计数跨崩溃累加（改造前 Attempts 会被 runOne 重置，等于没有额度）。
func TestRescueInterruptedConsumesBudget(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-crash-budget"
	if err := st.Create(&model.Task{ID: id, Title: "反复崩溃", State: model.StateCandidate, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning, model.StateInterrupted} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	// 连续救援：前 maxCrashRescues 次应重新入队，之后应判死
	for i := 1; i <= maxCrashRescues; i++ {
		cur, ok := st.Get(id)
		if !ok || cur.State != model.StateInterrupted {
			t.Fatalf("第 %d 轮开始前应为 interrupted，实际 %+v", i, cur)
		}
		nt := e.rescueInterrupted(cur)
		if nt == nil {
			t.Fatalf("第 %d 次救援（额度 %d）不应判死", i, maxCrashRescues)
		}
		if nt.State != model.StateQueued {
			t.Fatalf("救援成功应回到 queued，实际 %s", nt.State)
		}
		after, _ := st.Get(id)
		if after.CrashRescues != i {
			t.Fatalf("CrashRescues = %d, want %d（额度必须累加）", after.CrashRescues, i)
		}
		// 模拟再次崩溃：queued -> running -> interrupted
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.Transition(id, model.StateInterrupted, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	// 额度已耗尽：本次必须判死且不再入队
	cur, _ := st.Get(id)
	if nt := e.rescueInterrupted(cur); nt != nil {
		t.Fatalf("额度耗尽后仍返回可入队任务，无界崩溃循环没被阻断: %+v", nt)
	}
	got, _ := st.Get(id)
	if got.State != model.StateFailed {
		t.Fatalf("额度耗尽应为 failed，实际 %s", got.State)
	}
	// 面板「失败原因分布」按 ErrorKind 聚合，必须能区分这类问题
	if got.ErrorKind != crashRescueKind {
		t.Fatalf("ErrorKind = %q, want %q（否则与执行失败混在一起，运维看不出是崩溃循环）", got.ErrorKind, crashRescueKind)
	}
	if got.Error == "" {
		t.Fatal("判死必须写人读原因")
	}
}

// TestRescueInterruptedIsTerminal 确认判死后不会被再次救活（终态）。
func TestRescueInterruptedIsTerminal(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-crash-terminal"
	if err := st.Create(&model.Task{ID: id, Title: "判死", State: model.StateCandidate, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning, model.StateInterrupted} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	// 把额度直接打满，再救援 → 判死
	if _, err := st.Update(id, func(x *model.Task) error { x.CrashRescues = maxCrashRescues; return nil }); err != nil {
		t.Fatal(err)
	}
	cur, _ := st.Get(id)
	if e.rescueInterrupted(cur) != nil {
		t.Fatal("额度已满应返回 nil")
	}
	// 判死后再走救援路径不得复活（注意：failed -> queued 本身是合法的
	// **人工重试**通道，这里锁的是"救援逻辑不会自动把它拉回去"）。
	retry, _ := st.Get(id)
	if nt := e.rescueInterrupted(retry); nt != nil {
		t.Fatalf("已判死的任务不得被救援路径复活: %+v", nt)
	}
	again, _ := st.Get(id)
	if again.State != model.StateFailed {
		t.Fatalf("应为终态 failed，实际 %s", again.State)
	}
	if again.ErrorKind != crashRescueKind {
		t.Fatalf("ErrorKind = %q, want %q", again.ErrorKind, crashRescueKind)
	}
}

// TestRecoverAppliesRescueBudget 端到端：Recover() 走的就是带额度的救援分支。
func TestRecoverAppliesRescueBudget(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-recover-budget"
	if err := st.Create(&model.Task{ID: id, Title: "恢复", State: model.StateCandidate, MaxAttempts: 3}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	// 前 N 次 Recover 应把它救回 queued；第 N+1 次应判死
	for i := 1; i <= maxCrashRescues; i++ {
		if err := e.Recover(); err != nil {
			t.Fatal(err)
		}
		got, _ := st.Get(id)
		if got.State != model.StateQueued {
			t.Fatalf("第 %d 次 Recover 后应为 queued，实际 %s", i, got.State)
		}
		if got.CrashRescues != i {
			t.Fatalf("第 %d 次 Recover 后 CrashRescues = %d", i, got.CrashRescues)
		}
		// 再次崩溃（模拟进程死亡留下的 running）
		if _, err := st.Update(id, func(x *model.Task) error { x.State = model.StateRunning; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.Recover(); err != nil {
		t.Fatal(err)
	}
	final, _ := st.Get(id)
	if final.State != model.StateFailed {
		t.Fatalf("额度耗尽后第 %d 次 Recover 应判死，实际 %s —— 无界崩溃循环未被阻断", maxCrashRescues+1, final.State)
	}
	if final.ErrorKind != crashRescueKind {
		t.Fatalf("ErrorKind = %q", final.ErrorKind)
	}
}

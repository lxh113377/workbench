package engine

// 重新规划分支的判据测试。
//
// 这一支是"闭环"的最后一段：返工用尽、方案层面仍然错时，必须回到方案模型重出方案。
// 判据分三层：
//  1. 触发条件 —— 返工轮用尽 && 方案轮还有额度 && 不是退化方案；
//  2. 状态效果 —— PlanRounds+1、任务回到 queued、错误类别标为 replan；
//  3. 前提还原 —— 重新规划前必须把 Prompt 还原成原始需求（不是上一轮返工 prompt）。

import (
	"strings"
	"testing"

	"workbench/internal/blueprint"
	"workbench/internal/config"
	"workbench/internal/model"
)

func planEnabledCfg() config.Config {
	c := config.DefaultConfig()
	c.Plan.Enabled = true
	c.Plan.ReplanRounds = 2
	return c
}

// TestShouldReplanGates 钉住四个触发判据。
func TestShouldReplanGates(t *testing.T) {
	cfg := planEnabledCfg()
	task := &model.Task{ID: "t1"}
	solid := &blueprint.Blueprint{Approach: "x", Steps: []blueprint.Step{{Do: "a"}}}

	if !shouldReplan(task, solid, cfg) {
		t.Fatal("首次验收失败（有方案、有额度）应进入重新规划")
	}

	// 1) 方案阶段关闭 -> 永不重规划
	off := cfg
	off.Plan.Enabled = false
	if shouldReplan(task, solid, off) {
		t.Fatal("方案阶段关闭时不得重新规划（兼容模式）")
	}

	// 2) 轮数用尽
	usedUp := &model.Task{ID: "t2", PlanRounds: 2}
	if shouldReplan(usedUp, solid, cfg) {
		t.Fatal("PlanRounds 达到 ReplanRounds 后必须停止（防无限重规划）")
	}

	// 3) 额度为 0
	zero := cfg
	zero.Plan.ReplanRounds = 0
	if shouldReplan(task, solid, zero) {
		t.Fatal("ReplanRounds=0 表示关闭重新规划")
	}

	// 4) 退化方案 -> 没有方案可改
	deg := &blueprint.Blueprint{Degraded: true}
	if shouldReplan(task, deg, cfg) {
		t.Fatal("退化方案（方案层当时不可用）不得进入重新规划：没有方案可改")
	}

	// 5) nil 方案（老任务/异常）仍允许：否则历史任务永远学不到"重新想"这条路
	if !shouldReplan(task, nil, cfg) {
		t.Fatal("无方案记录时应当允许重新规划")
	}
}

// TestScheduleReplanResetsToQueuedAndCountsRound 钉住状态效果与前提还原。
func TestScheduleReplanResetsToQueuedAndCountsRound(t *testing.T) {
	e, st, _ := newTestEngine(t)
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Plan.Enabled = true
		c.Plan.ReplanRounds = 2
	}); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()

	original := "原始需求：写一个能打印 ok 的脚本"
	task := &model.Task{ID: "task-replan", Title: "写脚本", State: model.StateCandidate, Prompt: original}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, stt := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(task.ID, stt, "test", ""); err != nil {
			t.Fatalf("迁移 %s 失败: %v", stt, err)
		}
	}
	// 模拟：返工环已经把 Prompt 换成了返工说明（这正是必须还原的场景）。
	if _, err := st.Update(task.ID, func(x *model.Task) error {
		x.Prompt = "## 返工说明（第 2 轮）\n你上一次的产出没有通过验收……"
		x.RepairRounds = 3
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 原始 prompt 由 store 落盘（真实路径）；这里显式写一份以模拟 store 行为。
	if err := st.SaveTaskArtifact(task.ID, "prompt.md", []byte(original)); err != nil {
		t.Fatal(err)
	}

	runTask, _ := st.Get(task.ID)
	// 入队前置：scheduleReplan 走 running->queued，需要队列侧不报错（q 为空也能 Push）。
	e.scheduleReplan(runTask, &model.VerifyReport{Pass: false, FailReason: "python main.py 退出码 1"}, planEnabledCfg())

	after, ok := st.Get(task.ID)
	if !ok {
		t.Fatal("任务丢失")
	}
	if after.PlanRounds != 1 {
		t.Fatalf("PlanRounds = %d, want 1", after.PlanRounds)
	}
	if after.State != model.StateQueued {
		t.Fatalf("state = %s, want queued（重新规划必须回到队列）", after.State)
	}
	if after.ErrorKind != "replan" {
		t.Fatalf("ErrorKind = %q, want replan", after.ErrorKind)
	}
	if after.Prompt != original {
		t.Fatalf("重新规划前必须把 Prompt 还原为原始需求，实际: %.80q", after.Prompt)
	}
	if after.RepairRounds != 3 {
		t.Fatalf("重新规划不应重置 RepairRounds，实际 %d", after.RepairRounds)
	}
}

// TestCurrentPlanReadsLatestRound 钉住"方案是可复核证据"（读盘而非内存）。
func TestCurrentPlanReadsLatestRound(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-plan-read", Title: "t", State: model.StateCandidate, Prompt: "p"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if got := currentPlan(task.ID, e); got != nil {
		t.Fatalf("还没有方案时应返回 nil，实际 %+v", got)
	}
	// 落两轮方案，必须读到轮号更大的那一版。
	for round, approach := range map[int]string{0: "第一版思路", 1: "第二版思路"} {
		bp := &blueprint.Blueprint{Approach: approach, Round: round}
		stg := &planStage{Blueprint: bp, Model: "m"}
		e.savePlan(task, stg, "prompt", round)
	}
	got := currentPlan(task.ID, e)
	if got == nil {
		t.Fatal("方案已落盘却读不到")
	}
	if got.Approach != "第二版思路" {
		t.Fatalf("应读到最新一版方案，实际 %q", got.Approach)
	}
}

// TestPlanFailureContextCollectsEvidence 钉住"重新规划要拿到真凭实据"。
func TestPlanFailureContextCollectsEvidence(t *testing.T) {
	cfg := planEnabledCfg()
	// 首轮无证据
	fresh := &model.Task{ID: "t", Prompt: "p"}
	if got := planFailureContext(fresh, cfg); got != "" {
		t.Fatalf("首轮不应有失败上下文，实际: %q", got)
	}
	// 有验收报告时，必须带上命令、退出码与 stderr
	failed := &model.Task{
		ID: "t2", RepairRounds: 2, ErrorKind: "verify_failed", Error: "验收未通过",
		Verify: &model.VerifyReport{
			Pass: false, FailReason: "命令失败",
			Outcomes: []model.VerifyOutcome{{
				Cmd: "python main.py", Exit: 1, Expect: 0, Pass: false,
				Stderr: "SyntaxError: invalid syntax",
			}},
		},
	}
	got := planFailureContext(failed, cfg)
	for _, want := range []string{"2 轮返工", "python main.py", "退出码: 1", "SyntaxError"} {
		if !strings.Contains(got, want) {
			t.Fatalf("失败上下文缺少 %q:\n%s", want, got)
		}
	}
}

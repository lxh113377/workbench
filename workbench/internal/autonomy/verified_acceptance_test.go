package autonomy

// verified_acceptance_test.go 守护「c-verified 判据必须真的读到任务的 Verified 标记」。
//
// 现场缺陷（2026-10-19 09:16）：任务已经 verified=true、verify.json pass=true，
// 目标进度里 c-verified 仍是 0/1 —— 因为 CompleteStage 调 Evaluate 时固定传
// Tasks: nil，Evaluate 里的 taskByID 永远为空，VerifiedCount 恒为 0。
// 后果：目标永远无法完成，goalDone/nextGoal 永不到达（自主闭环断在最后一环）。

import (
	"testing"

	"workbench/internal/model"
)

// TestCompleteStageCountsVerifiedTask 是本轮现场缺陷的回归测试。
func TestCompleteStageCountsVerifiedTask(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MinQuality = 1
	m, _ := newTestManager(t, cfg)
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	// 目标要求：1 个阶段 + 该阶段产出通过真实执行验收
	g.Criteria = []Criterion{
		{ID: "c-done", Desc: "完成 1 个阶段", Kind: CriterionTaskDone, Threshold: 1},
		{ID: "c-verified", Desc: "至少 1 个阶段通过真实执行验收", Kind: CriterionVerified, Threshold: 1},
	}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}

	// 任务已经过真实执行验收（Verified=true）
	task := &model.Task{ID: "t-verified", Title: "已验收阶段", Verified: true,
		Critique: &model.Critique{Quality: 9}}
	outcome := m.CompleteStage(g.ID, task, 600, nil, []*model.Task{task})

	if !outcome.GoalCompleted {
		t.Fatalf("Verified=true 的阶段必须让目标完成，实际 reason=%q criteria=%+v",
			outcome.Acceptance.Reason, outcome.Acceptance.Criteria)
	}
	if outcome.Acceptance.VerifiedCount != 1 {
		t.Fatalf("VerifiedCount 应为 1，实际 %d（说明任务表没被读进去）", outcome.Acceptance.VerifiedCount)
	}
}

// TestCompleteStageRejectsUnverifiedTask 反向守护：未过真实验收的任务不得让目标完成。
func TestCompleteStageRejectsUnverifiedTask(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MinQuality = 1
	m, _ := newTestManager(t, cfg)
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	g.Criteria = []Criterion{
		{ID: "c-done", Desc: "完成 1 个阶段", Kind: CriterionTaskDone, Threshold: 1},
		{ID: "c-verified", Desc: "至少 1 个阶段通过真实执行验收", Kind: CriterionVerified, Threshold: 1},
	}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	// Verified=false（旧的纯 Markdown 产出）
	task := &model.Task{ID: "t-unverified", Title: "未验收阶段", Verified: false,
		Critique: &model.Critique{Quality: 9}}
	outcome := m.CompleteStage(g.ID, task, 600, nil, []*model.Task{task})
	if outcome.GoalCompleted {
		t.Fatal("未过真实验收的阶段不得让目标完成（否则 c-verified 形同虚设）")
	}
}

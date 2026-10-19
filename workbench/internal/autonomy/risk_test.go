package autonomy

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func TestCheckStageRiskLowQuality(t *testing.T) {
	goal := &Goal{ID: "g", Status: GoalActive, Stages: []Stage{{Index: 0, Passed: true}}}
	task := &model.Task{ID: "t", Title: "低质", Critique: &model.Critique{Quality: 3}}
	cfg := testCfg()
	rep, stop := CheckStageRisk(goal, task, cfg, nil)
	if stop {
		t.Fatal("单个低质阶段不应直接阻塞")
	}
	if rep.Level == RiskNone {
		t.Fatal("低质应被识别为风险")
	}
	found := false
	for _, it := range rep.Items {
		if it.Code == "quality.belowMin" && it.Action == "rollback" {
			found = true
		}
	}
	if !found {
		t.Fatalf("缺少质量风险项: %+v", rep.Items)
	}
}

func TestCheckStageRiskBlocksOnBudget(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 2
	goal := &Goal{ID: "g", Status: GoalActive, Stages: []Stage{{Index: 0}, {Index: 1}}}
	task := &model.Task{ID: "t", Title: "第三阶段", Critique: &model.Critique{Quality: 8}}
	rep, stop := CheckStageRisk(goal, task, cfg, nil)
	if !stop {
		t.Fatalf("阶段数超上限时必须停止并阻塞: %+v", rep)
	}
	if rep.Level != RiskHigh {
		t.Fatalf("预算耗尽应为高风险，实际 %s", rep.Level)
	}
}

func TestCheckStageRiskAllowsCompletedBudget(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 1
	goal := &Goal{ID: "g", Status: GoalActive,
		Stages:   []Stage{{Index: 0}},
		Criteria: []Criterion{{ID: "c", Kind: CriterionTaskDone, Threshold: 1, Current: 1, Passed: true}},
	}
	task := &model.Task{ID: "t", Title: "已完成", Critique: &model.Critique{Quality: 8}}
	_, stop := CheckStageRisk(goal, task, cfg, nil)
	if stop {
		t.Fatal("标准已全部通过时预算到点不应阻塞（目标可以正常收尾）")
	}
}

func TestClampBudget(t *testing.T) {
	bad := config.AutonomyBudget{MaxTasksPerGoal: -1, MaxStages: 999, MinQuality: 0, MaxRollbacks: -5, StageEvery: 0}
	got := ClampBudget(bad)
	if got.MaxTasksPerGoal <= 0 || got.MaxTasksPerGoal > 200 {
		t.Fatalf("maxTasksPerGoal 未钳制: %d", got.MaxTasksPerGoal)
	}
	if got.MaxStages <= 0 || got.MaxStages > 50 {
		t.Fatalf("maxStages 未钳制: %d", got.MaxStages)
	}
	if got.MinQuality < 1 || got.MinQuality > 10 {
		t.Fatalf("minQuality 未钳制: %d", got.MinQuality)
	}
	if got.MaxRollbacks < 0 || got.MaxRollbacks > 20 {
		t.Fatalf("maxRollbacks 未钳制: %d", got.MaxRollbacks)
	}
	if got.StageEvery < 1 || got.StageEvery > 10 {
		t.Fatalf("stageEvery 未钳制: %d", got.StageEvery)
	}
}

func TestShouldRollbackRespectsBudget(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxRollbacks = 2
	goal := &Goal{ID: "g", Rollbacks: 2}
	v := QualityVerdict{Pass: false, Quality: 2, MinQuality: 6}
	if ShouldRollback(goal, v, cfg) {
		t.Fatal("回退次数已达上限时不应再回退")
	}
	goal.Rollbacks = 1
	if !ShouldRollback(goal, v, cfg) {
		t.Fatal("未达上限时应回退")
	}
	if ShouldRollback(goal, QualityVerdict{Pass: true}, cfg) {
		t.Fatal("已通过的阶段不应回退")
	}
}

func TestCompleteStageBlocksOnRollbackExhausted(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxRollbacks = 1
	cfg.Budget.MaxStages = 10
	m, _ := newTestManager(t, cfg)
	g, _ := m.Bootstrap()
	low := func(id string) *model.Task {
		return &model.Task{ID: id, Title: "低质" + id, Critique: &model.Critique{Quality: 2}}
	}
	out1 := m.CompleteStage(g.ID, low("t1"), 100, nil, nil)
	if !out1.RolledBack || out1.Blocked {
		t.Fatalf("第一次低质应回退且不阻塞: %+v", out1)
	}
	out2 := m.CompleteStage(g.ID, low("t2"), 100, nil, nil)
	if !out2.Blocked {
		t.Fatalf("回退次数耗尽后应阻塞目标: %+v", out2)
	}
	cur, _ := m.Active()
	if cur.Status != GoalBlocked || cur.BlockReason == "" {
		t.Fatalf("目标应处于 blocked 且有原因: %+v", cur)
	}
}

func TestDecisionsAreAppendOnly(t *testing.T) {
	m, _ := newTestManager(t, testCfg())
	g, _ := m.Bootstrap()
	before := len(m.Decisions(1000))
	task := &model.Task{ID: "t1", Title: "正常", Critique: &model.Critique{Quality: 9}}
	m.CompleteStage(g.ID, task, 800, nil, nil)
	after := m.Decisions(1000)
	if len(after) <= before {
		t.Fatalf("验收后决策数应增加: before=%d after=%d", before, len(after))
	}
	for _, d := range after {
		if d.Actor == "" {
			t.Fatalf("决策缺少主体: %+v", d)
		}
		if d.Kind == "" && d.Reason == "" {
			t.Fatalf("决策缺少类型与原因: %+v", d)
		}
	}
}

func TestDisabledAutonomyStillBootstrapsOnlyWhenAsked(t *testing.T) {
	cfg := testCfg()
	cfg.Enabled = false
	m, _ := newTestManager(t, cfg)
	if _, err := m.Bootstrap(); err != nil {
		t.Fatalf("显式 Bootstrap 不应报错: %v", err)
	}
}

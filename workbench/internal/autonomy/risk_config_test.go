package autonomy

// risk_config_test.go 守护「配置里的风险开关真的生效」（2026-10-19 现场审计）。
//
// 现场：config 里有一组**面向运维的安全旋钮**，注释写得很清楚，但**从未被任何代码读取**：
//   Risk.EscalateMinQuality  —— 「低于此值升级为高危」
//   Risk.BlockOnRepeat       —— 「空转是否直接阻塞（默认 false=仅告警）」
//   Rollback.OnCostOverrun   —— 「预算超限是否回退」
//   Bounds.MaxDraftsPerRound —— 「每轮最多草案数（防扇出爆炸）」
//
// 后果：运维在 config.json 里调这些值**毫无效果**，而系统看起来「有这些保护」。
// 这正是需求 3「边界约束规则 / 风险校验机制 / 异常回退策略」的核心开关。
//
// 本测试先钉住正确语义，再让实现跟上。

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// mkGoalWithRollbacks 造一个带回退计数的目标（避免触发 rollbacks 上限）。
func mkGoalWithRollbacks(n int) *Goal {
	return &Goal{ID: "g1", Status: GoalActive, Title: "测试目标", MaxStages: 6, Rollbacks: n}
}

// mkTaskWithQuality 造一个自评为 q 的任务。
// 注意：质量取自 t.Critique.Quality（见 qualityOf 实现），不是 t.Scores。
func mkTaskWithQuality(q int) *model.Task {
	return &model.Task{ID: "t1", Title: "阶段任务", State: model.StateDone,
		Critique: &model.Critique{Quality: q}}
}

// TestEscalateMinQualityRaisesLevel 断言：质量低于 EscalateMinQuality 时，
// 风险等级必须被**升级为高危**（而不是停留在 medium）。
func TestEscalateMinQualityRaisesLevel(t *testing.T) {
	// 注意测试前提（我第一次写错了）：
	// escalate 只在「已经低于 minQ」的前提下才起作用 —— 它是**升级**而非**触发**。
	// 因此必须让 minQ 高于被测质量，否则根本进不了质量分支。
	cfg := config.DefaultAutonomyConfig()
	cfg.Budget.MinQuality = 8 // 8 > 3，确保进入质量分支
	cfg.Risk.EscalateMinQuality = 5

	goal := mkGoalWithRollbacks(0)
	// 质量 3 < minQ(8) 触发；且 3 < escalate(5) => 升级为 high
	rep, _ := CheckStageRisk(goal, mkTaskWithQuality(3), cfg, nil)
	if rep.Level != RiskHigh {
		t.Fatalf("质量 3 低于 EscalateMinQuality=5，等级应为 high，实际 %s", rep.Level)
	}
	// 质量 7 < minQ(8) 触发；但 7 >= escalate(5) => 保持 medium，不升级
	rep2, _ := CheckStageRisk(goal, mkTaskWithQuality(7), cfg, nil)
	if rep2.Level == RiskHigh {
		t.Fatalf("质量 7 不低于 EscalateMinQuality=5，不应为 high，实际 %s", rep2.Level)
	}
	if rep2.Level != RiskMedium {
		t.Fatalf("质量 7 低于下限但未达升级阈值，应为 medium，实际 %s", rep2.Level)
	}
}

// TestBlockOnRepeatActuallyBlocks 断言：BlockOnRepeat=true 时，「空转重复」
// 必须真的停下规划（stop=true），而不只是记一条 low 级提示。
func TestBlockOnRepeatActuallyBlocks(t *testing.T) {
	base := config.DefaultAutonomyConfig()
	base.Budget.MinQuality = 1 // 隔离质量因素
	base.Risk.EscalateMinQuality = 1
	recent := []string{"重复的标题内容用于空转判定", "另一个标题"}
	dupTask := &model.Task{ID: "t2", Title: "重复的标题内容用于空转判定", Summary: "有摘要",
		State: model.StateDone, Critique: &model.Critique{Quality: 9}}

	// 默认（false）：只告警，不停
	off := base
	off.Risk.BlockOnRepeat = false
	_, stopOff := CheckStageRisk(mkGoalWithRollbacks(0), dupTask, off, recent)
	if stopOff {
		t.Fatal("BlockOnRepeat=false 时不应阻断规划")
	}

	// 开启（true）：必须阻断
	on := base
	on.Risk.BlockOnRepeat = true
	rep, stopOn := CheckStageRisk(mkGoalWithRollbacks(0), dupTask, on, recent)
	if !stopOn {
		t.Fatalf("BlockOnRepeat=true 时空转必须阻断规划，实际未阻断（report=%+v）", rep)
	}
}

// TestMaxDraftsPerRoundClampsPlanner 断言「边界硬上限」真的生效。
//
// 现场缺陷：Bounds.MaxDraftsPerRound 与 Plan.MaxDrafts 语义重复，但前者从未被读取。
// 修法：让它以「边界」身份钳制规划产出数（更小者生效）。
func TestMaxDraftsPerRoundClampsPlanner(t *testing.T) {
	newMgr := func(planMax, boundCap int) *Manager {
		cfg := config.DefaultAutonomyConfig()
		cfg.Plan.MaxDrafts = planMax
		cfg.Bounds.MaxDraftsPerRound = boundCap
		return NewManager(nil, func() config.AutonomyConfig { return cfg }, nil, func() int64 { return 1 })
	}

	// 边界更小 => 以边界为准
	m := newMgr(5, 1)
	if got := m.planner.MaxDraftsForTest(); got != 1 {
		t.Fatalf("边界上限 1 应钳制规划产出为 1，实际 %d", got)
	}
	// 规划目标更小 => 以规划为准（边界只做上限，不放宽）
	m2 := newMgr(2, 5)
	if got := m2.planner.MaxDraftsForTest(); got != 2 {
		t.Fatalf("规划目标 2 小于边界 5 时应用 2，实际 %d", got)
	}
	// 边界未配置（0）=> 不干预
	m3 := newMgr(3, 0)
	if got := m3.planner.MaxDraftsForTest(); got != 3 {
		t.Fatalf("边界未配置时不应干预，实际 %d", got)
	}
}

// TestOnCostOverrunControlsRollback 断言：预算超限时，OnCostOverrun 决定
// 「回退」还是「仅阻塞」。默认 true => 回退项必须出现。
func TestOnCostOverrunControlsRollback(t *testing.T) {
	base := config.DefaultAutonomyConfig()
	base.Budget.MinQuality = 1
	base.Budget.MaxStages = 1
	base.Risk.EscalateMinQuality = 1
	goal := mkGoalWithRollbacks(0)
	goal.Stages = []Stage{{Index: 0, Title: "阶段一", Passed: false}} // 已达上限且未验收
	good := mkTaskWithQuality(9)

	// 默认 true：预算超限应允许回退（同时仍会 block）
	on := base
	on.Rollback.OnCostOverrun = true
	repOn, _ := CheckStageRisk(goal, good, on, nil)
	hasRollbackOn := false
	for _, it := range repOn.Items {
		if it.Action == "rollback" {
			hasRollbackOn = true
		}
	}
	if !hasRollbackOn {
		t.Fatalf("OnCostOverrun=true 时预算超限应产出 rollback 项，实际 %+v", repOn)
	}

	// 关闭 false：不得出现 rollback，只 block
	off := base
	off.Rollback.OnCostOverrun = false
	repOff, stopOff := CheckStageRisk(goal, good, off, nil)
	for _, it := range repOff.Items {
		if it.Action == "rollback" {
			t.Fatalf("OnCostOverrun=false 时不应产出 rollback 项，实际 %+v", repOff)
		}
	}
	if !stopOff {
		t.Fatal("预算超限无论如何都必须阻断目标")
	}
}

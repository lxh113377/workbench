package autonomy

// 本文件实现「风险校验与异常回退」：自主循环不能只会在顺利时往前跑，
// 必须在质量下滑、预算耗尽、连续失败时自动踩刹车。
//
// 三类风险（与 docs/自主运行方案.md 的 R1-R3 对应）：
//   R1 质量风险：阶段自评低于下限 => 回退该阶段（不计入进度），连续回退达上限则阻塞目标；
//   R2 预算风险：单目标任务数/阶段数/回退次数超限 => 阻塞目标，等待人工确认后恢复；
//   R3 空转风险：生成物重复、领域越界、Prompt 无交付物 => 规划期拒绝（Bounds），不进执行。
//
// 回退语义刻意做成"软回退"：不删除任何已产出文件、不修改历史任务状态，
// 只在目标树里把该阶段标记为未通过并记一条 rollback 决策。这样：
//   - 产出永远可查（审计友好）；
//   - 回退可逆（人工可把阶段改回 passed 并 resume 目标）；
//   - 决策有据（回退原因与证据落在 decisions.jsonl）。

import (
	"fmt"

	"workbench/internal/config"
	"workbench/internal/model"
)

// RiskLevel 是风险等级。
type RiskLevel string

const (
	RiskNone   RiskLevel = "none"
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

// RiskItem 是一条风险项。
type RiskItem struct {
	Code   string    `json:"code"`
	Level  RiskLevel `json:"level"`
	Detail string    `json:"detail"`
	Action string    `json:"action"` // 建议动作：rollback / block / reject / continue
}

// RiskReport 是一次风险校验的结果。
type RiskReport struct {
	Level RiskLevel  `json:"level"`
	Items []RiskItem `json:"items"`
}

// CheckStageRisk 对刚完成的一个阶段做即时风险校验（任务级，发生在目标验收之前）。
//
// 判定顺序：质量 -> 重复 -> 预算。前面的命中会让 stop=true，调用方据此决定是否继续规划。
func CheckStageRisk(goal *Goal, t *model.Task, cfg config.AutonomyConfig, recentTitles []string) (RiskReport, bool) {
	rep := RiskReport{Level: RiskNone}
	if goal == nil || t == nil {
		return rep, false
	}
	// R1 质量风险
	minQ := cfg.Budget.MinQuality
	if minQ <= 0 {
		minQ = goal.MinQuality
	}
	if minQ <= 0 {
		minQ = 6
	}
	if q := qualityOf(t); q < minQ {
		rep.Items = append(rep.Items, RiskItem{
			Code: "quality.belowMin", Level: RiskMedium,
			Detail: fmt.Sprintf("阶段自评 %d/10 低于下限 %d", q, minQ),
			Action: "rollback",
		})
		rep.Level = RiskMedium
	}
	// R3 空转风险：产出摘要与近期高度重复
	if t.Summary != "" && duplicatedLoose(t.Title, recentTitles) {
		rep.Items = append(rep.Items, RiskItem{
			Code: "loop.duplicated", Level: RiskLow,
			Detail: "本轮标题与近期产出高度相似，可能空转",
			Action: "continue",
		})
		if rep.Level == RiskNone {
			rep.Level = RiskLow
		}
	}
	// R2 预算风险：阶段数/任务数达到上限
	maxStages := cfg.Budget.MaxStages
	if maxStages <= 0 {
		maxStages = goal.MaxStages
	}
	if maxStages > 0 && len(goal.Stages) >= maxStages && !allCriteriaPassed(goal) {
		rep.Items = append(rep.Items, RiskItem{
			Code: "budget.stagesExhausted", Level: RiskHigh,
			Detail: fmt.Sprintf("阶段数已达上限 %d 但目标仍未验收通过", maxStages),
			Action: "block",
		})
		rep.Level = RiskHigh
		return rep, true
	}
	// 回退次数超限
	maxRollbacks := cfg.Budget.MaxRollbacks
	if maxRollbacks <= 0 {
		maxRollbacks = 3
	}
	if goal.Rollbacks >= maxRollbacks {
		rep.Items = append(rep.Items, RiskItem{
			Code: "budget.rollbacksExhausted", Level: RiskHigh,
			Detail: fmt.Sprintf("回退次数已达上限 %d", maxRollbacks),
			Action: "block",
		})
		rep.Level = RiskHigh
		return rep, true
	}
	stop := false
	for _, it := range rep.Items {
		if it.Action == "block" {
			stop = true
		}
	}
	return rep, stop
}

// allCriteriaPassed 判断目标的所有非人工判据是否已通过（用于区分"预算耗尽但已完成"）。
func allCriteriaPassed(goal *Goal) bool {
	if goal == nil || len(goal.Criteria) == 0 {
		return false
	}
	for _, c := range goal.Criteria {
		if c.Kind == CriterionManual {
			continue
		}
		if !c.Passed {
			return false
		}
	}
	return true
}

// RollbackReason 描述一次阶段回退的原因（写进决策台账与按钮提示）。
func RollbackReason(v QualityVerdict) string {
	if v.Pass {
		return ""
	}
	if v.Reason != "" {
		return v.Reason
	}
	return "阶段质量低于下限，回退且不计入目标进度"
}

// ShouldRollback 判断是否应触发阶段回退。
func ShouldRollback(goal *Goal, v QualityVerdict, cfg config.AutonomyConfig) bool {
	if goal == nil || v.Pass {
		return false
	}
	maxRollbacks := cfg.Budget.MaxRollbacks
	if maxRollbacks <= 0 {
		maxRollbacks = 3
	}
	// 已超上限时不再回退（改由 RiskReport 触发 block，避免无限回退循环）。
	if goal.Rollbacks >= maxRollbacks {
		return false
	}
	// 默认开启回退；配置显式关闭时只在"零质量"这种极端情况下兜底回退，
	// 避免关闭开关后低质产出被无限计入目标进度。
	if cfg.Rollback.OnLowQuality {
		return true
	}
	return v.Quality <= 0
}

// ClampBudget 把配置预算钳制到合法区间，返回钳制后的值（Normalize 之外的第二道防线）。
func ClampBudget(b config.AutonomyBudget) config.AutonomyBudget {
	if b.MaxTasksPerGoal <= 0 {
		b.MaxTasksPerGoal = 12
	}
	if b.MaxTasksPerGoal > 200 {
		b.MaxTasksPerGoal = 200
	}
	if b.MaxStages <= 0 {
		b.MaxStages = 6
	}
	if b.MaxStages > 50 {
		b.MaxStages = 50
	}
	if b.MinQuality < 1 {
		b.MinQuality = 6
	}
	if b.MinQuality > 10 {
		b.MinQuality = 10
	}
	if b.MaxRollbacks < 0 {
		b.MaxRollbacks = 3
	}
	if b.MaxRollbacks > 20 {
		b.MaxRollbacks = 20
	}
	if b.StageEvery < 1 {
		b.StageEvery = 1
	}
	if b.StageEvery > 10 {
		b.StageEvery = 10
	}
	return b
}

// duplicatedLoose 宽松重复判定：归一化后前缀 16 字相同即视为重复（用于空转提示，不阻断）。
func duplicatedLoose(title string, recent []string) bool {
	n := normTitle(title)
	if n == "" {
		return false
	}
	for _, r := range recent {
		rn := normTitle(r)
		if rn == "" {
			continue
		}
		if rn == n || len(rn) >= 8 && len(n) >= 8 && rn[:8] == n[:8] {
			return true
		}
	}
	return false
}

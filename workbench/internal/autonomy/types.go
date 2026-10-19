// Package autonomy 实现「自主任务规划 + 闭环自主执行 + 自主验收 + 下一目标推导」。
//
// 设计定位（对应目标要求 1-4）：
//
//  1. 动态任务规划：Goal/Stage/Criterion 三层模型。Goal 是项目级目标（可来自配置、
//     也可由上一目标自评后自动推导），Stage 是阶段性目标，Criterion 是机器可判的验收标准。
//     规划不再是「从 24 个模板里挑一个」，而是「先读项目目标 → 推导当前阶段 → 生成任务草案」。
//  2. 闭环自主执行：Engine 在任务完成后调用 Acceptance 做自主验收；通过则累计目标进度，
//     目标完成则调用 NextGoal 自动推导下一目标并入库；不通过则 RiskCheck 给出重规划/回退建议。
//  3. 兼容与可控：本包不替换模板链路。Autonomy.Enabled=false 时引擎行为与改造前完全一致；
//     所有生成物都要过 Bounds 边界校验与风险预算（任务数/阶段数/回退次数/质量下限）。
//  4. 方案与验证标准：见 docs/自主运行方案.md，判据由 autonomy_*_test.go 与 e2e 自主段覆盖。
//
// 依赖约束：本包只依赖 model / config（纯标准库），不 import engine / store / web，
// 保证可被单测充分覆盖，也不会把调度逻辑耦合进规划逻辑。
package autonomy

import (
	"strings"

	"workbench/internal/model"
)

// GoalStatus 是项目级目标的生命周期状态。
//
// 与 model.TaskState 是两套状态机：任务状态是"这一篇产出走到哪一步"，
// 目标状态是"这条自主线还要不要继续跑"。
type GoalStatus string

const (
	// GoalActive 目标进行中：允许生成阶段任务。
	GoalActive GoalStatus = "active"
	// GoalCompleted 目标已完成（自主验收全通过）：允许自动推导下一目标。
	GoalCompleted GoalStatus = "completed"
	// GoalBlocked 目标被阻塞：预算耗尽或连续验收失败，暂停自动推进（保留人工解锁）。
	GoalBlocked GoalStatus = "blocked"
	// GoalAbandoned 目标被放弃（人工操作或推导失败兜底）。
	GoalAbandoned GoalStatus = "abandoned"
)

// CriterionKind 是验收标准的判据类型。
//
// 刻意只保留「可从既有台账直接读到」的判据，不引入新的执行面：
// 这样自主验收不需要额外跑模型，也不增加成本与不确定性。
type CriterionKind string

const (
	// CriterionTaskDone 需要 N 个任务落到 done。
	CriterionTaskDone CriterionKind = "tasksDone"
	// CriterionMinQuality 最近 N 个任务的自评均分不低于 Threshold。
	CriterionMinQuality CriterionKind = "minQuality"
	// CriterionMinWords 产出正文不少于 Threshold 字（字数门禁）。
	CriterionMinWords CriterionKind = "minWords"
	// CriterionDistinctDomains 覆盖不少于 N 个领域（多样性门禁）。
	CriterionDistinctDomains CriterionKind = "distinctDomains"
	// CriterionManual 人工判据（自动流程不判定通过，只提示；用于人工确认类目标）。
	CriterionManual CriterionKind = "manual"
	// CriterionVerified 要求已通过阶段中有 N 个**真实执行验收**通过（P3 接线）。
	//
	// 这是把"字数够、自评高"升级为"产物真能跑"的关键判据：
	// 改造前自主验收只看字数与自评分，模型写一篇漂亮但不可运行的作文也能通过；
	// 引入本判据后，目标完成必须包含可复核的执行证据（verify.json pass）。
	CriterionVerified CriterionKind = "verifiedTasks"
	// CriterionVerifiedRatio 要求已通过阶段的**验收通过率**不低于 Threshold（0-1）。
	// 与 Verified 的区别：前者看绝对数量，后者看比例，适合产出量大的长期目标。
	CriterionVerifiedRatio CriterionKind = "verifiedRatio"
)

// Criterion 是一条可机器判定的验收标准。
//
// Current/Passed 由 Acceptance 在每次验收后回写，保存在 goals.json 里，
// 因此"目标完成到什么程度"是持久化事实而不是内存推断。
type Criterion struct {
	ID        string        `json:"id"`
	Desc      string        `json:"desc"`
	Kind      CriterionKind `json:"kind"`
	Threshold float64       `json:"threshold"`
	Current   float64       `json:"current"`
	Passed    bool          `json:"passed"`
	Detail    string        `json:"detail,omitempty"`
}

// Stage 是一个阶段性目标：一次任务产出 + 它的自评结果。
//
// 任务与阶段是 1:1（一条自主任务对应一个阶段推进），保持"可回溯到具体产出"。
type Stage struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	TaskID  string `json:"taskId"`
	Quality int    `json:"quality"`
	Words   int    `json:"words"` // 本阶段产出字数（自主验收的字数口径）
	Passed  bool   `json:"passed"`
	Note    string `json:"note,omitempty"`
	At      int64  `json:"at"`
}

// Evidence 是一条验收证据（指向可复核的磁盘产物或任务 ID）。
type Evidence struct {
	At     int64  `json:"at"`
	TaskID string `json:"taskId,omitempty"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Goal 是项目级自主目标。
//
// 关键字段解释：
//   - Intent：自然语言目标（注入 Prompt 的那一句"项目目标"）；
//   - Criteria：机器可判的验收标准，全部 Passed 才算目标完成；
//   - Stages：已执行的阶段轨迹（可回溯每个阶段用了哪个任务）；
//   - Rollbacks/Budget：预算与回退计数，防止自主循环失控；
//   - Source：goal 来源（config=配置初始目标，derived=上一目标自动推导，manual=人工添加）。
type Goal struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Intent      string      `json:"intent"`
	Domain      string      `json:"domain"`
	Status      GoalStatus  `json:"status"`
	Source      string      `json:"source"`
	ParentID    string      `json:"parentId,omitempty"`
	Progress    float64     `json:"progress"`
	Criteria    []Criterion `json:"criteria"`
	Stages      []Stage     `json:"stages"`
	Evidence    []Evidence  `json:"evidence"`
	Rollbacks   int         `json:"rollbacks"`
	MaxTasks    int         `json:"maxTasks"`
	MaxStages   int         `json:"maxStages"`
	MinQuality  int         `json:"minQuality"`
	CreatedAt   int64       `json:"createdAt"`
	UpdatedAt   int64       `json:"updatedAt"`
	CompletedAt int64       `json:"completedAt,omitempty"`
	BlockReason string      `json:"blockReason,omitempty"`
}

// GoalsFile 是 goals.json 的落盘结构。
type GoalsFile struct {
	SchemaVersion int     `json:"schemaVersion"`
	ActiveID      string  `json:"activeId"`
	Goals         []*Goal `json:"goals"`
	SavedAt       int64   `json:"savedAt"`
}

// DecisionKind 是自主决策类型（决策台账用）。
type DecisionKind string

const (
	// DecisionPlan 生成了一条阶段任务。
	DecisionPlan DecisionKind = "plan"
	// DecisionAccept 自主验收通过。
	DecisionAccept DecisionKind = "accept"
	// DecisionReject 自主验收不通过（触发重规划/回退）。
	DecisionReject DecisionKind = "reject"
	// DecisionRollback 阶段回退（低质量产出不计入目标进度）。
	DecisionRollback DecisionKind = "rollback"
	// DecisionGoalDone 目标完成。
	DecisionGoalDone DecisionKind = "goalDone"
	// DecisionNextGoal 自动推导出下一目标。
	DecisionNextGoal DecisionKind = "nextGoal"
	// DecisionBlock 目标被阻塞。
	DecisionBlock DecisionKind = "block"
	// DecisionBoundReject 生成物被边界校验拒绝。
	DecisionBoundReject DecisionKind = "boundReject"
	// DecisionResume 目标被人工恢复。
	DecisionResume DecisionKind = "resume"
	// DecisionDegrade 通道能力缺口导致某类判据不可达，按预案降级为人工判据（留痕，不静默）。
	DecisionDegrade DecisionKind = "degrade"
)

// Decision 是一条自主决策记录（decisions.jsonl 追加写，不可变）。
type Decision struct {
	ID       string       `json:"id"`
	At       int64        `json:"at"`
	GoalID   string       `json:"goalId"`
	TaskID   string       `json:"taskId,omitempty"`
	Kind     DecisionKind `json:"kind"`
	Reason   string       `json:"reason"`
	Evidence string       `json:"evidence,omitempty"`
	Actor    string       `json:"actor"` // autonomy/engine/user
}

// cleanText 去掉首尾空白并把连续空行压成一行（用于 Intent/Reason 落盘整洁）。
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if t := strings.TrimSpace(ln); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

// IsOpen 判断目标是否仍在推进（active）。
func (g *Goal) IsOpen() bool { return g != nil && g.Status == GoalActive }

// ProgressPercent 返回进度百分比（0-100，四舍五入到整数）。
func (g *Goal) ProgressPercent() int {
	if g == nil {
		return 0
	}
	p := g.Progress
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return int(p + 0.5)
}

// appendStage 追加一个阶段（上限由调用方控制）。
func (g *Goal) appendStage(st Stage) {
	g.Stages = append(g.Stages, st)
	if st.At > g.UpdatedAt {
		g.UpdatedAt = st.At
	}
}

// addEvidence 追加一条证据（最多保留 50 条，超量丢最旧的，防止文件无限膨胀）。
func (g *Goal) addEvidence(ev Evidence) {
	g.Evidence = append(g.Evidence, ev)
	if len(g.Evidence) > 50 {
		g.Evidence = g.Evidence[len(g.Evidence)-50:]
	}
}

// qualityOf 安全读取任务的自评质量（无自评返回 0）。
func qualityOf(t *model.Task) int {
	if t == nil || t.Critique == nil {
		return 0
	}
	q := t.Critique.Quality
	if q < 0 {
		return 0
	}
	if q > 10 {
		return 10
	}
	return q
}

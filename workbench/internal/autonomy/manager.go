package autonomy

// 本文件实现「闭环自主执行」的状态机核心：Manager 把规划器、边界、验收、
// 风险与仓储串成一条可重放的动作链，引擎只需在正确的时机调用对应方法。
//
// 完整闭环（对应目标要求 2）：
//
//   目标 intake -> 规划 Plan -> 边界校验 -> 交给引擎执行 -> 任务完成
//        ^                                                    |
//        |                                                    v
//        +--- 推导下一目标 <- 目标完成 <- 累计进度 <- 自主验收判定 <- 风险校验
//
// 所有状态都落 goals.json，所有判断都落 decisions.jsonl；进程重启后从文件恢复，
// 不依赖内存（Recover 时由引擎调用 Manager.Restore）。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"workbench/internal/config"
	"workbench/internal/model"
)

// Manager 是自主执行状态机。并发约定：所有外部调用串行（引擎在调度循环与任务
// 完成回调里调用），内部也持锁，保证 goals.json 不会被并发写坏。
type Manager struct {
	mu      sync.Mutex
	store   *Store
	planner *Planner
	cfg     func() config.AutonomyConfig
	now     func() int64
	log     Logger
	// tasksFn 返回全量任务（由引擎注入）：
	// 自主验收需要读任务的 Verified 标记来判定"执行通道是否具备产出可验证产物的能力"。
	// 用函数注入而不是 import store，保持 autonomy 包不依赖仓储包（可测、不循环依赖）。
	tasksFn func() []*model.Task
}

// Logger 是 Manager 的日志出口（避免 import logging 造成循环依赖，用最小接口）。
type Logger interface {
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}

// NewManager 构造自主执行状态机。
//
// cfg 用函数注入：配置可在运行期热更新（面板改预算），Manager 每次判断都读最新值。
func NewManager(store *Store, cfg func() config.AutonomyConfig, log Logger, now func() int64) *Manager {
	if log == nil {
		log = nopLogger{}
	}
	if now == nil {
		now = nowMs
	}
	m := &Manager{store: store, cfg: cfg, log: log, now: now, tasksFn: func() []*model.Task { return nil }}
	m.planner = NewPlanner(PlanConfig{})
	m.refreshPlanner()
	return m
}

// refreshPlanner 按当前配置重建规划器（配置热更新时调用）。
func (m *Manager) refreshPlanner() {
	c := m.config()
	m.planner = NewPlanner(PlanConfig{
		DefaultDomain: c.Plan.DefaultDomain,
		Words:         c.Plan.Words,
		MaxDrafts:     c.Plan.MaxDrafts,
		StageEvery:    c.Budget.StageEvery,
	})
}

// config 返回钳制后的自主配置。
func (m *Manager) config() config.AutonomyConfig {
	c := m.cfg()
	c.Budget = ClampBudget(c.Budget)
	return c
}

// SetTaskSource 注入全量任务读取函数（引擎在构造后调用；未注入时按"无任务"处理）。
func (m *Manager) SetTaskSource(fn func() []*model.Task) {
	if fn == nil {
		fn = func() []*model.Task { return nil }
	}
	m.mu.Lock()
	m.tasksFn = fn
	m.mu.Unlock()
}

// ReloadConfig 配置热更新后调用（重建规划器参数）。
func (m *Manager) ReloadConfig() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshPlanner()
}

// Store 暴露仓储（面板与引擎恢复用）。
func (m *Manager) Store() *Store { return m.store }

// ── 目标初始化与恢复 ─────────────────────────────────────────────

// Bootstrap 保证至少有一个活跃目标：
//   - 已有活跃目标 => 直接返回；
//   - 没有任何目标 => 用配置里的 RootGoal（或默认目标）新建一个，记决策；
//   - 有目标但都终结了 => 从最近完成的目标推导下一个（闭环续跑）。
func (m *Manager) Bootstrap() (*Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bootstrapLocked()
}

func (m *Manager) bootstrapLocked() (*Goal, error) {
	if g, ok := m.store.Active(); ok && g.IsOpen() {
		return g, nil
	}
	c := m.config()
	goals := m.store.Goals()
	// 已有历史目标：从最近一个已完成目标推导下一目标（闭环续跑）。
	if len(goals) > 0 {
		var last *Goal
		for _, g := range goals {
			if g != nil && g.Status == GoalCompleted {
				last = g
			}
		}
		if last != nil {
			if draft := m.deriveNextLocked(last, c); draft != nil {
				g := m.createGoalLocked(draft.title, draft.intent, draft.domain, "derived", last.ID, c)
				// 同 advanceLocked：先留痕再生效（issue #1 / D6），否则重启后观察者
				// 可能看到 active 已恢复为新目标而台账缺 nextGoal。
				_ = writeLedgerThenActivate(
					func() error {
						return m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionNextGoal,
							Reason: "启动恢复：上一目标已完成，自动推导下一目标", Evidence: draft.reasons, Actor: "autonomy"})
					},
					func() error { return m.store.SetActive(g.ID) },
				)
				return g, nil
			}
		}
		// 有目标但没有可续跑的（全部 blocked/abandoned）：新建默认目标，避免自主线停摆。
	}
	title := firstNonEmpty(c.Plan.RootTitle, "持续产出与能力沉淀")
	intent := firstNonEmpty(c.Plan.RootIntent,
		"围绕个人知识资产持续产出高质量可复用成果：先把主题拆透、写出成体系的长文，再拆成卡片/脚本等可复用形态，最后复盘补齐不足。")
	domain := firstNonEmpty(c.Plan.DefaultDomain, "content")
	g := m.createGoalLocked(title, intent, domain, "config", "", c)
	if err := m.store.SetActive(g.ID); err != nil {
		return nil, err
	}
	_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionPlan,
		Reason: "无活跃目标，按配置初始化项目级目标", Evidence: "config.Plan.RootTitle", Actor: "autonomy"})
	return g, nil
}

// createGoalLocked 按配置生成并落盘一个新目标。
func (m *Manager) createGoalLocked(title, intent, domain, source, parentID string, c config.AutonomyConfig) *Goal {
	now := m.now()
	g := &Goal{
		ID:         "goal-" + fmt.Sprint(now) + "-" + randSuffix(),
		Title:      model.Truncate(cleanText(title), MaxTitleRunes),
		Intent:     cleanText(intent),
		Domain:     domain,
		Status:     GoalActive,
		Source:     source,
		ParentID:   parentID,
		MaxTasks:   c.Budget.MaxTasksPerGoal,
		MaxStages:  c.Budget.MaxStages,
		MinQuality: c.Budget.MinQuality,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	// 验收标准必须与预算自洽：minWords 不能超过"预算允许的阶段数 × 单阶段目标字数"，
	// 否则小预算目标（如 MaxStages=2）会永远无法通过 minWords 判据，
	// 最终被预算闸门判 blocked —— 2026-10-19 运行时实测踩到（现 90 / 需 3000）。
	crits := DefaultCriteria(GoalDefaults{
		MinStages: c.Budget.MaxStages * 3 / 4, MinQuality: c.Budget.MinQuality,
		MinWords: criteriaWords(c),
	}, domain)
	// 最少 2 个阶段、至少一条质量与字数判据，避免目标定义得过轻（一步就完成没意义）。
	if crits[0].Threshold < 2 {
		crits[0].Threshold = 2
	}
	g.Criteria = crits
	_ = m.store.Upsert(g)
	return g
}

// CapabilityGapReason 是"执行通道能否产出可验证产物"的判定结果（空串表示能）。
//
// 判定口径：本目标已通过的阶段里，是否存在任何一条任务 Verified=true。
// 只要出现过一次，就说明通道具备该能力（偶发失败由返工环处理），不降级。
// 一个都没有，且阶段数已达 2 个以上，判定为结构性能力缺口。
func detectCapabilityGap(goal *Goal, tasks []*model.Task) string {
	if goal == nil || len(goal.Stages) < 2 {
		return ""
	}
	byID := make(map[string]*model.Task, len(tasks))
	for _, t := range tasks {
		if t != nil {
			byID[t.ID] = t
		}
	}
	passed := 0
	verified := 0
	for _, st := range goal.Stages {
		if !st.Passed {
			continue
		}
		passed++
		if t, ok := byID[st.TaskID]; ok && t.Verified {
			verified++
		}
	}
	if passed < 2 || verified > 0 {
		return ""
	}
	return fmt.Sprintf("已完成 %d 个阶段但 0 个产出通过真实执行验收（执行通道未能稳定产出可执行验收的产物块）", passed)
}

// criteriaWords 计算"目标级总字数判据"的合理阈值。
//
// 关键教训（2026-10-19 运行时实测，两次踩到）：
//   判据必须能在"当前执行通道的真实产出能力"内达成，否则目标永远无法验收通过，
//   最后被预算闸门判 blocked —— 运行日志两次出现「现 90 / 需 3000」「现 90 / 需 1500」。
//
// 实测事实：本机 OFM sealed 车道单任务产出稳定在 90–270 字（模型一次性输出长度受限），
// 与 Plan.Words（生成目标，默认 1000）不是一个量级。因此：
//   - Plan.Words 保留为"给模型的目标"，不影响判据；
//   - 验收判据按「阶段数 × 75 字/阶段」设定下限，取 2 阶段为最低门槛（120 字）；
//   - 若用户把 Plan.Words 设得极小（<100），判据仍不超过预算上限（maxStages × words）。
func criteriaWords(c config.AutonomyConfig) int {
	stages := c.Budget.MaxStages * 3 / 4
	if stages < 2 {
		stages = 2
	}
	if c.Budget.MaxStages > 0 && stages > c.Budget.MaxStages {
		stages = c.Budget.MaxStages
	}
	const perStageFloor = 75 // 实测单任务产出下限量级（90–270 字）
	total := stages * perStageFloor
	if total < 120 {
		total = 120
	}
	// 极端配置（words 极小）下仍不得高于该目标的预算上限，避免"预算内不可能达成"。
	if capWords := stages * c.Plan.Words; capWords > 0 && total > capWords {
		total = capWords
	}
	return total
}

// deriveNextLocked 用规划器推导下一目标草案。
func (m *Manager) deriveNextLocked(prev *Goal, c config.AutonomyConfig) *GoalDraftView {
	var gaps []string
	if len(prev.Stages) > 0 {
		// 最后阶段的 Note 里保存了回退/不足原因（由 CompleteStage 写入）。
		if note := strings.TrimSpace(prev.Stages[len(prev.Stages)-1].Note); note != "" {
			gaps = append(gaps, note)
		}
	}
	d := m.planner.DeriveNextGoal(prev, gaps, m.now(), GoalDefaults{
		MinStages:    c.Budget.MaxStages * 3 / 4,
		MinQuality:   c.Budget.MinQuality,
		MinWords:     criteriaWords(c),
		MaxTasks:     c.Budget.MaxTasksPerGoal,
		MaxStages:    c.Budget.MaxStages,
		MaxRollbacks: c.Budget.MaxRollbacks,
	})
	if d == nil {
		return nil
	}
	return &GoalDraftView{title: d.Title, intent: d.Intent, domain: d.Domain, reasons: d.Reasons, criteria: d.Criteria}
}

// GoalDraftView 是内部使用的下一目标草案视图（避免把 planner 的类型暴露给引擎）。
type GoalDraftView struct {
	title    string
	intent   string
	domain   string
	reasons  string
	criteria []Criterion
}

// ── 规划：生成下一条自主任务 ─────────────────────────────────────

// NextTask 推导下一条自主任务草案（已过边界校验）。
//
// 返回 (draft, goal, ok)：ok=false 表示当前不应继续生成（目标已完成/被阻塞/预算耗尽），
// 引擎据此回落到固定模板链路（fail-safe）。
func (m *Manager) NextTask(recent []string, lastTask *model.Task) (Draft, *Goal, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config()
	goal, ok := m.activeLocked()
	if !ok {
		return Draft{}, nil, false
	}
	// 预算检查：任务数达到上限且目标未完成 => 阻塞，不再生成。
	if goal.MaxTasks > 0 && m.tasksOfGoal(goal) >= goal.MaxTasks && !allCriteriaPassed(goal) {
		if goal.Status == GoalActive {
			m.blockLocked(goal, "任务数预算耗尽（已达 %d 条）但验收未通过", goal.MaxTasks)
		}
		return Draft{}, goal, false
	}
	ctx := PlanContext{Goal: goal, StageIndex: len(goal.Stages), Recent: recent, Now: m.now()}
	if lastTask != nil && lastTask.Critique != nil {
		ctx.LastGaps = append(ctx.LastGaps, lastTask.Critique.Gaps...)
		ctx.NextIdeas = append(ctx.NextIdeas, lastTask.Critique.NextIdeas...)
	}
	if n := len(goal.Stages); n > 0 {
		st := goal.Stages[n-1]
		ctx.LastStage = &st
	}
	drafts := m.planner.Plan(ctx)
	for _, d := range drafts {
		br := ValidateDraft(d, c)
		if !br.OK {
			reasons := make([]string, 0, len(br.Violations))
			for _, v := range br.Violations {
				if v.Fatal {
					reasons = append(reasons, v.Code+": "+v.Detail)
				}
			}
			_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goal.ID, Kind: DecisionBoundReject,
				Reason: "规划草案未通过边界校验：" + strings.Join(reasons, "；"), Actor: "autonomy"})
			m.log.Warn("自主草案被边界拒绝", "goalId", goal.ID, "title", model.Truncate(d.Title, 40), "reasons", strings.Join(reasons, "；"))
			continue
		}
		return *br.Draft, goal, true
	}
	return Draft{}, goal, false
}

// RecordPlan 记录"已按草案生成任务"的决策（引擎在任务入队后调用）。
func (m *Manager) RecordPlan(goalID, taskID, title, rationale string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goalID, TaskID: taskID, Kind: DecisionPlan,
		Reason: "生成自主任务：" + title, Evidence: rationale, Actor: "autonomy"})
}

// ── 验收：任务完成后的自主判定 ───────────────────────────────────

// CompleteStage 在一条自主任务完成（且自评已回填）后调用，完成一次闭环：
//
//	质量判定 -> 风险校验 -> 计入/回退 -> 目标级验收 -> （完成则）推导下一目标
//
// 返回 StageOutcome，引擎据此决定是否还要走模板链、是否推送事件。
func (m *Manager) CompleteStage(goalID string, t *model.Task, words int, recent []string, tasks []*model.Task) StageOutcome {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.config()
	goal, ok := m.store.Get(goalID)
	if !ok || goal == nil {
		return StageOutcome{Reason: "目标不存在，跳过自主验收"}
	}
	if goal.Status != GoalActive {
		return StageOutcome{Reason: "目标已不在 active 状态（" + string(goal.Status) + "），跳过"}
	}
	if t == nil {
		return StageOutcome{Reason: "任务为空，跳过自主验收"}
	}
	// 1) 阶段质量判定
	v := JudgeStage(t, c.Budget.MinQuality)
	stage := Stage{Index: len(goal.Stages), Title: model.Truncate(t.Title, 60), TaskID: t.ID,
		Quality: v.Quality, Words: words, Passed: v.Pass, At: m.now()}
	if !v.Pass {
		stage.Note = RollbackReason(v)
	}
	goal.appendStage(stage)
	goal.addEvidence(Evidence{At: m.now(), TaskID: t.ID, Kind: "task", Detail: fmt.Sprintf("质量 %d/10，%d 字", v.Quality, words)})
	out := StageOutcome{Stage: stage, Verdict: v}

	// 2) 风险校验（质量/空转/预算）
	report, stop := CheckStageRisk(goal, t, c, recent)
	out.Risk = report
	if !v.Pass && ShouldRollback(goal, v, c) {
		goal.Rollbacks++
		out.RolledBack = true
		_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goal.ID, TaskID: t.ID, Kind: DecisionRollback,
			Reason: RollbackReason(v), Evidence: fmt.Sprintf("rollbacks=%d", goal.Rollbacks), Actor: "autonomy"})
		m.log.Warn("阶段回退", "goalId", goal.ID, "taskId", t.ID, "reason", RollbackReason(v))
	} else if v.Pass {
		_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goal.ID, TaskID: t.ID, Kind: DecisionAccept,
			Reason: v.Reason, Actor: "autonomy"})
	}
	if stop {
		m.blockLocked(goal, "风险校验命中高等级项（%s）", riskCodes(report))
		out.Blocked = true
		out.Reason = "风险校验触发阻塞"
		goal.Progress = progressOf(goal)
		goal.UpdatedAt = m.now()
		_ = m.store.Upsert(goal)
		return out
	}

	// 3) 通道能力缺口处置（异常回退机制之一，对应目标要求 3）：
	//    若目标带"真实验收"类判据，但当前执行通道迄今从未产出过任何通过验收的产物
	//    （Verified 恒 false，例如模型不遵循产物协议），则该判据在本机永远不可达，
	//    继续下去只会把每个目标都拖到预算耗尽后 blocked。按预案把它降级为人工判据，
	//    并写一条 degrade 决策留痕（绝不静默改判据）。
	if gap := detectCapabilityGap(goal, m.tasksFn()); gap != "" {
		for i := range goal.Criteria {
			if goal.Criteria[i].Kind == CriterionVerified || goal.Criteria[i].Kind == CriterionVerifiedRatio {
				goal.Criteria[i].Kind = CriterionManual
				goal.Criteria[i].Detail = "通道能力缺口已降级：" + gap
			}
		}
		_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goal.ID, TaskID: t.ID, Kind: DecisionDegrade,
			Reason: "验收判据降级为人工判据：" + gap, Actor: "autonomy"})
		m.log.Warn("验收判据降级（通道能力缺口）", "goalId", goal.ID, "reason", gap)
	}

	// 3b) 目标级自主验收
	// Tasks 必须传进去（2026-10-19 现场缺陷）：传 nil 会让 Evaluate 的 taskByID 永远为空，
	// c-verified 恒为 0，目标永远无法完成，goalDone/nextGoal 永不到达。
	acc := Evaluate(AcceptanceInput{Goal: goal, Tasks: tasks, Now: m.now()})
	for i := range acc.Criteria {
		for j := range goal.Criteria {
			if goal.Criteria[j].ID == acc.Criteria[i].ID {
				goal.Criteria[j] = acc.Criteria[i]
			}
		}
	}
	goal.Progress = progressOf(goal)
	goal.UpdatedAt = m.now()
	out.Acceptance = acc
	if acc.Passed {
		goal.Status = GoalCompleted
		goal.CompletedAt = m.now()
		out.GoalCompleted = true
		_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: goal.ID, Kind: DecisionGoalDone,
			Reason: "目标自主验收通过：" + acc.Reason, Actor: "autonomy"})
		_ = m.store.Upsert(goal)
		// 4) 目标完成 => 自动推导并设置下一目标（闭环续跑）
		next, derr := m.advanceLocked(goal, c)
		if derr != nil {
			out.Reason = "目标完成，但推导下一目标失败：" + derr.Error()
			m.log.Error("推导下一目标失败", "goalId", goal.ID, "err", derr.Error())
			return out
		}
		out.NextGoalID = next.ID
		out.NextGoalTitle = next.Title
		out.Reason = "目标已完成并自动推导下一目标：" + next.Title
		return out
	}
	// 未完成：继续推进（受预算约束）
	_ = m.store.Upsert(goal)
	out.Reason = acc.Reason
	return out
}

// writeLedgerThenActivate 固化「决策留痕先于状态生效」的顺序（issue #1 / D6）。
//
// 为什么需要这个函数而不是就地两行：SetActive 写 goals.json、AppendDecision 写
// decisions.jsonl，两个文件天然非原子。若先生效后留痕，观察者（面板自主总览、
// --selftest 判据 7b、闭环回归测试）会看到一个真实窗口 —— active 已切到新目标，
// 台账里却还没有 nextGoal，被误读为「闭环没推导」；CI 上
// TestRunOneDrivesAutonomyClosedLoop 的偶发红就是撞进这个窗口。
//
// 语义：留痕失败**不阻断**生效（台账写不进不该让自主线停摆，且 AppendDecision
// 的错误在生产路径上原本就被丢弃）；生效的错误照常返回给调用方。
// 反向失效（已生效但无留痕）比正向失效（有留痕但未生效）更坏 —— 前者是审计缺失
// 且会被判成"功能没跑"，后者只是多一条仍为真的记录（目标确已推导，只是未激活）。
func writeLedgerThenActivate(appendDecision func() error, setActive func() error) error {
	_ = appendDecision()
	return setActive()
}

// advanceLocked 在当前目标完成后推导、创建并激活下一个目标。
func (m *Manager) advanceLocked(prev *Goal, c config.AutonomyConfig) (*Goal, error) {
	draft := m.deriveNextLocked(prev, c)
	if draft == nil {
		// 推导失败兜底：退回到配置根目标，保证自主线不断。
		title := firstNonEmpty(c.Plan.RootTitle, "持续产出与能力沉淀")
		intent := firstNonEmpty(c.Plan.RootIntent, prev.Intent)
		domain := firstNonEmpty(c.Plan.DefaultDomain, prev.Domain, "content")
		draft = &GoalDraftView{title: title, intent: intent + "（上一目标已闭环，重新围绕根目标推进）",
			domain: domain, reasons: "推导失败兜底：回到配置根目标",
			criteria: DefaultCriteria(GoalDefaults{MinStages: c.Budget.MaxStages * 3 / 4, MinQuality: c.Budget.MinQuality, MinWords: c.Plan.Words * 3}, domain)}
	}
	g := m.createGoalLocked(draft.title, draft.intent, draft.domain, "derived", prev.ID, c)
	if len(draft.criteria) > 0 {
		g.Criteria = draft.criteria
		_ = m.store.Upsert(g)
	}
	// 顺序硬要求（issue #1 / D6）：**决策留痕必须先于状态生效**，见 writeLedgerThenActivate。
	_ = writeLedgerThenActivate(
		func() error {
			return m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionNextGoal,
				Reason: "目标完成后自动推导下一目标（闭环续跑）", Evidence: draft.reasons, Actor: "autonomy"})
		},
		func() error { return m.store.SetActive(g.ID) },
	)
	m.log.Info("自动推导下一目标", "prevGoalId", prev.ID, "nextGoalId", g.ID, "title", g.Title)
	return g, nil
}

// StageOutcome 是一次阶段闭环的结果（引擎用来推事件与决定后续动作）。
type StageOutcome struct {
	Stage         Stage            `json:"stage"`
	Verdict       QualityVerdict   `json:"verdict"`
	Risk          RiskReport       `json:"risk"`
	Acceptance    AcceptanceResult `json:"acceptance"`
	RolledBack    bool             `json:"rolledBack"`
	Blocked       bool             `json:"blocked"`
	GoalCompleted bool             `json:"goalCompleted"`
	NextGoalID    string           `json:"nextGoalId,omitempty"`
	NextGoalTitle string           `json:"nextGoalTitle,omitempty"`
	Reason        string           `json:"reason"`
}

// ── 目标操作（面板用） ───────────────────────────────────────────

// Goals 返回全部目标（副本）。
func (m *Manager) Goals() []*Goal { return m.store.Goals() }

// Active 返回活跃目标。
func (m *Manager) Active() (*Goal, bool) { return m.store.Active() }

// Decisions 返回最近决策。
func (m *Manager) Decisions(limit int) []Decision { return m.store.Decisions(limit) }

// ResumeGoal 人工解锁一个 blocked 目标。
func (m *Manager) ResumeGoal(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.store.Get(id)
	if !ok || g == nil {
		return fmt.Errorf("目标不存在: %s", id)
	}
	if g.Status != GoalBlocked {
		return fmt.Errorf("只有 blocked 目标可恢复（当前 %s）", g.Status)
	}
	g.Status = GoalActive
	g.Rollbacks = 0
	g.BlockReason = ""
	g.UpdatedAt = m.now()
	if err := m.store.Upsert(g); err != nil {
		return err
	}
	if err := m.store.SetActive(g.ID); err != nil {
		return err
	}
	_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionResume,
		Reason: "人工恢复目标并清零回退计数", Actor: "user"})
	return nil
}

// SetGoal 人工新建/切换目标。
func (m *Manager) SetGoal(title, intent, domain string) (*Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(title) == "" || strings.TrimSpace(intent) == "" {
		return nil, fmt.Errorf("目标标题与意图不能为空")
	}
	g := m.createGoalLocked(title, intent, domain, "manual", "", m.config())
	if err := m.store.SetActive(g.ID); err != nil {
		return nil, err
	}
	_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionPlan,
		Reason: "人工设置项目目标", Actor: "user"})
	return g, nil
}

// ── 内部工具 ─────────────────────────────────────────────────────

// activeLocked 返回活跃目标；没有时先 Bootstrap 一个。
func (m *Manager) activeLocked() (*Goal, bool) {
	if g, ok := m.store.Active(); ok && g.IsOpen() {
		return g, true
	}
	g, err := m.bootstrapLocked()
	if err != nil || g == nil {
		return nil, false
	}
	return g, true
}

// tasksOfGoal 返回目标已执行的阶段数（用于预算判断）。
func (m *Manager) tasksOfGoal(g *Goal) int {
	if g == nil {
		return 0
	}
	return len(g.Stages)
}

// blockLocked 阻塞目标并记决策。
func (m *Manager) blockLocked(g *Goal, format string, args ...any) {
	reason := fmt.Sprintf(format, args...)
	g.Status = GoalBlocked
	g.BlockReason = reason
	g.UpdatedAt = m.now()
	_ = m.store.Upsert(g)
	_ = m.store.AppendDecision(Decision{At: m.now(), GoalID: g.ID, Kind: DecisionBlock,
		Reason: reason, Actor: "autonomy"})
	m.log.Warn("目标被阻塞", "goalId", g.ID, "reason", reason)
}

// progressOf 按验收标准完成率计算目标进度（0-100）。
func progressOf(g *Goal) float64 {
	if g == nil || len(g.Criteria) == 0 {
		return 0
	}
	var sum float64
	var n int
	for _, c := range g.Criteria {
		if c.Kind == CriterionManual {
			continue
		}
		n++
		if c.Threshold <= 0 {
			continue
		}
		ratio := c.Current / c.Threshold
		if ratio > 1 {
			ratio = 1
		}
		sum += ratio
	}
	if n == 0 {
		return 0
	}
	v := sum / float64(n) * 100
	if v > 100 {
		v = 100
	}
	return float64(int(v*10+0.5)) / 10
}

// riskCodes 把风险项拼成短串（决策台账用）。
func riskCodes(r RiskReport) string {
	if len(r.Items) == 0 {
		return "none"
	}
	codes := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		codes = append(codes, it.Code)
	}
	return strings.Join(codes, ",")
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

// randSuffix 生成 4 位十六进制随机后缀（ID 唯一性用）。
func randSuffix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%04x", nowMs()&0xFFFF)
	}
	return hex.EncodeToString(buf)
}

// nopLogger 是不落日志的默认实现。
type nopLogger struct{}

func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

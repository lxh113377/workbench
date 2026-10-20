package engine

// 本文件是「自主任务规划与闭环执行」子系统与调度引擎的接线层（目标要求 1-3）。
//
// 职责边界：
//   - autonomy 包只做纯逻辑（规划/边界/验收/风险/持久化），不碰调度；
//   - 本文件负责在正确时机调用它，并把结果翻译成任务、事件与日志。
//
// 接线点只有四处，刻意保持最小侵入：
//   1. supplyCandidates：队列水位低于阈值时，先问自主规划器要一条任务；
//      规划不出（目标完成/阻塞/边界拒绝/关闭自主）时自动回落到原模板链路 —— 兼容性要求；
//   2. postProcess：自主任务完成并拿到自评后，交给 Manager 做自主验收与下一目标推导；
//      模板任务仍走原来的 extendChain，两条链路互不干扰；
//   3. Recover/Start：启动时 bootstrap 目标树，恢复上次的活跃目标；
//   4. 对外读取：Summary/Goals/Decisions 供 API/CLI/面板使用。

import (
	"context"
	"fmt"
	"strings"
	"time"

	"workbench/internal/autonomy"
	"workbench/internal/config"
	"workbench/internal/evaluate"
	"workbench/internal/model"
)

// autonomyLogger 把引擎的结构化日志器适配成 autonomy.Logger（延迟取 e.log）。
type autonomyLogger struct{ e *Engine }

func (l autonomyLogger) Info(msg string, kv ...any)  { l.e.log.At("autonomy").Info(msg, kv...) }
func (l autonomyLogger) Warn(msg string, kv ...any)  { l.e.log.At("autonomy").Warn(msg, kv...) }
func (l autonomyLogger) Error(msg string, kv ...any) { l.e.log.At("autonomy").Error(msg, kv...) }

// initAutonomy 打开自主目标仓储并构造状态机。失败时降级为「未启用自主」，
// 绝不让自主子系统拖垮主工作台（与治理子系统的 advisory 定位一致）。
func (e *Engine) initAutonomy() {
	st, err := autonomy.OpenStore(e.layout.AutonomyFile("goals.json"), e.layout.AutonomyFile("decisions.jsonl"))
	if err != nil {
		e.autonomy = nil
		// 构造期用 warnDeferred（缺陷 D12）：此处 e.log 还是 Nop（main 在 New()
		// 返回后才 SetLogger），直接 Warn 会写进 io.Discard —— 自主链路没起来但
		// 日志里一条原因都没有，只能靠猜。缓冲后由 SetLogger 冲刷出来。
		e.warnDeferred("自主子系统初始化失败，已降级为固定模板模式", "err", err.Error())
		return
	}
	e.autonomy = autonomy.NewManager(st, func() config.AutonomyConfig {
		return e.cstore.Get().Autonomy
	}, autonomyLogger{e: e}, func() int64 { return model.Now() })
	// 注入任务读取（自主验收要判断"通道能否产出可验证产物"，需要读任务的 Verified 标记）。
	e.autonomy.SetTaskSource(func() []*model.Task { return e.store.List() })
}

// SetAutonomy 注入自主状态机（测试用；生产由 initAutonomy 构造）。
func (e *Engine) SetAutonomy(m *autonomy.Manager) { e.autonomy = m }

// Autonomy 返回自主状态机（可能为 nil）。
func (e *Engine) Autonomy() *autonomy.Manager { return e.autonomy }

// autonomyEnabled 判断自主链路当前是否生效。
func (e *Engine) autonomyEnabled() bool {
	return e.autonomy != nil && e.cstore.Get().Autonomy.Enabled
}

// bootstrapAutonomy 保证启动后有活跃目标（有则复用，无则按配置创建）。
func (e *Engine) bootstrapAutonomy() {
	if !e.autonomyEnabled() {
		return
	}
	g, err := e.autonomy.Bootstrap()
	if err != nil {
		e.log.Warn("自主目标初始化失败", "err", err.Error())
		return
	}
	if g != nil {
		e.log.Info("自主目标就绪", "goalId", g.ID, "title", g.Title, "domain", g.Domain, "stages", len(g.Stages))
		e.emitEvent(model.Event{Type: "autonomy", At: model.Now(), Msg: "自主目标就绪：" + g.Title,
			Data: map[string]any{"goalId": g.ID, "title": g.Title, "intent": g.Intent, "domain": g.Domain}})
	}
}

// supplyAutonomous 尝试由自主规划器生成一条任务。
//
// 返回 true 表示"本轮已由自主链路处理"（无论是否真的产出了任务），
// 调用方据此跳过模板供给；返回 false 表示自主链路当前不可用，回落到模板池。
func (e *Engine) supplyAutonomous(ctx context.Context, cfg config.Config) bool {
	if !e.autonomyEnabled() {
		return false
	}
	if !cfg.Autonomy.Enabled {
		return false
	}
	// 水位控制（2026-10-19 现场缺陷修正）：
	//
	// 改造前判据是「队列总长 >= concurrency 就不再规划」。实测 soak 实例跑 30 分钟
	// 一条自主任务都没生成：模板链路把队列稳定维持在 3 条（> concurrency=2），
	// 于是自主链路**永远**在第一步就 return true，目标树 stages 一直 0。
	// 这不是"没触发"，是"被模板积压永久饿死"。
	//
	// 正确判据：只统计**自主任务**在队列里的数量。自主线本来就是串行推进
	// （下面还有 inFlight/pendingAcceptance 两道去重），队列里最多留 1 条自主任务
	// 就够；模板积压再多也不该挡住自主链路（两条链路各有各的水位）。
	autonomyQueued := 0
	for _, t := range e.q.Snapshot() {
		if t != nil && t.Source == "autonomy" {
			autonomyQueued++
		}
	}
	if autonomyQueued >= 1 {
		return true
	}
	// 在途去重（实测缺陷：同一条自主线在阶段 0 会连续规划出两条同名任务）。
	// 根因是"阶段序号来自 goal.Stages"，而 Stages 只在任务完成并自评后才回填，
	// 于是上一个任务还在跑时规划器看到的仍是第 0 阶段 → 重复规划同一阶段。
	// 修法：本目标只要还有非终态任务，就不再规划新的（自主线串行推进）。
	allTasks := e.store.List()
	activeGoal, hasGoal := e.autonomy.Active()
	if hasGoal && activeGoal != nil {
		if inFlight := inFlightTasksOfGoal(allTasks, activeGoal.ID); len(inFlight) > 0 {
			return true
		}
		// 已完成但尚未验收的任务同样视为"在途"：postProcess 是异步的，
		// 任务落 done 到自主验收写入 stages 之间有窗口期，此时若继续规划，
		// 会基于过期的阶段序号生成同一阶段的任务（实测 07:46 沙箱复现）。
		if pending := pendingAcceptance(allTasks, activeGoal); pending {
			return true
		}
	}
	recent := recentTitles(allTasks, 30)
	draft, goal, ok := e.autonomy.NextTask(recent, e.lastCompletedTask())
	if !ok || goal == nil {
		// 目标完成/阻塞/预算耗尽：交由模板链路兜底，保证工作台仍有产出。
		return false
	}
	now := model.Now()
	task := &model.Task{
		ID: "task-" + fmt.Sprint(now) + "-" + randSuffix(),
		// 同一目标用同一条链：队列的"每链串行"保证阶段顺序执行（阶段 N+1 依赖 N 的产出）。
		ChainID:      "autochain-" + goal.ID,
		TemplateID:   "",
		Title:        draft.Title,
		Prompt:       draft.Prompt,
		Domain:       draft.Domain,
		Tags:         []string{"自主"},
		Priority:     4,
		State:        model.StateCandidate,
		Source:       "autonomy",
		GoalID:       goal.ID,
		ChainDepth:   len(goal.Stages),
		MaxAttempts:  3,
		OutputFormat: "md",
		TimeoutMin:   cfg.Executor.DefaultTimeout,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if draft.Words > 0 && task.TimeoutMin < 20 {
		// 长文任务给足时间：默认 30 分钟与模板一致，避免小配置把自主任务卡在超时上。
		task.TimeoutMin = 30
	}
	scores := e.eval.Evaluate(evaluate.Input{Task: task, History: e.store.List(), Now: now, DailyCount: e.tasksToday()})
	task.Scores = scores
	if err := e.store.Create(task); err != nil {
		e.log.Warn("自主任务落库失败", "err", err.Error())
		return false
	}
	if !evaluate.Approve(scores) {
		_, _ = e.store.Transition(task.ID, model.StateRejected, "autonomy", "自主任务未过评估阈值")
		e.autonomy.RecordPlan(goal.ID, task.ID, task.Title, "评估拒绝："+scores.Reason)
		e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: task, Msg: "自主任务被评估拒绝"})
		return true // 已由自主链路处理（避免同轮模板重复补货）
	}
	if _, err := e.store.Transition(task.ID, model.StateApproved, "autonomy", "自主规划自动批准"); err != nil {
		return true
	}
	q, err := e.store.Transition(task.ID, model.StateQueued, "autonomy", "自主任务入队")
	if err != nil {
		return true
	}
	e.q.Push(q)
	e.autonomy.RecordPlan(goal.ID, task.ID, task.Title, draft.Rationale)
	e.log.Info("自主任务已生成", "goalId", goal.ID, "taskId", task.ID,
		"title", model.Truncate(task.Title, 60), "domain", task.Domain, "rationale", model.Truncate(draft.Rationale, 120))
	e.emitEvent(model.Event{Type: "autonomy", At: model.Now(), TaskID: task.ID, Msg: "自主规划任务：" + task.Title,
		Data: map[string]any{"goalId": goal.ID, "title": task.Title, "rationale": draft.Rationale, "stage": len(goal.Stages) + 1}})
	return true
}

// tasksOfGoalForAcceptance 返回某个自主目标下的全部任务（供验收统计 Verified）。
//
// 为什么要单独取：Verify 标记在任务对象上，而目标树里只存 taskId。
// 验收时若不把任务表传进去，c-verified 永远是 0（现场实测踩到）。
func (e *Engine) tasksOfGoalForAcceptance(goalID string) []*model.Task {
	if goalID == "" {
		return nil
	}
	var out []*model.Task
	for _, t := range e.store.List() {
		if t != nil && t.GoalID == goalID {
			out = append(out, t)
		}
	}
	return out
}

// finishAutonomous 在自主任务完成、自评回填后执行闭环验收。
//
// 完整链路：质量判定 → 风险校验 → 回退/计入 → 目标级自主验收 → 完成则推导下一目标。
func (e *Engine) finishAutonomous(task *model.Task, content []byte, critique *model.Critique, cfg config.Config) {
	if e.autonomy == nil || task == nil || task.GoalID == "" {
		return
	}
	words := autonomy.CountWords(string(content))
	recent := recentTitles(e.store.List(), 30)
	// 把该目标下的全部任务传进自主验收（2026-10-19 现场缺陷修正）：
	// 改造前 CompleteStage 内部以 Tasks: nil 调 Evaluate，导致 taskByID 永远为空，
	// c-verified / c-verified-ratio 恒为 0 —— 实测任务 verified=true、verify.json
	// pass=true，目标却显示 c-verified:0/1，永远无法完成。
	outcome := e.autonomy.CompleteStage(task.GoalID, task, words, recent, e.tasksOfGoalForAcceptance(task.GoalID))
	e.log.At("autonomy").Info("自主验收完成", "goalId", task.GoalID, "taskId", task.ID, "words", words,
		"pass", outcome.Verdict.Pass, "rolledBack", outcome.RolledBack, "blocked", outcome.Blocked,
		"goalCompleted", outcome.GoalCompleted, "reason", model.Truncate(outcome.Reason, 160))
	onew := model.Event{Type: "autonomy", At: model.Now(), TaskID: task.ID, Msg: "自主验收：" + model.Truncate(outcome.Reason, 120),
		Data: map[string]any{
			"goalId": task.GoalID, "words": words, "verdict": outcome.Verdict, "risk": outcome.Risk,
			"acceptance": outcome.Acceptance, "rolledBack": outcome.RolledBack, "blocked": outcome.Blocked,
			"goalCompleted": outcome.GoalCompleted, "nextGoalId": outcome.NextGoalID, "nextGoalTitle": outcome.NextGoalTitle,
		}}
	e.emitEvent(onew)
	if outcome.RolledBack {
		e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID, Msg: "阶段回退：" + model.Truncate(outcome.Verdict.Reason, 160)})
	}
	if outcome.Blocked {
		e.emitEvent(model.Event{Type: "warn", At: model.Now(), Msg: "自主目标已阻塞（等待人工确认后恢复）：" + model.Truncate(outcome.Reason, 160)})
	}
	if outcome.GoalCompleted {
		e.emitEvent(model.Event{Type: "autonomy", At: model.Now(), Msg: "目标已完成，自动推导下一目标：" + outcome.NextGoalTitle,
			Data: map[string]any{"nextGoalId": outcome.NextGoalID, "nextGoalTitle": outcome.NextGoalTitle}})
	}
}

// autonomySummary 是面板与 API 使用的自主总览。
type autonomySummary struct {
	Enabled   bool                `json:"enabled"`
	Goals     []*autonomy.Goal    `json:"goals"`
	Active    *autonomy.Goal      `json:"active"`
	ActiveID  string              `json:"activeId"`
	Decisions []autonomy.Decision `json:"decisions"`
	Stage     int                 `json:"stage"`
	Progress  int                 `json:"progress"`
}

// AutonomySummary 返回自主子系统总览（未启用时 Enabled=false，其余为空）。
func (e *Engine) AutonomySummary(decisionLimit int) autonomySummary {
	sum := autonomySummary{Enabled: e.autonomyEnabled()}
	if e.autonomy == nil {
		return sum
	}
	sum.Goals = e.autonomy.Goals()
	if g, ok := e.autonomy.Active(); ok {
		sum.Active = g
		sum.ActiveID = g.ID
		sum.Stage = len(g.Stages)
		sum.Progress = g.ProgressPercent()
	}
	sum.Decisions = e.autonomy.Decisions(decisionLimit)
	return sum
}

// SetAutonomyGoal 人工设置/切换项目目标（面板与 CLI 用）。
func (e *Engine) SetAutonomyGoal(title, intent, domain string) (*autonomy.Goal, error) {
	if e.autonomy == nil {
		return nil, fmt.Errorf("自主子系统未初始化")
	}
	g, err := e.autonomy.SetGoal(title, intent, domain)
	if err != nil {
		return nil, err
	}
	e.log.Info("人工设置自主目标", "goalId", g.ID, "title", g.Title)
	e.emitEvent(model.Event{Type: "autonomy", At: model.Now(), Msg: "人工设置项目目标：" + g.Title,
		Data: map[string]any{"goalId": g.ID, "title": g.Title, "intent": g.Intent}})
	return g, nil
}

// ResumeAutonomyGoal 人工解锁 blocked 目标。
func (e *Engine) ResumeAutonomyGoal(id string) error {
	if e.autonomy == nil {
		return fmt.Errorf("自主子系统未初始化")
	}
	if err := e.autonomy.ResumeGoal(id); err != nil {
		return err
	}
	e.emitEvent(model.Event{Type: "autonomy", At: model.Now(), Msg: "自主目标已恢复运行：" + id})
	return nil
}

// pendingAcceptance 判断该目标是否存在"已完成但未写入 stages"的任务。
//
// 判据：任务 source=autonomy 且 state=done，但 goal.Stages 里没有它的 taskId。
// 这类任务是异步后处理（postProcess）的中间态，不能据此推进阶段序号。
func pendingAcceptance(all []*model.Task, goal *autonomy.Goal) bool {
	if goal == nil {
		return false
	}
	recorded := make(map[string]bool, len(goal.Stages))
	for _, st := range goal.Stages {
		if st.TaskID != "" {
			recorded[st.TaskID] = true
		}
	}
	for _, t := range all {
		if t == nil || t.GoalID != goal.ID {
			continue
		}
		if t.State == model.StateDone && !recorded[t.ID] {
			return true
		}
	}
	return false
}

// inFlightTasksOfGoal 返回某自主目标下仍在途（非终态）的任务。
//
// 用途：自主线必须串行推进 —— 阶段 N 的产出是阶段 N+1 的输入，
// 在途时再规划会导致同名重复任务与上下文错乱（2026-10-19 沙箱实测）。
func inFlightTasksOfGoal(all []*model.Task, goalID string) []*model.Task {
	if goalID == "" {
		return nil
	}
	var out []*model.Task
	for _, t := range all {
		if t == nil || t.GoalID != goalID {
			continue
		}
		if !t.State.IsTerminal() && t.State != model.StateRejected {
			out = append(out, t)
		}
	}
	return out
}

// WaitInflight 等待后台后处理（自评/自主验收/链式生成）结束，最多等 timeout。
//
// 用途：单测与优雅退出。postProcess 是异步 goroutine，测试里若不等待，
// TempDir 清理会与它正在写的文件竞争而报 "directory is not empty"（实测）。
// 返回 true 表示已全部结束，false 表示超时仍有在跑。
func (e *Engine) WaitInflight(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		e.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// lastCompletedTask 返回最近一条已完成任务（供规划器读取自评不足与后继想法）。
func (e *Engine) lastCompletedTask() *model.Task {
	all := e.store.List()
	for i := len(all) - 1; i >= 0; i-- {
		t := all[i]
		if t != nil && t.State == model.StateDone {
			return t
		}
	}
	return nil
}

// tasksToday 返回今日已生成任务数（与 supplyCandidates 口径一致）。
func (e *Engine) tasksToday() int {
	start := startOfToday()
	n := 0
	for _, t := range e.store.List() {
		if t.CreatedAt >= start {
			n++
		}
	}
	return n
}

// recentTitles 取最近 limit 条已完成任务标题（防重复规划）。
func recentTitles(all []*model.Task, limit int) []string {
	if limit <= 0 {
		limit = 20
	}
	out := make([]string, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if all[i] != nil && all[i].State == model.StateDone {
			out = append(out, all[i].Title)
		}
	}
	return out
}

// AutonomyStatusLine 返回一行自主状态摘要（托盘/日志/CLI/面板用）。
func (e *Engine) AutonomyStatusLine() string { return e.autonomyStatusLine() }

// autonomyStatusLine 返回一行自主状态摘要（托盘/日志/CLI 用）。
func (e *Engine) autonomyStatusLine() string {
	if !e.autonomyEnabled() {
		return "自主：未启用（固定模板模式）"
	}
	g, ok := e.autonomy.Active()
	if !ok || g == nil {
		return "自主：无活跃目标"
	}
	return fmt.Sprintf("自主：%s（阶段 %d，进度 %d%%）", strings.TrimSpace(g.Title), len(g.Stages), g.ProgressPercent())
}

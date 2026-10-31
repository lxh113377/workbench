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
		// 构造期用 warnDeferred（L6 接线收口）：此处 e.log 还是 Nop（main 在 New()
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
	// 每日上限是**终止条件**，必须先于规划检查（2026-10-19 现场缺陷）。
	//
	// 现场数据（6.8 小时真实运行，decisions.jsonl 1408 条）：
	//   plan 1360 条 vs accept 只有 33 条（41:1），其中 980 条 evidence 是
	//   「已达每日任务上限」，连续 766 次同一拒绝，中位间隔 3000ms。
	//
	// 机理：tick 每 3 秒跑一次 -> 走到这里的规划 -> Evaluate 命中 DailyCap -> 拒绝、
	//   任务落 rejected **不入队** -> 「自主线串行」水位判据（autonomyQueued>=1）永远
	//   不成立 -> 下一个 tick 再来一遍。形成「规划 -> 拒绝 -> 3 秒后再规划」的空转热循环：
	//   **零产出，但台账每 3 秒膨胀一条**（7x24 下会写爆磁盘、淹没真实故障信号）。
	//
	// 修法：在**规划之前**判定上限，命中即直接返回 false（把机会让给模板链路，
	// 由它自己的 daily 判据决定是否补货），从根上断掉这个循环。
	// 注意返回 false 而不是 true：自主链路此刻只是「今天不干活」，
	// 不是「已处理完本轮的补货职责」—— 返回 true 会挡住模板链路。
	if e.tasksToday() >= cfg.Eval.DailyCap && cfg.Eval.DailyCap > 0 {
		// 决策点自己发声（round 148 · T104）：额度已耗尽 ⇒ 自主链路今天不干活。
		// 现场一次风暴烧穿整日额度后，两条供货链同时 return，8.5 小时里日志只剩「路由表已更新」——
		// 静默停机对 7×24 系统是最坏的失效形态（详见 supply_cap_log.go）。限频 10 分钟，不影响本行返回语义。
		e.logDailyCapBurn(cfg, startOfToday())
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
	// 拒绝熔断（2026-10-21 现场缺陷）：同一目标连续被评估拒绝到阈值后，
	// 本 tick 不再规划，改为把机会让给模板链路 —— 否则"恒定被拒的草案"
	// 会以每 3 秒一条的速度刷爆台账并吃满当日配额（现场 2000 条 / 12 小时零产出）。
	//
	// 注意熔断只影响"本轮是否规划"，不清空、不修改任何台账；
	// 候选修复（配额口径、新颖度口径）落地后 streak 自然归零。
	if hasGoal && activeGoal != nil {
		if streak, tripped := e.rejectionPressure(activeGoal.ID, cfg.Autonomy.RejectStreakWindowMinutes); tripped {
			e.logRejectBreakerOnce(activeGoal.ID, streak)
			return false
		}
	}
	// round 127 · T82：供料侧改用含「同目标未交付草案」的集合，否则轮转计数器不前进 ⇒ 恒定标题被连拒。
	// 目标 id 在 154 行附近已取到（activeGoal）；无活跃目标时传空串，行为退化成只看 Done。
	supplyGoalID := ""
	if activeGoal != nil {
		supplyGoalID = activeGoal.ID
	}
	recent := supplyRecentTitles(allTasks, supplyGoalID, 30)
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
	// 时间上界（2026-10-19 现场缺陷修复）：
	//
	// 现场铁证（真实运行时间线）：
	//   21:11:43  任务完成 [task-...173184-c87c]  <- 任务变 done，验收尚未写入 stages
	//   21:12:11  引擎已启动                    <- 运维重启（换二进制）打断异步验收
	//   => 任务成了「孤儿」：state=done 但 goal.stages 里没有它的 taskId
	//   => 本函数恒返回 true => supplyAutonomous 永久不规划（实测 36.5 分钟零活动）
	//
	// 为什么必须有上界：7x24 下重启是常态（换二进制/自愈/宕机），
	// 不可能要求「永不在 done→记录 的窗口内重启」。一旦命中，自主链路**永久死锁**，
	// 而且没有任何日志说明卡住了（只有 plan 归零）——比「偶发重复规划」严重得多。
	//
	// 语义：孤儿只在**时间窗口内**算「在途」（用于防止窗口期重复规划）；
	// 超过窗口说明这次验收已经失去机会，宁可让规划继续（任务可能被重做），
	// 也不能永久停摆。
	const orphanGraceMs = 10 * 60 * 1000 // 10 分钟：远大于正常验收耗时，又远小于人工发现延迟
	now := model.Now()
	for _, t := range all {
		if t == nil || t.GoalID != goal.ID {
			continue
		}
		if t.State != model.StateDone || recorded[t.ID] {
			continue
		}
		// FinishedAt 为 0 时退回 CreatedAt（老数据兼容）。
		ts := t.FinishedAt
		if ts == 0 {
			ts = t.CreatedAt
		}
		if ts == 0 || now-ts <= orphanGraceMs {
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

// tasksToday 返回今日**有效工作量**（与 supplyCandidates 口径一致）。
//
// 口径演化（2026-10-21 现场缺陷，直接造成"运行中但今日产出 0"）：
//
// 旧口径把**所有**当天创建的任务都算进去，包括 state=rejected 的评估拒绝草案。
// 评估拒绝的草案从未执行、从未产出、也不消耗模型额度 —— 它只是"这条点子不行"，
// 不是"今天已经干了这么多活"。把它计入每日上限的直接后果：
//
//	现场 2026-10-21 00:00-01:39 连续产生 2000 条被拒草案（标题逐字相同），
//	01:39:59 之后 dailyCap=2000 被吃满 =>
//	  supplyAutonomous 在规划前就 return false（每日上限是终止条件，by design）
//	  supplyCandidates 的 daily 判据也命中 => 直接 return
//	=> **两条供货链同时停机，此后一整天零任务、零产出**，而面板仍显示"运行中"。
//	  这正是用户报告的"显示运行中但显示产出为 0"。
//
// 正确口径：已拒草案不计入每日上限（它们不是工作），但拒绝压力另有独立闸门
// 约束（见 rejectionPressure），避免"不计入上限"退化成"无限刷台账"。
func (e *Engine) tasksToday() int {
	start := startOfToday()
	n := 0
	for _, t := range e.store.List() {
		if t == nil || t.CreatedAt < start {
			continue
		}
		if t.State == model.StateRejected {
			continue // 被拒草案不是工作量
		}
		n++
	}
	return n
}

// rejectStreakLimit 是"同一目标连续被评估拒绝"的熔断阈值。
//
// 为什么必须有（2026-10-21 现场缺陷）：规划器产出的草案若因任何原因恒定被评估器
// 拒绝（本轮根因是标题被字节截断吃掉了阶段后缀，导致每轮产出逐字相同的标题），
// 调度循环每 3 秒重试一次，一晚上写下 2000 条拒绝与 5724 条决策台账，
// 而**产出始终为 0**。这类"看起来在规划、实际在空转"的失效必须自己叫停。
//
// 阈值取 5：给规划器留出跨过"某个阶段暂时产不出合格草案"的余量，
// 又不至于让空转持续到把 dailyCap 吃满。
const rejectStreakLimit = 5

// rejectionPressure 返回「同一自主目标连续被拒次数」的熔断判定。
//
// 语义：
//   - streak >= rejectStreakLimit 时返回 true，本 tick 不再规划；
//   - 一旦出现被受理（入队）的自主任务，streak 自然归零（只看尾部连续段）；
//   - 只统计 autonomy 来源的 rejected，且只数**连续**尾部，不看历史总量。
func (e *Engine) rejectionPressure(goalID string, windowMinutes int) (int, bool) {
	if goalID == "" {
		return 0, false
	}
	if windowMinutes <= 0 {
		windowMinutes = 10
	}
	// 半开的重试口（round 47 · T35）：只有**最近**几分钟内的连续拒绝才算熔断压力；
	// 陈旧尾巴不计入 —— 否则生成器被「几小时前的拒绝」永久掐死，且永远无法靠「被受理」解套（死锁）。
	cutoff := model.Now() - int64(windowMinutes)*60_000
	all := e.store.List()
	streak := 0
	// 从最新往回数：遇到非 autonomy/非本目标/非 rejected 的任务即停。
	for i := len(all) - 1; i >= 0; i-- {
		t := all[i]
		if t == nil || t.Source != "autonomy" || t.GoalID != goalID {
			continue
		}
		if t.State != model.StateRejected {
			break
		}
		if t.CreatedAt < cutoff {
			continue // 陈旧拒绝：跳过（不构成本轮压力）
		}
		streak++
		if streak >= rejectStreakLimit {
			break
		}
	}
	return streak, streak >= rejectStreakLimit
}

// logRejectBreakerOnce 记录一次拒绝熔断（同一目标每秒只写一条，避免日志噪音）。
//
// 为什么要记：熔断是"主动停机"，如果不留痕，现场就只剩"没有新任务"这个现象 ——
// 那正是本轮 32 小时零产出最难排查的地方（没有任何错误、没有任何告警）。
func (e *Engine) logRejectBreakerOnce(goalID string, streak int) {
	e.rejectBreakerMu.Lock()
	if e.rejectBreakerAt == nil {
		e.rejectBreakerAt = map[string]int64{}
	}
	last := e.rejectBreakerAt[goalID]
	now := model.Now()
	if now-last < 60_000 {
		e.rejectBreakerMu.Unlock()
		return
	}
	e.rejectBreakerAt[goalID] = now
	e.rejectBreakerMu.Unlock()
	msg := fmt.Sprintf("自主规划熔断：目标 %s 连续 %d 条草案被评估拒绝，暂停规划（等待评估口径修复或人工介入）", goalID, streak)
	e.log.Warn("自主规划熔断", "goalId", goalID, "streak", streak)
	e.emitEvent(model.Event{Type: "warn", At: now, Msg: msg})
}

// supplyRecentTitles 是给「自主供料」用的 Recent 集合（round 127 · T82）。
//
// 为什么不能直接用 recentTitles（只取 Done）——现场（2026-10-30 07:08→08:34）：
// 目标阶段 2 的标题是 `小步修（正确性/第1轮）`，planner 的轮转号取自 ctx.Recent 里
// 「小步修（」出现的次数（planner.go 里我自己在注释写着「含被拒草案」）。
// 但 recentTitles 只收 State==Done ⇒ 一条从未交付成功的阶段（当前 c-done=2/阈值更高），
// 计数器**永远为 0** ⇒ 每次规划都算出同一个标题 ⇒ evaluator 按精确指纹判重连拒，
// 触发拒绝熔断 ⇒ 实测 07:08/07:48/08:22 各只放出 1 条、08:34 又连拒 5 次（约 35 分钟一个循环）。
//
// 口径：Done 照旧（不限目标），再补**同目标**的未交付草案（candidate/evaluating/rejected/
// approved/queued/running）—— 只补同目标是因为轮转号属于该目标的阶梯进度，跨目标会互相污染。
// 判重不受此处影响：evaluator 用的是它自己的 e.recent 缓存（含 Delivered 语义），两套集合不混用。
func supplyRecentTitles(all []*model.Task, goalID string, limit int) []string {
	if limit <= 0 {
		limit = 20
	}
	out := make([]string, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		t := all[i]
		if t == nil {
			continue
		}
		if t.State == model.StateDone {
			out = append(out, t.Title)
			continue
		}
		if goalID != "" && t.GoalID == goalID && strings.TrimSpace(t.Title) != "" {
			out = append(out, t.Title)
		}
	}
	return out
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

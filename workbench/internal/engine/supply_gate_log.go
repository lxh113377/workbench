package engine

import (
	"workbench/internal/model"
)

// supplyBlockedEvidence 判断「供料门此刻被挡住」要不要留痕（round 128 · T83 第一块砖）。
//
// 现场（2026-10-30 09:10，agent 根实读）：12 条模板任务 parked 在队列里（state=queued、
// attempts=0/3、age=37 分钟），而供料门是 `if e.q.Len() == 0` ⇒ 自主规划与模板补货**同时**
// 被挡住；tick 每 3 秒一次，于是每小时静默跳过约 1200 次，日志里一条痕迹都没有。
// 这正是本轮"T82 线上判据取不到"的直接原因（decisions 条数=0 不是"没触发"，是没走到规划）。
//
// 口径拆成两问（否则要么漏报要么刷屏）：
//
//	· blocked：门当前是否关着（队列非空）；
//	· worthLog：是否值得记 —— 只在「队列有货却没有在跑的任务」时值得（那是 parked 占位饿死供料）；
//	  有货且正在消化是**正常态**，记了只是噪音（本会话已被日志噪音反噬过多次）。
func supplyBlockedEvidence(queued, inflight int) (blocked, worthLog bool) {
	if queued <= 0 {
		return false, false // 队列空 ⇒ 门开着，下一 tick 自然补货
	}
	if inflight > 0 {
		return true, false // 有货且在消化：正常态，不留痕
	}
	return true, true
}

// runningCount 数在跑任务（sync.Map 计数）。
//
// 为什么不复用 q.Len()：队列长度只说明"有多少待取"，说明不了"有没有人在干活"——
// T70 的病灶恰恰是"有待取但永远取不完/取不动"，两者必须分开看。
func (e *Engine) runningCount() int {
	n := 0
	e.runningIDs.Range(func(_, _ interface{}) bool {
		n++
		return true
	})
	return n
}

// logSupplyBlockedOnce 在「供料门被 parked 任务挡住」时记一条限频 WARN。
//
// 处置选择：**只观测、不改门条件**。门写成 `q.Len()==0` 是有意的（engine.go:824 的注释说明理由：
// 队列还有货时又补一批，会让"闸门过期后任务是否真被取走"无法观测，也违背"队列是唯一真值来源"）。
// 所以 T70 的修法（改成"无在跑任务即可补货"）必须先有**频率证据**才动手 —— 本轮就产这个证据：
// 若这条 WARN 在长窗口里反复出现，说明 parked 饿死供料是常态而非偶发，值得改门；反之不动。
// 限频 60 秒（tick 是 3 秒一轮，不限频会把 1200 次/小时全刷进日志，反而淹没信号）。
func (e *Engine) logSupplyBlockedOnce() {
	queued := e.q.Len()
	inflight := e.runningCount()
	blocked, worth := supplyBlockedEvidence(queued, inflight)
	if !blocked || !worth {
		return
	}
	e.supplyBlockMu.Lock()
	now := model.Now()
	if now-e.supplyBlockAt < 60_000 {
		e.supplyBlockMu.Unlock()
		return
	}
	e.supplyBlockAt = now
	e.supplyBlockMu.Unlock()
	e.log.Warn("供料门被挡：队列有货但无在跑任务",
		"queued", queued, "inflight", inflight,
		"note", "自主与模板补货均要求 q.Len()==0，本 tick 不规划；持续出现=parked 任务饿死供料（T70）")
}

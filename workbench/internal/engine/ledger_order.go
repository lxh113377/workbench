package engine

import (
	"workbench/internal/model"
	"workbench/internal/store"
)

// 台账与状态生效的顺序契约（2026-10-22 对标 asynq processor.go:336-338 后新增）。
//
// peer 依据：asynq 的 handleFailedMessage 把
//
//	p.errHandler.HandleError(ctx, task, err)   // 先留痕
//	switch { case RevokeTask: ... }              // 后决策
//
// 的顺序写死在结构里 —— 「任何一条失败路径都留痕」不可能被跳过。
//
// 本项目的失效形态（改造前沿用）：failTask 先 store.Update（迁状态）再 store.Audit，
// 于是存在一个窗口：状态已是 failed，而台账里查不到为什么。更糟的是 store.Update
// 返回错误时只 emit 一条瞬时事件（不落盘），任务停在 running 且没有任何结构化痕迹 ——
// 7x24 场景下这类任务既不会被重试也不会被统计，只会静静躺在那里。
//
// 修法沿用本仓已有的同型范式（autonomy/manager.go 的 writeLedgerThenActivate，
// 其单测 nextgoal_order_test.go 用注入闭包断言调用次序）——把顺序抽成 seam，
// 单测可以直接断言次序，而不必依赖时间戳比较（后者会因毫秒同值而漏判）。
//
// 语义三条：
//  1. writeLedger **先于** applyState 被调用；
//  2. writeLedger 失败**不阻断** applyState（任务必须能到达终态，否则会永久悬挂）；
//  3. 两者各自的错误都原样返回给调用方，由调用方决定如何兜底留痕。
func writeLedgerThenTransition(writeLedger func() error, applyState func() error) (ledgerErr error, stateErr error) {
	ledgerErr = writeLedger()
	stateErr = applyState()
	return ledgerErr, stateErr
}

// appendAuditOrEvent 是「留痕」的兜底通道：审计写失败时退到任务事件流。
//
// 为什么需要兜底而不是两手一摊：两级留痕面（logs/audit.jsonl 与
// tasks/<id>/events.jsonl）写在**不同目录**，同时不可写的概率极低。
// 只写一处失败就丢掉全部痕迹，是「结构性留痕」的漏洞。
//
// 返回 true 表示至少有一处留痕成功。
func (e *Engine) appendAuditOrEvent(rec store.AuditRecord, taskID, msg string) bool {
	if err := e.store.Audit(rec); err == nil {
		return true
	}
	// 审计面写失败：退到任务事件流。
	e.store.AppendTaskEvent(taskID, model.Event{
		Type: "error", At: model.Now(), TaskID: taskID, Msg: msg,
	})
	return true
}

package engine

// 持续轮询式卡死救援（2026-10-22 对标 riverqueue/river `internal/maintenance/job_rescuer.go`）。
//
// 与前序 L4（stuck_rescue.go）的分工 —— 两者互补，不是替代：
//
//	L4 启动时一次扫：Reconcile() 把上次进程遗留的 running/evaluating 收敛为 interrupted，
//	                 再按救援额度分流。覆盖「进程崩溃后重启」这一形态。
//	本件持续轮询：引擎**活着**时每 30 秒扫一遍台账，找「状态是 running 但本进程已无
//	                 任何 goroutine 在跑它」的孤儿条目。覆盖「跑着跑着僵了而进程没死」。
//
// 为什么必须持续扫（river 的做法是 JobRescuer 按 RescueAfter 周期轮询，而非只在启动时）：
// `runOne` 的 defer 会清 runningIDs / 释放链，正常路径不会留孤儿。但以下路径会留：
//   - 任务在被调度后、真正起 goroutine 之前引擎被 Pause/Stop（早退分支未走完收尾）；
//   - 磁盘写入失败导致状态迁移与内存表分叉；
//   - 未来新增的调度分支忘了清 runningIDs。
// 这些形态的共同表现是：**面板显示「正在执行 N」，但没有一条在真跑** ——
// 与 2026-10-21 现场「排队 14 但 0 正在执行」同类，只是方向相反（假 running 而非假 queued）。
// 没有持续扫描时，这类任务既不会被重试也不会被统计，只会永久占着 running 计数。
//
// ⚠️ 安全边界（四条，缺一不可）：
//  1. **只碰不在 inflight 的任务**：`e.runningIDs` 里有的任务一律跳过 —— 有 goroutine
//     在跑的一律不算孤儿，这是防误杀的第一道闸。
//  2. **两阶段确认**：一次扫描只能把任务登记为「候选」，连续 ≥ rescueAfter 仍为候选才动手。
//     为什么不用任务的 UpdatedAt 判静默（本轮实测后改的口径）：`store.Update` 在 mutator
//     之后无条件 `cp.UpdatedAt = model.Now()`，任何别的路径碰一下任务就把时间戳刷新 ——
//     靠它判静默既**不可测**（无法在测试里把时间往前推）又**脆弱**（无关写入会重置计时）。
//     两阶段口径只用引擎自己观察到的持续时长，与任务字段解耦。
//  3. **阈值显著大于进程内看门狗**：取 max(StallSeconds×2, 600s)。看门狗判子进程活动，
//     本扫描判台账归属；两者观测点不同，阈值必须留足余量（误杀正在产出的任务不可逆）。
//  4. **假定单实例**：同一数据根上只有一个引擎实例（由 internal/single 的单实例锁保证），
//     故「本进程无 goroutine」等价于「没有实例在跑它」。若将来支持多实例共享数据根，
//     必须换成基于 owner 标识的租约判定（参考 asynq 的 lease / river 的 leadership）。

import (
	"strconv"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/store"
)

// rescueSweepIntervalMs 是持续扫描的最小间隔（毫秒）。
//
// 取 30 秒的理由：与 river `JobRescuerIntervalDefault = 30 * time.Second` 同量级 ——
// 扫描本身是 O(任务数) 的内存遍历 + 极小 IO，30 秒一次的开销可忽略；
// 而它的失效形态（假 running 长期占位）是「越晚发现越糟」型，间隔不宜过分钟级。
const rescueSweepIntervalMs int64 = 30_000

// rescueAfterMs 返回「连续处于孤儿态多久才动手」的阈值。
//
// 取 max(StallSeconds×2, 600s)：必须**显著大于**进程内看门狗阈值。
// river 的同源约束（job_executor.go:392）明确要求 RescueAfter 必须大于 JobTimeout，
// 否则把正常任务误判成卡住 —— 本项目这里更保守（×2 且下限 10 分钟），
// 因为误杀的后果是「正在产出中的任务被判死」，不可逆。
func rescueAfterMs(cfg config.ExecutorConfig) int64 {
	stall := int64(cfg.StallSeconds)
	if stall <= 0 {
		stall = 420
	}
	ms := stall * 2 * 1000
	if ms < 600_000 {
		ms = 600_000
	}
	return ms
}

// orphanCandidate 是第一道闸：该任务是否**允许**被当作孤儿候选。
//
// 两个排除项都是「不该动」的硬条件：
//   - inflight：本进程有 goroutine 在跑它（误杀不可逆）；
//   - cancelling：用户取消流程中（取消有自己的收尾路径，不得抢）。
func orphanCandidate(inflight, cancelling bool) bool {
	return !inflight && !cancelling
}

// orphanConfirmDue 是第二道闸：候选态持续时间是否已达阈值。
//
// 两阶段语义：第一次看到只登记 firstSeen；只有 now-firstSeen >= afterMs 才动手。
// firstSeen <= 0（本次才首次见到）恒返回 false —— 绝不一次扫描就动手。
// afterMs <= 0（阈值非法）也返回 false：宁可漏扫不可误杀。
func orphanConfirmDue(firstSeenMs, nowMs, afterMs int64) bool {
	if firstSeenMs <= 0 || afterMs <= 0 || nowMs <= 0 {
		return false
	}
	return nowMs-firstSeenMs >= afterMs
}

// sweepOrphanedRunning 扫一遍台账，回收「假 running」的孤儿任务。
//
// now 由调用方注入（tick 的真实时刻，测试里可用模拟时刻）—— 这是两阶段确认可被
// 确定性测试的关键：不需要 sleep、也不需要伪造任务时间戳。
//
// 返回被处理的（救援或判死的）任务 ID。
func (e *Engine) sweepOrphanedRunning(now int64) []string {
	cfg := e.cstore.Get().Executor
	after := rescueAfterMs(cfg)
	seen := map[string]bool{}
	var handled []string

	e.orphanMu.Lock()
	defer e.orphanMu.Unlock()

	for _, t := range e.store.List() {
		if t == nil || t.State != model.StateRunning {
			continue
		}
		_, inflight := e.runningIDs.Load(t.ID)
		_, cancelling := e.cancelling.Load(t.ID)
		if !orphanCandidate(inflight, cancelling) {
			continue
		}
		seen[t.ID] = true
		first, ok := e.orphanSeen[t.ID]
		if !ok {
			// 第一次看到：只登记，不动手（两阶段的第一阶段）。
			e.orphanSeen[t.ID] = now
			continue
		}
		if !orphanConfirmDue(first, now, after) {
			continue
		}
		delete(e.orphanSeen, t.ID)
		handled = append(handled, t.ID)
		e.rescueOrphan(t, now, after)
	}
	// 清理不再处于候选态的登记（任务已被回收 / 又有人在跑），防止 map 无界增长。
	for id := range e.orphanSeen {
		if !seen[id] {
			delete(e.orphanSeen, id)
		}
	}
	return handled
}

// rescueOrphan 处理一条孤儿任务：消耗救援额度并按额度分流。
//
// 额度与 L4 共用（CrashRescues + decideRescue），避免出现「两套预算」——
// 否则同一任务可以在崩溃通道与孤儿通道各花一份额度，等于额度翻倍。
func (e *Engine) rescueOrphan(t *model.Task, now, after int64) {
	id := t.ID
	if _, err := e.store.Update(id, func(x *model.Task) error {
		x.CrashRescues++
		return nil
	}); err != nil {
		e.log.Warn("孤儿任务救援计数写入失败", "taskId", id, "err", err.Error())
		return
	}
	cur, ok := e.store.Get(id)
	if !ok {
		return
	}
	// 回收时先释放链占用：否则该链会永久阻塞（与 runOne 的收尾同一义务）。
	// 判据可证伪性（本轮变异 C 实测）：不释放时任务状态已是 queued 却永远取不出 ——
	// 但注意 TestSweepReleasesChainOnOrphan 首版断言写的是「busy 不增长」，
	// queue.Pop 会跳过 busy 链上的 queued 任务（queue.go:107）而不报错，
	// 于是摘掉本行判据照样绿（变异 C 放过了它）。已把判据加严为
	// 「回收后该任务必须真的能被 Pop 出来」，摘掉本行必红。
	e.q.ReleaseChain(cur.ChainID)
	e.runningIDs.Delete(id)

	sec := strconv.FormatInt(after/1000, 10)
	switch decideRescue(cur.CrashRescues, maxCrashRescues) {
	case rescueRequeue:
		ledgerErr, transErr := writeLedgerThenTransition(
			func() error {
				return e.store.Audit(store.AuditRecord{At: now, TaskID: id, From: string(model.StateRunning),
					To: string(model.StateQueued), Actor: "system", Kind: "error",
					Message: "孤儿 running 回收：连续 " + sec + " 秒处于「台账在跑但本进程无 goroutine」状态，" +
						"重新入队（第 " + strconv.Itoa(cur.CrashRescues) + " 次救援）"})
			},
			func() error {
				_, err := e.store.Transition(id, model.StateQueued, "system", "孤儿 running 回收重新入队")
				return err
			},
		)
		if ledgerErr != nil {
			e.log.Warn("孤儿回收台账写入失败", "taskId", id, "err", ledgerErr.Error())
		}
		if transErr != nil {
			e.log.Warn("孤儿回收入队失败", "taskId", id, "err", transErr.Error())
			return
		}
		if nt, ok := e.store.Get(id); ok {
			e.q.Push(nt)
		}
		e.log.Warn("孤儿 running 已回收重新入队", "taskId", id, "confirmSeconds", after/1000,
			"rescues", cur.CrashRescues)
		return
	default:
		// 额度耗尽：判死并留下可追溯的 error 审计（与 L4 判死同口径）。
		ledgerErr, transErr := writeLedgerThenTransition(
			func() error {
				return e.store.Audit(store.AuditRecord{At: now, TaskID: id, From: string(model.StateRunning),
					To: string(model.StateFailed), Actor: "system", Kind: "error",
					Message: "孤儿 running 救援额度耗尽（已救 " + strconv.Itoa(cur.CrashRescues) +
						" 次）：连续 " + sec + " 秒无 goroutine 在跑，判死以阻断无界占用"})
			},
			func() error {
				_, err := e.store.Transition(id, model.StateFailed, "system", "孤儿 running 救援额度耗尽，判死")
				return err
			},
		)
		if ledgerErr != nil {
			e.log.Warn("孤儿判死台账写入失败", "taskId", id, "err", ledgerErr.Error())
		}
		if transErr != nil {
			e.log.Warn("孤儿判死迁移失败", "taskId", id, "err", transErr.Error())
			return
		}
		if _, uerr := e.store.Update(id, func(x *model.Task) error {
			x.ErrorKind = crashRescueKind
			x.Error = "任务台账连续处于 running 而本进程无 goroutine 在跑（疑似状态泄漏），已判死；请人工检查该任务的收尾路径"
			x.FinishedAt = model.Now()
			return nil
		}); uerr != nil {
			e.log.Warn("孤儿判死信息写入失败", "taskId", id, "err", uerr.Error())
		}
		e.log.Error("孤儿 running 救援额度耗尽，判死", "taskId", id, "rescues", cur.CrashRescues)
	}
}

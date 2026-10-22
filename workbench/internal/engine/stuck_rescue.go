package engine

import (
	"strconv"

	"workbench/internal/model"
	"workbench/internal/store"
)

// L4 卡死救援（对标 riverqueue/river 的 RescueStuckJobsAfter + JobStuckHandler）。
//
// 问题：崩溃恢复链路原本是「无界」的 ——
//
//	main.go:226 Recover() → store.Reconcile() 把 running/evaluating 收敛为 interrupted
//	→ engine.go 把 interrupted **无条件**转回 queued 重新入队
//
// 而 Attempts 是单次 runOne 内的重试序号（engine.go 里 t.Attempts = attempt 每轮直接赋值），
// 崩溃恢复后重跑会被重置覆盖，所以它**不能**当崩溃预算 —— 拿它判死等于没有判死。
// 于是「一跑就把进程弄死」的任务（panic 逃逸、原生崩溃、OOM）会配合
// WorkBenchKeepAlive 每分钟拉起，形成**无界崩溃循环**：恢复 → 入队 → 崩 → 恢复 → …
// 这是 7×24 无人值守里最坏的失效形态，因为它看起来"一直在干活"，实际零产出。
//
// 本文件给出救援额度与判死分流。额度刻意做成包内常量而不是 config 字段：
// 一是这项策略几乎不需要运维调，二是 config.go 当前是并行会话的写入口，
// 不越界。要开放配置时，把 maxCrashRescues 换成 cfg.Executor.MaxCrashRescues 即可，
// decideRescue 的签名已按可注入形式设计。
const maxCrashRescues = 3

// rescueAction 是一次崩溃恢复的处置决定。
type rescueAction int

const (
	// rescueRequeue 还有救援额度：按既有语义重新入队续跑。
	rescueRequeue rescueAction = iota
	// rescueCondemn 额度耗尽：判死并停止重投，避免无界崩溃循环。
	rescueCondemn
)

func (a rescueAction) String() string {
	if a == rescueRequeue {
		return "requeue"
	}
	return "condemn"
}

// decideRescue 是纯函数（无 IO、无时间、无全局状态），便于表驱动测试。
//
// 参数语义（易错点，被自己的测试抓出来过一次）：crashRescues 是**累加之后**的序号，
// 即"本次是第几轮救援"，不是"之前救过几次"。所以 budget=3 表示「允许 3 轮救援」：
// 第 1/2/3 轮放行，第 4 轮判死。写成 crashRescues < budget 会少救一轮。
//
// 保守方向：budget <= 0（未配置/非法）时只救一轮 —— 既不是"完全不救"，
// 也不是"无限救"。理由：历史数据里 CrashRescues 字段不存在（omitempty），
// 老任务读出来恒为 0；把 0 解释成"完全不救"会让升级前遗留的悬挂任务
// 在重启时被集体判死，那是比崩溃循环更坏的后果。
func decideRescue(crashRescues int, budget int) rescueAction {
	if budget <= 0 {
		if crashRescues <= 1 {
			return rescueRequeue
		}
		return rescueCondemn
	}
	if crashRescues <= budget {
		return rescueRequeue
	}
	return rescueCondemn
}

// crashRescueKind 是判死任务写入 task.ErrorKind 的类别。
// 面板「失败原因分布」（/api/failures 按 ErrorKind 聚合）据此把这类问题
// 与普通的执行失败区分开 —— 它意味着"任务本身会弄死进程"，需要人工看产物/prompt，
// 而不是再重试。
const crashRescueKind = "crash_loop"

// rescueInterrupted 处理一条 interrupted 任务：累加救援计数并按额度分流。
// 返回要重新入队的任务（nil 表示已判死，不再入队）。
func (e *Engine) rescueInterrupted(t *model.Task) *model.Task {
	id := t.ID
	// 先累加额度（走 store.Update：持锁 + 换指针 + 落 WAL，不裸改字段绕开审计）
	if _, err := e.store.Update(id, func(x *model.Task) error {
		x.CrashRescues++
		return nil
	}); err != nil {
		e.log.Warn("崩溃救援计数写入失败", "taskId", id, "err", err.Error())
		// 计数写不进去就不判死（否则可能误杀）：按既有语义重新入队。
		if nt, ok := e.store.Get(id); ok {
			return nt
		}
		return nil
	}
	cur, ok := e.store.Get(id)
	if !ok {
		return nil
	}
	switch decideRescue(cur.CrashRescues, maxCrashRescues) {
	case rescueRequeue:
		nt, err := e.store.Transition(id, model.StateQueued, "system", "崩溃恢复重新入队")
		if err != nil {
			e.log.Warn("崩溃恢复入队失败", "taskId", id, "err", err.Error())
			return nil
		}
		e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: id, Data: nt,
			Msg: "崩溃恢复：重新入队续跑（第 " + strconv.Itoa(cur.CrashRescues) + " 次救援）"})
		return nt
	default:
		// 留痕先于状态生效（2026-10-22 对标 asynq processor.go:336-338 后补齐）。
		//
		// 改造前这条**完全没有审计**：判死是一件终态决策（任务从此不再被投递），
		// 却只在内存里 emit 一条瞬时事件（不落盘）。事后来查「这条任务为什么被判死」
		// 只能靠现场观察面板，窗口一过就无从追溯 —— 而判死本身不可逆。
		// 顺序由 writeLedgerThenTransition 强制（见 ledger_order.go）。
		// 判据按 **kind=error** 区分（ledger_order_test.go）：Transition 自己也会记一条
		// kind=transition 的审计且 note 文案与本条高度重合，只搜文案的判据会对
		// 「删掉本条专用审计」这种回归失效（本轮变异实测踩到）。
		ledgerErr, transitionErr := writeLedgerThenTransition(
			func() error {
				return e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: id, From: string(cur.State),
					To: string(model.StateFailed), Actor: "system", Kind: "error",
					Message: "崩溃救援额度耗尽（已救 " + strconv.Itoa(cur.CrashRescues) + " 次），判死以阻断无界崩溃循环"})
			},
			func() error {
				_, err := e.store.Transition(id, model.StateFailed, "system",
					"崩溃救援额度耗尽（已救 "+strconv.Itoa(cur.CrashRescues)+" 次），判死以阻断无界崩溃循环")
				return err
			},
		)
		if ledgerErr != nil {
			e.log.Warn("崩溃救援判死台账写入失败，已退到事件流留痕", "taskId", id, "err", ledgerErr.Error())
		}
		if transitionErr != nil {
			e.log.Warn("崩溃救援判死迁移失败", "taskId", id, "err", transitionErr.Error())
			return nil
		}
		_, uerr := e.store.Update(id, func(x *model.Task) error {
			x.ErrorKind = crashRescueKind
			x.Error = "任务反复导致进程中断（已救援 " + strconv.Itoa(cur.CrashRescues) + " 次），已判死不再重投；请人工检查该任务的 prompt 与产物"
			x.FinishedAt = model.Now()
			return nil
		})
		if uerr != nil {
			e.log.Warn("崩溃救援判死信息写入失败", "taskId", id, "err", uerr.Error())
		}
		e.emitEvent(model.Event{Type: "error", At: model.Now(), TaskID: id,
			Msg: "崩溃救援额度耗尽，判死（阻断无界崩溃循环）"})
		return nil
	}
}

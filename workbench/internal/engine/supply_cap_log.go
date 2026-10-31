package engine

import (
	"fmt"
	"sort"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/retry"
)

// envBurnEvidence 统计「今日计入每日上限的任务」里有多少是**环境性失败**（round 148 · T104）。
//
// 口径复用 retry.Kind.Retryable() 这一既有单一来源，不在这里另列类别清单：
//
//	· transport_failed / not_served / rate_limited / quota_exhausted / boot_failed / no_lane /
//	  upstream_failed / timeout / pool_wait_capped 等"环境此刻不给力"的类别都可重试 ⇒ 计入环境性；
//	· verify_failed / crash_loop / replan / panic / transition_failed 是任务侧事实，不可重试 ⇒ 不计；
//	· cancelled 由 `!Retryable()` 天然排除（实测 Cancelled.Retryable()==false，retry.go 的 case 名单里没有它），
//	  所以**不在此重复写一遍**。冗余判据的危害不是多余，而是让人误以为"这行修正了什么"——它其实是装饰：
//	  真改动 Retryable 名单时没人会回头看它。（round 148 我曾把这条注释写成"虽属可重试类别"，
//	  是把不成立的事写成事实；本轮用 runtime 探针实测后改正。）
//
// 为什么不再造清单（本会话同族第 10 次的预防）：T57/T61/T72/T80/T84/T86 全是"同一契约多处实现"，
// 新增类别时只改一处 ⇒ 另一处静默失效。
func envBurnEvidence(tasks []*model.Task, startOfDay int64) (counted int, envFailures int, topKind string) {
	byKind := map[string]int{}
	for _, t := range tasks {
		if t == nil || t.CreatedAt < startOfDay {
			continue
		}
		// 与 tasksToday() 的口径必须完全一致（被拒草案不计入每日上限）：
		// 一旦这里多算或少算，读数就会与"是否命中上限"的产品判据分叉（2026-10-21 同类缺陷的教训）。
		if t.State == model.StateRejected {
			continue
		}
		counted++
		if t.State != model.StateFailed {
			continue // 只统计"已判死"的烧额度者；done/queued 不是浪费
		}
		k := retry.ParseKind(t.ErrorKind)
		// 只问 Retryable()：口径单一来源，不在此加第二条判据（理由见函数头 cancelled 那条）。
		if !k.Retryable() {
			continue
		}
		envFailures++
		if t.ErrorKind != "" {
			byKind[t.ErrorKind]++
		}
	}
	// topKind 必须**确定**：Go 的 map 遍历顺序随机，直接取最大值会让同一份数据在不同进程里
	// 报出不同主因（判据无法复算）。⇒ 先按计数降序，再按 kind 字典序定序。
	keys := make([]string, 0, len(byKind))
	for kk := range byKind {
		keys = append(keys, kk)
	}
	sort.Slice(keys, func(i, j int) bool {
		if byKind[keys[i]] != byKind[keys[j]] {
			return byKind[keys[i]] > byKind[keys[j]]
		}
		return keys[i] < keys[j]
	})
	if len(keys) > 0 {
		topKind = keys[0]
	}
	return counted, envFailures, topKind
}

// logDailyCapBurn 在「每日额度命中」时把静默停机变成可发现的事实（限频）。
//
// 现场（2026-10-31，agent 根真值 = queue.json ⊕ queue.wal.jsonl 重放）：
//
//	今日计入上限的 201 条任务里 **200 条是环境性失败**（errorKind=transport_failed 占 199 条），
//	一次上游网络风暴在 1.5 小时内烧穿整日预算；此后 8.5 小时整机只剩「模型路由表已更新」一种日志，
//	自主与模板两条供货链同时 return，**一条说明都没有**，而面板仍显示"运行中"。
//	这与 supply_gate_log.go 修的是同一类失效（静默空转是 24/7 系统最坏的形态），闸门不同而已。
//
// 观测点放在**决策点本身**（DailyCap 命中要 return 的那一刻），不是决策之前 —— T96 的教训：
// 把探针放在"还没做决定"的位置，读到的是"没决定"，会把正常重探误报成停摆。
// 限频 10 分钟：tick 每 3 秒一次，不限频每小时会刷 1200 条，反而淹没信号。
func (e *Engine) logDailyCapBurn(cfg config.Config, startOfDay int64) {
	counted, envFailures, topKind := envBurnEvidence(e.store.List(), startOfDay)
	e.dailyBurnMu.Lock()
	now := model.Now()
	if now-e.dailyBurnAt < 600_000 {
		e.dailyBurnMu.Unlock()
		return
	}
	e.dailyBurnAt = now
	e.dailyBurnMu.Unlock()
	msg := fmt.Sprintf("今日额度已被环境性失败烧穿：自主与模板补货同时停机（计数 %d、环境性失败 %d、主因 %s、上限 %d）",
		counted, envFailures, orNoneKind(topKind), cfg.Eval.DailyCap)
	e.log.Warn(msg, "counted", counted, "envFailures", envFailures, "dailyCap", cfg.Eval.DailyCap, "topKind", topKind)
	e.emitEvent(model.Event{Type: "warn", At: now, Msg: msg})
}

func orNoneKind(s string) string {
	if s == "" {
		return "无（今日无环境性失败，额度是被正常产出/任务侧失败用完的）"
	}
	return s
}

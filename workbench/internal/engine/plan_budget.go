package engine

import "time"

// planStageBudget 把「方案阶段总预算（分钟）」收敛成一个安全的 Duration（round 43 · T23）。
//
// 为什么需要（2026-10-27 实测）：方案阶段走 failover 逐条车道重试，单次上限 8 分钟；一个 15 分钟观察窗口被
// **一条任务**的方案阶段吃光（8m0s 超时 + 139s 卡死 + 3×4s boot_failed + 134s 卡死），执行阶段一次都没跑到 ——
// 而产物只能由执行阶段产出。阶段预算到点就退化直执行，把时间让给执行阶段。
//
// 取值口径：未配置/越界（<1 或 >60 分钟）⇒ 4 分钟；合法值原样返回。
// 取 4 的理由：够两轮「单次调用 + 一轮重试」，又不会把整任务预算（默认 30 分钟）吃掉大头。
func planStageBudget(minutes int) time.Duration {
	if minutes < 1 || minutes > 60 {
		minutes = 4
	}
	return time.Duration(minutes) * time.Minute
}

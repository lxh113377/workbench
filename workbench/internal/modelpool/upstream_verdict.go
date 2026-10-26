package modelpool

import "strings"

// upstreamVerdictState 把 OFM（our-free-model）**上游判定**映射为探测态。
//
// 返回 (state, kind, reason)：state == "" 表示「上游没有判定，按端点探测结果处理」。
//
// 为什么需要（2026-10-26 round 17 实测）：availability.json 的实测分布是 1 个 region-blocked +
// **7 个 throttled**（"Free usage limit reached…"），而 probeSpecs 原先只把
// unavailable / region-blocked / deprecated 判为不可用 —— **throttled 被当成健康**。
// 后果：调度器把额度已耗尽的免费车道当作可用，每个任务都挑中它们，
// 每次尝试烧掉 120–150 s 且**一个 token 都不产出**（实测子会话 tokenUsage 全 0）。
//
// 口径（保守，避免误杀）：只有上游**明确**说"这个模型现在用不了"才判 down ——
//
//	unavailable / region-blocked / deprecated / throttled —— 其余（available/ok/空/未知）不判。
//
// 「throttled 可能只是额度暂时耗尽」由既有冷却与周期探测负责恢复：down 之后仍会被重新探测，
// 上游恢复可用即回到调度队列（与 T15-② 的「探测只管可达性」互补：这里管上游判定，那里管执行产出）。
func upstreamVerdictState(verdict string) (State, string, string) {
	switch strings.ToLower(strings.TrimSpace(verdict)) {
	case "unavailable", "region-blocked", "deprecated":
		return StateDown, "model_unavailable", "上游判定该模型不可用"
	case "throttled", "rate-limited", "rate_limited", "quota-exceeded", "quota_exceeded":
		return StateDown, "model_throttled", "上游判定该模型额度耗尽/被限流"
	}
	return "", "", ""
}

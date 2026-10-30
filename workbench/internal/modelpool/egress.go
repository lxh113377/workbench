package modelpool

import (
	"strings"
)

// egressKey 归一一条车道的**出口标识**（round 132 · P0-2 更正）：
//
// 为什么需要它（现场实测 2026-10-30）：本机池内 38 条 spec 的 baseUrl **全部**是
// http://127.0.0.1:8326/v1（明文回环，走 https 的车道数=0）⇒ 探测路径上的连通性/清单同步
// 是**同一个回环端点**的事，不是 38 条独立网络路径。
//
// 这一点原先只写在 failover.go 的注释里（"同池其他车道的网络路径不同"），而那个前提
// 在本部署下是**假的** —— 拿假前提做的归因（transport_failed 算到单条车道头上、逐条冷却）
// 会系统性地把一个出口级故障摊成整池故障。注释不该承载这种事实：**能算的就别靠注释说**，
// 所以这里把它变成函数，并由 egress_test.go 用真实拓扑做判据（含"空 baseUrl 也算同一出口"）。
// ⚠️ 适用面（round 133 我自己犯过一次，写在这里防复发）：这里归一的是 **spec.baseUrl 所代表的那一跳**
// —— 即 WorkBench 自己的探测/清单路径（probe.go 用 baseURL(s)）。它**不等于执行面**：实际执行由 dsh
// 子进程发出，而 executor.go:854 给 dsh 写的覆盖层只有 provider: our-free-model + model: <id>、
// 不含 baseUrl ⇒ dsh 连哪个上游由 OFM 插件自己的配置决定（其中确有 https 上游）。
// ⇒ 不许拿"探测面只有一个出口"去否证执行面的证书问题，也不许据此断言逐车道冷却必然错。

func egressKey(s Spec, gatewayBase string) string {
	// 空 BaseURL 的既定语义是"走 OFM 内置网关"（见 Spec.BaseURL 的字段注释），
	// 而内置网关就是本机 127.0.0.1:8326 —— 所以它必须归一到同一个 key，
	// 否则"38 条车道共用一个出口"会被算成"38 个不同出口"，判据反过来。
	base := strings.TrimSpace(s.BaseURL)
	if base == "" {
		base = strings.TrimSpace(gatewayBase)
	}
	if base == "" {
		// 连内置网关地址都不知道时不硬猜：给一个明确的"未知出口"，
		// 让它与任何真实出口都不相等（宁可漏判共用，不可把不同出口误合成一个）。
		return "(unknown)"
	}
	// 归一化口径（大小写、末尾斜杠、以及 /v1 这类路径后缀都不该分裂出两个出口）：
	// 出口的本质是 scheme://host[:port]，路径差异是同一出口上的不同 API 面。
	u := strings.ToLower(base)
	if i := strings.Index(u, "://"); i >= 0 {
		rest := u[i+3:]
		if k := strings.IndexAny(rest, "/"); k >= 0 {
			rest = rest[:k]
		}
		return u[:i+3] + rest
	}
	return u
}

// egressGroups 按出口给车道分组（键=出口，值=该出口上的车道数）。
func egressGroups(specs []Spec, gatewayBase string) map[string]int {
	out := make(map[string]int, 4)
	for _, s := range specs {
		out[egressKey(s, gatewayBase)]++
	}
	return out
}

// sharesOneEgress 回答「这批车道是否都走同一个出口」。
// 少于 2 条车道时返回 false —— 单条车道谈不上"共用"，判据不该在样本不足时报真。
func sharesOneEgress(specs []Spec, gatewayBase string) bool {
	if len(specs) < 2 {
		return false
	}
	return len(egressGroups(specs, gatewayBase)) == 1
}

package modelpool

import "strings"

// egressScoped 回答「这次失败该按出口级处置吗（换模型没有意义）」（round 134 · T86 第一档）。
//
// 取证依据（并行子代理 + 我独立复算，两条通路一致；详见报告 E12 与
// xtmp/egress-vs-lane-2026-10-30.md）：transport 类失败内部**不是一个东西** ——
//
//	· self-signed 类归因事件 1,799 条，与「他车道同窗成功」共存 = 0/1799 = 0.0% ⇒ 出口级；
//	· stream_cut 91 条，±60s 共存 = 41/91 = 45.1% ⇒ 真车道级（实测样本：z-ai/glm-5.3 报流断时
//	  liquid/lfm-2.5-2.6b:free 正常产出）⇒ 换模型正是正确处置。
//
// 所以判 **MIXED**：约四分之三的 transport 事件该按出口处置，但一刀切会把 21~33 条健康车道一起熔掉。
//
// 为什么按正文指纹而不是按 Kind：同一个 transport_failed 既可能是出口级（证书链）也可能是车道级
// （对端 ETIMEDOUT），Kind 分不出来，只有上游原文能分。
// 这与 bootAttributionKind（T17②）同构 —— 那里也是"从正文认确定性描述"来纠正 Kind 的归因。
func egressScoped(kind, detail string) bool {
	// 只对表里标了 scopeByFingerprint 的 Kind 做正文判定（不在这里比 Kind 常量，避免又造一份私有清单）：not_served 已是出口级事实（表 + bootAttribution 处理），
	// 限流族由 laneDispositions 的 stopSwitch 列处理，其余是任务/车道能力问题（换模型有意义）。
	if !laneScopeByFingerprint(kind) {
		return false
	}
	low := strings.ToLower(detail)
	for _, fp := range egressFailureFingerprints {
		if strings.Contains(low, fp) {
			return true
		}
	}
	return false
}

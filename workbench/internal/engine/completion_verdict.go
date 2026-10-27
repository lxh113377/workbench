package engine

import (
	"strings"

	"workbench/internal/model"
)

// defaultArtifactDomains 是「交付物必须是文件」的默认域集（round 46 · T28）。
//
// 依据：目标是提升**工程质量**（代码/构建/前端），这些域的交付物是工作空间里的真实文件；
// 而 content/knowledge/report 等域的交付物就是**正文本身**，无产物块属正常形态 ——
// 那条 D7 契约（prose 型成功后仍要跑语义后处理）必须继续成立。
var defaultArtifactDomains = []string{"code", "quality", "frontend"}

// artifactRequired 判定该域是否要求产物块（`cfg.Executor.ArtifactDomains` 为空/零值时用默认集）。
//
// 判据只做**精确域匹配**（大小写不敏感、去空白）：不做前缀/正则，避免把 content 误判进工程域。
func artifactRequired(domain string, allowed []string) bool {
	list := allowed
	if len(list) == 0 {
		list = defaultArtifactDomains
	}
	return matchesExactFold(domain, list)
}

// matchesExactFold 判定 s 是否精确命中 list（大小写不敏感、去空白）。
func matchesExactFold(s string, list []string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, a := range list {
		if s == strings.ToLower(strings.TrimSpace(a)) {
			return true
		}
	}
	return false
}

// defaultArtifactExemptTemplates 是「即便域要求产物块，也不强制」的默认模板集（round 50 · T41）。
//
// 为什么要有代码层默认（而不是只靠 config 默认值）：`DefaultConfig()` 只对**新装**生效 ——
// 已在跑的 config.json（schema 6）里没有 `artifactExemptTemplates` 键 ⇒ 反序列化得到 nil ⇒
// 若 nil 当「无豁免」处理，T37 这条修正对**存量安装永不生效**（实测这台机器就是 schema 6）。
// 与 `ArtifactDomains` 的取舍一致：空值 ⇒ 用默认集（代价是无法表达「一个都不豁免」，已记录）。
var defaultArtifactExemptTemplates = []string{
	"code-review", "code-snippet-library", "code-test-plan", "quality-audit", "review-five-axis",
}

// artifactExempted 判定该模板是否被豁免产物块要求（零值/空 ⇒ 用默认集，见上）。
func artifactExempted(templateID string, exempt []string) bool {
	list := exempt
	if len(list) == 0 {
		list = defaultArtifactExemptTemplates
	}
	return matchesExactFold(templateID, list)
}

// requiresArtifactBlock 是 T37 的判定入口：**先模板豁免、后域** ——
// 豁免命中的模板哪怕域在工程集里也判「不要求产物」。
func requiresArtifactBlock(domain, templateID string, allowedDomains, exemptTemplates []string) bool {
	if artifactExempted(templateID, exemptTemplates) {
		return false
	}
	return artifactRequired(domain, allowedDomains)
}

// completionVerdict 判定一次「执行器成功返回」的产出该落哪个终态（round 46 · T28）。
//
// 三态口径：
//   - 报告为 nil **且该域要求产物** ⇒ `degraded`，理由「无产物块」（与「交了没过」区分，便于台账/面板一眼分辨）；
//   - 报告为 nil 且该域不要求产物（内容/报告类）⇒ `done`（正文即交付，D7 契约）；
//   - 报告 pass=false ⇒ `degraded`；报告 pass=true ⇒ `done`。
func completionVerdict(rep *model.VerifyReport, requireArtifact bool) (state model.TaskState, note string) {
	switch {
	case rep == nil && requireArtifact:
		return model.StateDegraded, "无产物块：模型未按产物协议交付，已降级"
	case rep != nil && !rep.Pass:
		return model.StateDegraded, "产出未通过验收，已降级"
	default:
		return model.StateDone, "执行成功"
	}
}

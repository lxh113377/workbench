package autonomy

import (
	"strings"
	"testing"
)

// TestAutonomyPromptsCarryDeliverableGate 钉住自主两条 prompt 都带交付物门禁（round 61 · T59）。
//
// 为什么钉它：自主 prompt 原先只要求「直接输出 Markdown 正文」，与执行器的产物协议互斥 ——
// 现场后果是听话按 prompt 的车道被判 no_artifact、听话按协议的车道交的自写脚本又跑不过。
// 门禁条款是这两侧唯一的交集，缺任何一条 prompt 都会退回旧形态。
func TestAutonomyPromptsCarryDeliverableGate(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 400})
	goal := &Goal{Intent: "提升工作空间项目的全栈工程质量"}

	ladder := p.buildLadderPrompt(goal, "对工作空间做一次五轴体检", PlanContext{StageIndex: 0})
	derived := p.enrichPrompt(goal, "补齐上一轮缺失的证据与门禁建议", 1, []string{"证据不足"})

	// 分级要求（round 69 对齐）：引擎侧的报告门禁会**拒绝未分级的报告**（T67，f1addb9 落地），
	// 若提示词不同步要求分级，自主报告必然被自己的门禁判红 —— 提示词与门禁必须同口径。
	// 断言**要求短语**而不是「分级」这个词：本轮负控实测，删掉整条要求后文里仍有「不分级的缺陷清单…」，
	// 只查词会让判据变哑 —— 判据必须钉住「要求本身在不在」。
	need := []string{"## 交付物门禁", "## 结论", "## 证据", "## 门禁建议", "docs/", "file:line", "报告必须**分级**", "P0 阻塞"}
	for name, prompt := range map[string]string{"buildLadderPrompt": ladder, "enrichPrompt": derived} {
		for _, k := range need {
			if !strings.Contains(prompt, k) {
				t.Fatalf("%s 的 prompt 缺少交付物门禁要素 %q", name, k)
			}
		}
		if strings.Contains(prompt, "直接输出可用的 Markdown 正文") {
			t.Fatalf("%s 仍在要求「直接输出 Markdown 正文」，与产物协议互斥", name)
		}
	}
}

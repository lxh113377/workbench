package blueprint

import (
	"strings"
	"testing"
)

// TestPlanPromptMinimalDelivery 钉住方案提示词的第 7 条「最小可验收交付」（round 41 · T17③）。
//
// 为什么：40 轮实测没有任何合规产物落地；方案若一上来就设计「大而全」，弱车道连第一步都交不出来。
// 第 7 条要求首轮优先「1 个文件 + 1 条本机可执行的验收命令」，让「能交出来」排在「交得漂亮」前面。
func TestPlanPromptMinimalDelivery(t *testing.T) {
	p := PlanPrompt("目标意图", "任务标题", "任务描述", 200, "")
	for _, want := range []string{"7. 首轮按", "最小可验收交付", "1 个文件", "1 条本机可执行的验收命令"} {
		if !strings.Contains(p, want) {
			t.Errorf("方案提示词缺少「最小可验收交付」要素：%q", want)
		}
	}
	// 顺序契约：第 6 条（禁自有语法）之后才是第 7 条；且第 1 条「只输出 JSON」仍在。
	if strings.Index(p, "7. 首轮按") < strings.Index(p, "6. 不要输出你自己的工具调用语法") {
		t.Error("第 7 条必须排在第 6 条之后")
	}
	if !strings.Contains(p, "1. 只输出一个 JSON 对象") {
		t.Error("第 1 条硬性要求（只输出 JSON）不得缺失")
	}
	// 体量参考必须原样透传（分档降载是在调用方算好的，提示词不得再改数字）。
	if !strings.Contains(p, "约 200 字") {
		t.Error("体量参考未按传入值渲染")
	}
}

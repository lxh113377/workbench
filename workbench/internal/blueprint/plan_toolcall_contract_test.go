package blueprint

import (
	"strings"
	"testing"
)

// TestPlanPromptForbidsOwnToolCalls 钉住方案提示词里的第 6 条硬性要求（T18④-2）。
//
// 由来（round 31 定向实验）：把方案模型钉到会吐字的车道后，失败形态仍是 `<|tool_call_start|>[bash(...)]`
// 这类**自有工具调用语法**（工作台不执行），于是反复落成「方案 JSON 解析失败」⇒ 任务退化直执行。
// 该条款必须写进方案提示词本身，而不是只在产出协议里（两个阶段是分开调用的）。
func TestPlanPromptForbidsOwnToolCalls(t *testing.T) {
	p := PlanPrompt("目标意图", "任务标题", "任务描述", 1000, "")
	for _, want := range []string{"6. 不要输出你自己的工具调用语法", "不会执行", "<|tool_call_start|>", "<dots_function_call>"} {
		if !strings.Contains(p, want) {
			t.Errorf("方案提示词缺少「禁止自有工具语法」要素：%q", want)
		}
	}
	// 别把「只输出一个 JSON 对象」这条根本要求挤掉或降级。
	if !strings.Contains(p, "1. 只输出一个 JSON 对象") {
		t.Error("第 1 条硬性要求（只输出 JSON）不得缺失")
	}
	// 顺序契约：第 5 条之后才是第 6 条（人读提示词时的编号连续性）。
	if strings.Index(p, "6. 不要输出你自己的工具调用语法") < strings.Index(p, "5. 禁止写【省略】") {
		t.Error("第 6 条必须排在第 5 条之后")
	}
}

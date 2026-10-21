package executor

// overlay_reasoning_test.go 守护「模型覆盖层不得给不支持的模型写 reasoningEffort」。
//
// 现场缺陷（2026-10-21，与「运行中但产出 0」同轮排查发现，且是**最后一层**阻塞）：
//   dist/workbench-output/tasks/<id>/events.jsonl 里，任务的第一轮 turn 立刻结束，
//   原因写得很清楚：
//     {"kind":"error","error":{"message":"provider \"our-free-model\" model
//      \"combo:auto-main\" does not support reasoning effort \"high\"",
//      "code":"UNSUPPORTED_REASONING_EFFORT"}}
//   然后引擎换模型重试，每个新模型又是同样一句话 —— 因为覆盖层**无条件**写死了
//   reasoningEffort: high。于是任务永远拿不到最终答案，直到重试次数耗尽。
//
// 为什么这是致命的：它不是"某个模型坏了"，而是**所有不支持该字段的模型全部不可用**。
// 现场模型池 52 个模型里有多个组合车道/免费车道不支持显式 reasoning effort，
// 而覆盖层把它们一律写死 —— 供给链看起来有 31 个可用模型，实际每个先被这一步拒。
//
// 判据：
//   1) 覆盖层默认**不写** reasoningEffort（让通道用模型自己的默认值）；
//   2) 需要显式指定时可传，但必须能被调用方控制（不是写死）；
//   3) 模型 ID 为空时必须有可用兜底（历史行为不能退化）。

import (
	"strings"
	"testing"
)

// TestModelOverlayDoesNotForceReasoningEffort 是现场缺陷的直接回归。
//
// 修复前该断言会红：覆盖层无条件含 "reasoningEffort: high"。
func TestModelOverlayDoesNotForceReasoningEffort(t *testing.T) {
	got := modelOverlay("combo:auto-main")
	if strings.Contains(got, "reasoningEffort") {
		t.Fatalf("覆盖层不得给模型强加 reasoningEffort（不支持该字段的模型会被直接拒绝，\n"+
			"现场表现即 'does not support reasoning effort \"high\"'）：\n%s", got)
	}
}

// TestModelOverlayCarriesModel 断言模型 ID 必须真的写进覆盖层。
func TestModelOverlayCarriesModel(t *testing.T) {
	got := modelOverlay("codearts/glm-5.3")
	if !strings.Contains(got, "codearts/glm-5.3") {
		t.Fatalf("覆盖层必须带上模型 ID：\n%s", got)
	}
	if !strings.Contains(got, "provider: our-free-model") {
		t.Fatalf("覆盖层必须指定 our-free-model provider：\n%s", got)
	}
	// 必须是合法 YAML 结构（能被 --patch 消费）：两行缩进键值。
	if !strings.Contains(got, "- id: agent-default-model") {
		t.Fatalf("覆盖层必须以 agent-default-model 条目开头：\n%s", got)
	}
}

// TestModelOverlayEmptyFallsBack 断言空模型 ID 有兜底（历史行为不得退化）。
func TestModelOverlayEmptyFallsBack(t *testing.T) {
	got := modelOverlay("   ")
	if strings.TrimSpace(got) == "" {
		t.Fatal("空模型 ID 时也必须产出可用覆盖层")
	}
	if !strings.Contains(got, "model: ") {
		t.Fatalf("空模型 ID 时必须回退到默认模型：\n%s", got)
	}
}

// TestModelOverlayWithExplicitEffort 断言"需要时才写"的能力仍在。
//
// 设计：默认不写；只有调用方显式要求时才追加该字段。这样既修掉现场缺陷，
// 又保留"想用高推理模型时可以要"的能力。
func TestModelOverlayWithExplicitEffort(t *testing.T) {
	got := modelOverlayWithEffort("codearts/glm-5.3", "high")
	if !strings.Contains(got, "reasoningEffort: high") {
		t.Fatalf("显式要求推理档位时必须写进去：\n%s", got)
	}
	if got := modelOverlayWithEffort("codearts/glm-5.3", ""); strings.Contains(got, "reasoningEffort") {
		t.Fatalf("未显式要求时不得写推理档位：\n%s", got)
	}
}

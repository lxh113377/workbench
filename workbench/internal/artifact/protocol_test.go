package artifact

import (
	"strings"
	"testing"
)

// TestProtocolInstructionRendersRealFences 是两层转义事故的回归测试。
//
// 现场（2026-10-19）：
//
//	第一版：围栏写成 Go 转义序列的双重转义 -> 模型读到字面量 "\\u0060"
//	第二版：只拼了单个反引号 -> 围栏变成单个反引号而不是三个
//
// 两次都导致协议说明整段不可用，模型根本没法照做，P3 因此形同虚设。
//
// 本测试校验渲染后出现的是真正的三个反引号。
func TestProtocolInstructionRendersRealFences(t *testing.T) {
	s := ProtocolInstruction
	fence := strings.Repeat("`", 3)

	if !strings.Contains(s, fence) {
		t.Fatalf("协议正文必须包含三个反引号围栏，实际渲染:\n%s", s)
	}
	// 必须出现至少 4 次围栏（manifest 起止 + file 起止）
	if n := strings.Count(s, fence); n < 4 {
		t.Fatalf("围栏出现 %d 次，至少要 4 次（manifest/file 各一对）:\n%s", n, s)
	}
	// 不允许残留转义序列
	if strings.Contains(s, "\\u0060") {
		t.Fatal("协议正文残留转义序列，模型会读到乱码")
	}
	// 关键标记必须齐全
	for _, want := range []string{"<<<ARTIFACTS>>>", "<<<END>>>", "manifest", "verify", "expectExit"} {
		if !strings.Contains(s, want) {
			t.Fatalf("协议正文缺少关键标记 %q", want)
		}
	}
}

// TestProtocolInstructionFenceLines 保证围栏行本身正确（不是同行粘连）。
func TestProtocolInstructionFenceLines(t *testing.T) {
	s := ProtocolInstruction
	fence := strings.Repeat("`", 3)
	lines := strings.Split(s, "\n")
	foundManifest := false
	foundFile := false
	for _, l := range lines {
		if l == fence+"manifest" {
			foundManifest = true
		}
		if strings.HasPrefix(l, fence+"file:") {
			foundFile = true
		}
	}
	if !foundManifest {
		t.Fatal("缺少 manifest 围栏行")
	}
	if !foundFile {
		t.Fatal("缺少 file: 围栏行")
	}
}

// TestProtocolInstructionForbidsOwnToolCalls 钉住「不许吐自有工具调用语法」这条硬性要求（T18④）。
//
// 由来（round 24 live 实测）：会吐字的车道（dots-studio / kilo-auto / liquid）都在输出**自己的**工具语法 ——
// <dots_function_call><exec>…、<|tool_call_start|>[ls(path=…)]<|tool_call_end|> —— 工作台不执行它们，
// 于是整个回答被判 no_artifact/未完成（连续 3 条车道因此被拒）。协议文本必须把这条写死，否则模型照旧习惯输出。
func TestProtocolInstructionForbidsOwnToolCalls(t *testing.T) {
	s := ProtocolInstruction
	for _, want := range []string{"不要输出你自己的工具调用语法", "不会执行", "<dots_function_call>", "<|tool_call_start|>"} {
		if !strings.Contains(s, want) {
			t.Errorf("协议文本缺少「禁止自有工具调用语法」要素：%q", want)
		}
	}
	// 该要求必须是硬性要求清单里的一条（编号 6），而不是散落的建议句。
	if !strings.Contains(s, "6. 不要输出你自己的工具调用语法") {
		t.Error("禁止自有工具调用语法应作为第 6 条硬性要求出现")
	}
}

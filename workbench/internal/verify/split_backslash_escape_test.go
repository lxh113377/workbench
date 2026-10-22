package verify

import "testing"

// TestSplitCommandBackslashEscape 复现 docs/缺陷-验收命令反反杠污染-2026-10-19.md。
//
// 缺陷：模型的 manifest cmd 按 JSON 规则本应写 \"，若写成 \\"（过度转义），
// 反转义后就剩下一个字面 \ 紧跟引号。旧 splitCommand 的引号状态机不识别 \"，
// 于是 \ 残留进 argv，pwsh 收到 `\if ...` 报「术语不识别」—— 该任务的真实验收
// **永远不可能通过**，反复返工直到 repairRounds 耗尽，
// 并把「模型转义问题」误报成「产物不可运行」。
//
// 该测试 2026-10-19 写成后放在 tests/regression/（**不在任何模块内**），从未被 go test 执行过；
// 2026-10-23 移到本包后首次运行即 FAIL（证明它此前的「通过」是假的）。
//
// 期望值修正（2026-10-23）：原期望 argv[3] 完全不含反斜杠，那是**错误的** ——
// 输入 `\\"` 的语义是「第一个 \ 转义第二个 \ => 字面 \」，随后 " 开启引号。
// 故 argv[3] 应以一个字面 \ 开头、以一个  结尾。真正要防的缺陷是
// 「引号状态机错乱导致 token 被切碎」，不是「反斜杠必须消失」。
func TestSplitCommandBackslashEscape(t *testing.T) {
	cmd := `pwsh -NoProfile -Command \\"if (Test-Path a.md) { exit 0 } else { exit 1 }\\"`
	argv, err := splitCommand(cmd)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(argv) != 4 {
		t.Fatalf("argv 长度 %d != 4（引号状态机错乱把脚本切碎了）: %q", len(argv), argv)
	}
	// \" 语义：第一个 \ 转义第二个 \ => 字面 \；随后 " 开启引号。
	want := "\\if (Test-Path a.md) { exit 0 } else { exit 1 }\\"
	if argv[3] != want {
		t.Errorf("argv[3] = %q, want %q", argv[3], want)
	}
}

// TestSplitCommandEscapedQuoteInsideQuotes 才是本缺陷的**最小真复现**：
// 引号**已开启**后遇到 \" —— 旧实现在引号内不识别转义，把 \ 当普通字符、
// 把 " 当成闭合引号，于是脚本从该处被切成两半。
//
// 2026-10-23 实现首轮曾写错判据（在引号**外**测 \"）：引号外的 \" 转义后
// 本就不该开启引号，空格分割是**正确**行为，那样写会造出一个假失败。
// 真缺陷只在引号内出现，故本测试从引号内构造。
func TestSplitCommandEscapedQuoteInsideQuotes(t *testing.T) {
	// 外层双引号包裹整段脚本；脚本内含 \"（转义的引号）。
	cmd := `pwsh -NoProfile -Command "say \"hi\" done"`
	argv, err := splitCommand(cmd)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(argv) != 4 {
		t.Fatalf("argv 长度 %d != 4（引号内的 \" 把脚本切碎了）: %q", len(argv), argv)
	}
	// \" 在引号内 => 字面 "（不闭合引号）
	want := `say "hi" done`
	if argv[3] != want {
		t.Errorf("argv[3] = %q, want %q（引号内转义未被处理）", argv[3], want)
	}
}

package artifact

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// runeIndexOf 返回 sub 在 s 中的**字符**（rune）下标；找不到返回 -1。
//
// 为什么不用 strings.Index：协议正文含大量中文，字节下标会把同一段前缀
// 凭空放大约 2-3 倍，用字节下标去卡「前 200 字符」会得出错误结论。
func runeIndexOf(s, sub string) int {
	b := strings.Index(s, sub)
	if b < 0 {
		return -1
	}
	return utf8.RuneCountInString(s[:b])
}

func headRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

func tailRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[len(rs)-n:])
}

// TestProtocolInstructionMinimalShapeComesFirst 是 T39 判据①：最小可交付形态
// 必须落在**第一屏**，且 <<<ARTIFACTS>>> 至少出现两次（前置 + 结尾双保险）。
//
// 背景（2026-10-27 live）：弱车道 final 文本 0/41 含 <<<ARTIFACTS>>>；唯一被
// 证明有效的措辞是 engine/repair.go 返工提示词里的「必须以 <<<ARTIFACTS>>> 开头」。
// T39 把同样的话术前置到首轮协议 —— 只读第一屏也必须看到产物块长什么样。
//
// 有牙性（负向对照，实测）：把这段前置搬回标题 -> 散文 -> 示例的原顺序后，
// firstARTIFACTS 从字符下标 32 跌到 122（越界 80）、firstEND 从 189 跌到 325
// （跌出第一屏）、计数从 4 降到 1 —— 本测四条断言同时判红。
func TestProtocolInstructionMinimalShapeComesFirst(t *testing.T) {
	s := ProtocolInstruction

	// 留证：协议长度与三要素的实测字符下标（-v 可见，失败时也会打印）。
	t.Logf("runeLen=%d firstARTIFACTS=%d firstEND=%d artifactsCount=%d",
		utf8.RuneCountInString(s),
		runeIndexOf(s, "<<<ARTIFACTS>>>"),
		runeIndexOf(s, "<<<END>>>"),
		strings.Count(s, "<<<ARTIFACTS>>>"))

	// ① 第一屏（前 200 字符）必须同时出现三要素：块标记、manifest 示例、块结束。
	head := headRunes(s, 200)
	for _, want := range []string{"<<<ARTIFACTS>>>", "manifest", "<<<END>>>"} {
		if !strings.Contains(head, want) {
			t.Fatalf("第一屏（前 200 字符）缺少最小可交付形态要素 %q；当前第一屏=%s", want, head)
		}
	}

	// 块标记必须足够靠前（不能等到第二屏才出现）。
	if i := runeIndexOf(s, "<<<ARTIFACTS>>>"); i < 0 || i > 80 {
		t.Fatalf("首个 <<<ARTIFACTS>>> 的字符下标为 %d，要求 0 <= i <= 80", i)
	}

	// ③ <<<END>>> 也必须落在第一屏内。
	if i := runeIndexOf(s, "<<<END>>>"); i < 0 || i >= 200 {
		t.Fatalf("首个 <<<END>>> 的字符下标为 %d，要求落在第一屏（< 200）内", i)
	}

	// ② 至少两次：首屏一次 + 结尾一次。
	if n := strings.Count(s, "<<<ARTIFACTS>>>"); n < 2 {
		t.Fatalf("<<<ARTIFACTS>>> 出现 %d 次，要求 >= 2（前置 + 结尾各一次）", n)
	}

}

// TestProtocolInstructionEndsWithReminder 是 T39 判据②：结尾必须再点一次
// 「没有 <<<ARTIFACTS>>> 块 = 没有交付」，让只扫尾部的弱车道也被拦住。
func TestProtocolInstructionEndsWithReminder(t *testing.T) {
	s := ProtocolInstruction
	const tail = 120

	if !strings.Contains(tailRunes(s, tail), "没有 <<<ARTIFACTS>>> 块 = 没有交付") {
		t.Fatalf("协议结尾（最后 %d 字符）缺少「没有 <<<ARTIFACTS>>> 块 = 没有交付」提醒；实际结尾=%s", tail, tailRunes(s, tail))
	}

	// 最后一次块标记必须真的在尾部，而不是中途某处碰巧命中。
	rs := []rune(s)
	last := -1
	marker := []rune("<<<ARTIFACTS>>>")
	for i := 0; i+len(marker) <= len(rs); i++ {
		if string(rs[i:i+len(marker)]) == string(marker) {
			last = i
		}
	}
	if last < len(rs)-tail {
		t.Fatalf("最后一次 <<<ARTIFACTS>>> 在字符下标 %d，未落在结尾 %d 字符内", last, tail)
	}
}

// TestProtocolInstructionKeepsHardRequirementsAndFences 是 T39 判据③：
// 重排不得丢原有 6 条硬性要求，也不得把 FENCE 拼坏（历史两层转义事故的守门）。
func TestProtocolInstructionKeepsHardRequirementsAndFences(t *testing.T) {
	s := ProtocolInstruction

	for _, want := range []string{
		"硬性要求：",
		"1. 文件内容必须完整可运行",
		"2. 路径只能是相对路径",
		"3. verify 至少 1 条",
		"4. 若不产出代码",
		"5. 单个文件不超过 2MB",
		"6. 不要输出你自己的工具调用语法",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("重排后丢了原有硬性要求 %q", want)
		}
	}

	// FENCE 拼接后不得出现残缺围栏：任何反引号连续段都必须恰好 3 个。
	fenceRe := regexp.MustCompile("`+")
	for _, m := range fenceRe.FindAllString(s, -1) {
		if m != FENCE {
			t.Fatalf("出现残缺围栏 %q（要求恰好 3 个反引号）", m)
		}
	}
	// 围栏必须成对（manifest / file 各一对），总数为偶数且 >= 4。
	if n := strings.Count(s, FENCE); n < 4 || n%2 != 0 {
		t.Fatalf("围栏出现 %d 次，要求 >= 4 且为偶数（起止成对）", n)
	}
	// 围栏行本身必须可解析（manifest / file）。
	if !strings.Contains(s, FENCE+"manifest") || !strings.Contains(s, FENCE+"file:") {
		t.Fatal("缺少 manifest 或 file: 围栏行，解析器读不懂")
	}
}

package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestLongUnbrokenStringsWrap 钉住「长串内容不得撑破容器」。
//
// 一手缺陷（2026-10-22 极端内容实测，CDP 注入构造值）：
// 产出里常出现 URL / 哈希 / 代码标识符这类**无空格长串**。修前实测：
//
//	.doc 里 600 个无空格 ASCII -> clientW 898 但 scrollW 5831（溢出 6.5 倍）
//	pre 里 800 个无换行字符   -> scrollW 5657
//	表格 300 字单元格        -> 表格被撑到 2334px（容器只有 900px）
//	整页横向溢出 docOverflowX = 979px
//
// 中文靠 CJK 原生断行不受影响（同用例中文 scrollW 正常），只有 ASCII 长串需要显式规则。
// 修后四项全部归位（898/898/900，docOverflowX=-10 即无溢出）。
func TestLongUnbrokenStringsWrap(t *testing.T) {
	// 抽出 .doc / pre / td 三条选择器的声明块，逐条断言断行能力
	cases := []struct{ name, sel string }{
		{".doc 产出面板", ".doc{"},
		{"pre/logbox 代码块", "pre,.logbox{"},
		{"td 表格单元格", "th,td{overflow-wrap:anywhere;word-break:break-word}"},
	}
	for _, c := range cases {
		i := strings.Index(appCSS, c.sel)
		if i < 0 {
			t.Errorf("找不到 %s 的样式块（%q）", c.name, c.sel)
			continue
		}
		block := appCSS[i:]
		if e := strings.Index(block, "}"); e > 0 {
			block = block[:e]
		}
		if !strings.Contains(block, "overflow-wrap:anywhere") {
			t.Errorf("%s 缺少 overflow-wrap:anywhere —— 无空格长串会把容器撑破", c.name)
		}
	}
}

// TestPreMustBePreWrapToWrap 钉住「pre 必须先 pre-wrap 才能断行」。
//
// 本轮实测的一个**反直觉点**：只给 <pre> 加 overflow-wrap/word-break **无效** ——
// pre 默认 white-space:pre 根本不换行。第一版修复只加了 break 规则，
// 复测 scrollW 仍是 5657（分毫未动）；补上 white-space:pre-wrap 才真正生效。
// 判据分别断言「重新生效」与「边距保持」两个约束。
func TestPreMustBePreWrapToWrap(t *testing.T) {
	i := strings.Index(appCSS, "pre,.logbox{")
	if i < 0 {
		t.Fatal("找不到 pre 样式块")
	}
	block := appCSS[i:]
	if e := strings.Index(block, "}"); e > 0 {
		block = block[:e]
	}
	if !strings.Contains(block, "white-space:pre-wrap") {
		t.Error("pre 缺 white-space:pre-wrap —— 有 break 规则也不会换行（实测 scrollW 不变）")
	}
	if !strings.Contains(block, "overflow-wrap:anywhere") {
		t.Error("pre 缺 overflow-wrap:anywhere")
	}
	// 反向保险：不得退化成 pre-line / nowrap（会破坏代码缩进语义）
	if strings.Contains(block, "white-space:pre-line") || strings.Contains(block, "white-space:nowrap") {
		t.Error("pre 的 white-space 退化为 pre-line/nowrap —— 代码缩进语义会坏")
	}
}

// TestNoGlobalHorizontalOverflowRule 兜底：断言页面根级不靠 overflow-x:hidden 掩盖问题。
//
// 遮住溢出是最常见也最坏的「修法」：视觉上看不出，内容实际被裁掉不可达。
// 本轮修的是让内容真的能断行，而不是加 overflow-x:hidden 把它藏起来。
func TestNoGlobalHorizontalOverflowRule(t *testing.T) {
	re := regexp.MustCompile("(?m)^\\s*(html|body)\\s*\\{[^}]*overflow-x:\\s*hidden")
	if re.MatchString(appCSS) {
		t.Error("html/body 上出现 overflow-x:hidden —— 会把横向溢出藏起来而不是修好它")
	}
}

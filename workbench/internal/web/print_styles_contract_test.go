package web

import (
	"strings"
	"testing"
)

// TestPrintStylesheetExists 钉住「全站必须有 @media print 规则」。
//
// 一手实测（2026-10-22 CDP 打印介质）：此前全站**零** print 规则 ——
// 打印预览里 body 仍是 rgb(4,7,13) 深底 + 极光渐变 + 顶栏 + 按钮残骸，
// 纸上是满页黑块、完全不可用。而界面本身有「打印自检报告」按钮，用户真的会按 Ctrl+P。
// 一条 print 规则都没有 ⇒ 这个场景从来没被设计过。
func TestPrintStylesheetExists(t *testing.T) {
	if !strings.Contains(appCSS, "@media print") {
		t.Fatal("CSS 缺少 @media print：打印预览会印出深色背景与导航残骸（实测满页黑块）")
	}
}

// TestPrintStylesForceLightSurface 钉住打印版的「白纸黑字」关键约束。
// 逐条断言的是**打印版必须成立的四件事**，任何一条丢了都会让纸质件不可用：
//
//	① 背景强制白色（否则省墨模式也会印出黑块/灰底）；
//	② 文字强制深色（深底浅字在纸上等于白字，什么都看不见）；
//	③ 导航与操作件隐去（打印不点按钮，顶栏/页签/工具栏只会浪费纸）；
//	④ 折叠块强制展开（纸质件无法点击展开，未展开的 details 等于内容丢失）。
func TestPrintStylesForceLightSurface(t *testing.T) {
	start := strings.Index(appCSS, "@media print")
	if start < 0 {
		t.Fatal("没有 @media print 块")
	}
	block := appCSS[start:]
	if end := strings.Index(block, "\n}"); end > 0 {
		block = block[:end]
	}
	checks := []struct{ name, needle string }{
		{"背景强制白色", "background:#fff !important;color:#111 !important;"},
		{"文字强制深色", "color:#111 !important"},
		{"隐去顶栏", ".topbar-shell"},
		{"隐去页签", ".tabs"},
		{"隐去按钮", ".btn"},
		{"隐去工具栏", ".toolbar"},
		{"折叠块强制展开", "details:not([open]) > .fold-body{display:block !important}"},
		{"长表格不截断", ".table-scroll{max-height:none !important"},
		{"关掉装饰伪元素", "body.home::before"},
	}
	for _, c := range checks {
		if !strings.Contains(block, c.needle) {
			t.Errorf("打印样式缺少「%s」（%q）", c.name, c.needle)
		}
	}
}

// TestPrintBlockHidesNotDuplicates 反向保险：打印块里不得出现 display:block 式的
// 「把导航显示出来」写法 —— 曾见过有人为了「打印时也能导航」把顶栏强制 display:block，
// 结果每页都多印一条无用的黑条。
func TestPrintBlockHidesNotDuplicates(t *testing.T) {
	start := strings.Index(appCSS, "@media print")
	if start < 0 {
		t.Skip("无 print 块")
	}
	block := appCSS[start:]
	if end := strings.Index(block, "\n}"); end > 0 {
		block = block[:end]
	}
	// 反向：print 块内不得出现深色背景（白纸黑字是硬约束，改回深底即判红）。
	for _, badBg := range []string{"background:#000", "background:#04070d", "background:var(--bg)", "background:var(--panel)"} {
		if strings.Contains(block, badBg) {
			t.Errorf("打印块出现深色背景 %q —— 打印到纸上会是黑块", badBg)
		}
	}
	for _, bad := range []string{".topbar-shell{display:block", ".tabs{display:flex", ".btn{display:inline-flex"} {
		if strings.Contains(block, bad) {
			t.Errorf("打印块把导航/按钮显示出来了：%q（打印稿不该有操作面）", bad)
		}
	}
}

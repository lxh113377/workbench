package web

import (
	"strings"
	"testing"
)

// TestAppJSEscCoversAllDangerousChars 钉住 esc() 转义函数对五个危险字符的全覆盖。
//
// 为什么需要（2026-10-24 round 91）：
//
//	appJS 有 18 处 innerHTML 赋值，逐个读代码确认当前全部用户可控数据都过了 esc()，
//	代码质量是好的。但这条 XSS 防线只有一个根基函数 esc() —— 它若被改坏
//	（少一个字符、正则写漏、映射表缺项），18 处全部同时失守，而没有任何测试守着它。
//
//	本测试第一版想判「innerHTML 拼接里的裸标识符」，实测 15 处全是误报：
//	head/body 这类局部变量是纯字面量或内部已 esc 的产物（rowOf 内部转义），
//	静态分析穿透不了变量数据流。这正是「判据形态选错」的典型案例 ——
//	不是调参能救的，故换判据：守 esc() 本身（可证伪、零误报、且是真正的单点根基）。
//
// 判据：
//  1. esc 的定义存在；
//  2. 其 replace 正则覆盖 & < > 双引号 单引号 五个字符；
//  3. ESC 映射表对五个字符都有对应实体；
//  4. esc 对 null/undefined 不炸；
//  5. esc 确实被大量使用（不是死码）。
func TestAppJSEscCoversAllDangerousChars(t *testing.T) {
	js := appJS
	if js == "" {
		t.Fatal("appJS 为空 —— 判据失去受理面")
	}
	constIdx := strings.Index(js, "const esc =")
	if constIdx < 0 {
		t.Fatal("找不到 esc 定义 —— XSS 防线的根基函数被改名或删除？18 处 innerHTML 会同时失守")
	}
	// 【round 92 修漏检】首版取 esc 定义后 300 字节窗口查「包含某字符」—— 窗口里
	// 的 '' : s 兜底表达式自带单引号，导致「含 '」恒真，变异（删掉正则里的 '）漏检。
	// 修法：精确抽出 .replace(/.../ 的正则字面量，只在那里面查。
	escDef := js[constIdx : constIdx+300]
	reIdx := strings.Index(escDef, ".replace(/")
	if reIdx < 0 {
		t.Fatal("esc() 里找不到 .replace(/.../ —— 转义实现形态变了，判据要跟着更新")
	}
	reStart := reIdx + len(".replace(/)") - 1 // 指向正则的起始 /
	reEnd := strings.Index(escDef[reStart+1:], "/")
	if reEnd < 0 {
		t.Fatal("esc() 的 replace 正则没有闭合 / —— 实现形态变了")
	}
	reLit := escDef[reStart+1 : reStart+1+reEnd]
	chars := []string{"&", "<", ">", "\"", "'"}
	for _, c := range chars {
		if !strings.Contains(reLit, c) {
			t.Errorf("esc() 的 replace 正则 %q 缺少字符 %q —— 该字符不过转义，innerHTML 注入可直接用", reLit, c)
		}
	}
	escIdx := strings.Index(js, "const ESC =")
	if escIdx < 0 {
		t.Fatal("找不到 ESC 映射表")
	}
	escMap := js[escIdx : escIdx+300]
	ents := []string{"&amp;", "&lt;", "&gt;", "&quot;", "&#39;"}
	for _, e := range ents {
		if !strings.Contains(escMap, e) {
			t.Errorf("ESC 映射表缺少实体 %s", e)
		}
	}
	if !strings.Contains(escDef, "== null") {
		t.Error("esc() 缺 null/undefined 兜底 —— 空值会转成 null 字样进 UI")
	}
	uses := strings.Count(js, "esc(")
	if uses < 5 {
		t.Errorf("esc( 只用了 %d 处 —— 要么大量 innerHTML 没转义，要么 esc 已是死码", uses)
	}
	t.Logf("esc() 契约合规：五字符全覆盖 + 五实体齐全 + null 兜底；全脚本 esc( 使用 %d 处", uses)
}

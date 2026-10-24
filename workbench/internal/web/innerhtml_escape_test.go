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
	// 【round 109 扩覆盖面】本测试原本只测 appJS 一份 —— 这正是 poolJS 的
	// esc 漏抄单引号映射却无人发现的原因（判据逻辑是对的，跑错了对象）。
	// 现改为三份各跑：helper 在三份里是**独立副本**（内嵌脚本不共享顶层 const），
	// 任何一份的 esc 坏掉，它那份 innerHTML 就同时失守。
	for _, b := range []struct {
		name string
		js   string
	}{{"appJS", appJS}, {"govJS", govJS}, {"poolJS", poolJS}} {
		t.Run(b.name, func(t *testing.T) { checkEscContract(t, b.name, b.js) })
	}
}

func checkEscContract(t *testing.T, blobName, js string) {
	if js == "" {
		t.Fatal("内嵌脚本为空 —— 判据失去受理面")
	}
	constIdx := strings.Index(js, "const esc =")
	if constIdx < 0 {
		t.Fatal("找不到 esc 定义 —— XSS 防线的根基函数被改名或删除？该页所有 innerHTML 会同时失守")
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
	// 【round 109 形态无关化】原本要求「必须有 const ESC 表」，但三份实现形态不同：
	//   appJS / poolJS 用 const ESC 映射表；govJS 把映射**内联**在回调里（无 const ESC）。
	//   那是风格差异不是缺陷 —— 原判据会对 govJS 误报（实测撞上 t.Fatal）。
	// 改为查「五实体在整份脚本里各至少出现一次」：三份实测每份都恰好 1 次（只存在于 esc 相关处），
	// 而本轮抓到的缺陷正是 poolJS 缺 &#39;（计数 0）—— 既不误报又能判出真漏抄。
	ents := []string{"&amp;", "&lt;", "&gt;", "&quot;", "&#39;"}
	for _, e := range ents {
		if !strings.Contains(js, e) {
			t.Errorf("%s 的 esc 缺实体 %s —— 对应字符不被转义（映射内联或表缺项都算），innerHTML 注入可直接用", blobName, e)
		}
	}
	if !strings.Contains(escDef, "== null") {
		t.Error("esc() 缺 null/undefined 兜底 —— 空值会转成 null 字样进 UI")
	}
	uses := strings.Count(js, "esc(")
	if uses < 5 {
		t.Errorf("esc( 只用了 %d 处 —— 要么大量 innerHTML 没转义，要么 esc 已是死码", uses)
	}
	// 【round 109 新增】禁静默透传：兜底只能是「映射缺失即出错」，不能原样返回。
	// 实测病灶：poolJS 曾是 c => ESC[c] || c，且漏抄单引号映射 ——
	// 正则声称处理 ' ，映射却不处理，|| c 把它变成**静默原样透传**（比报错更难发现）。
	// 另两份都是 c => ESC[c]（缺映射会产出 "undefined" 字样，走查立刻可见）。
	if strings.Contains(js, "ESC[c] || c") {
		t.Errorf("%s 的 esc 用 `ESC[c] || c` 静默透传兜底 —— 映射缺项时会原样输出危险字符且不报错", blobName)
	}
	t.Logf("%s: esc() 契约合规（五字符全覆盖 + 无静默透传；全脚本 esc( 使用 %d 处）", blobName, uses)
}

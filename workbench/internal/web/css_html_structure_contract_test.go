package web

import (
	"strings"
	"testing"
)

// cssBraceScan 单次遍历 CSS，统计真正参与结构的花括号：开数、闭数，以及过程中的最深负值。
// 字符串字面量（单/双引号，含反斜杠转义）与 /* */ 注释里的括号一律不计。
//
// 为什么不直接 strings.Count 数个数：当前 appCSS 的 474 对花括号里，引号串与注释**恰好都不含**
// 花括号（实测：45 个引号串 0 处含、153 个注释 0 处含），所以朴素计数今天结果正确。
// 但 CSS 有 content:'}' 这种合法写法 —— 将来有人写 content:'{' 或注释里带括号，朴素计数就会
// **假红**，而假红的代价是门禁被人关掉（本仓 round 64/116 在册教训）。
//
// 用数值 rune 常量（123/125/39/34/47/42/92）而非字符字面量：本仓多层字符串改写会吃掉反斜杠，
// 这是反复踩过的坑，宁可写成数字也不写转义。
func cssBraceScan(src string) (open int, close int, minDepth int) {
	const (
		lbrace = rune(123)
		rbrace = rune(125)
		quote1 = rune(39)
		quote2 = rune(34)
		slash  = rune(47)
		star   = rune(42)
		ashes  = rune(92)
	)
	var strQ rune
	inComment := false
	escaped := false
	depth := 0
	rs := []rune(src)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if inComment {
			if r == slash && i > 0 && rs[i-1] == star {
				inComment = false
			}
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if strQ != 0 {
			switch r {
			case ashes:
				escaped = true
			case strQ:
				strQ = 0
			}
			continue
		}
		switch r {
		case quote1, quote2:
			strQ = r
		case slash:
			if i+1 < len(rs) && rs[i+1] == star {
				inComment = true
				i++
			}
		case lbrace:
			open++
			depth++
		case rbrace:
			close++
			depth--
			if depth < minDepth {
				minDepth = depth
			}
		}
	}
	return open, close, minDepth
}

// tagPair 数一个 HTML 标签的开/闭。开标签算两种形态：<tag 后跟空格 或 后跟尖括号，
// 因为本仓页面里 <body class=...> 与 <head> 同时存在；只数 "<tag" 会把 <header 也算进去。
func tagPair(s, tag string) (int, int) {
	return strings.Count(s, "<"+tag+" ") + strings.Count(s, "<"+tag+">"), strings.Count(s, "</"+tag+">")
}

const (
	sQuote = string(rune(39))
	dQuote = string(rune(34))
	bslash = string(rune(92))
)

// TestCSSBraceScannerIsSound 先验证扫描器本身，再让它去判生产代码。
// 判据若比被测对象更笨就会制造假红（本仓「判据本身误报」家族的定规）。
// 每个非平凡用例都额外反证：若朴素计数结果与本扫描器相同，说明该用例没有区分力，
// 等于用「恰好正确」的朴素实现冒充被测逻辑过关。
func TestCSSBraceScannerIsSound(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantOpen  int
		wantClose int
		wantMin   int
		hasEdge   bool // true = 含「朴素计数会算错」的干扰括号，参与反证
	}{
		{"基线：无干扰括号", ".a{color:red}.b{top:0}", 2, 2, 0, false},
		{"单引号串里的闭括号不算结构", ".a{content:" + sQuote + "}" + sQuote + "}", 1, 1, 0, true},
		{"双引号同理", ".a{content:" + dQuote + "}" + dQuote + "}", 1, 1, 0, true},
		{"注释里的括号不算结构", ".a{/* 注释 } 里有括号 */color:red}", 1, 1, 0, true},
		{"转义引号不提前结束字符串", ".a{content:" + sQuote + "x" + bslash + sQuote + "y}" + sQuote + ";}", 1, 1, 0, true},
		{"开头就出现闭括号则深度为负", ".a}", 0, 1, -1, false},
	}
	for _, c := range cases {
		o, cl, min := cssBraceScan(c.src)
		if o != c.wantOpen || cl != c.wantClose || min != c.wantMin {
			t.Errorf("%s: 计到 %d/%d/深度%d，期望 %d/%d/深度%d（源:%q）", c.name, o, cl, min, c.wantOpen, c.wantClose, c.wantMin, c.src)
		}
		if !c.hasEdge {
			continue
			// 反证只适用于含干扰括号的用例：minDepth 类用例不含干扰括号，
			// 朴素计数与扫描器的开闭数必然相同，套反证就是我自己写错用例（本轮真发生了）。
		}
		no, nc := strings.Count(c.src, "{"), strings.Count(c.src, "}")
		if no == o && nc == cl {
			t.Errorf("%s: 朴素计数结果也是 %d/%d —— 该用例没区分力（换不到任何保护）", c.name, no, nc)
		}
	}
}

// TestEmbeddedCSSAndHTMLAreWellFormed 守住内嵌 CSS / HTML 的结构完整性。
//
// 为什么必须有（2026-10-25 空白取证，临时 worktree 跑完即删）：与 JS 同族 —— 内嵌 CSS/HTML
// 也是 Go raw string，括号或标签写坏时 go build / go vet / go test 全绿、CI 全绿，只有浏览器
// 打开才看得出来。实测：删掉 appCSS 末尾一个右花括号（第 1404 行，刻意避开任何测试的字符串
// 锚点），web 包 rc=0 **全绿** —— 16 个测试函数（分布在 9 个文件、26 处引用）全是「某个类名在不在」的文本断言，
// 没有一个校验结构。CSS 少一个 } 会把它后面的规则整段吞进上一条，表现是「部分样式静默失效」，
// 比整页白屏更难发现。
//
// 判据取编译后的常量值本身（不解析 Go 源码），与 quality-audit 要求 ⑤ 同规。
//
// 明确不纳入的两项（都因实测存在歧义，写死必假红）：
//   - style 标签配平：modelPoolPage 实测 style=2/1，第二个 "<style" 出现在**注释文字**里
//     （"池专项规则必须写在页内 <style>，"）—— 纯文本计数分不开注释与真标签，故不判。
//   - CSS 嵌套深度是否归零：@media 合法地让深度在不同层级收口，这里只判「开闭总数相等」
//     与「不出现负深度」；判树形结构需要一个 CSS 解析器，收益不匹配。
func TestEmbeddedCSSAndHTMLAreWellFormed(t *testing.T) {
	t.Run("appCSS 花括号配平", func(t *testing.T) {
		// 受理面哨兵：常量若因声明改写变成空串，配平依然成立（0==0）而判据在比空气。
		// 下限取首次实测字节值（78184）的三成量级、向下取整到千位 —— 是宽松近似，
		// 目的是抓「整段丢失」这种数量级差异；抬高阈值只会增加假红风险。
		if len(appCSS) < 23000 {
			t.Fatalf("appCSS 只有 %d 字节，远低于登记基线 78184 —— 判据此刻比的是空或残内容", len(appCSS))
		}
		open, close, minDepth := cssBraceScan(appCSS)
		if open == 0 {
			t.Fatal("appCSS 一条规则都没有 —— 常量本身有问题，判据空转")
		}
		if open != close {
			t.Errorf("appCSS 结构花括号不配平：%d 个开、%d 个闭 —— 少一个 } 会把它后面的规则整段吞进上一条，"+
				"表现为部分样式静默失效", open, close)
		}
		if minDepth < 0 {
			t.Errorf("appCSS 出现负深度（最深 %d）—— 有闭括号没有对应开括号，通常是常量开头被截断", minDepth)
		}
		t.Logf("appCSS 结构花括号 %d/%d 配平（字节 %d）", open, close, len(appCSS))
	})

	for _, pg := range []struct {
		name string
		src  string
	}{
		{"indexPage", indexPage},
		{"adminPage", adminPage},
		{"governancePage", governancePage},
		{"modelPoolPage", modelPoolPage},
	} {
		t.Run(pg.name+" 标签配平", func(t *testing.T) {
			// 下面每条断言本身就足以抓「整段丢失」（空串会让计数全为 0），
			// 故不再设字节下限 —— 上一轮我把 JS 字符数当字节数写过阈值，教训在此。
			if n := strings.Count(pg.src, "<!DOCTYPE html>"); n != 1 {
				t.Errorf("%s 的 DOCTYPE 出现 %d 次（应为 1）—— 缺失会让浏览器进怪异模式，样式整体偏移", pg.name, n)
			}
			for _, tag := range []string{"html", "head", "body", "script"} {
				open, close := tagPair(pg.src, tag)
				if open == 0 {
					t.Errorf("%s 完全没有 <%s> 标签 —— 页面结构缺失或常量被截断，判据在比空气", pg.name, tag)
					continue
				}
				if open != close {
					t.Errorf("%s 的 <%s> 不配平：%d 开 / %d 闭", pg.name, tag, open, close)
				}
			}
		})
	}
}

package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestInlineEditorsAreKeyboardDismissible 钉住「内联编辑器必须能用 Escape 关闭且焦点回退」。
//
// 一手缺口（2026-10-22 前端走查实测）：池编辑器与模板编辑器此前只能靠鼠标点「取消」/「保存」
// 离开 —— 池编辑器有取消按钮但无 Escape 绑定，模板编辑器连取消按钮都没有。
// 键盘用户进得去出不来：Escape 是「可关闭」的通用约定，焦点回退是它的成对要求
// （否则关闭后焦点掉到 body，Tab 要从头找起）。
//
// 判据取三个可核事实（不依赖渲染）：① 两处编辑器各有 Escape 绑定；② 各有回焦记录；
// ③ 各有一条显式退出通道（取消按钮或关闭方法）。
func TestInlineEditorsAreKeyboardDismissible(t *testing.T) {
	cases := []struct {
		name string
		src  string
		need []string
	}{
		{
			name: "模型池内联编辑器",
			src:  poolJS,
			need: []string{
				"editorReturnFocus = document.activeElement", // 断言赋值本身：标识符在声明与关闭函数里都出现，
				// 只查标识符会让「摘掉记录触发元素那一步」静默通过（变异 M-C 实测漏判）
				"document.addEventListener('keydown'", // 只挂一次的文档级绑定
				"ev.key !== 'Escape'",                 // 键判据
				"closePoolEditor",                     // 关闭动作（含 renderAdd 重绘）
				"btnCancel",                           // 可见的鼠标退出通道
			},
		},
		{
			name: "模板内联编辑器",
			src:  appJS,
			need: []string{
				"box._returnFocus = document.activeElement", // 同上：断言赋值本身（新建/编辑两处）
				"closeTemplateEditor",                       // 关闭方法（取消按钮与 Escape 共用）
				"ev.key !== 'Escape'",                       // 键判据
				">取消</button>",                              // 可见的鼠标退出通道（本轮补的第一个）
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.src == "" {
				t.Fatal("源码为空，判据失去受理面")
			}
			for _, n := range tc.need {
				if !strings.Contains(tc.src, n) {
					t.Errorf("缺少键盘可达性构件 %q（照着补：Escape 绑定 / 回焦记录 / 取消入口）", n)
				}
			}
		})
	}
}

// TestTopbarMenuEscapeInAllBlobs 钉住「顶栏『更多』菜单三份都要能用 Escape 关闭」。
//
// 一手缺陷（2026-10-24 实测取证，非推测）：adminPage / governancePage / modelPoolPage
// 三个页面都有 id='btnMore' + id='topMore'，但菜单块里只有 appJS 绑了 Escape。
// appJS 的注释把这条当硬性要求（「键盘用户用 Enter 打开后，鼠标外点逻辑对他们不存在」），
// govJS / poolJS 都漏了 —— 更糟的是 poolJS 另有注释「与顶栏「更多」菜单的 Escape 处理同型」，
// 拿一个当时并不存在的实现当参照（文档描述了一个没有的行为）。
// 三份内嵌脚本是各自独立的副本：一份里修的另两份不会自动跟上（同 esc 单引号映射那次）。
//
// 判据取菜单块内三件事实：① Escape 键判据；② 关闭动作 topMore.hidden = true；③ 回焦 btnMore.focus()。
// 三者缺一即不通过（行为测试实测：改前 govJS/poolJS 打开后按 Escape 无效、焦点不归按钮）。
func TestTopbarMenuEscapeInAllBlobs(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"主面板 appJS", appJS},
		{"治理台 govJS", govJS},
		{"模型池 poolJS", poolJS},
	} {
		if tc.src == "" {
			t.Fatalf("%s 为空 —— 判据失去受理面", tc.name)
		}
		start := strings.Index(tc.src, "const topMore = ")
		if start < 0 {
			t.Fatalf("%s 找不到顶栏「更多」菜单块（结构变了须同步本测试，不许静默空转）", tc.name)
		}
		rest := tc.src[start:]
		end := strings.Index(rest, "\n  }\n")
		if end < 0 {
			t.Fatalf("%s 菜单块未闭合 —— 判据失去受理面", tc.name)
		}
		blk := rest[:end]
		for _, need := range []string{"ev.key === 'Escape'", "topMore.hidden = true", "btnMore.focus()"} {
			if !strings.Contains(blk, need) {
				t.Errorf("%s 的顶栏菜单缺键盘可达性构件 %q（Enter 能打开就该能用 Escape 关掉并回焦）", tc.name, need)
			}
		}
	}
}

// TestKeyboardBindingsNotInRepeatablePaths 钉住「键盘监听不得挂进每次渲染都会跑的路径」。
//
// 【round 111 用实质判据替换计数判据】原实现是「keydown 总数 > 2 判红」，两个方向都错：
//
//	· 假红 —— 本仓合法就有 3 处（菜单 / 编辑器 / 待确认态各一），再加第四个合法入口就撞线；
//	  本轮给 gov/pool 补菜单 Escape 后 poolJS 到 3 处，正是这条把我挡住的。
//	· 假绿 —— 若只有一处但写进 render 体内，按一次 Escape 触发 N 次关闭 + N 次 focus，
//	  原缺陷照样漏。真实风险是「监听器累积」，不是「数量多」。
//
// 故改判：addEventListener('keydown') 不得出现在会被反复调用的函数体（render* / load / fill*）内。
// 实测三份今天都是 0；把绑定注入 renderList 体内则被抓到（双向验证）。
func TestKeyboardBindingsNotInRepeatablePaths(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"主面板", appJS},
		{"治理台", govJS},
		{"模型池", poolJS},
	} {
		n := strings.Count(tc.src, "addEventListener('keydown'")
		if n == 0 {
			t.Errorf("%s：没有任何 keydown 绑定（键盘不可关闭）", tc.name)
			continue
		}
		for _, fn := range repeatableFuncBodies(tc.src) {
			if strings.Contains(fn.body, "addEventListener('keydown'") {
				t.Errorf("%s：%s 内挂了 keydown 监听 —— 该函数会被反复调用，监听器随重绘累积（按一次 Escape 触发多次关闭）。移到顶层只挂一次", tc.name, fn.name)
			}
		}
	}
}

// repeatableFuncBodies 返回所有「会被反复调用」的函数名与函数体。
// 命名规律取 render* / load / fill*：这三类每次渲染或每次刷新都会整段跑一遍。
func repeatableFuncBodies(js string) []struct{ name, body string } {
	lines := strings.Split(js, "\n")
	var out []struct{ name, body string }
	for i, l := range lines {
		m := repeatableHead.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		depth, seen := 0, false
		var sb strings.Builder
		for k := i; k < len(lines); k++ {
			sb.WriteString(lines[k])
			sb.WriteString("\n")
			for _, c := range lines[k] {
				if c == '{' {
					depth++
					seen = true
				} else if c == '}' {
					depth--
				}
			}
			if seen && depth == 0 {
				break
			}
		}
		out = append(out, struct{ name, body string }{m[1], sb.String()})
	}
	return out
}

// repeatableHead 匹配两空格缩进的顶层定义行（内嵌 JS 的顶层就是这个缩进）。
var repeatableHead = regexp.MustCompile(`^  (?:const|let|function|async function) (render\w*|load|fill\w*)\b`)

package web

import (
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

// TestInlineEditorEscapeBindsOnce 钉住 Escape 绑定只出现一次：
// 若把 addEventListener('keydown') 写进每次渲染的 render 函数里，监听器会随重绘累积
// （按一次 Escape 触发 N 次关闭 + N 次 focus），这类缺陷单看功能「也能用」，
// 但会把键盘操作变成不可预期的抖动。
func TestInlineEditorEscapeBindsOnce(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"模型池", poolJS},
		{"主面板", appJS},
	} {
		n := strings.Count(tc.src, "addEventListener('keydown'")
		if n == 0 {
			t.Errorf("%s：没有任何 keydown 绑定（键盘不可关闭）", tc.name)
		}
		if n > 2 {
			t.Errorf("%s：keydown 绑定 %d 处，过多 —— 检查是否写进了每次渲染的路径（会累积监听）", tc.name, n)
		}
	}
}

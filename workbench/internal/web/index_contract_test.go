package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestIndexPageEntryContract 守护首页入口契约（2026-10-19 老大当轮指令改版）。
//
// 旧契约（留档 git 历史）：首页只允许「1 按钮 + 1 隐藏后门」，管理面板只能靠
// 悬停显形的「·」进入。老大现场反馈「其它子页面的入口在哪？出现在这个页面上」
// ——当轮指令高于既往口径（P0.19/#24），入口改为可见。
//
// 新契约：
//   - 仍然只有 1 个按钮（唯一的启动/进入动作）；
//   - 子页面入口以可见链接条呈现：管理面板 / 模型池 / 治理，三个都必须出现；
//   - 入口不得再对辅助技术隐藏（aria-hidden 是旧后门特征，现在反向判红）；
//   - 每条 href 必须携带 token（可达性不能被样式牺牲）；
//   - 禁止的仍是「配置项」：输入框/下拉/文本域不得出现在首页。
func TestIndexPageEntryContract(t *testing.T) {
	html := indexHTML("tok-123", "running")

	// 1) 按钮数量必须恰好 1
	if n := strings.Count(html, "<button"); n != 1 {
		t.Fatalf("首页必须只有 1 个按钮，实际 %d 个", n)
	}
	// 2) 不允许出现表单类交互元素（配置项）
	for _, bad := range []string{"<input", "<select", "<textarea", `role="menu"`} {
		if strings.Contains(html, bad) {
			t.Fatalf("首页不允许出现 %q（入口是链接，不是配置面板）", bad)
		}
	}
	// 3) 链接必须恰好 3 个：管理面板 / 模型池 / 治理
	links := regexp.MustCompile(`<a\s[^>]*>`).FindAllString(html, -1)
	if len(links) != 3 {
		t.Fatalf("首页必须有 3 个子页面入口链接，实际 %d 个", len(links))
	}
	for _, need := range []string{"/admin?token=tok-123", "/modelpool?token=tok-123", "/governance?token=tok-123"} {
		if !strings.Contains(html, need) {
			t.Fatalf("缺少子页面入口 %q（老大指令：入口出现在首页上）", need)
		}
	}
	// 4) 入口必须可见可读：不得带 aria-hidden / tabindex=-1（旧后门特征）
	for _, l := range links {
		if strings.Contains(l, "aria-hidden") || strings.Contains(l, `tabindex="-1"`) {
			t.Fatalf("入口链接被藏起来了（%q）——可见性是本轮指令的核心", l)
		}
	}
	// 5) 入口文字必须是功能名（点得出是什么）
	for _, word := range []string{">管理面板</a>", ">模型池</a>", ">治理</a>"} {
		if !strings.Contains(html, word) {
			t.Fatalf("入口文字缺少 %q（图标/暗语不满足「入口在哪」的诉求）", word)
		}
	}
}

// TestIndexPageHasSingleStartAction 守护按钮语义：唯一按钮就是「开始/工作台」启动动作。
func TestIndexPageHasSingleStartAction(t *testing.T) {
	html := indexHTML("t", "idle")
	if !strings.Contains(html, `id="startBtn"`) {
		t.Fatal("首页唯一按钮必须是启动按钮 startBtn")
	}
	if !strings.Contains(html, "开始") {
		t.Fatal("启动按钮文案必须含「开始」")
	}
}

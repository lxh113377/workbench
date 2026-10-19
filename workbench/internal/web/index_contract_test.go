package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestIndexPageMinimalContract 守护「极简首页」的产品契约（目标要求 1）：
//
//   - 首页只能有一个按钮（唯一的启动动作）；
//   - 不允许出现可见的配置项/菜单/导航（输入框、下拉、导航栏都算违规）；
//   - 唯一的可选入口必须是**隐藏式后门**：文字不可见（aria-hidden）、且不作为
//     可见的「面板/设置」字样出现在页面上（目标要求 6：隐藏式可配置界面）。
//
// 这条用例的价值：首页一旦被人加了一个「设置」按钮或导航链接，测试会立刻红，
// 而不是等到人工走查才发现"极简"被破坏。
func TestIndexPageMinimalContract(t *testing.T) {
	html := indexHTML("tok-123", "running")

	// 1) 按钮数量必须恰好 1
	if n := strings.Count(html, "<button"); n != 1 {
		t.Fatalf("首页必须只有 1 个按钮，实际 %d 个", n)
	}
	// 2) 不允许出现表单类交互元素（配置项）
	for _, bad := range []string{"<input", "<select", "<textarea", "<nav", "role=\"menu\""} {
		if strings.Contains(html, bad) {
			t.Fatalf("极简首页不允许出现 %q（应为无配置项、无菜单）", bad)
		}
	}
	// 3) 唯一的链接必须是隐藏后门：aria-hidden + 不可见文字
	links := regexp.MustCompile(`<a\s[^>]*>`).FindAllString(html, -1)
	if len(links) != 1 {
		t.Fatalf("首页最多允许 1 个链接（隐藏后门），实际 %d 个", len(links))
	}
	if !strings.Contains(links[0], "aria-hidden=\"true\"") {
		t.Fatalf("后门链接必须对辅助技术隐藏（aria-hidden），实际 %q", links[0])
	}
	// 4) 后门文字不得是可读的功能名（避免视觉上出现"面板/设置"入口）
	for _, word := range []string{">面板<", ">设置<", "管理面板</a>"} {
		if strings.Contains(html, word) {
			t.Fatalf("首页不得出现可见的功能入口字样 %q", word)
		}
	}
	// 5) 后门 href 必须仍指向管理面板并携带 token（可达性不能被"隐藏"牺牲）
	if !strings.Contains(html, "/admin?token=tok-123") {
		t.Fatal("隐藏后门必须仍指向 /admin 并携带 token（配置面必须可达）")
	}
}

// TestIndexPageHasSingleStartAction 守护按钮语义：唯一按钮就是「开始运行」。
func TestIndexPageHasSingleStartAction(t *testing.T) {
	html := indexHTML("t", "idle")
	if !strings.Contains(html, `id="startBtn"`) {
		t.Fatal("首页唯一按钮必须是启动按钮 startBtn")
	}
	if !strings.Contains(html, "开始运行") {
		t.Fatal("启动按钮文案必须是「开始运行」")
	}
}

// TestHiddenBackdoorIsDotOnly 守护后门的视觉形态：只有一个不可见字符 + 透明色规则。
func TestHiddenBackdoorIsDotOnly(t *testing.T) {
	html := indexHTML("t", "idle")
	m := regexp.MustCompile(`<a class="home-admin"[^>]*>([^<]*)</a>`).FindStringSubmatch(html)
	if m == nil {
		t.Fatal("缺少隐藏后门链接（配置面不可达 = 目标要求 6 不满足）")
	}
	if strings.TrimSpace(m[1]) == "" {
		t.Fatal("后门链接不能为空（需可点击面积）")
	}
	if len([]rune(m[1])) > 2 {
		t.Fatalf("后门文字必须是单字符点标记，实际 %q（会变成可见功能入口）", m[1])
	}
	if !strings.Contains(appCSS, "color:transparent") {
		t.Fatal("CSS 必须让后门默认透明（隐藏式），悬停/聚焦才显形")
	}
}

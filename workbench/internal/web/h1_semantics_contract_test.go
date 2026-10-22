package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestBackofficePagesHaveH1 钉住「每个后台页必须有且仅有一个 h1」。
//
// 一手缺陷（2026-10-23 Awwwards 内容维度验收）：三张后台页的主标题此前是
// `<span class="pagetitle">` —— 视觉是大标题，语义上**没有任何层级**，全站 h1 数为 0。
// 屏幕阅读器用户因此无法用「按标题跳转」定位主标题（无障碍核心导航方式）。
// 实测：修复前 admin/governance/modelpool 的 h1 数均为 0，DOM 里只有 h3。
// 修：三处 span 改 h1 + 重置 h1 默认 margin（.pagebar 是 flex 容器）。
// 首页**不**加 h1：首页契约明令禁止标题元素（第一屏必须极简，只允许 1 button + 3 a），
// 且 <title> 已提供页面名、<main> 提供 landmark，属设计决策而非缺陷。
func TestBackofficePagesHaveH1(t *testing.T) {
	pages := map[string]string{"admin": adminPage, "governance": governancePage, "modelpool": modelPoolPage}
	h1re := regexp.MustCompile(`<h1[^>]*>([^<]*)</h1>`)
	for name, html := range pages {
		h1 := h1re.FindAllStringSubmatch(html, -1)
		if len(h1) != 1 {
			t.Errorf("%s 的 h1 数量 = %d，应为 1（主标题是屏幕阅读器的核心导航锚点）", name, len(h1))
			continue
		}
		if strings.TrimSpace(h1[0][1]) == "" {
			t.Errorf("%s 的 h1 文本为空", name)
		}
	}
}

// TestNoPagetitleSpanLeft 反向保险：`.pagetitle` 必须是 h1，不得退回 span。
func TestNoPagetitleSpanLeft(t *testing.T) {
	bad := regexp.MustCompile(`<span[^>]*class="[^"]*pagetitle`)
	all := map[string]string{"admin": adminPage, "governance": governancePage, "modelpool": modelPoolPage}
	for name, html := range all {
		if m := bad.FindString(html); m != "" {
			t.Errorf("%s 里 .pagetitle 仍是 span（%s）—— 视觉标题必须有语义层级", name, m)
		}
	}
}

// TestPagetitleH1KeepsMarginReset 钉住「h1 有 margin:0 重置」。
//
// .pagebar 是 flex 容器；h1 默认 margin 会破坏基线对齐，必须显式重置。
func TestPagetitleH1KeepsMarginReset(t *testing.T) {
	i := strings.Index(appCSS, ".pagetitle{")
	if i < 0 {
		t.Fatal("找不到 .pagetitle 样式")
	}
	body := appCSS[i:]
	if e := strings.Index(body, "}"); e > 0 {
		body = body[:e]
	}
	if !strings.Contains(body, "margin:0") {
		t.Error(".pagetitle 缺 margin:0 —— h1 默认 margin 会破坏 .pagebar flex 对齐")
	}
}

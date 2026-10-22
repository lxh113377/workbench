package web

import (
	"strings"
	"testing"
)

// TestSharedCardLanguageAcrossPages 钉住「四页共用同一套卡片语言」。
//
// 跨页一致性审计（2026-10-22，CDP 逐页读 computedStyle）：治理台的 .gov-card 用
// 圆角（--radius 12px）+ margin 14px + 实边框，而管理/池的 .card 用 HUD 切角
// （clip-path + radius 0）+ margin 18px + inset 描边 —— 同一产品两种卡片语言。
// 设计体系头注释写明「HUD 面板的角不是圆的，是斜切的」（clip-path 9 处、--cut 为主语言），
// 故 .gov-card 是异类，已对齐 .card。
//
// 判据断言两卡基座的关键度量同源：切角、radius 0、18px 节奏、inset 描边。
func TestSharedCardLanguageAcrossPages(t *testing.T) {
	idx := strings.Index(appCSS, ".card{")
	if idx < 0 {
		t.Fatal("找不到 .card 基座")
	}
	card := appCSS[idx : idx+700]
	idx2 := strings.Index(appCSS, ".gov-card{")
	if idx2 < 0 {
		t.Fatal("找不到 .gov-card 基座")
	}
	gov := appCSS[idx2 : idx2+900]
	for _, c := range []struct{ name, needle string }{
		{"两卡都用切角 clip-path", "clip-path:polygon("},
		{"两卡 radius 为 0（切角不能被圆角覆盖）", "border-radius:0"},
		{"两卡同一 18px 纵向节奏", "margin:0 0 18px"},
		{"两卡用 inset 描边", "inset 0 0 0 1px"},
	} {
		if !strings.Contains(card, c.needle) {
			t.Errorf(".card 缺少「%s」：%q", c.name, c.needle)
		}
		if !strings.Contains(gov, c.needle) {
			t.Errorf(".gov-card 缺少「%s」：%q（跨页卡片语言不一致）", c.name, c.needle)
		}
	}
}

// TestContentTopPaddingUnified 钉住「四页内容区上边距同值」。
//
// 一手实测：管理面板的 DOM 是 topbar-shell → tabs → content，而
// `.topbar-shell + .content` 是相邻兄弟选择器、匹配不到（中间隔着 .tabs），
// 于是有页签的页回落 26px、无页签的页 14px —— 同一产品两种顶距。
// 修法用通用兄弟 `~` + `.tabs + .content` 双保险；本判据钉住这两条都在。
func TestContentTopPaddingUnified(t *testing.T) {
	need := []string{".tabs + .content", ".topbar-shell ~ .content"}
	for _, n := range need {
		if !strings.Contains(appCSS, n) {
			t.Errorf("缺少选择器 %q —— 有页签的页面会回落 26px 顶距（跨页不一致）", n)
		}
	}
}

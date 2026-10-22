package web

import (
	"strings"
	"testing"
)

// TestWideScreenTickScaleIsEnhanced 钉住「宽屏取景框刻度必须随画布放大」。
//
// 一手实测（2026-10-23 CDP 逐档读 ::after 计算样式）：1440 / 1920 / 2560 三档读到
// **完全相同**的刻度色（alpha .5/.35）、线宽（1.5px）、间距（46px）—— 而 2560 的
// 画布面积是 1440 的 3.2 倍，刻度被稀释成看不见，「仪器感」在大屏上消失。
// 修：@media (min-width:1600px) 里加粗（1.5→2px）、提亮（.35→.5 / .5→.72）、
// 拉大刻度尺高度（7/5→9/7px）与辉光（.55→.7）、opacity .85→.92。
// 实测修后 1440 保持原值、1920/2560 生效增强值。
func TestWideScreenTickScaleIsEnhanced(t *testing.T) {
	i := strings.Index(appCSS, "@media (min-width:1600px)")
	if i < 0 {
		t.Fatal("缺少 @media (min-width:1600px) 宽屏断点")
	}
	// 取到该媒体块结束（下一个 @media 之前）
	block := appCSS[i:]
	if e := strings.Index(block[10:], "@media "); e > 0 {
		block = block[:e+10]
	}
	if !strings.Contains(block, ".home-main::after") {
		t.Error("宽屏断点未调整首页取景框 —— 三档宽度刻度同值，大屏被稀释")
	}
	for _, c := range []struct{ name, needle string }{
		{"线宽加粗到 2px", "--tw:2px"},
		{"刻度提亮到 .72", "rgba(150,215,255,.72)"},
		{"下边刻度提亮到 .5", "rgba(150,215,255,.5)"},
		{"刻度尺高度 9/7px", "100% 9px, 100% 7px"},
		{"辉光提到 .7", "rgba(var(--accent-rgb),.7)"},
		{"opacity .92", "opacity:.92"},
	} {
		if !strings.Contains(block, c.needle) {
			t.Errorf("宽屏取景框缺少「%s」：%q", c.name, c.needle)
		}
	}
}

// TestBaseTickStaysSubtle 反向保险：基础档（<1600）不得被提亮。
//
// 大屏要提亮是「距离远、画布大」；小屏靠得近，刻度若提亮会变成噪点抢主体。
// 判据：基础 ::after 的刻度仍是 130,200,255（低亮度那组）。
func TestBaseTickStaysSubtle(t *testing.T) {
	i := strings.Index(appCSS, ".home-main::after{")
	if i < 0 {
		t.Fatal("找不到基础 .home-main::after 定义")
	}
	body := appCSS[i:]
	if e := strings.Index(body[20:], "}"); e > 0 {
		body = body[:e+20]
	}
	if !strings.Contains(body, "rgba(130,200,255,.5)") {
		t.Error("基础档上边刻度不是低亮度 rgba(130,200,255,.5) —— 小屏会被噪点抢主体")
	}
	if strings.Contains(body, "rgba(150,215,255") {
		t.Error("基础档被提亮成 150,215,255 —— 提亮只应发生在 min-width:1600px")
	}
}

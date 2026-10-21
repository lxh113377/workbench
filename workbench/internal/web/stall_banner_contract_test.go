package web

// stall_banner_contract_test.go 守护「运行中但产出 0 必须能自证原因」（2026-10-21 现场）。
//
// 用户报告的原话就是「显示运行中但显示产出为 0」。这不只是引擎不产出的问题，
// 还是**面板不能说清为什么**的问题：三个读数（运行中 / 0 产出 / 0 排队）
// 互相矛盾，界面上没有一个字解释原因，排查只能靠翻 queue.json。
//
// 本文件守三段接线（任何一段断掉，"运行中但 0 产出"就会重新变成黑箱）：
//   1) 面板脚本读取服务端 stallReason 并按 code 是否为空决定渲染；
//   2) 总览页渲染 stall-banner（原因 + 补充证据）；
//   3) 顶栏也把停摆原因顶到读数位置（用户不进总览也能看到）。

import (
	"strings"
	"testing"
)

// TestOverviewRendersStallBanner 断言总览页渲染停摆横幅，且带补充证据。
func TestOverviewRendersStallBanner(t *testing.T) {
	js := appJS
	if !strings.Contains(js, "stallReason") {
		t.Fatal("面板脚本必须读取 stallReason（否则后端给了原因前端也不显示）")
	}
	if !strings.Contains(js, "stall-banner") {
		t.Fatal("总览必须渲染 stall-banner 元素")
	}
	if !strings.Contains(js, "stall.text") {
		t.Fatal("横幅必须显示服务端给的人话原因（stall.text）")
	}
	if !strings.Contains(js, "stall.detail") {
		t.Fatal("横幅必须显示补充证据（stall.detail），否则用户只知道坏了不知道差什么")
	}
	// 有产出时不得渲染横幅（避免变成常驻噪音，久看即失效）。
	if !strings.Contains(js, "stall.code") {
		t.Fatal("横幅必须按 stall.code 是否为空决定是否渲染（有产出不该出现）")
	}
}

// TestTopBarSurfacesStall 断言顶栏也给出停摆原因（不进总览页也能看到）。
func TestTopBarSurfacesStall(t *testing.T) {
	js := appJS
	idx := strings.Index(js, "async function refreshTop()")
	if idx < 0 {
		t.Fatal("找不到 refreshTop（顶栏渲染入口）")
	}
	tail := js[idx:]
	if end := strings.Index(tail, "const btnPause"); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "stallReason") {
		t.Fatal("refreshTop 必须读取 stallReason（顶栏是用户最常看的一行）")
	}
	if !strings.Contains(tail, "stallTop") {
		t.Fatal("refreshTop 必须把停摆原因写进顶栏文案")
	}
}

// TestStallBannerCSSPresent 断言横幅有样式（否则渲染出来是裸文本，视觉上等于没做）。
func TestStallBannerCSSPresent(t *testing.T) {
	for _, need := range []string{".stall-banner", ".stall-banner .stall-icon"} {
		if !strings.Contains(appCSS, need) {
			t.Fatalf("缺少样式规则 %q", need)
		}
	}
}

package browser

// headless_test.go -- 「无头执行」环境层判定的判据（2026-10-25 round 168 补；此前本包 0% 覆盖）。
//
// 为什么必须补：internal/browser 的 headless() 是「绝不弹窗」的**环境层硬保证** ——
// Open() 的第一句就是它。它判错的两种方向都贵：
//   · 漏判（该 true 却 false）=> CI / 计划任务 / 无人值守机照样往桌面推窗口（老大 2026-10-19 反复投诉的现场）；
//   · 误判（该 false 却 true）=> 用户显式打开面板却毫无反应（「点了打不开」）。
// 本包此前**零测试**，且两份平台实现都与 cmd/workbench/headless.go 漂移（本轮实测：白名单硬编码、
// CI 变量没 TrimSpace）。现在判定收成 headless.go 一份，本文件把口径钉住，防止第四次漂移。
//
// 口径（与 cmd/workbench/headless.go 逐字一致，那边已有同型测试）：
//   · WB_HEADLESS 取 1/true/yes/on **且** 大小写与前后空白不敏感；
//   · 其他一切值（含显式关闭的 0/false/no/off 与未知值）都不得判无头；
//   · CI 变量**去空白后**非空即无头；纯空白不算。

import "testing"

func TestHeadlessEnvValueTrueForms(t *testing.T) {
	// 覆盖大小写与空白的多种写法 —— 其中 tRuE / Yes / On / 带空白 正是本轮实测漏判的那几种。
	for _, v := range []string{"1", "true", "TRUE", "True", "tRuE", "yes", "YES", "Yes", "on", "ON", "On", " 1 ", "\ttrue\n", "  yes  "} {
		if !headlessEnvValue(v) {
			t.Errorf("WB_HEADLESS=%q 应判为无头（大小写与空白必须不敏感）", v)
		}
	}
}

func TestHeadlessEnvValueFalseForms(t *testing.T) {
	// 显式关闭值与未知值一律 false：把 "0"/"false" 判成无头 = 用户点了没反应。
	for _, v := range []string{"", "   ", "0", "false", "FALSE", "no", "off", "disabled", "true1", "1x"} {
		if headlessEnvValue(v) {
			t.Errorf("WB_HEADLESS=%q 不应判为无头（显式关闭必须能弹面板）", v)
		}
	}
}

func TestHeadlessMarkerAloneIsEnough(t *testing.T) {
	// 两源是「任一」不是「都」：只有 marker、CI 为空时必须无头。
	t.Setenv("CI", "")
	t.Setenv(headlessEnvMarker, "On")
	if !headless() {
		t.Fatal("WB_HEADLESS=On 单独成立时应判无头")
	}
}

func TestHeadlessCIAnyNonEmptyAfterTrim(t *testing.T) {
	// CI 口径：去空白后非空即无头（各平台取值不一：true/1/yes/构建号）。
	t.Setenv(headlessEnvMarker, "")
	for _, v := range []string{"true", "1", "yes", "build-1234", "github", "ANY", "  true  "} {
		t.Setenv("CI", v)
		if !headless() {
			t.Errorf("CI=%q（去空白后非空）应判为无头", v)
		}
	}
}

func TestHeadlessCIBlankIsNotHeadless(t *testing.T) {
	// 反向（本轮实测缺陷之二）：旧实现用 os.Getenv("CI") != "" ⇒ CI="   " 会被判无头，
	// 于是本地用户永远看不到面板。去空白后为空必须**不**判无头。
	t.Setenv(headlessEnvMarker, "")
	for _, v := range []string{"", "   ", "\t", "\n"} {
		t.Setenv("CI", v)
		if headless() {
			t.Errorf("CI=%q 去空白后为空，不应判为无头", v)
		}
	}
}

// TestOpenDoesNothingWhenHeadless 钉死「硬保证的第一句」：无头时 Open **不得启动任何浏览器**。
//
// 为什么值得单测：Open 的前两句就是 headless() 判定，随后才是 exec.Command 起 Edge/rundll32。
// 只要判定为真，就必须直接返回 nil —— 若哪天有人把判定挪到启动之后（或加个「先探测一下」），
// 这条测试会当场红；否则窗口就会在 CI/计划任务里真的弹出来（2026-10-19 现场投诉的形态）。
// 本测试只走无头分支，因此**不会**真的拉起浏览器进程（不会污染桌面 —— 这正是本包要在测试里守的纪律）。
func TestOpenDoesNothingWhenHeadless(t *testing.T) {
	t.Setenv(headlessEnvMarker, "On") // 顺带用本轮修的混合大小写形态
	if err := Open("http://127.0.0.1:1/should-not-open"); err != nil {
		t.Fatalf("无头模式下 Open 应直接返回 nil，实得 %v", err)
	}
}

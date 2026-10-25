package web

import (
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestHomeLabelsMatchServerAndScript 守护「首帧文案 == 脚本文案」。
//
// 为什么值得单独立一条：2026-10-19 本轮实测到一次闪变 ——
// 服务端首帧渲染 running 态文案是「工作台」，而 appJS 的 PHASES 表里写的还是「运行中」，
// 于是页面一加载脚本就把按钮文案改回去，肉眼与探针读数都跟着漂。
// 判据：Go 侧四个阶段的 label/hint 必须逐字出现在 appJS 的 PHASES 表里。
func TestHomeLabelsMatchServerAndScript(t *testing.T) {
	for _, ph := range []model.EnginePhase{model.PhaseRunning, model.PhasePaused, model.PhaseStopped, model.PhaseIdle} {
		label := phaseLabel(ph)
		hint := phaseHint(ph)
		if label == "" || hint == "" {
			t.Fatalf("阶段 %s 的 label/hint 不应为空", ph)
		}
		// 允许脚本侧写成 text: '工作台' / hint: '工作台运行中，点击进入'
		if !strings.Contains(appJS, "text: '"+label+"'") {
			t.Fatalf("脚本 PHASES 未包含服务端文案 text: '%s'（阶段 %s）——首帧会闪变", label, ph)
		}
		if !strings.Contains(appJS, "hint: '"+hint+"'") {
			t.Fatalf("脚本 PHASES 未包含服务端 hint: '%s'（阶段 %s）", hint, ph)
		}
	}
	// 首页可见文字长度仍是「极简」的硬约束：label 必须 ≤3 字。
	for _, ph := range []model.EnginePhase{model.PhaseRunning, model.PhasePaused, model.PhaseStopped, model.PhaseIdle} {
		if n := len([]rune(phaseLabel(ph))); n > 3 {
			t.Fatalf("首页按钮文案 %q 有 %d 字，超过 3 字（会撑破「极简」读数）", phaseLabel(ph), n)
		}
	}
}

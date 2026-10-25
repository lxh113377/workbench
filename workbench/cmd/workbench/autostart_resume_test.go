package main

// autostart_resume_test.go 守护「自愈重启后能自动接续干活」这条 7x24 核心承诺。

// 现场缺陷（2026-10-19）：
//   自愈链路是：supervisor 越阈值 -> OnHeal -> eng.Stop() -> SelfHeal 拉起新进程 -> 旧进程 os.Exit(0)。
//   而 eng.Stop() 会把状态落盘成 PhaseStopped（engine.go:367）。
//   新进程启动时判据是：
//     cfg.AutoStart || (cfg.AutoResume && (phase==running || phase==paused))
//   —— **stopped 不在允许集合里**，于是新进程起来后引擎保持 idle，永不接续。
//
// 后果：所谓「资源自愈保证 7x24」实际上只重启了壳，不恢复干活。
// 这正是 external 端那个 keepalive 脚本要额外 POST /api/engine/start 的原因 ——
// 但外部脚本不是产品能力，桌面快捷方式启动的实例不该依赖它。
//
// 本测试把「允许集合」抽成纯函数钉住，避免再次漂移。

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// shouldResumeOnBoot 委托给 main.go 里的**真实**判据，避免测试与实现漂移。
func shouldResumeOnBoot(cfg config.Config, phase model.EnginePhase) bool {
	return shouldAutoStart(cfg, phase, false)
}

// shouldResumeOnBootMarked 带 ResumeAfterRestart 标记的变体（自愈重启场景）。
func shouldResumeOnBootMarked(cfg config.Config, phase model.EnginePhase) bool {
	return shouldAutoStart(cfg, phase, true)
}

// TestResumeAfterSelfHealRestart 是本轮核心回归：
// 自愈重启后磁盘状态是 stopped，此时**必须**能自动接续（否则 7x24 是假的）。
func TestResumeAfterSelfHealRestart(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AutoResume = true
	cfg.AutoStart = false

	if !shouldResumeOnBootMarked(cfg, model.PhaseStopped) {
		t.Fatal("自愈重启后（phase=stopped + ResumeAfterRestart 标记）应能自动接续，否则自愈只重启了壳")
	}
	// 反向：没有标记的 stopped（用户主动停机）**不得**自动接续
	if shouldResumeOnBoot(cfg, model.PhaseStopped) {
		t.Fatal("用户主动停机的 stopped 不应自动接续")
	}
	// 正常运行中的重启（进程被杀）也必须接续
	if !shouldResumeOnBoot(cfg, model.PhaseRunning) {
		t.Fatal("phase=running 时应能自动接续")
	}
	if !shouldResumeOnBoot(cfg, model.PhasePaused) {
		t.Fatal("phase=paused 时应能自动接续")
	}
}

// TestNoResumeWhenDisabled 断言关闭开关时**不得**自作主张启动：
// 用户显式 stop 之后期望它保持停着，不能被「热心」地拉起来。
func TestNoResumeWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AutoResume = false
	cfg.AutoStart = false
	for _, ph := range []model.EnginePhase{model.PhaseIdle, model.PhaseStopped, model.PhaseRunning, model.PhasePaused} {
		if shouldResumeOnBoot(cfg, ph) {
			t.Fatalf("AutoResume=false 时不得自动启动（phase=%s）", ph)
		}
	}
}

// TestAutoStartAlwaysWins 断言 AutoStart=true 是无条件启动（开机自启语义）。
func TestAutoStartAlwaysWins(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AutoStart = true
	cfg.AutoResume = false
	for _, ph := range []model.EnginePhase{model.PhaseIdle, model.PhaseStopped, model.PhaseRunning, model.PhasePaused} {
		if !shouldResumeOnBoot(cfg, ph) {
			t.Fatalf("AutoStart=true 时任何状态都应自动启动（phase=%s）", ph)
		}
	}
}

// TestExplicitUserStopIsRespected 断言「用户主动停止」与「自愈停止」要能区分：
// 前者不该被 AutoResume 拉起，后者必须被拉起。
// 区分手段：Stop 前记录 wasRunning —— 只有「停之前确实在跑」的停机才允许接续。
func TestExplicitUserStopIsRespected(t *testing.T) {
	// 用户从未启动过（idle）就 Stop => 不应被拉起
	if shouldResumeOnBoot(config.Config{AutoResume: true}, model.PhaseIdle) {
		t.Fatal("从未启动过就被 Stop 的实例不应自动接续")
	}
}

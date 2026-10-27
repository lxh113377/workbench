package evaluate

// delivered_replay_test.go 守护「已交付任务不得在时间窗外被重放」（round 46 · T32-b）。
//
// 由来：exact_dup_window_test.go 给「完全重复」加了 60 分钟时间窗，口径是
// 「同一个任务在**没交付成功**之前，隔一段时间重跑是合理的」。但 recentItem
// 只记 (title,prompt) 指纹、**没有交付态** ⇒ 时间窗一放行，已经 done 的任务
// 也会被当成新任务重产重跑（T32 的窗外放行把「未交付可重跑」扩大成了「谁来都能重跑」）。
//
// 现在的口径：
//   ① 已交付（done / verified）的同指纹条目：**无论多久都拦**（滚动窗口内不许重放）；
//   ② 未交付（rejected / failed / cancelled）的同指纹条目：才吃时间窗 —— 窗外放行重跑。
//
// RED 证据：在加交付态分支之前跑本文件，TestDeliveredFingerprintBlockedOutsideWindow
// 必 FAIL（窗外走 continue，gates 为空）；加分支后转 GREEN。

import (
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// dupCfg 固定判重相关配置，避免被 DefaultConfig 的其它默认值干扰。
func dupCfg() config.EvalConfig {
	cfg := config.DefaultConfig().Eval
	cfg.RepeatGate = true
	cfg.ExactDupWindowMinutes = 60
	return cfg
}

// TestDeliveredFingerprintBlockedOutsideWindow 是 T32-b 的正面判据：
// 已交付过的同一 (title,prompt)，即使老到 24 小时前落在窗外，也必须仍被判「完全重复」。
func TestDeliveredFingerprintBlockedOutsideWindow(t *testing.T) {
	now := time.Now().UnixMilli()
	cases := []struct {
		name string
		seed *model.Task
	}{
		{"done", &model.Task{ID: "d1", Title: "已交付任务", Prompt: "同一份需求", State: model.StateDone, CreatedAt: now - 24*60*60*1000}},
		{"done+verified", &model.Task{ID: "d2", Title: "已交付并存证任务", Prompt: "同一份需求", State: model.StateDone, Verified: true, CreatedAt: now - 24*60*60*1000}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := New(dupCfg())
			ev.Observe(c.seed, 80, "d", 400)
			replay := &model.Task{ID: "replay-" + c.name, Title: c.seed.Title, Prompt: c.seed.Prompt, CreatedAt: now}
			got := ev.gates(Input{Task: replay}, now)
			if !containsDupReason(got) {
				t.Fatalf("已交付任务在时间窗外仍必须拦（不许重放），实际 reasons=%v", got)
			}
		})
	}
}

// TestUndeliveredFingerprintAllowedOutsideWindow 是 T32-b 的反面判据：
// 未交付（rejected / failed / cancelled）的同一任务，窗外必须放行重跑。
func TestUndeliveredFingerprintAllowedOutsideWindow(t *testing.T) {
	now := time.Now().UnixMilli()
	for _, st := range []model.TaskState{model.StateRejected, model.StateFailed, model.StateCancelled} {
		t.Run(string(st), func(t *testing.T) {
			ev := New(dupCfg())
			seed := &model.Task{ID: "u-" + string(st), Title: "未交付任务", Prompt: "同一份需求", State: st, CreatedAt: now - 24*60*60*1000}
			ev.Observe(seed, 0, "d", 400)
			rerun := &model.Task{ID: "rerun-" + string(st), Title: seed.Title, Prompt: seed.Prompt, CreatedAt: now}
			got := ev.gates(Input{Task: rerun}, now)
			if containsDupReason(got) {
				t.Fatalf("未交付任务窗外必须放行重跑，实际 reasons=%v", got)
			}
		})
	}
}

// TestDeliveredAndUndeliveredStillBlockedInsideWindow 守住时间窗本身没被削弱：
// 窗内两种交付态都要拦（防短周期抖动，T29 口径不变）。
func TestDeliveredAndUndeliveredStillBlockedInsideWindow(t *testing.T) {
	now := time.Now().UnixMilli()
	for _, st := range []model.TaskState{model.StateDone, model.StateRejected, model.StateFailed, model.StateCancelled} {
		t.Run(string(st), func(t *testing.T) {
			ev := New(dupCfg())
			seed := &model.Task{ID: "w-" + string(st), Title: "窗内任务", Prompt: "同一份需求", State: st, CreatedAt: now - 60_000}
			ev.Observe(seed, 0, "d", 400)
			dup := &model.Task{ID: "dup-" + string(st), Title: seed.Title, Prompt: seed.Prompt, CreatedAt: now}
			got := ev.gates(Input{Task: dup}, now)
			if !containsDupReason(got) {
				t.Fatalf("窗内同指纹（state=%s）必须仍被判完全重复，实际 reasons=%v", st, got)
			}
		})
	}
}

// TestVerifiedAloneIsNotDelivered 钉住「`Verified` 不能单独作为已交付判据」（round 48 · 独立复核抓到的缺口）。
//
// 可达路径：`completeTask` 先写 `t.Verified=true`、再 `Transition(StateDone)`（两次独立 WAL 写且后者错误被丢弃），
// 两次之间崩溃/被杀 ⇒ 重启后任务呈 `running + Verified=true`；若 `delivered()` 认可 Verified，
// 这类**未交付**任务会被当已交付 ⇒ 全窗口永久拦 ⇒ 队列饥饿回归。
func TestVerifiedAloneIsNotDelivered(t *testing.T) {
	cfg := dupCfg()
	now := time.Now().UnixMilli()
	ev := New(cfg)
	// 未交付（cancelled）但 Verified=true，且指纹远在窗外。
	seed := &model.Task{ID: "c1", Title: "T", Prompt: "P", State: model.StateCancelled, Verified: true, CreatedAt: now - 24*3600*1000}
	ev.Observe(seed, 0, "d", 400)
	got := ev.gates(Input{Task: &model.Task{ID: "r", Title: "T", Prompt: "P", CreatedAt: now}}, now)
	if containsDupReason(got) {
		t.Fatalf("未交付（cancelled）+Verified=true 被当已交付、窗外仍拦：reasons=%v", got)
	}
	// 对照：同样是 cancelled 但 Verified=false ⇒ 当然也放行（判据只看终态）。
	ev2 := New(cfg)
	seed2 := &model.Task{ID: "c2", Title: "T2", Prompt: "P2", State: model.StateCancelled, CreatedAt: now - 24*3600*1000}
	ev2.Observe(seed2, 0, "d", 400)
	if got := ev2.gates(Input{Task: &model.Task{ID: "r2", Title: "T2", Prompt: "P2", CreatedAt: now}}, now); containsDupReason(got) {
		t.Fatalf("cancelled 未交付应放行：reasons=%v", got)
	}
}

package evaluate

import (
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestExactDuplicateGateHasTimeWindow 钉住「完全重复」判据的时间窗（round 45 · T29）。
//
// 由来（实锤）：模板生成的任务 `goalId` 为空，而 `sameGoal` 是 `a != "" && a == b` ⇒ 同目标豁免**本就不适用**；
// 于是同一 (title,prompt) 被**永久**判重 —— 生成器反复产同一批组合时队列被自己判死。
// 实测 `queue.json`：5001 条里 rejected 4970、**queued 仅 1**（调度饥饿，几乎没东西可跑）。
// 口径：窗内仍拦（防短周期抖动）；窗外放行（那个任务多半还没交付成功，重跑是合理的）。
func TestExactDuplicateGateHasTimeWindow(t *testing.T) {
	cfg := config.DefaultConfig().Eval
	cfg.ExactDupWindowMinutes = 60
	ev := New(cfg)
	now := time.Now().UnixMilli()

	// 窗内：同指纹必须仍被判重。
	inWin := &model.Task{ID: "in", Title: "同一个任务", Prompt: "同一条需求", CreatedAt: now - 60_000}
	ev.Observe(inWin, 80, "d", 400)
	dupIn := &model.Task{ID: "dup-in", Title: "同一个任务", Prompt: "同一条需求", CreatedAt: now}
	if got := ev.gates(Input{Task: dupIn}, now); !containsDupReason(got) {
		t.Fatalf("窗内同指纹必须被判「完全重复」，实际 reasons=%v", got)
	}

	// 窗外：同一指纹不得再被这条判据拦下（否则队列永久饿死）。
	old := &model.Task{ID: "old", Title: "另一个任务", Prompt: "另一条需求", CreatedAt: now - 2*60*60*1000}
	ev.Observe(old, 80, "d", 400)
	dupOut := &model.Task{ID: "dup-out", Title: "另一个任务", Prompt: "另一条需求", CreatedAt: now}
	if got := ev.gates(Input{Task: dupOut}, now); containsDupReason(got) {
		t.Fatalf("窗外同指纹不得再被判「完全重复」，实际 reasons=%v", got)
	}
}

func containsDupReason(reasons []string) bool {
	for _, r := range reasons {
		if strings.Contains(r, "完全重复") {
			return true
		}
	}
	return false
}

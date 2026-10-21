package engine

// 双角色闭环与既有"返工/降级"契约的边界测试。
//
// 三个终态的分界必须被钉死（否则闭环要么断、要么无限重试）：
//   - 返工有余量          -> repair 环（方案不变，改实现）；
//   - 返工用尽、重规划有余量 -> replan（回到方案层改前提）；
//   - 两者都用尽          -> degraded 终态（有产出但不计入今日产出）。
//
// 改造前只有 repair->degraded 两段；新增 replan 后，"用尽"必须两维同时判。
// 这组用例就是那个边界的守卫：任何一边写错，闭环就会在错误的位置终止。

import (
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestRepairExhaustedButReplanAvailableGoesQueued 钉住中间那一档。
func TestRepairExhaustedButReplanAvailableGoesQueued(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-replan-boundary"
	task, _ := seedRunningTask(t, e, st, taskID)
	_ = os.MkdirAll(filepath.Join(layout.TaskDir(taskID), "workspace"), 0o755)

	d := config.DefaultConfig()
	// 返工用尽，但重规划还有额度 —— 必须进 replan，不能落 degraded。
	if _, err := st.Update(taskID, func(x *model.Task) error {
		x.RepairRounds = d.Executor.RepairRounds
		x.PlanRounds = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = st.Get(taskID)

	const fence = "```"
	finalText := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"bad.py"}],"verify":[{"cmd":"python bad.py","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:bad.py\nraise SystemExit(9)\n" + fence + "\n<<<END>>>\n"
	running, _ := st.Get(taskID)
	res := &executor.Result{FinalText: finalText}
	e.completeTask(task, running, res, "m", d)

	got, _ := st.Get(taskID)
	if got.State != model.StateQueued {
		t.Fatalf("返工用尽但重规划有余量时状态 = %s, want queued（应进入重新规划，而不是直接降级）", got.State)
	}
	if got.PlanRounds != 1 {
		t.Fatalf("PlanRounds = %d, want 1", got.PlanRounds)
	}
	if got.ErrorKind != "replan" {
		t.Fatalf("ErrorKind = %q, want replan", got.ErrorKind)
	}
	if got.Verified {
		t.Fatal("重新规划中的任务不得标记为已验收")
	}
}

// TestRepairAvailableStillPrefersRepair 钉住优先级：返工优先于重规划。
//
// 理由：改方案 = 推翻重来，成本远高于"同一方案再修一次"。
// 若顺序反了，每个小实现错误都会触发一次完整重规划，吞吐会塌。
func TestRepairAvailableStillPrefersRepair(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-repair-priority"
	task, _ := seedRunningTask(t, e, st, taskID)
	_ = os.MkdirAll(filepath.Join(layout.TaskDir(taskID), "workspace"), 0o755)

	d := config.DefaultConfig()
	if _, err := st.Update(taskID, func(x *model.Task) error {
		x.RepairRounds = 0
		x.PlanRounds = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	task, _ = st.Get(taskID)

	const fence = "```"
	finalText := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"bad.py"}],"verify":[{"cmd":"python bad.py","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:bad.py\nraise SystemExit(9)\n" + fence + "\n<<<END>>>\n"
	running, _ := st.Get(taskID)
	res := &executor.Result{FinalText: finalText}
	e.completeTask(task, running, res, "m", d)

	got, _ := st.Get(taskID)
	if got.State != model.StateQueued {
		t.Fatalf("返工有余量时状态 = %s, want queued", got.State)
	}
	if got.RepairRounds != 1 {
		t.Fatalf("RepairRounds = %d, want 1（返工优先）", got.RepairRounds)
	}
	if got.PlanRounds != 0 {
		t.Fatalf("PlanRounds = %d, want 0（不得抢先重规划）", got.PlanRounds)
	}
}

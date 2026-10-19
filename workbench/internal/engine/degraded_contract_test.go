package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestCompleteTaskDegradedOnVerifyFail 是 P3 的**端到端契约测试**：
// 一个真实 DSH 回答（含产物块但验收命令会失败）经过 completeTask 后，
// 任务必须落到 StateDegraded、Verified=false，且 Verify 报告落盘。
//
// 它锁死了一个真实 bug：第一版 completeTask 在验收判定之前就用 store.Update
// 直接写了 State=done，随后 done→degraded 迁移被状态机拒绝且错误被忽略，
// 导致 degraded 永远不生效（隔离实例实测 verified=false 却是 state=done）。
func TestCompleteTaskDegradedOnVerifyFail(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-degraded-e2e"
	task := &model.Task{
		ID: taskID, Title: "降级契约", State: model.StateCandidate,
		OutputFormat: "md", MaxAttempts: 3, Domain: "code",
		// 预置为已用满返工额度：本测试锁定的是"额度用尽后的降级语义"，
		// 返工路径由 repair_test.go 单独覆盖。
		RepairRounds: config.DefaultConfig().Executor.RepairRounds,
	}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(taskID, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	running, _ := st.Get(taskID)
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)

	const fence = "```"
	finalText := "产出如下。\n\n<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"bad.py"}],"verify":[{"cmd":"python bad.py","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:bad.py\nraise SystemExit(7)\n" + fence + "\n<<<END>>>\n"

	res := &executor.Result{SessionID: "session-test", FinalText: finalText}
	e.completeTask(task, running, res, "test-model", config.DefaultConfig())

	got, ok := st.Get(taskID)
	if !ok {
		t.Fatal("任务应存在")
	}
	if got.State != model.StateDegraded {
		t.Fatalf("验收失败的任务状态 = %s, want degraded（不能是 done）", got.State)
	}
	if got.Verified {
		t.Fatal("验收失败的任务 Verified 必须为 false")
	}
	if got.Verify == nil || got.Verify.Pass {
		t.Fatalf("Verify 报告应为失败: %+v", got.Verify)
	}
	if !strings.Contains(got.Verify.FailReason, "退出码") {
		t.Fatalf("失败原因应含退出码信息: %q", got.Verify.FailReason)
	}
	if _, err := os.Stat(filepath.Join(taskDir, "verify.json")); err != nil {
		t.Fatal("verify.json 未落盘（验收证据缺失）")
	}
	// 降级不计入今日产出
	if s := e.Stats(); s.TodayDone != 0 || s.DegradedTasks != 1 {
		t.Fatalf("统计口径错误: todayDone=%d degraded=%d", s.TodayDone, s.DegradedTasks)
	}
}

// TestCompleteTaskDoneOnVerifyPass 是对照组：验收通过必须落 done 且 Verified=true。
func TestCompleteTaskDoneOnVerifyPass(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-done-e2e"
	task := &model.Task{ID: taskID, Title: "成功契约", State: model.StateCandidate, OutputFormat: "md", MaxAttempts: 3}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(taskID, to, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	running, _ := st.Get(taskID)
	_ = os.MkdirAll(filepath.Join(layout.TaskDir(taskID), "workspace"), 0o755)

	const fence = "```"
	finalText := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"ok.md"}],"verify":[{"cmd":"pwsh -NoProfile -NonInteractive -Command \"if (Test-Path ok.md) { exit 0 } else { exit 1 }\"","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:ok.md\n# ok\n" + fence + "\n<<<END>>>\n"

	res := &executor.Result{SessionID: "session-ok", FinalText: finalText}
	e.completeTask(task, running, res, "test-model", config.DefaultConfig())

	got, _ := st.Get(taskID)
	if got.State != model.StateDone {
		t.Fatalf("验收通过的任务状态 = %s, want done", got.State)
	}
	if !got.Verified {
		t.Fatal("验收通过的任务 Verified 必须为 true")
	}
	if s := e.Stats(); s.TodayDone != 1 {
		t.Fatalf("todayDone = %d, want 1", s.TodayDone)
	}
}

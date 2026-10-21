package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		// 双角色闭环（2026-10-21）新增了"重新规划"这一段：返工用尽后先回到
		// 方案模型重出方案，再不行才落 degraded。所以"额度用尽"现在有两个维度，
		// 要锁住终态语义必须两条额度都设满。
		//
		// 注意：这不是放宽断言 —— 下面仍然逐条断言 degraded / Verified=false /
		// FailReason 含退出码 / 不计今日产出，只是把"用尽"的前置条件补全。
		PlanRounds: config.DefaultConfig().Plan.ReplanRounds,
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

	finalText := failArtifactsFinal(t, pickTestInterp(t), "产出如下。", 7)

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

	finalText := artifactsFinal(t, pickTestInterp(t), "成果如下。", "ok.md", "# ok\n")

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

	// 等待 completeTask 派生的异步 postProcess 收尾。
	//
	// 为什么要等（2026-10-19 flaky 根因）：completeTask 会 go e.postProcess(...)；
	// 后者写入任务目录。测试不等就结束，t.TempDir 的 RemoveAll 会与它竞争，
	// 报 directory is not empty（默认并行度下约 1/3 概率，门禁因此偶红）。
	e.WaitInflight(5 * time.Second)
}

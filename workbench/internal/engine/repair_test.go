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

func seedRunningTask(t *testing.T, e *Engine, st interface {
	Create(*model.Task) error
	Transition(string, model.TaskState, string, string) (*model.Task, error)
	Get(string) (*model.Task, bool)
}, id string) (*model.Task, string) {
	t.Helper()
	task := &model.Task{ID: id, Title: "返工测试", State: model.StateCandidate, OutputFormat: "md", MaxAttempts: 3, Domain: "code"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	running, _ := st.Get(id)
	return task, running.ChainID
}

// TestRepairLoopRequeuesWithEvidence 覆盖 P3 返工环：
// 验收失败且还有返工额度时，任务必须退回 queued（不是 degraded），
// prompt 被换成含失败证据的返工 prompt，并落盘 prompt.repair-N.md。
func TestRepairLoopRequeuesWithEvidence(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-repair-1"
	task, _ := seedRunningTask(t, e, st, taskID)
	_ = os.MkdirAll(filepath.Join(layout.TaskDir(taskID), "workspace"), 0o755)

	const fence = "```"
	finalText := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"bad.py"}],"verify":[{"cmd":"python bad.py","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:bad.py\nraise SystemExit(9)\n" + fence + "\n<<<END>>>\n"
	running, _ := st.Get(taskID)
	res := &executor.Result{SessionID: "s-repair", FinalText: finalText}
	e.completeTask(task, running, res, "m", config.DefaultConfig())

	got, _ := st.Get(taskID)
	if got.State != model.StateQueued {
		t.Fatalf("有返工额度时状态 = %s, want queued（不应直接降级）", got.State)
	}
	if got.RepairRounds != 1 {
		t.Fatalf("RepairRounds = %d, want 1", got.RepairRounds)
	}
	if !strings.Contains(got.Prompt, "返工说明") {
		t.Fatalf("prompt 未替换为返工 prompt: %q", got.Prompt[:min(120, len(got.Prompt))])
	}
	if !strings.Contains(got.Prompt, "退出码") {
		t.Fatal("返工 prompt 必须包含上次失败的具体证据")
	}
	if _, err := os.Stat(layout.TaskFile(taskID, "prompt.repair-1.md")); err != nil {
		t.Fatal("返工 prompt 未落盘（审计证据缺失）")
	}
	if _, err := os.Stat(layout.TaskFile(taskID, "verify-round-1.json")); err != nil {
		t.Fatal("本轮验收证据未落盘")
	}
}

// TestRepairExhaustedFallsToDegraded 覆盖返工额度用尽：必须落 degraded。
func TestRepairExhaustedFallsToDegraded(t *testing.T) {
	e, st, layout := newTestEngine(t)
	taskID := "task-repair-exhausted"
	task, _ := seedRunningTask(t, e, st, taskID)
	_ = os.MkdirAll(filepath.Join(layout.TaskDir(taskID), "workspace"), 0o755)
	// 预先设成已用满 3 轮
	if _, err := st.Update(taskID, func(x *model.Task) error { x.RepairRounds = 3; return nil }); err != nil {
		t.Fatal(err)
	}
	task, _ = st.Get(taskID)

	const fence = "```"
	finalText := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" +
		`{"files":[{"path":"bad.py"}],"verify":[{"cmd":"python bad.py","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:bad.py\nraise SystemExit(9)\n" + fence + "\n<<<END>>>\n"
	running, _ := st.Get(taskID)
	res := &executor.Result{FinalText: finalText}
	e.completeTask(task, running, res, "m", config.DefaultConfig())

	got, _ := st.Get(taskID)
	if got.State != model.StateDegraded {
		t.Fatalf("返工额度用尽后状态 = %s, want degraded", got.State)
	}
	if got.Verified {
		t.Fatal("降级任务 Verified 必须为 false")
	}
}

// TestRepairPromptContainsPrevFiles 确认返工 prompt 回填上一版文件全文。
func TestRepairPromptContainsPrevFiles(t *testing.T) {
	task := &model.Task{ID: "t", Prompt: "原始任务：写一个工具"}
	prev := []artifactFile{{Path: "a.py", Body: "print('old')"}}
	rep := &model.VerifyReport{
		Pass: false, FailReason: "#1 命令失败",
		Outcomes: []model.VerifyOutcome{{Cmd: "python a.py", Exit: 2, Expect: 0, Stderr: "Traceback..."}},
	}
	got := repairPrompt(task, prev, rep, 2)
	for _, want := range []string{"第 2 轮", "原始任务：写一个工具", "python a.py", "Traceback", "print('old')", "ARTIFACTS"} {
		if !strings.Contains(got, want) {
			t.Fatalf("返工 prompt 缺少 %q", want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

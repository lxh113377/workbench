package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestMaterializeRejectsTinyOutput 是本轮生产数据审计发现的回归测试。
//
// 现场证据（2026-10-19 08:53，生产工作台）：77 篇 state=done 的产出里
// 有 9 篇不足 500 字节，最小 54 字节，内容是：
//
//	"我先查看当前工作区的结构和现有文件。"
//	"看起来当前不在 plan mode，我直接开始执行任务。先检查工作区中脚本的现状。"
//
// 这些是"声称要做但没做"的过程独白，却被判 done 并计入今日产出。
//
// 期望：过短产出在解析前就被判失败，落 verify.json，不进入 done。
func TestMaterializeRejectsTinyOutput(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-tiny-output"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "过短产出", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	task, _ := e.store.Get(taskID)

	for _, tiny := range []string{
		"我先查看当前工作区的结构和现有文件。",
		"看起来当前不在 plan mode，我直接开始执行任务。先检查工作区中脚本的现状。",
		"好的。",
	} {
		rep := e.materializeArtifacts(task, tiny, taskDir, config.DefaultConfig())
		if rep == nil {
			t.Fatalf("过短产出 %q 必须返回失败报告", tiny)
		}
		if rep.Pass {
			t.Fatalf("过短产出 %q 不应判通过", tiny)
		}
		if !strings.Contains(rep.FailReason, "过短") {
			t.Fatalf("失败原因应指出过短: %q", rep.FailReason)
		}
	}
	// 证据必须落盘
	if _, err := os.Stat(filepath.Join(taskDir, "verify.json")); err != nil {
		t.Fatal("过短产出的失败报告必须落盘 verify.json")
	}
}

// TestMaterializeAcceptsShortButReal 保证不误杀"短但有效"的成果。
//
// 200 字下限是保守值：一份带结论的要点清单通常 300-800 字，
// 这里验证 200 字以上的真实内容不会被拦。
func TestMaterializeAcceptsShortButReal(t *testing.T) {
	e, _, layout := newTestEngine(t)
	taskID := "task-short-real"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "短但有效", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	task, _ := e.store.Get(taskID)

	// 构造一份约 300 字的真实要点清单 + 合规产物块
	body := strings.Repeat("这里是正文段落，包含实质性结论与可执行建议。", 15)
	const fence = "```"
	answer := "# 要点清单\n\n" + body + "\n\n<<<ARTIFACTS>>>\n" +
		fence + "manifest\n" +
		`{"files":[{"path":"out.md"}],"verify":[{"cmd":"pwsh -NoProfile -NonInteractive -Command \"if (Test-Path out.md) { exit 0 } else { exit 1 }\"","expectExit":0,"timeout":30}]}` +
		"\n" + fence + "\n" + fence + "file:out.md\n# 成果\n" + body + "\n" + fence + "\n<<<END>>>\n"

	rep := e.materializeArtifacts(task, answer, taskDir, config.DefaultConfig())
	if rep == nil {
		t.Fatal("真实内容应被解析")
	}
	if !rep.Pass {
		t.Fatalf("短但真实的成果应通过验收: %s", rep.FailReason)
	}
}

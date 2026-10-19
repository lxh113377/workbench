package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/workspace"
)

// TestWorkspaceProjectModeUnchanged 覆盖"新功能不破坏原逻辑"：
// 默认配置下任务工作目录仍是数据根下的沙箱，engine 的工作目录视图保持 project。
func TestWorkspaceProjectModeUnchanged(t *testing.T) {
	e, _, layout := newTestEngine(t)
	if e.Workspace() == nil {
		t.Fatal("引擎应持有工作目录管理器")
	}
	if e.Workspace().IsExternal() {
		t.Fatal("默认必须是项目模式")
	}
	wd := e.WorkDirFor("task-x")
	want := filepath.Join(layout.DataRoot, "tasks", "task-x", "workspace")
	if !strings.EqualFold(wd, want) {
		t.Fatalf("项目模式工作目录 = %q, want %q", wd, want)
	}
	view := e.WorkspaceView()
	if view["mode"] != workspace.ModeProject {
		t.Fatalf("视图模式 = %v", view["mode"])
	}
	// 项目模式下 prompt 不注入工作目录上下文（保持与改造前一致的 prompt）
	if txt := e.workspaceContextText(); txt != "" {
		t.Fatalf("项目模式不应注入上下文: %q", txt)
	}
}

// TestWorkspaceSelectExternalPersists 覆盖切换链路：校验 → 配置落盘 → 引擎热生效 → 上下文注入。
func TestWorkspaceSelectExternalPersists(t *testing.T) {
	e, _, layout := newTestEngine(t)
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte("# 真实项目"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(target, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := e.SelectWorkspaceDir(target)
	if err != nil {
		t.Fatalf("切换失败: %v", err)
	}
	if !v.Allowed {
		t.Fatalf("校验应通过: %+v", v)
	}
	if !e.Workspace().IsExternal() {
		t.Fatal("引擎应切到 external")
	}
	if wd := e.WorkDirFor("task-y"); !strings.EqualFold(wd, target) {
		t.Fatalf("external 工作目录 = %q, want %q", wd, target)
	}
	// 配置落盘（重启后可恢复）
	var cfg config.Config
	if err := config.ReadJSON(layout.ConfigFile, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Workspace.Mode != workspace.ModeExternal || !strings.EqualFold(cfg.Workspace.Root, target) {
		t.Fatalf("配置未落盘: %+v", cfg.Workspace)
	}
	// 上下文注入：必须包含目录树与协议提示
	ctx := e.workspaceContextText()
	if !strings.Contains(ctx, target) || !strings.Contains(ctx, "README.md") || !strings.Contains(ctx, "ARTIFACTS") {
		t.Fatalf("上下文不完整:\n%s", ctx)
	}
	// 切回项目模式
	e.ResetWorkspaceProject()
	if e.Workspace().IsExternal() {
		t.Fatal("应切回项目模式")
	}
	if wd := e.WorkDirFor("task-y"); !strings.HasPrefix(wd, layout.DataRoot) {
		t.Fatalf("切回后工作目录应在数据根下: %q", wd)
	}
}

// TestWorkspaceRejectsForbidden 覆盖越权：系统目录/项目自身/磁盘根都被拒绝且不改变状态。
func TestWorkspaceRejectsForbidden(t *testing.T) {
	e, _, layout := newTestEngine(t)
	for _, bad := range []string{layout.Root, layout.DataRoot, filepath.VolumeName(layout.Root) + string(os.PathSeparator), "relative"} {
		if _, err := e.SelectWorkspaceDir(bad); err == nil {
			t.Fatalf("应拒绝目录: %s", bad)
		}
	}
	if e.Workspace().IsExternal() {
		t.Fatal("失败的选择不应改变模式")
	}
}

// TestMaterializeArtifactsRespectsGuard 覆盖产物落盘的护栏接线：
// 产物写入用户目录成功；不会误写任务沙箱。
func TestMaterializeArtifactsRespectsGuard(t *testing.T) {
	e, _, layout := newTestEngine(t)
	target := t.TempDir()
	if _, err := e.SelectWorkspaceDir(target); err != nil {
		t.Fatal(err)
	}
	taskID := "task-ws-art"
	if err := e.store.Create(&model.Task{ID: taskID, Title: "外部目录产物", State: model.StateCandidate}); err != nil {
		t.Fatal(err)
	}
	task, _ := e.store.Get(taskID)
	task.WorkRoot = target
	taskDir := layout.TaskDir(taskID)
	_ = os.MkdirAll(filepath.Join(taskDir, "workspace"), 0o755)
	fence := strings.Repeat(string(rune(96)), 3)
	manifest := "{\"files\":[{\"path\":\"docs/hello.md\"}],\"verify\":[{\"cmd\":\"pwsh -NoProfile -NonInteractive -Command \\\"if (Test-Path docs/hello.md) { exit 0 } else { exit 1 }\\\"\",\"expectExit\":0,\"timeout\":30}]}"
	final := "<<<ARTIFACTS>>>\n" + fence + "manifest\n" + manifest + "\n" + fence + "\n" + fence + "file:docs/hello.md\n# 外部目录成功\n" + fence + "\n<<<END>>>\n"
	rep := e.materializeArtifacts(task, final, taskDir, config.DefaultConfig())
	if rep == nil || !rep.Pass {
		t.Fatalf("外部目录产物应落盘并通过验收: %+v", rep)
	}
	got, err := os.ReadFile(filepath.Join(target, "docs", "hello.md"))
	if err != nil {
		t.Fatalf("文件未落到用户目录: %v", err)
	}
	if !strings.Contains(string(got), "外部目录成功") {
		t.Fatalf("内容不对: %q", string(got))
	}
	// 确认没有误写任务沙箱
	if _, err := os.Stat(filepath.Join(taskDir, "workspace", "docs", "hello.md")); err == nil {
		t.Fatal("external 模式下不应再写任务沙箱")
	}
}

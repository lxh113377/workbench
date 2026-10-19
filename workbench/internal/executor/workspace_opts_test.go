package executor

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestBuildArgsWithPatches 覆盖多覆盖层拼装：顺序稳定、空层被跳过、json 输出始终在最后。
func TestBuildArgsWithPatches(t *testing.T) {
	args := BuildArgsWithPatches("C:/app/lib/desktop-cli.js", "web-desktop", []string{"a.yml", "", "b.yml"})
	joined := strings.Join(args, " ")
	if strings.Count(joined, "--patch") != 2 {
		t.Fatalf("应跳过空覆盖层: %v", args)
	}
	if !strings.HasPrefix(joined, "--expose-internals") || !strings.Contains(joined, "--profile web-desktop") {
		t.Fatalf("基础参数缺失: %v", args)
	}
	if args[len(args)-2] != "--json" || args[len(args)-1] != "-" {
		t.Fatalf("json 输出必须在最后: %v", args)
	}
	// 单层便捷函数与多层等价
	one := BuildArgs("cli.js", "p", "only.yml")
	two := BuildArgsWithPatches("cli.js", "p", []string{"only.yml"})
	if strings.Join(one, "|") != strings.Join(two, "|") {
		t.Fatalf("单层拼装不一致: %v vs %v", one, two)
	}
}

// TestSandboxOverlayYAML 覆盖运行层补丁内容：必须是 YAML 且把 sandboxPolicy 设为 danger-full-access。
func TestSandboxOverlayYAML(t *testing.T) {
	if !strings.Contains(SandboxOverlayYAML, "id: sandboxPolicy") {
		t.Fatalf("补丁必须命中 sandboxPolicy 服务: %q", SandboxOverlayYAML)
	}
	if !strings.Contains(SandboxOverlayYAML, "mode: danger-full-access") {
		t.Fatalf("补丁必须放开沙箱: %q", SandboxOverlayYAML)
	}
}

// TestRunnerAllowAllSwitch 覆盖开关语义：默认关闭（项目模式），可显式打开（external 模式）。
func TestRunnerAllowAllSwitch(t *testing.T) {
	layout, err := config.BuildLayout(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	r := New(config.DefaultConfig().Executor, layout)
	if r.AllowAll() {
		t.Fatal("默认必须是关闭（与改造前一致）")
	}
	r.SetAllowAll(true)
	if !r.AllowAll() {
		t.Fatal("设置后应为打开")
	}
	r.SetAllowAll(false)
	if r.AllowAll() {
		t.Fatal("关闭应生效")
	}
}

// TestRunnerWorkspaceContextHook 覆盖上下文钩子：未注入返回空串，注入后原样返回。
func TestRunnerWorkspaceContextHook(t *testing.T) {
	r := New(config.DefaultConfig().Executor, config.Layout{})
	if got := r.workspaceContext(); got != "" {
		t.Fatalf("未注入时应为空: %q", got)
	}
	r.SetWorkspaceContext(func() string { return "## 工作目录上下文" })
	if got := r.workspaceContext(); got == "" {
		t.Fatal("注入后应返回上下文")
	}
}

// TestTaskWorkRootField 覆盖任务持久化字段：external 模式记录真实工作目录，旧数据零值安全。
func TestTaskWorkRootField(t *testing.T) {
	var zero model.Task
	if zero.WorkRoot != "" {
		t.Fatalf("零值应为空串（项目模式）: %q", zero.WorkRoot)
	}
	tk := model.Task{ID: "x", WorkRoot: "D:/proj"}
	if tk.WorkRoot != "D:/proj" {
		t.Fatalf("WorkRoot 未生效")
	}
}

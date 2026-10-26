package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkspaceCLILandsAuditRecords 钉住「CLI 与面板两条路径的审计同构」。
//
// 为什么需要（round 2 现场 + round 5 定位）：面板/引擎路径 engine.SelectWorkspaceDir 会走到
// Manager.SelectExternal / Manager.ResetProject，两者都落 workspace-audit.jsonl 的 select / reset 记录；
// 而 --workspace select|reset 这条 CLI 路径直写 cstore.Update、根本不经过 Manager ⇒ 审计里只有 validate 行。
// 实测后果：workbench-agent 的 workspace-audit.jsonl 在 select 成功之后仍只有两条 validate
// —— 「谁在何时把工作目录换成哪个」不可追溯。
//
// 判法是黑盒：在临时数据根里构建并运行真实二进制，跑一次 select 与一次 reset，审计文件必须各出现一条对应记录。
// 用真实二进制而非单元缝，是因为缺陷恰在「CLI 不经过 Manager」这条接线处。
func TestWorkspaceCLILandsAuditRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构建真实二进制")
	}
	root := t.TempDir()
	exe := filepath.Join(root, "wb-ws-audit.exe")
	build := hiddenTestCmd("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建受测二进制失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	auditPath := filepath.Join(root, "workbench-output", "workspace-audit.jsonl")

	// 靶子目录：另一个 t.TempDir()（与 exe 目录同级的兄弟目录），不落在工作台自身目录或其父目录内。
	target := t.TempDir()
	sel := hiddenTestCmd(exe, "--workspace", "select", target)
	if out, err := sel.CombinedOutput(); err != nil {
		t.Fatalf("--workspace select 失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	text := readAudit(t, auditPath)
	if !strings.Contains(text, `"op":"select"`) {
		t.Errorf("CLI select 未落 op=select 审计（与面板路径不同构）：\n%s", text)
	}
	if !strings.Contains(text, `"allowed":true`) {
		t.Errorf("CLI select 的审计行缺少 allowed:true：\n%s", text)
	}

	res := hiddenTestCmd(exe, "--workspace", "reset")
	if out, err := res.CombinedOutput(); err != nil {
		t.Fatalf("--workspace reset 失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if text2 := readAudit(t, auditPath); !strings.Contains(text2, `"op":"reset"`) {
		t.Errorf("CLI reset 未落 op=reset 审计（与面板路径不同构）：\n%s", text2)
	}
}

func readAudit(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到工作目录审计文件 %s: %v", path, err)
	}
	if len(raw) == 0 {
		t.Fatalf("工作目录审计文件为空：%s", path)
	}
	return string(raw)
}

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMainDispatchKeepsUIProbeFlag 守护 CLI 分发表不被并行编辑吞掉。
//
// 事故（2026-10-19 19:17 实测）：`case "--ui-probe"` 那一格在一次并发改写 main.go 时整段消失，
// 源码能编译、测试全绿，但主入口二进制跑 --ui-probe 会**静默回落**成 GUI 启动
// （打印「工作台已在运行：…」后退出 0）。前端门禁因此拿不到确定性通道，
// 整轮读数变成 unverified —— 属于「没测成被当成没问题」，比断言失败更危险。
// 这里用真实二进制做 black-box 判据：跑 `--ui-probe` 必须出现探针就绪字样，
// 且不得出现回落路径的文案。
func TestMainDispatchKeepsUIProbeFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构建真实二进制")
	}
	exe := filepath.Join(t.TempDir(), "wb-dispatch.exe")
	build := hiddenTestCmd("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建受测二进制失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	dir := t.TempDir()
	// 挑一个几乎不可能被占用的端口；探针只跑 5 秒，起完即退。
	// 探针实例也必须无头：裸 exec.Command 会让 CONSOLE 子系统的 EXE 闪黑窗。
	cmd := hiddenTestCmd(exe, "--ui-probe", "--dir", dir, "--port", "17987", "--seconds", "5")
	out, _ := cmd.CombinedOutput()
	text := string(out)

	if strings.Contains(text, "工作台已在运行") {
		t.Fatalf("--ui-probe 回落成 GUI 启动（分发表缺这一格）：%s", strings.TrimSpace(text))
	}
	if !strings.Contains(text, "UI_PROBE_READY") {
		t.Fatalf("--ui-probe 未进入探针通道（期望 UI_PROBE_READY）：%s", strings.TrimSpace(text))
	}
	if _, err := os.Stat(filepath.Join(dir, "ui-probe.url")); err != nil {
		t.Fatalf("探针未写出 ui-probe.url（前端门禁靠它拿 token）: %v", err)
	}
}

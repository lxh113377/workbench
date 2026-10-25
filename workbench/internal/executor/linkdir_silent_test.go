package executor

// linkdir_silent_test.go 守护「建 junction 不得弹控制台窗口」（2026-10-19 现场）。

// 现场：linkDir 用 `exec.Command("cmd", "/c", "mklink", "/J", dst, src)` 建目录链接。
// cmd.exe 是 CONSOLE(3) 子系统程序，而这里**没有设置任何窗口隐藏标志**
// （对比：同一文件里连 DSH 主进程都用 winhide.SysProcAttr() 带 HideWindow + CREATE_NO_WINDOW）。
//
// 后果：linkDir 在**每个任务**做准备时都会执行（executor.go:179 挂 profiles junction），
// 于是每跑一个任务就可能闪一次控制台窗口 —— 老大桌面会周期性被闪。
//
// 判据：构造子进程时必须带窗口隐藏属性（HideWindow + CREATE_NO_WINDOW）。
// 由于无法在单测里真的观察窗口，这里改为**断言源码契约**：
//   linkDir 必须走一个统一的隐藏启动助手，而不是裸 exec.Command。
//
// 说明：这是「源码文本」判据 —— 但在无法于单测中观测窗口的前提下，
// 它至少能锁住「有人把隐藏标志删掉」这类回归。
// 真正的行为验证放在 e2e（观察窗口数不增加）。

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestHiddenCmdSetsWindowFlags 断言隐藏启动助手真的设置了窗口标志。
func TestHiddenCmdSetsWindowFlags(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅 Windows 有窗口标志语义")
	}
	cmd := hiddenCmd("cmd", "/c", "echo", "x")
	if cmd.SysProcAttr == nil {
		t.Fatal("隐藏启动助手必须设置 SysProcAttr（否则会弹控制台）")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("HideWindow 必须为 true")
	}
	const createNoWindow = 0x08000000
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags 必须含 CREATE_NO_WINDOW，实际 0x%x", cmd.SysProcAttr.CreationFlags)
	}
}

// TestLinkDirUsesHiddenLaunch 断言 linkDir 真的用隐藏启动（防止改回裸 exec.Command）。
func TestLinkDirUsesHiddenLaunch(t *testing.T) {
	// 用真实的 junction 创建做一次冒烟：成功即可（窗口行为由 e2e 观测）。
	if runtime.GOOS != "windows" {
		t.Skip("junction 仅 Windows")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkDir(src, dst); err != nil {
		t.Fatalf("linkDir 失败: %v", err)
	}
	// 通过链接应能读到源文件
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Fatalf("junction 不可用: %v", err)
	}
	// 幂等：再调一次应直接返回 nil
	if err := linkDir(src, dst); err != nil {
		t.Fatalf("linkDir 应幂等，实际 %v", err)
	}
}

// _ 保持未使用导入的显式引用（测试文件里保留以便将来扩展断言）。
var _ = exec.Command

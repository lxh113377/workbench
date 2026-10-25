//go:build windows

package web

// autostart_silent_test.go 守护「开机自启相关的 spawn 全部隐藏窗口」（2026-10-19 现场）。
//
// 现场：reg.exe 是 CONSOLE(3) 程序，而 autostartEnabled 在每次打开设置面板时都会跑、
// setAutostart 是面板上的用户动作 —— 不隐藏就会持续闪黑窗。
//
// 判据：本文件里的 spawn 必须走 winhide.SysProcAttr（而不是裸 exec.Command）。
// 这里做一次真实调用冒烟 + 一次纯函数断言。
import (
	"testing"
	"workbench/internal/winhide"
)

func TestHiddenProcAttrHidesWindow(t *testing.T) {
	sp := winhide.SysProcAttr()
	if sp == nil {
		t.Fatal("winhide.SysProcAttr 必须返回非 nil（否则会弹控制台）")
	}
	if !sp.HideWindow {
		t.Fatal("HideWindow 必须为 true")
	}
	const createNoWindow = 0x08000000
	if sp.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags 必须含 CREATE_NO_WINDOW，实际 0x%x", sp.CreationFlags)
	}
}

// TestAutostartEnabledSmoke 冒烟：查询路径可跑且不 panic（窗口行为由标志保证）。
func TestAutostartEnabledSmoke(t *testing.T) {
	_, _ = autostartEnabled()
}

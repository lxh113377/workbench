//go:build windows

package winhide

import "testing"

// TestSysProcAttrHidesWindow 钉住「不弹黑框」这条保证的两个字段必须同时在场。
//
// 现场背景（2026-10-19 老大反复投诉）：reg.exe / icacls 这类 CONSOLE 子系统程序在 GUI 进程里
// 被起时，若不隐藏就会闪黑窗；而 round 170 实测到 internal/browser 的 Edge 路径**只写了
// HideWindow、没写 CreationFlags** —— 注释却写着「配合 CREATE_NO_WINDOW 保证不闪壳窗口」。
// 收敛到本包后，这两个字段只剩一处定义，本测试就是那处的守门人。
func TestSysProcAttrHidesWindow(t *testing.T) {
	sp := SysProcAttr()
	if sp == nil {
		t.Fatal("SysProcAttr 必须返回非 nil（否则会弹控制台）")
	}
	if !sp.HideWindow {
		t.Fatal("HideWindow 必须为 true")
	}
	const createNoWindow = 0x08000000 // CREATE_NO_WINDOW
	if sp.CreationFlags&createNoWindow == 0 {
		t.Fatalf("CreationFlags 必须含 CREATE_NO_WINDOW，实际 0x%x", sp.CreationFlags)
	}
}

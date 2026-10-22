//go:build windows

package web

import "syscall"

// hiddenProcAttr 返回「不弹控制台窗口」的进程属性（2026-10-19 现场修复）。
//
// 为什么 web 包也需要：setAutostart 会 spawn CONSOLE 子系统的 reg.exe，
// 而这是面板上的用户动作 —— 不隐藏就会闪黑窗。
//
// 与 dshprofile.hiddenProcAttr / shortcut.hiddenAttr / executor.hiddenSysProcAttr 同款语义；
// 这类「每个 spawn 点各写一份」的重复正是漏掉第四处的原因，长期应统一到一个公共包。
func hiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

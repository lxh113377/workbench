//go:build windows

package tray

import "syscall"

// hiddenProcAttr 让 icacls 这类辅助进程不弹控制台窗口。
// GUI 子系统的进程里再起控制台程序仍可能闪窗，必须显式隐藏。
func hiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

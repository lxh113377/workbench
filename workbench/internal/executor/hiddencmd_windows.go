//go:build windows

package executor

import "syscall"

// hiddenSysProcAttr 返回「不弹控制台窗口」的进程属性。
func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindowFlag}
}

//go:build !windows

package executor

import "syscall"

// hiddenSysProcAttr 在非 Windows 平台无窗口语义，返回零值。
func hiddenSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

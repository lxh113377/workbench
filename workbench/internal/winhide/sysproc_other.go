//go:build !windows

package winhide

import "syscall"

// SysProcAttr 在非 Windows 平台无窗口语义，返回 nil（与 syscall.SysProcAttr 的零值等价）。
func SysProcAttr() *syscall.SysProcAttr { return nil }

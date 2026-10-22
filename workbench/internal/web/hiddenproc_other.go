//go:build !windows

package web

import "syscall"

// hiddenProcAttr 在非 Windows 平台无窗口语义。
func hiddenProcAttr() *syscall.SysProcAttr { return nil }

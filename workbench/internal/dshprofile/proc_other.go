//go:build !windows

package dshprofile

import "syscall"

// hiddenProcAttr 在非 Windows 平台无操作。
func hiddenProcAttr() *syscall.SysProcAttr { return nil }

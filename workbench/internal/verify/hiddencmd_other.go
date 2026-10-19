//go:build !windows

package verify

import "syscall"

// hiddenCmdAttr 在非 Windows 平台无窗口语义（本就走伪终端/无控制台）。
func hiddenCmdAttr() *syscall.SysProcAttr { return nil }

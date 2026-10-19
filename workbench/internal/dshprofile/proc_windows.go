//go:build windows

package dshprofile

import "syscall"

// hiddenProcAttr 让子进程不弹黑框。
func hiddenProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

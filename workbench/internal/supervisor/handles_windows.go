//go:build windows

package supervisor

import (
	"syscall"
	"unsafe"
)

var (
	k32             = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrent  = k32.NewProc("GetCurrentProcess")
	procHandleCount = k32.NewProc("GetProcessHandleCount")
)

// handleCount 返回当前进程句柄数量（泄漏检测核心指标）。
func handleCount() int {
	h, _, _ := procGetCurrent.Call()
	var count uint32
	r, _, _ := procHandleCount.Call(h, uintptr(unsafe.Pointer(&count)))
	if r == 0 {
		return 0
	}
	return int(count)
}

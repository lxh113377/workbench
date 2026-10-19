//go:build windows

package single

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	k32            = syscall.NewLazyDLL("kernel32.dll")
	procLockFile   = k32.NewProc("LockFile")
	procUnlockFile = k32.NewProc("UnlockFile")
)

// lockFileExclusive 用 LockFile 对整个文件加独占锁（跨进程、跨会话有效）。
func lockFileExclusive(f *os.File) error {
	h := syscall.Handle(f.Fd())
	r, _, err := procLockFile.Call(uintptr(h), 0, 0, 1, 0)
	if r == 0 {
		return err
	}
	return nil
}

// unlockFile 解除文件锁。
func unlockFile(f *os.File) {
	if f == nil {
		return
	}
	h := syscall.Handle(f.Fd())
	procUnlockFile.Call(uintptr(h), 0, 0, 1, 0)
}

var _ = unsafe.Pointer(nil)

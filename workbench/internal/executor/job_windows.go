//go:build windows

package executor

import (
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW = kernel32.NewProc("CreateJobObjectW")
	procAssignProcess    = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJob     = kernel32.NewProc("TerminateJobObject")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
)

// jobSysProcAttr 让子进程独立于父进程的 Ctrl+C，但仍可被 Job Object 整树回收。
func jobSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

// job 包装一个 Windows Job Object，用于可靠地杀掉整棵子进程树
// （Electron 主进程 + 其 renderer/utility 子孙）。
type job struct {
	handle syscall.Handle
}

func attachJob(p *osProcess) *job {
	h, _, _ := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return &job{}
	}
	j := &job{handle: syscall.Handle(h)}
	ph, err := syscall.OpenProcess(0x1F0FFF, false, uint32(p.pid))
	if err == nil {
		procAssignProcess.Call(uintptr(j.handle), uintptr(ph))
		syscall.CloseHandle(ph)
	}
	return j
}

func (j *job) kill() {
	if j == nil || j.handle == 0 {
		return
	}
	procTerminateJob.Call(uintptr(j.handle), 1)
}

func (j *job) close() {
	if j == nil || j.handle == 0 {
		return
	}
	procCloseHandle.Call(uintptr(j.handle))
	j.handle = 0
}

var _ = unsafe.Pointer(nil)

//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// hiddenTestCmd 构造一个不会弹窗的测试子进程命令（Windows 实现）。
//
// HideWindow 处理"程序自己申请了窗口"的情况；CREATE_NO_WINDOW 处理
// "CONSOLE 子系统程序被分配控制台"的情况 —— 两者都要，缺一个就会闪窗。
//
// 平台拆分原因（2026-10-22 容器门禁实测）：syscall.SysProcAttr.HideWindow / CreationFlags
// 只在 Windows 存在，写在无 tag 的 _test.go 里会让 Linux 上 go vet ./... 直接红：
// vet: unknown field HideWindow in struct literal of type syscall.SysProcAttr
// 而本包门禁要求容器内 vet 全绿，故按平台各给一份实现（对调用方零改动）。
func hiddenTestCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	cmd.Env = headlessTestEnv()
	return cmd
}

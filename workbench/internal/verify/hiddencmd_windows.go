//go:build windows

package verify

import "syscall"

// hiddenCmdAttr 让验收命令不创建控制台窗口。
//
// 为什么需要：模型写的命令可能是 powershell/python 这类控制台程序，
// 默认派生会在用户桌面上闪一个黑窗；更要紧的是，只要存在控制台，
// PowerShell 的交互式安全提示就有地方弹出来并等人按键。
// CREATE_NO_WINDOW(0x08000000) + HideWindow 从源头取消这个载体。
func hiddenCmdAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

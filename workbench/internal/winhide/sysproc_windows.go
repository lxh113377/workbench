//go:build windows

package winhide

import "syscall"

// SysProcAttr 返回「不弹控制台窗口」的进程属性。
//
// 为什么要有这个包（2026-10-25 round 172，执行本仓《判据面与缺陷族》报告 §6.2 的建议）：
// 此前这条保证在 6 个包里被**复制了 12 份、名字 5 种**（hiddenProcAttr / hiddenAttr /
// hiddenSysProcAttr / hiddenCmdAttr / jobSysProcAttr），其中 internal/web 的注释自己就写着
// 「这类『每个 spawn 点各写一份』的重复正是漏掉第四处的原因，长期应统一到一个公共包」。
// 实测代价：internal/browser 的 Edge 启动路径漏了 CreationFlags 而没人发现（round 170），
// 同一轮还量出这条保证有 7 个不同名字。收敛到一处后，「漏掉第 N 处」这类缺陷就不再是概率问题。
func SysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

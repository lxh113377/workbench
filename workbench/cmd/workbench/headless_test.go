package main

// headless_test.go 提供「测试期间绝不弹窗」的统一约定（2026-10-21 老大指令：
// 「不要给我跳出来窗口，无头执行」）。
//
// 背景：本包的黑盒测试要构建**真实二进制**并起实例（静默契约、CLI 分发表、
// 退出缺口等）。此前这些 spawn 都是裸 exec.Command，于是：
//   1) 被测 EXE 若为 CONSOLE 子系统或走到打开面板分支，桌面会闪黑窗/弹 Edge；
//   2) t.TempDir 里的实例还会真的把面板推到用户桌面（老大反复反馈"又跳窗了"）。
// 这属于"测试污染桌面"，与被测逻辑无关，必须从环境层根除。
//
// 两条硬约定（本文件把约定变成可复用的助手，避免每处各写一份再漏）：
//   A. 所有子进程 spawn 必须走 hiddenTestCmd —— 隐藏窗口 + CREATE_NO_WINDOW；
//   B. 所有子进程环境必须带 WB_HEADLESS=1 —— browser.Open 直接不动手，
//      被测二进制即使走到"弹面板"分支也不会真的拉起 Edge。

import (
	"os"
	"os/exec"
	"syscall"
)

// hiddenTestCmd 构造一个不会弹窗的测试子进程命令。
//
// HideWindow 处理"程序自己申请了窗口"的情况；CREATE_NO_WINDOW 处理
// "CONSOLE 子系统程序被分配控制台"的情况 —— 两者都要，缺一个就会闪窗。
func hiddenTestCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	cmd.Env = headlessTestEnv()
	return cmd
}

// headlessTestEnv 返回带无头标记的完整环境（继承当前环境，只覆盖/追加开关）。
//
// 名字带 Test 与生产侧的 headlessEnv() 区分：前者返回 env 切片（喂给子进程），
// 后者返回 bool（判定本进程）。同名不同义是最容易埋坑的重复，坚决不共用一个名字。
func headlessTestEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if len(kv) > len(headlessEnvMarker) && kv[:len(headlessEnvMarker)+1] == headlessEnvMarker+"=" {
			out = append(out, headlessEnvMarker+"=1")
			replaced = true
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, headlessEnvMarker+"=1")
	}
	return out
}

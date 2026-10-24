//go:build !windows

package main

import "os/exec"

// hiddenTestCmd 构造一个不会弹窗的测试子进程命令（非 Windows 实现）。
//
// 非 Windows 平台没有"控制台窗口被分配/弹出"的语义，直接 exec.Command 即可；
// 但必须保持与 Windows 版**同名同签名**，否则 4 个调用方文件（main_dispatch_test.go /
// plan_selftest_dispatch_test.go / silent_contract_test.go / spawn_cleanup_test.go）
// 在容器内会因缺符号而编译失败。
func hiddenTestCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = headlessTestEnv()
	return cmd
}

//go:build !windows

package shortcut

import "fmt"

// Create 在非 Windows 平台不做任何事。
func Create() error {
	return fmt.Errorf("当前平台不支持创建 Windows 快捷方式")
}

// Guard 在非 Windows 平台恒为 false。
//
// 平台耦合修复（2026-10-19，Docker 干净容器实测直接抓到）：
// Guard 只在 shortcut_windows.go 里定义，而 cmd/workbench/main.go 无条件调用它，
// 于是 GOOS=linux 构建会报 undefined: shortcut.Guard —— Linux 侧编译不过。
// 快捷方式本身是 Windows 概念，非 Windows 一律视为“非正式安装位置”（跳过），
// 语义与 Windows 侧的非正式副本一致。
func Guard() bool { return false }

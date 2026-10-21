package main

// headless.go -- 无头执行的环境层判定（2026-10-21 老大指令：
// 「不要给我跳出来窗口，无头执行」）。
//
// 为什么放在**产品代码**而不是只在测试里：无头是一个真实的运行期需求，
// 不只测试需要。计划任务、CI、远程会话、无人值守机都要求"绝不弹窗"。
// 若只做在测试里，产品在那些场景仍然会往桌面推 Edge --app 窗口。
//
// 双层语义（与 openPanel / shouldOpenPanel 配合）：
//   代码层：shouldOpenPanel(silent) —— 本次启动**是否声明**要弹面板；
//   环境层：headlessEnv()          —— 本机**是否允许**弹面板。
// 只有两者都放行才真的弹。环境层覆盖代码层，是"绝不弹窗"的硬保证。

import (
	"os"
	"strings"
)

// headlessEnvMarker 是无头开关的变量名（internal/browser 读同一个名字）。
const headlessEnvMarker = "WB_HEADLESS"

// headlessEnv 判断本进程是否处于"无头执行"。
//
// 取值口径（任一成立即无头）：
//   - WB_HEADLESS = 1/true/yes/on（大小写不敏感）
//   - 标准 CI 环境变量存在（CI=true/1/任意非空）—— CI 上弹窗毫无意义且会挂住
//
// 注意：这里刻意**不**读命令行（--silent / --headless 由 runApp 解析后
// 通过 shouldOpenPanel 表达），保持单一职责：env 只管 env。
func headlessEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(headlessEnvMarker))) {
	case "1", "true", "yes", "on":
		return true
	}
	return strings.TrimSpace(os.Getenv("CI")) != ""
}

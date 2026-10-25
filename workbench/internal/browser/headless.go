package browser

// headless.go -- 「无头执行」的环境层判定（唯一实现，跨平台）。
//
// 为什么要把判定收成一份（2026-10-25 round 168 实测缺陷）：
// 此前 browser_windows.go 与 browser_other.go **各带一份 headless()**，两份都是硬编码白名单
//   1 / true / TRUE / True / yes / YES / on / ON
// —— 而注释写的是「大小写不敏感」。实测 `WB_HEADLESS=tRuE` / `Yes` / `On` / `" true "`
// 全都**不认**，于是测试与无人值守场景照样往桌面推窗口，正是注释说要防的那件事。
// 同族第二处：两份都把 `os.Getenv("CI") != ""` 当判据，**没有 TrimSpace** ——
// `CI="   "`（带空白的空值）会被判成无头，本地用户点快捷方式永远打不开面板
// （cmd/workbench 的 headless_env_test.go 已把这条点名为缺陷）。
// 根因是三份实现漂移：cmd/workbench/headless.go 的那份是**对的**且有测试，browser 的两份是复制后没跟上。
// 处置：判定收成本文件一份，两个平台文件只留 Open；口径与 cmd 侧逐字一致。

import (
	"os"
	"strings"
)

// headlessEnvMarker 是无头开关的变量名（与 cmd/workbench 读同一个名字）。
const headlessEnvMarker = "WB_HEADLESS"

// headlessEnvValue 判断标记值是否表达「无头」：大小写与前后空白都不敏感。
// 只认 1/true/yes/on 四种真值 —— 显式关闭值（0/false/no/off）与未知值一律 false，
// 否则「用户显式关掉无头」会变成「点了打不开」。
func headlessEnvValue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// headless 判断本进程是否处于「无头执行」（任一成立即无头）：
//   - WB_HEADLESS = 1/true/yes/on（大小写与空白不敏感）
//   - 标准 CI 环境变量**去掉空白后**仍非空（CI 上弹窗毫无意义且会把流水线挂住）
func headless() bool {
	if headlessEnvValue(os.Getenv(headlessEnvMarker)) {
		return true
	}
	return strings.TrimSpace(os.Getenv("CI")) != ""
}

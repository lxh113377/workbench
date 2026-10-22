package config

import (
	"workbench/internal/retention"
)

// 保留策略配置（2026-10-19 新增）。
//
// 单独放一个文件：避免与并行改动在 config.go 上反复冲突
// （现场实测同一次编辑被 3 次 "file changed" 拒绝）。
//
// 字段挂在 Config.Retention 上，JSON key = "retention"。

// RetentionConfig 是 retention.Policy 的配置别名（便于面板与 JSON 序列化）。
type RetentionConfig = retention.Policy

// DefaultRetentionConfig 返回默认保留策略。
func DefaultRetentionConfig() RetentionConfig { return retention.DefaultPolicy() }

// NormalizeRetention 补齐保留策略的零值（用户不配也能安全运行）。
func NormalizeRetention(c *Config) {
	if c == nil {
		return
	}
	if c.Retention.DSHHomeDays <= 0 {
		c.Retention.DSHHomeDays = retention.DefaultDSHHomeDays
	}
	if c.Retention.WorkspaceDays <= 0 {
		c.Retention.WorkspaceDays = retention.DefaultWorkspaceDays
	}
	if c.Retention.TaskDirDays <= 0 {
		c.Retention.TaskDirDays = retention.DefaultTaskDirDays
	}
	// 终态任务记录保留（2026-10-23 新增，治「queue.json 只增不减」）：
	// 实测 queue.json 69MB / 33,740 条（终态 33,733、在跑 7），快照全量序列化导致
	// 常驻 170MB / 高峰 752MB，而基线 residentMax 64MB。
	// 零值必须补默认：<=0 会让 PruneTasks 拿 0 天 / 0 条上限 —— 前者把所有终态当超龄
	// （危险），后者被 `cap > 0` 守卫跳过（无害但等于不生效）。两种都不可接受。
	if c.Retention.TerminalTaskDays <= 0 {
		c.Retention.TerminalTaskDays = retention.DefaultTerminalTaskDays
	}
	if c.Retention.MaxTerminalTasks <= 0 {
		c.Retention.MaxTerminalTasks = retention.DefaultMaxTerminalTasks
	}
}

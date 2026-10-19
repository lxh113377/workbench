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
}

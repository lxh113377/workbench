package config

// retention_normalize_test.go 守护「保留策略零值必须被 Normalize 补齐」（2026-10-19 现场）。
//
// 现场：config/retention.go 定义了 NormalizeRetention（注释：「补齐保留策略的零值（用户不配也能安全运行）」），
// 但 **Config.Normalize() 从不调用它** —— 实测把 config.json 的 retention 写成 0/0/0 后，
// `--prune` 读到的 policy 就是 0/0/0，而不是安全默认 3/30/90。
//
// 这与 TaskDirDays 那次同类：字段/函数都齐全，唯独**调用链断了一节**。
// 后果：0 天保留期意味着「什么都算过期」—— 比不回收更危险（可能立刻回收大量目录）。
import (
	"testing"
	"workbench/internal/retention"
)

// TestNormalizeFillsZeroRetention 断言 Normalize 会把 retention 零值补成默认。
func TestNormalizeFillsZeroRetention(t *testing.T) {
	c := DefaultConfig()
	// 模拟用户手改配置 / 旧版本迁移：把三个字段清零
	c.Retention.DSHHomeDays = 0
	c.Retention.WorkspaceDays = 0
	c.Retention.TaskDirDays = 0

	c.Normalize()

	if c.Retention.DSHHomeDays <= 0 {
		t.Fatalf("DSHHomeDays 零值应被补齐，实际 %d（0 意味着「全部立即过期」，比不回收更危险）", c.Retention.DSHHomeDays)
	}
	if c.Retention.WorkspaceDays <= 0 {
		t.Fatalf("WorkspaceDays 零值应被补齐，实际 %d", c.Retention.WorkspaceDays)
	}
	if c.Retention.TaskDirDays <= 0 {
		t.Fatalf("TaskDirDays 零值应被补齐，实际 %d", c.Retention.TaskDirDays)
	}
}

// TestNormalizeKeepsExplicitRetention 断言**用户显式设过的值必须保留**（不能一律覆盖成默认）。
func TestNormalizeKeepsExplicitRetention(t *testing.T) {
	c := DefaultConfig()
	c.Retention.DSHHomeDays = 7
	c.Retention.WorkspaceDays = 60
	c.Retention.TaskDirDays = 365

	c.Normalize()

	if c.Retention.DSHHomeDays != 7 {
		t.Fatalf("用户显式设置的 DSHHomeDays=7 不应被覆盖，实际 %d", c.Retention.DSHHomeDays)
	}
	if c.Retention.WorkspaceDays != 60 {
		t.Fatalf("用户显式设置的 WorkspaceDays=60 不应被覆盖，实际 %d", c.Retention.WorkspaceDays)
	}
	if c.Retention.TaskDirDays != 365 {
		t.Fatalf("用户显式设置的 TaskDirDays=365 不应被覆盖，实际 %d", c.Retention.TaskDirDays)
	}
}

// TestCheckRetentionWiredIntoNormalize 断言调用链没断（防再次「函数在但没人调」）。
func TestCheckRetentionWiredIntoNormalize(t *testing.T) {
	// 用一个哨兵：把 retention 设成非法负值，Normalize 后必须变成正数。
	c := DefaultConfig()
	c.Retention.DSHHomeDays = -1
	c.Retention.WorkspaceDays = -1
	c.Retention.TaskDirDays = -1
	c.Normalize()
	if c.Retention.DSHHomeDays <= 0 || c.Retention.WorkspaceDays <= 0 || c.Retention.TaskDirDays <= 0 {
		t.Fatalf("负值 retention 必须被补齐（说明 Normalize 未调用 NormalizeRetention），实际 %+v", c.Retention)
	}
}

// TestNormalizeRetentionFillsTerminalTaskLimits 守护新增的终态记录保留字段。
//
// 为什么单独一条（2026-10-23）：这两个字段的零值语义是**危险的** ——
// TerminalTaskDays<=0 会让 PruneTasks 把所有终态当「超龄」（删光历史），
// MaxTerminalTasks<=0 则被 `cap > 0` 守卫跳过（等于策略不生效）。
// 两者都必须被 Normalize 补齐成默认值，不能靠调用方自觉。
func TestNormalizeRetentionFillsTerminalTaskLimits(t *testing.T) {
	for _, zero := range []int{0, -1, -100} {
		c := DefaultConfig()
		c.Retention.TerminalTaskDays = zero
		c.Retention.MaxTerminalTasks = zero
		c.Normalize()
		if c.Retention.TerminalTaskDays != retention.DefaultTerminalTaskDays {
			t.Fatalf("TerminalTaskDays=%d 应补齐为 %d，实际 %d（否则所有终态会被当超龄删光）",
				zero, retention.DefaultTerminalTaskDays, c.Retention.TerminalTaskDays)
		}
		if c.Retention.MaxTerminalTasks != retention.DefaultMaxTerminalTasks {
			t.Fatalf("MaxTerminalTasks=%d 应补齐为 %d，实际 %d（否则上限策略不生效）",
				zero, retention.DefaultMaxTerminalTasks, c.Retention.MaxTerminalTasks)
		}
	}
}

// TestNormalizeRetentionKeepsExplicitTerminalLimits 反向判据：显式设置不得被覆盖。
func TestNormalizeRetentionKeepsExplicitTerminalLimits(t *testing.T) {
	c := DefaultConfig()
	c.Retention.TerminalTaskDays = 30
	c.Retention.MaxTerminalTasks = 20000
	c.Normalize()
	if c.Retention.TerminalTaskDays != 30 {
		t.Fatalf("显式 TerminalTaskDays 被覆盖: %d", c.Retention.TerminalTaskDays)
	}
	if c.Retention.MaxTerminalTasks != 20000 {
		t.Fatalf("显式 MaxTerminalTasks 被覆盖: %d", c.Retention.MaxTerminalTasks)
	}
}

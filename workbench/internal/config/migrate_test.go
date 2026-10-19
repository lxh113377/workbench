package config

import "testing"

// TestMigrationEnablesP3 是本轮现场缺陷的回归测试。
//
// 现场（2026-10-19）：生产 config.json 里 appendProtocol=false、repairRounds=0，
// 导致 P3 的解析/落盘/验收链路一次都没跑过（275 个任务 verified=0、degraded=0）。
//
// 第一版迁移只看 schemaVersion<3，但现场版本号已被别的模块提前写成 3，
// 迁移永远不命中。因此判据改为「字段零值 + 版本上限」。
func TestMigrationEnablesP3(t *testing.T) {
	d := DefaultConfig()

	// 模拟旧安装：字段全零（版本号可能是 3，因为别的模块先升过）
	for _, ver := range []int{1, 2, 3} {
		c := DefaultConfig()
		c.SchemaVersion = ver
		c.Executor.AppendProtocol = false
		c.Executor.RepairRounds = 0
		c.Normalize()

		if !c.Executor.AppendProtocol {
			t.Fatalf("schemaVersion=%d 升级后 appendProtocol 必须为 true（否则 P3 永不生效）", ver)
		}
		if c.Executor.RepairRounds != d.Executor.RepairRounds {
			t.Fatalf("schemaVersion=%d 升级后 repairRounds = %d, want %d", ver, c.Executor.RepairRounds, d.Executor.RepairRounds)
		}
		if c.SchemaVersion != SchemaVersion {
			t.Fatalf("schemaVersion=%d 升级后 = %d, want %d", ver, c.SchemaVersion, SchemaVersion)
		}
	}
}

// TestMigrationRespectsExplicitOff 已是未来版本（用户显式关闭后版本被提升）不得覆盖。
func TestMigrationRespectsExplicitOff(t *testing.T) {
	c := DefaultConfig()
	c.SchemaVersion = SchemaVersion // 已是最新
	c.Executor.AppendProtocol = false
	c.Executor.RepairRounds = 1
	c.Normalize()

	if c.Executor.AppendProtocol {
		t.Fatal("已是最新版本时，用户显式关闭的 appendProtocol 被覆盖了")
	}
	if c.Executor.RepairRounds != 1 {
		t.Fatalf("已是最新版本时，用户设置的 repairRounds 被覆盖: %d", c.Executor.RepairRounds)
	}
}

// TestMigrationV1AlsoFixesWatchdog 从 v1 一步升级要同时完成看门狗迁移。
func TestMigrationV1AlsoFixesWatchdog(t *testing.T) {
	c := DefaultConfig()
	c.SchemaVersion = 1
	c.Executor.StallSeconds = 180
	c.Executor.AppendProtocol = false
	c.Executor.RepairRounds = 0
	c.Normalize()

	if c.Executor.StallSeconds != 420 {
		t.Fatalf("v1 升级后 stallSeconds = %d, want 420", c.Executor.StallSeconds)
	}
	if !c.Executor.AppendProtocol {
		t.Fatal("v1 升级后 appendProtocol 必须为 true")
	}
}

package config

// 方案阶段（双角色闭环）配置的迁移与规范化测试。
//
// 立规原话（老大 2026-10-21）：
//   「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」
//
// 这一组用例回答一个具体问题：**老安装升级后，双角色闭环会不会真的生效**。
// 历史教训（v3 P3 协议那次）：字段与迁移都写了，但迁移判据被提前抬升的版本号绕过，
// 结果 appendProtocol 永远是 false、repairRounds 永远是 0 —— 功能形同虚设。

import "testing"

// TestNormalizePlanEnablesForOldSchema 断言：升级路径（schema<6）必须打开方案阶段。
func TestNormalizePlanEnablesForOldSchema(t *testing.T) {
	for _, ver := range []int{1, 2, 3, 4, 5} {
		var p PlanConfig // 零值 = 老配置里没有这个段
		NormalizePlan(&p, ver)
		if !p.Enabled {
			t.Fatalf("schema=%d 升级后方案阶段必须开启（否则双角色闭环永不生效）", ver)
		}
		if !p.AppendPlanToPrompt {
			t.Fatalf("schema=%d 升级后 appendPlanToPrompt 必须为 true", ver)
		}
		if p.PlannerTier != "quality" {
			t.Fatalf("schema=%d 默认方案档位 = %q, want quality", ver, p.PlannerTier)
		}
		if p.PlannerMaxMinutes != 8 {
			t.Fatalf("schema=%d 默认方案超时 = %d, want 8", ver, p.PlannerMaxMinutes)
		}
	}
}

// TestNormalizePlanRespectsExplicitOff 断言：用户显式关闭不会被迁移强行打开。
func TestNormalizePlanRespectsExplicitOff(t *testing.T) {
	p := PlanConfig{Enabled: false, PlannerTier: "fast", AppendPlanToPrompt: false}
	NormalizePlan(&p, SchemaVersion) // 当前版本 = 用户显式配置
	if p.Enabled {
		t.Fatal("当前版本配置里显式关闭的方案阶段不得被打开")
	}
	if p.AppendPlanToPrompt {
		t.Fatal("当前版本配置里显式关闭的 appendPlanToPrompt 不得被打开")
	}
	if p.PlannerTier != "fast" {
		t.Fatalf("用户设置的档位被覆盖: %q", p.PlannerTier)
	}
}

// TestNormalizePlanClampsOutOfRange 断言越界值被钳制（面板可随意提交）。
func TestNormalizePlanClampsOutOfRange(t *testing.T) {
	p := PlanConfig{PlannerTier: "unknown-tier", PlannerMaxMinutes: 999, ReplanRounds: -3}
	NormalizePlan(&p, SchemaVersion)
	if p.PlannerTier != "quality" {
		t.Fatalf("非法档位应回落 quality，实际 %q", p.PlannerTier)
	}
	if p.PlannerMaxMinutes != 8 {
		t.Fatalf("超范围超时应回落 8，实际 %d", p.PlannerMaxMinutes)
	}
	// 负数/零都按"未配置"补默认值（不是钳到 0）：
	// 钳到 0 会让 ReplanRounds=0 变成"关闭重新规划"的隐形开关，
	// 与 plan.enabled 语义重叠 —— 关双角色闭环只应有一个权威开关。
	if p.ReplanRounds != 2 {
		t.Fatalf("负数重规划轮数应补默认 2，实际 %d", p.ReplanRounds)
	}
	p2 := PlanConfig{ReplanRounds: 99}
	NormalizePlan(&p2, SchemaVersion)
	if p2.ReplanRounds != 5 {
		t.Fatalf("过大重规划轮数应钳到 5，实际 %d", p2.ReplanRounds)
	}
}

// TestNormalizeRaisesSchemaToSix 断言整份配置的迁移把版本推到 6 且方案段就位。
func TestNormalizeRaisesSchemaToSix(t *testing.T) {
	c := Config{SchemaVersion: 5}
	c.Normalize()
	if c.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", c.SchemaVersion, SchemaVersion)
	}
	if !c.Plan.Enabled {
		t.Fatal("v5 升级到 v6 后方案阶段必须开启")
	}
	if c.Plan.ReplanRounds <= 0 {
		t.Fatalf("重规划轮数应按默认值补齐，实际 %d", c.Plan.ReplanRounds)
	}
}

// TestDefaultConfigEnablesPlan 断言出厂默认就带双角色闭环。
func TestDefaultConfigEnablesPlan(t *testing.T) {
	d := DefaultConfig()
	if !d.Plan.Enabled {
		t.Fatal("默认配置必须启用方案阶段")
	}
	if d.Plan.PlannerTier != "quality" {
		t.Fatalf("默认方案档位 = %q, want quality（思考用最好的模型）", d.Plan.PlannerTier)
	}
	if d.Plan.ReplanRounds >= d.Executor.RepairRounds {
		t.Fatalf("重规划轮数(%d) 应小于返工轮数(%d)：先用便宜手段", d.Plan.ReplanRounds, d.Executor.RepairRounds)
	}
}

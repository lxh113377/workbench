package config

import (
	"strings"
	"testing"
)

// 跨字段一致性校验（2026-10-22 对标 river client.go:528-534）。
//
// 失效形态与单字段越界不同：后者是「这个值不合法」，前者是
// 「每个值都合法、合在一起不 work」—— 更隐蔽（每个字段都过得了自己的钳制），
// 也更难排查（现场看不到任何错误）。
//
// 判据强度说明：全部断言 **Normalize() 之后的字段值 + Warnings 内容**，
// 而不是在测试里重写一遍判据逻辑。重写版在变异测试下照样绿。

// helper：造一份合法基线再只破坏目标字段。
func crossBase() Config {
	c := DefaultConfig()
	c.Normalize()
	return c
}

// TestCrossFieldStallBelowTimeout 钉死第 1 条：看门狗必须能先于硬超时触发。
//
// 死配置形态：StallSeconds >= DefaultTimeout*60 时，任务总被硬超时先接管，
// stalled 分类永不可达 —— 面板永远看不到「卡死」。
func TestCrossFieldStallBelowTimeout(t *testing.T) {
	c := crossBase()
	c.Executor.DefaultTimeout = 30          // 30min = 1800s
	c.Executor.StallSeconds = 3600          // >= 1800，死配置
	c.Warnings = nil
	c.Normalize()
	if c.Executor.StallSeconds >= c.Executor.DefaultTimeout*60 {
		t.Fatalf("StallSeconds=%d 仍不小于超时 %ds：stalled 仍是死配置",
			c.Executor.StallSeconds, c.Executor.DefaultTimeout*60)
	}
	var found bool
	for _, w := range c.Warnings {
		if strings.Contains(w, "executor.stallSeconds") {
			found = true
			if !strings.Contains(w, "死配置") {
				t.Fatalf("告警应说明这是死配置，got %q", w)
			}
		}
	}
	if !found {
		t.Fatalf("跨字段修正必须登记 Warnings，got %v", c.Warnings)
	}
}

// TestCrossFieldStallBelowTimeoutDoesNotFireOnDefault 是**反向判据**：
// 默认配置（420s < 1800s）本就自洽，不得被改动、不得产生告警。
// 没有这条，一份「宁可多改」的实现会把默认值也改掉（本轮第一次写反方向时，
// 正是它把默认 420 抬到 1860 才被既有测试抓出的）。
func TestCrossFieldStallBelowTimeoutDoesNotFireOnDefault(t *testing.T) {
	c := crossBase()
	c.Warnings = nil
	c.Normalize()
	if c.Executor.StallSeconds != 420 {
		t.Fatalf("默认 StallSeconds 被改动：%d, want 420（自洽配置不应被跨字段段触碰）", c.Executor.StallSeconds)
	}
	for _, w := range c.Warnings {
		if strings.Contains(w, "executor.stallSeconds") {
			t.Fatalf("自洽配置不得产生 stall 告警，got %q", w)
		}
	}
}

// TestCrossFieldReplanBelowRepair 钉死第 2 条：先用便宜的返工，后用贵的重规划。
func TestCrossFieldReplanBelowRepair(t *testing.T) {
	c := crossBase()
	c.Executor.RepairRounds = 3
	c.Plan.Enabled = true
	c.Plan.ReplanRounds = 5 // >= 返工轮数，顺序被推翻
	c.Warnings = nil
	c.Normalize()
	if c.Plan.ReplanRounds >= c.Executor.RepairRounds {
		t.Fatalf("ReplanRounds=%d 仍不小于 RepairRounds=%d", c.Plan.ReplanRounds, c.Executor.RepairRounds)
	}
	var found bool
	for _, w := range c.Warnings {
		if strings.Contains(w, "plan.replanRounds") {
			found = true
		}
	}
	if !found {
		t.Fatalf("跨字段修正必须登记 Warnings，got %v", c.Warnings)
	}
}

// TestCrossFieldReplanGuardOnZeroRepair 验证守卫：RepairRounds 为零时不得乱钳。
// 零值配置下该字段尚未补默认，比较没有意义；钳下去会把 NormalizePlan
// 刚补好的默认值打回 0（本轮实测被 TestNormalizeRaisesSchemaToSix 抓出的回归）。
func TestCrossFieldReplanGuardOnZeroRepair(t *testing.T) {
	c := Config{} // 全零配置，走默认迁移路径
	c.Normalize()
	if c.Executor.RepairRounds <= 0 {
		t.Fatalf("零值配置应补齐 RepairRounds，got %d", c.Executor.RepairRounds)
	}
	if !c.Plan.Enabled || c.Plan.ReplanRounds <= 0 {
		t.Fatalf("零值配置应开启方案并补齐 ReplanRounds，got enabled=%v replan=%d",
			c.Plan.Enabled, c.Plan.ReplanRounds)
	}
	if c.Plan.ReplanRounds >= c.Executor.RepairRounds {
		t.Fatalf("补齐后仍应满足 replan < repair，got %d >= %d", c.Plan.ReplanRounds, c.Executor.RepairRounds)
	}
}

// TestCrossFieldPlannerTimeoutBelowTaskTimeout 钉死第 3 条：
// 方案调用超时必须短于整个任务超时，否则方案阶段必然超时、执行阶段拿不到时间。
func TestCrossFieldPlannerTimeoutBelowTaskTimeout(t *testing.T) {
	c := crossBase()
	c.Executor.DefaultTimeout = 10 // 10min
	c.Plan.Enabled = true
	c.Plan.PlannerMaxMinutes = 30 // 比任务超时还长
	c.Warnings = nil
	c.Normalize()
	if c.Plan.PlannerMaxMinutes >= c.Executor.DefaultTimeout {
		t.Fatalf("PlannerMaxMinutes=%d 仍不小于任务超时 %dmin：方案阶段必然超时",
			c.Plan.PlannerMaxMinutes, c.Executor.DefaultTimeout)
	}
	if c.Plan.PlannerMaxMinutes < 1 {
		t.Fatalf("钳制后不得小于 1 分钟，got %d", c.Plan.PlannerMaxMinutes)
	}
}

// 为什么没有「每日配额必须大于并发」的用例（2026-10-22 本轮实测）：
//
// 该判据写了又被撤：它把"一天只产 1 条"这种**刻意收紧**的用户选择
// 当作矛盾配置抬到 200，被两条既有测试当场判红
// （TestDailyCapStopsAutonomySupply / TestStallReasonDailyCap）。
// 跨字段校验只修「功能不可达」的矛盾，不替用户调参。
// 这条留作反例记录，防止后人再写一遍。

// TestCrossFieldWarningsClearedByCanonicalCompare 钉死「Warnings 不入盘比较」。
// 若不抹平，磁盘（无该字段）与内存（有告警）永远有差异，
// 每次启动都会白写一次磁盘，违反「已迁移配置不应无谓重写」。
func TestCrossFieldWarningsClearedByCanonicalCompare(t *testing.T) {
	c := crossBase()
	c.Warnings = []string{"executor.stallSeconds: 测试"}
	canonicalizeForCompare(&c)
	if len(c.Warnings) != 0 {
		t.Fatalf("canonicalizeForCompare 应抹平 Warnings，got %v", c.Warnings)
	}
}

// TestCrossFieldCleanConfigNoWarnings 是总的反向判据：
// 一份完全自洽的配置不得产生任何跨字段告警（否则告警变成噪音，真问题被淹没）。
func TestCrossFieldCleanConfigNoWarnings(t *testing.T) {
	c := crossBase()
	c.Warnings = nil
	c.Normalize()
	if len(c.Warnings) != 0 {
		t.Fatalf("自洽配置不应有任何跨字段告警，got %v", c.Warnings)
	}
}

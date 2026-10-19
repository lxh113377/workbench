package engine

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/ratelimit"
)

// TestConfigHasRateLimitDefaults 保证默认配置带请求速率闸门。
//
// 2026-10-19 生产事故：08:00-08:04 每分钟 45-70 次模型请求打爆免费车道，
// 429 持续 50 分钟。根因是没有全局请求速率限制 —— 并发数只限制
// "同时跑几个任务"，不限制"每秒发多少请求"。
func TestConfigHasRateLimitDefaults(t *testing.T) {
	c := config.DefaultConfig()
	if c.Executor.RequestsPerMinute <= 0 {
		t.Fatal("默认配置必须有限速（requestsPerMinute > 0）")
	}
	if c.Executor.RequestBurst < 1 {
		t.Fatal("默认配置必须有令牌桶容量（requestBurst >= 1）")
	}
	// 20/分钟 ≈ 每 3 秒一次请求；这是 24/7 产出与车道容量之间的折中
	if c.Executor.RequestsPerMinute > 60 {
		t.Fatalf("默认限速过宽（%d/分钟），仍可能打爆车道", c.Executor.RequestsPerMinute)
	}
}

// TestRateLimitMigration 旧配置（无这两个字段）必须被自动补上。
func TestRateLimitMigration(t *testing.T) {
	c := config.DefaultConfig()
	c.SchemaVersion = 4
	c.Executor.RequestsPerMinute = 0 // 旧配置的零值 = 不限速
	c.Executor.RequestBurst = 0
	c.Normalize()

	if c.Executor.RequestsPerMinute <= 0 {
		t.Fatal("v4 配置升级后 requestsPerMinute 必须被补上")
	}
	if c.Executor.RequestBurst < 1 {
		t.Fatal("v4 配置升级后 requestBurst 必须被补上")
	}
	if c.SchemaVersion != config.SchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", c.SchemaVersion, config.SchemaVersion)
	}
}

// TestLimiterFromConfig 配置值能正确映射到限流器。
func TestLimiterFromConfig(t *testing.T) {
	c := config.DefaultConfig()
	lim := ratelimit.New(float64(c.Executor.RequestsPerMinute)/60.0, c.Executor.RequestBurst)
	if lim == nil {
		t.Fatal("有效配置应构造出限流器")
	}
	s := lim.Snapshot()
	if s.RatePerSecond <= 0 || s.Burst <= 0 {
		t.Fatalf("限流器状态异常: %+v", s)
	}
	// 关闭限速时应得到 nil
	if ratelimit.New(0, 3) != nil {
		t.Fatal("requestsPerMinute=0 应得到 nil 限流器（不限速）")
	}
}

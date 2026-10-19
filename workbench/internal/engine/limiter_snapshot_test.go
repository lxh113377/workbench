package engine

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/ratelimit"
)

// TestRateLimiterSnapshotReflectsWiring 覆盖限速闸门的可观测性。
//
// 2026-10-19 事故教训：闸门本身不可观测，就等于又一次黑盒 ——
// 「限速是否真的在工作」必须能从面板/API 读到，而不是靠翻日志猜。
func TestRateLimiterSnapshotReflectsWiring(t *testing.T) {
	e, _, _ := newTestEngine(t)

	// 未装闸门：应返回零值（表示不限速）
	s := e.RateLimiterSnapshot()
	if s.RatePerSecond != 0 || s.Burst != 0 {
		t.Fatalf("未装闸门时快照应为零值: %+v", s)
	}

	// 装上闸门后：快照必须如实反映速率与容量
	inner, ok := e.Runner().(interface{ SetLimiter(*ratelimit.Limiter) })
	if !ok {
		t.Skip("当前执行器不支持 SetLimiter")
	}
	inner.SetLimiter(ratelimit.New(0.5, 4)) // 30/分钟，桶容量 4
	s = e.RateLimiterSnapshot()
	if s.RatePerSecond != 0.5 {
		t.Fatalf("RatePerSecond = %v, want 0.5", s.RatePerSecond)
	}
	if s.Burst != 4 {
		t.Fatalf("Burst = %d, want 4", s.Burst)
	}
}

// TestRateLimiterConfigMatchesDefault 保证默认配置与该测试的期望一致。
func TestRateLimiterConfigMatchesDefault(t *testing.T) {
	c := config.DefaultConfig()
	if c.Executor.RequestsPerMinute != 20 {
		t.Fatalf("默认 requestsPerMinute = %d, want 20（约 3 秒一次请求）", c.Executor.RequestsPerMinute)
	}
	if c.Executor.RequestBurst != 3 {
		t.Fatalf("默认 requestBurst = %d, want 3", c.Executor.RequestBurst)
	}
}

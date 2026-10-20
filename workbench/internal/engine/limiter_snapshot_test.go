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

	// 新建引擎已按配置装好闸门（D9 修复，2026-10-20）：改造前这里断言「未装闸门=零值」，
	// 把「生产从未接线」这个缺陷当成了正确行为 —— requestsPerMinute=20 是装饰品，
	// 面板读数为零恰好掩盖了「限速没在工作」。现在读数必须非零。
	s := e.RateLimiterSnapshot()
	if s.RatePerSecond <= 0 {
		t.Fatalf("装配后限速快照不应为零值: %+v（零值=闸门又没接上，D9 回归）", s)
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

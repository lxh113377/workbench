package engine

import (
	"testing"
	"time"
)

// TestSetPoolCooldownEscalates 覆盖限流的指数退避：
// 连续 429 时冷却必须递增（5 → 10 → 20 → 上限 30 分钟），否则会在限流窗口内反复撞墙。
func TestSetPoolCooldownEscalates(t *testing.T) {
	e, _, _ := newTestEngine(t)
	now := time.Now().UnixMilli()

	e.setPoolCooldown(rateLimitCooldownSeconds)
	first := e.poolWaitUntil.Load()
	if first <= now {
		t.Fatalf("首次限流应设置未来闸门，实际 %d", first)
	}
	d1 := first - now
	if d1 < int64(rateLimitCooldownSeconds)*1000-5000 || d1 > int64(rateLimitCooldownSeconds)*1000+5000 {
		t.Fatalf("首次冷却应约 %d 秒，实际约 %d 秒", rateLimitCooldownSeconds, d1/1000)
	}

	e.setPoolCooldown(rateLimitCooldownSeconds)
	second := e.poolWaitUntil.Load()
	if second <= first {
		t.Fatalf("连续第二次限流冷却应更长: first=%d second=%d", first, second)
	}
}

// TestSetPoolCooldownOnlyExtends 保证并发限流不会互相缩短等待。
func TestSetPoolCooldownOnlyExtends(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.poolWaitUntil.Store(time.Now().Add(10 * time.Minute).UnixMilli())
	before := e.poolWaitUntil.Load()
	// 用更短的冷却调用：不应把闸门拉近
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 0
	e.rateLimit.lastAt = 0
	e.rateLimitMu.Unlock()
	e.setPoolCooldown(60)
	after := e.poolWaitUntil.Load()
	if after < before {
		t.Fatalf("闸门只能推进不能后退: before=%d after=%d", before, after)
	}
}

// TestClearRateLimit 保证成功一次后计数清零（车道恢复后重新从 5 分钟起）。
func TestClearRateLimit(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.clearRateLimit()
	e.rateLimitMu.Lock()
	consec := e.rateLimit.consecutive
	e.rateLimitMu.Unlock()
	if consec != 0 {
		t.Fatalf("成功后 consecutive = %d, want 0", consec)
	}
}

// TestRateLimitCapped 保证退避封顶，不会无限增长导致系统永久停摆。
func TestRateLimitCapped(t *testing.T) {
	e, _, _ := newTestEngine(t)
	for i := 0; i < 20; i++ {
		e.setPoolCooldown(rateLimitCooldownSeconds)
	}
	now := time.Now().UnixMilli()
	wait := (e.poolWaitUntil.Load() - now) / 1000
	if wait > int64(rateLimitBackoffCapSeconds)+10 {
		t.Fatalf("冷却封顶失效: %d 秒 > 上限 %d 秒", wait, rateLimitBackoffCapSeconds)
	}
}

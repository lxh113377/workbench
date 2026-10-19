package engine

import (
	"testing"
	"time"

	"workbench/internal/model"
)

// TestRateLimitStatusNotLimited 未限流时应是干净状态。
func TestRateLimitStatusNotLimited(t *testing.T) {
	e, _, _ := newTestEngine(t)
	st := e.RateLimitStatus()
	if st.Limited {
		t.Fatal("未限流时 Limited 应为 false")
	}
	if st.LastReason != "" {
		t.Fatalf("未限流时不应有理由: %q", st.LastReason)
	}
	if e.LimitedSuffix() != "" {
		t.Fatalf("未限流时后缀应为空: %q", e.LimitedSuffix())
	}
}

// TestRateLimitStatusLimited 限流中必须能自证（面板依赖它解释"为什么 0 产出"）。
func TestRateLimitStatusLimited(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.setPoolCooldown(rateLimitCooldownSeconds)

	st := e.RateLimitStatus()
	if !st.Limited {
		t.Fatal("限流冷却期间 Limited 应为 true")
	}
	if st.RemainingSeconds <= 0 || st.RemainingSeconds > rateLimitCooldownSeconds+5 {
		t.Fatalf("剩余秒数异常: %d", st.RemainingSeconds)
	}
	if st.Consecutive != 1 {
		t.Fatalf("consecutive = %d, want 1", st.Consecutive)
	}
	if st.LastReason == "" {
		t.Fatal("限流中必须给出人类可读理由（面板 tooltip 依赖它）")
	}
	if sfx := e.LimitedSuffix(); sfx == "" {
		t.Fatal("限流中后缀不应为空")
	}
}

// TestRateLimitStatusAfterCooldown 冷却过期后 Limited 必须变 false（不能一直谎报）。
func TestRateLimitStatusAfterCooldown(t *testing.T) {
	e, _, _ := newTestEngine(t)
	// 手动把闸门设到过去
	e.poolWaitUntil.Store(model.Now() - 1000)
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 2
	e.rateLimitMu.Unlock()

	st := e.RateLimitStatus()
	if st.Limited {
		t.Fatal("冷却已过期时 Limited 应为 false")
	}
	// 但应提示"等待验证车道是否恢复"
	if st.LastReason == "" {
		t.Fatal("冷却刚过应给出'等待验证'提示")
	}
}

// TestRateLimitStatusRemainingMonotonic 剩余时间应随时间递减。
func TestRateLimitStatusRemainingMonotonic(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.setPoolCooldown(60)
	first := e.RateLimitStatus().RemainingSeconds
	time.Sleep(1100 * time.Millisecond)
	second := e.RateLimitStatus().RemainingSeconds
	if second >= first {
		t.Fatalf("剩余时间未递减: %d -> %d", first, second)
	}
}

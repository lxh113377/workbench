package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock 是可注入的时钟，让限流测试不依赖真实时间。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestLimiter(rate float64, burst int, clk *fakeClock) *Limiter {
	l := New(rate, burst)
	l.now = clk.Now
	l.lastFill = clk.Now()
	return l
}

// TestNilLimiterAlwaysPasses nil 限流器表示不限流。
func TestNilLimiterAlwaysPasses(t *testing.T) {
	var l *Limiter
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("nil limiter 应始终放行: %v", err)
	}
	if !l.TryTake() {
		t.Fatal("nil limiter TryTake 应返回 true")
	}
}

// TestBurstThenThrottle 满桶时允许突发，取完后必须等待补充。
func TestBurstThenThrottle(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	l := newTestLimiter(2, 3, clk) // 2 个/秒，桶容量 3

	// 前 3 次应立即成功（桶是满的）
	for i := 0; i < 3; i++ {
		if !l.TryTake() {
			t.Fatalf("第 %d 次应能立即取到令牌（桶未空）", i+1)
		}
	}
	// 第 4 次应失败
	if l.TryTake() {
		t.Fatal("桶空后 TryTake 应返回 false")
	}
}

// TestRefillByTime 时间推进后令牌应补充。
func TestRefillByTime(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	l := newTestLimiter(2, 2, clk)
	l.TryTake()
	l.TryTake()
	if l.TryTake() {
		t.Fatal("桶应已空")
	}
	// 推进 0.5 秒：2/秒 × 0.5 秒 = 1 个令牌
	clk.Advance(500 * time.Millisecond)
	if !l.TryTake() {
		t.Fatal("推进 0.5 秒后应补充 1 个令牌")
	}
	if l.TryTake() {
		t.Fatal("刚取完不应还有令牌")
	}
}

// TestRefillCappedAtBurst 补充不能超过桶容量。
func TestRefillCappedAtBurst(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	l := newTestLimiter(10, 2, clk)
	clk.Advance(time.Hour) // 放很久
	s := l.Snapshot()
	if s.Tokens != 2 {
		t.Fatalf("令牌数应封顶在 2，实际 %.2f", s.Tokens)
	}
}

// TestWaitBlocksThenSucceeds 桶空时 Wait 应阻塞到有令牌。
func TestWaitBlocksThenSucceeds(t *testing.T) {
	// 用真实时钟但高频：1000/秒，桶容量 1
	l := New(1000, 1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("等待时间异常: %v", d)
	}
}

// TestWaitCancelled 等待期间 ctx 取消应立即返回。
func TestWaitCancelled(t *testing.T) {
	// 极低速率：等待会很久
	l := New(0.001, 1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := l.Wait(ctx)
	if err == nil {
		t.Fatal("ctx 超时后 Wait 应返回错误")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("取消响应太慢: %v", d)
	}
}

// TestRateZeroDisables 速率 <= 0 表示不限流。
func TestRateZeroDisables(t *testing.T) {
	if New(0, 10) != nil {
		t.Fatal("rate=0 应返回 nil（不限流）")
	}
	if New(-1, 10) != nil {
		t.Fatal("rate<0 应返回 nil（不限流）")
	}
}

// TestConcurrentWaitNoOverIssue 并发取令牌不得超发。
//
// 这是本限流器最关键的契约：修复的正是一分钟内打出 270 次请求的事故。
func TestConcurrentWaitNoOverIssue(t *testing.T) {
	// 100/秒，桶容量 10；并发 50 个取令牌，总耗时至少 (50-10)/100 = 0.4 秒
	l := New(100, 10)
	const n = 50
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = l.Wait(context.Background())
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)
	// 至少需要 (50-10)/100 = 0.4 秒；给一定余量，但必须明显 > 0.2 秒
	if elapsed < 200*time.Millisecond {
		t.Fatalf("并发取 %d 个令牌只用了 %v，说明未真正限速（可能超发）", n, elapsed)
	}
}

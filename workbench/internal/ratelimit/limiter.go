package ratelimit

// Package ratelimit 提供进程内的请求速率限制（令牌桶）。
//
// 为什么需要（2026-10-19 生产事故复盘）：
//
//	08:00-08:04 期间工作台每分钟发出 45-70 次模型请求（约 270 次/5分钟窗口），
//	远超 our-free-model 免费车道的容量，车道随即返回 429 并持续 50 分钟。
//	当时的组合是：8 路并发 × 每个任务失败后立即换下一个模型 × 25 个模型轮转。
//	一旦某个模型 3 秒失败，立刻换下一个，于是一分钟内能打出上百次请求。
//
// 结论：**并发控制解决不了这个问题**。并发数只限制"同时跑几个任务"，
// 不限制"每秒发出多少请求"。必须有一个全局的、跨任务的请求速率闸门。
//
// 设计：令牌桶（token bucket），容量 = burst，补充速率 = rate/秒。
//   - 允许短时突发（burst），否则正常单任务也会被卡；
//   - 长期平均速率被 rate 约束，保证不打爆车道；
//   - 等待是**阻塞但可取消**的，任务被取消时立即释放，不占槽。
//
// 刻意做成进程内单例：整个工作台只有一个车道，全局共享才有意义。

import (
	"context"
	"sync"
	"time"
)

// Limiter 是令牌桶限流器（并发安全）。
type Limiter struct {
	mu       sync.Mutex
	rate     float64 // 每秒补充的令牌数
	burst    float64 // 桶容量（允许的瞬时突发）
	tokens   float64 // 当前令牌数
	lastFill time.Time
	now      func() time.Time // 可注入时钟，便于测试
}

// New 构造限流器。rate <= 0 表示不限制（返回 nil，调用方按 nil 处理为放行）。
func New(ratePerSecond float64, burst int) *Limiter {
	if ratePerSecond <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &Limiter{
		rate:     ratePerSecond,
		burst:    float64(burst),
		tokens:   float64(burst), // 初始满桶：冷启动不被无谓拖慢
		lastFill: time.Now(),
		now:      time.Now,
	}
}

// Wait 取一个令牌，取不到就等到可以取（或 ctx 取消）。
// 返回 nil 表示已获得令牌；返回 ctx.Err() 表示等待被取消。
// nil 接收者是安全的：表示不限流，立即返回。
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	for {
		l.mu.Lock()
		l.refillLocked()
		if l.tokens >= 1 {
			l.tokens--
			l.mu.Unlock()
			return nil
		}
		// 还差多少令牌 -> 需要等多久
		need := 1 - l.tokens
		wait := time.Duration(need / l.rate * float64(time.Second))
		if wait < time.Millisecond {
			wait = time.Millisecond
		}
		l.mu.Unlock()

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// 循环重试：可能有并发调用者先取走了令牌
		}
	}
}

// TryTake 非阻塞地尝试取一个令牌，成功返回 true。
// 用于"探测类"请求：宁可跳过，也不排队等待。
func (l *Limiter) TryTake() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked()
	if l.tokens >= 1 {
		l.tokens--
		return true
	}
	return false
}

// Snapshot 返回当前状态（面板/诊断用）。
type Snapshot struct {
	RatePerSecond float64 `json:"ratePerSecond"`
	Burst         int     `json:"burst"`
	Tokens        float64 `json:"tokens"`
}

// Snapshot 返回当前令牌桶状态。nil 接收者返回零值。
func (l *Limiter) Snapshot() Snapshot {
	if l == nil {
		return Snapshot{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refillLocked()
	return Snapshot{RatePerSecond: l.rate, Burst: int(l.burst), Tokens: l.tokens}
}

// refillLocked 按经过的时间补充令牌（调用方必须持锁）。
func (l *Limiter) refillLocked() {
	now := l.now()
	elapsed := now.Sub(l.lastFill).Seconds()
	if elapsed <= 0 {
		return
	}
	l.tokens += elapsed * l.rate
	if l.tokens > l.burst {
		l.tokens = l.burst
	}
	l.lastFill = now
}

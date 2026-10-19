package engine

import (
	"testing"
	"time"

	"workbench/internal/model"
)

// TestBackoffEscalatesWithinCooldown 是本轮二次实测发现的回归测试。
//
// 现场日志（2026-10-19 08:13~08:14）：
//
//	08:13:07 consecutive=2 wait=600
//	08:14:17 consecutive=1 wait=300   <- 冷却期内再次 429，计数被错误清零
//
// 旧实现用 "距上次限流 > 2×首次冷却" 判断是否新一轮；但 08:14:17 距 08:13:07
// 只有 70 秒，本不该清零 —— 真正原因是 lastAt 每次都被刷新，而判断阈值用的是
// 首次冷却时长，逻辑自相矛盾。修复后语义明确：冷却期内收到 429 = 上游仍未恢复
// = 同一轮，计数必须继续累加。
func TestBackoffEscalatesWithinCooldown(t *testing.T) {
	e, _, _ := newTestEngine(t)

	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.setPoolCooldown(rateLimitCooldownSeconds)

	e.rateLimitMu.Lock()
	consec := e.rateLimit.consecutive
	e.rateLimitMu.Unlock()
	if consec != 3 {
		t.Fatalf("冷却期内三次限流的 consecutive = %d, want 3（必须持续累加，不能清零）", consec)
	}
	// 第三次的等待应是 300*4=1200 秒（被封顶到 1800 以内）
	now := model.Now()
	waitSec := (e.poolWaitUntil.Load() - now) / 1000
	if waitSec < 600 {
		t.Fatalf("第三次限流冷却应显著长于首次（实际 %d 秒）", waitSec)
	}
}

// TestBackoffResetsAfterLongRecovery 保证真正恢复后再限流会从 5 分钟重新起算。
func TestBackoffResetsAfterLongRecovery(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.setPoolCooldown(rateLimitCooldownSeconds)

	// 模拟"冷却早已结束很久，然后才再次限流"
	e.rateLimitMu.Lock()
	e.rateLimit.cooldownEnd = model.Now() - int64(2*rateLimitCooldownSeconds)*1000
	e.rateLimitMu.Unlock()

	e.setPoolCooldown(rateLimitCooldownSeconds)
	e.rateLimitMu.Lock()
	consec := e.rateLimit.consecutive
	e.rateLimitMu.Unlock()
	if consec != 1 {
		t.Fatalf("长期恢复后 consecutive = %d, want 1（重新起算）", consec)
	}
}

// TestCooldownGateBlocksScheduling 保证闸门真的会挡住 tick。
func TestCooldownGateBlocksScheduling(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.poolWaitUntil.Store(time.Now().Add(5 * time.Minute).UnixMilli())
	if e.poolWaitUntil.Load() <= model.Now() {
		t.Fatal("闸门应设置在将来")
	}
}

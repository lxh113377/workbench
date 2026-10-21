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

// TestEndCooldownEpisodeResetsEscalation 是「30 分钟档永久钉死」缺陷的回归。
//
// 现场（2026-10-21 20:0x~22:3x 冷却日志）：
//
//	21:45:13 consecutive=1 wait=300
//	21:56:55 consecutive=1 wait=300
//	22:02:04 consecutive=2 wait=600
//	22:02:06 consecutive=3 wait=1200
//	22:02:10 consecutive=4 wait=1800
//	22:19:20 consecutive=2 wait=600
//	22:19:26 consecutive=3 wait=1200   <- 闸门落在 22:39，期间 14 条任务一条没跑
//
// 旧语义里退避层级只在"成功一次"时清零，而闸门按住的恰恰是唯一能成功的那条路：
// 每次开闸后的第一批请求必然又吃 429，层级只涨不落，最终永久停在 1800 秒档 ——
// 把上游几秒级的限流窗口人为拖成"半小时一探、次次撞墙"。
// 新语义：闸门到点即视为冷却窗口走完，退避层级归零（每轮窗口一次公平重探）。
func TestEndCooldownEpisodeResetsEscalation(t *testing.T) {
	e, _, _ := newTestEngine(t)
	// 复刻现场：连续限流把层级推到 4（1800 秒档）。
	for i := 0; i < 4; i++ {
		e.setPoolCooldown(rateLimitCooldownSeconds)
	}
	e.rateLimitMu.Lock()
	before := e.rateLimit.consecutive
	e.rateLimitMu.Unlock()
	if before < 4 {
		t.Fatalf("准备阶段应升到第 4 档，实际 %d", before)
	}
	// 闸门到点：tick 的守卫是「until <= now 才继续」，所以先把它置为已过期，
	// 再走 0.5 步的归零分支（复刻真实时序，不假设闸门会自己变短）。
	e.poolWaitUntil.Store(model.Now() - 1000)
	e.endCooldownEpisode()
	e.rateLimitMu.Lock()
	after := e.rateLimit.consecutive
	e.rateLimitMu.Unlock()
	if after != 0 {
		t.Fatalf("冷却窗口走完后 consecutive = %d, want 0（否则下一轮 429 直接又跳 1800 秒档）", after)
	}
	// 再次限流必须从 5 分钟重新起算，而不是继承上一轮的 1800 秒。
	now := model.Now()
	e.setPoolCooldown(rateLimitCooldownSeconds)
	waitSec := (e.poolWaitUntil.Load() - now) / 1000
	if waitSec > 400 {
		t.Fatalf("新一轮限流冷却应回到 ~%d 秒，实际 %d 秒（退避没归零）", rateLimitCooldownSeconds, waitSec)
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

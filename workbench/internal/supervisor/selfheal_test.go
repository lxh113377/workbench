package supervisor

// selfheal_test.go 守护 7x24 的核心机制：资源越阈值 -> 连续 strikes -> 触发自愈回调。
//
// 为什么需要（2026-10-19 审计现场）：
// SelfHeal / strikes / StrikesToRestart 是「连续 24 小时不中断」的兜底机制，
// 但 internal/supervisor 下只有 metrics_csv_test.go 与 trend_test.go ——
// **自愈逻辑零测试覆盖**。一旦 strikes 计数或阈值比较写错，
// 结果是「要么永不重启（越跑越坏）」或「要么重启风暴（反复自杀）」，
// 而总验收与 e2e 都不会发现。
//
// 判据：evaluate 是纯逻辑（输入 Sample + 配置，产出是否触发 onHeal），
// 因此可以直接喂构造样本，无需真的越阈值或真的拉起子进程。

import (
	"sync/atomic"
	"testing"

	"workbench/internal/config"
)

// newTestManager 造一个只关心 evaluate 行为的 Manager（不启动采样循环）。
func newTestManager(cfg config.SupervisorConfig) (*Manager, *int32) {
	var calls int32
	m := New(config.Layout{}, func() config.SupervisorConfig { return cfg })
	m.OnHeal(func(string) { atomic.AddInt32(&calls, 1) })
	return m, &calls
}

func overMemSample(memMB float64) Sample {
	return Sample{At: 1, HeapMB: memMB}
}

// TestSelfHealFiresAfterStrikes 是本轮补的核心回归：
// 必须**连续** N 次越阈值才自愈，且第 N 次恰好触发一次。
func TestSelfHealFiresAfterStrikes(t *testing.T) {
	cfg := config.SupervisorConfig{Enabled: true, MemLimitMB: 100, StrikesToRestart: 3}
	m, calls := newTestManager(cfg)

	// 前 2 次越阈值：只累计，不得触发
	for i := 1; i <= 2; i++ {
		m.evaluate(overMemSample(200))
		if got := atomic.LoadInt32(calls); got != 0 {
			t.Fatalf("第 %d 次越阈值就触发了自愈（应等满 %d 次），calls=%d", i, cfg.StrikesToRestart, got)
		}
	}
	// 第 3 次：必须触发一次
	m.evaluate(overMemSample(200))
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Fatalf("第 %d 次越阈值应触发 1 次自愈，实际 %d", cfg.StrikesToRestart, got)
	}
}

// TestSelfHealResetsOnHealthySample 断言「strikes 必须连续」：
// 中间夹一次正常样本就清零，不能把断断续续的抖动累积成重启。
func TestSelfHealResetsOnHealthySample(t *testing.T) {
	cfg := config.SupervisorConfig{Enabled: true, MemLimitMB: 100, StrikesToRestart: 3}
	m, calls := newTestManager(cfg)
	m.evaluate(overMemSample(200)) // strike 1
	m.evaluate(overMemSample(200)) // strike 2
	m.evaluate(overMemSample(50))  // 正常 -> 清零
	m.evaluate(overMemSample(200)) // strike 1（重新开始）
	m.evaluate(overMemSample(200)) // strike 2
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("不连续的抖动不得触发自愈，实际触发 %d 次", got)
	}
}

// TestSelfHealDisabledDoesNothing 断言关闭时完全不动（避免「关了就没人管」的反向误解）。
func TestSelfHealDisabledDoesNothing(t *testing.T) {
	cfg := config.SupervisorConfig{Enabled: false, MemLimitMB: 1, StrikesToRestart: 1}
	m, calls := newTestManager(cfg)
	for i := 0; i < 5; i++ {
		m.evaluate(overMemSample(9999))
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("supervisor 关闭时不得触发自愈，实际 %d 次", got)
	}
}

// TestSelfHealZeroStrikesNeverRestarts 断言 StrikesToRestart<=0 是「永不自愈」的显式开关，
// 而不是被当成「立即自愈」（那会导致重启风暴）。
func TestSelfHealZeroStrikesNeverRestarts(t *testing.T) {
	cfg := config.SupervisorConfig{Enabled: true, MemLimitMB: 100, StrikesToRestart: 0}
	m, calls := newTestManager(cfg)
	for i := 0; i < 10; i++ {
		m.evaluate(overMemSample(9999))
	}
	if got := atomic.LoadInt32(calls); got != 0 {
		t.Fatalf("StrikesToRestart<=0 应表示永不自愈，实际触发 %d 次", got)
	}
}

// TestSelfHealEachLimitTrips 断言三种阈值（内存/句柄/goroutine）**各自**都能触发。
// 只测内存会漏掉另两条：句柄与 goroutine 泄漏是最常见的长跑问题。
func TestSelfHealEachLimitTrips(t *testing.T) {
	base := config.SupervisorConfig{Enabled: true, MemLimitMB: 100, HandleLimit: 100, GoroutineLimit: 100, StrikesToRestart: 1}
	cases := []struct {
		name string
		s    Sample
	}{
		{"heap", Sample{HeapMB: 200}},
		{"handles", Sample{Handles: 200}},
		{"goroutine", Sample{Goroutine: 200}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, calls := newTestManager(base)
			m.evaluate(c.s)
			if got := atomic.LoadInt32(calls); got != 1 {
				t.Fatalf("%s 越阈值应触发 1 次自愈，实际 %d", c.name, got)
			}
		})
	}
}

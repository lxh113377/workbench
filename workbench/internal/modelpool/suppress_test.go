package modelpool

import (
	"testing"
	"time"
)

// TestSuppressProbingSkipsProbeNow 覆盖限流期间的探测抑制。
//
// 现场问题（2026-10-19）：OFM 持续 429 时，探测每 180 秒仍打 25 个模型，
// 一边等冷却一边自己制造流量，等于永远恢复不了。
func TestSuppressProbingSkipsProbeNow(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown)})
	svc := NewService(store, nil, ServiceConfig{Enabled: false, DSHHome: t.TempDir()})
	svc.mgr = m

	svc.SuppressProbing(300, "上游限流 429")
	if suppressed, remain := svc.ProbingSuppressed(); !suppressed {
		t.Fatal("应处于探测抑制期")
	} else if remain < 290 || remain > 305 {
		t.Fatalf("剩余秒数异常: %d", remain)
	}

	// 抑制期内 ProbeNow 应直接返回，不做任何探测
	before := m.Specs()
	sum := svc.ProbeNow()
	if sum.Total != 0 {
		t.Fatalf("抑制期内不应探测，实际 Total=%d", sum.Total)
	}
	if len(m.Specs()) != len(before) {
		t.Fatal("抑制期内模型清单不应变化")
	}
}

// TestSuppressProbingNeverShortens 并发抑制不能被缩短。
func TestSuppressProbingNeverShortens(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := NewService(store, nil, ServiceConfig{Enabled: false, DSHHome: t.TempDir()})

	svc.SuppressProbing(600, "long")
	_, long := svc.ProbingSuppressed()
	svc.SuppressProbing(30, "short")
	_, after := svc.ProbingSuppressed()
	if after < long-2 {
		t.Fatalf("短抑制不应缩短已有的长抑制: %d -> %d", long, after)
	}
}

// TestProbingResumesAfterExpiry 抑制到期后必须恢复探测（不能永久静音）。
func TestProbingResumesAfterExpiry(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := NewService(store, nil, ServiceConfig{Enabled: false, DSHHome: t.TempDir()})
	svc.SuppressProbing(1, "short")
	time.Sleep(1200 * time.Millisecond)
	if suppressed, _ := svc.ProbingSuppressed(); suppressed {
		t.Fatal("抑制到期后应恢复探测")
	}
}

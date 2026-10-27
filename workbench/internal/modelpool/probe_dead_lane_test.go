package modelpool

import "testing"

// TestProbeDoesNotReviveDeadLane 钉住「探测不得把执行侧/出口级失败车道标回 healthy」（round 43 · T26）。
//
// 由来（独立审计铁证）：`ApplyProbe` 的 healthy 分支原先**无条件**写 `state=healthy`，
// 于是 `combo:auto-*` 的 state 每 ~66 秒被翻案一次（lastErrKind 一直是 `not_served`、consecFail 39/77/91），
// 每个任务都要白试这 3 条（boot_failed 占全部尝试 71%）。判据：出口级失败车道的 state 必须保持 down。
func TestProbeDoesNotReviveDeadLane(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", notServedKind, `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`)
	before := healthOf(t, m, "a")
	if before.State != StateDown {
		t.Fatalf("前置条件不成立：not_served 首击应转 down，实际 %s", before.State)
	}
	m.ApplyProbe([]ProbeResult{{Model: "a", OK: true, State: StateHealthy}})
	after := healthOf(t, m, "a")
	if after.State == StateHealthy {
		t.Fatal("探测成功不得把出口级失败车道标回 healthy（否则每任务白试）")
	}
	if after.ConsecFail != before.ConsecFail {
		t.Fatalf("探测改写了连续失败计数：%d → %d", before.ConsecFail, after.ConsecFail)
	}
	if after.ProbeOK == 0 {
		t.Fatal("可达性读数仍应记录（ProbeOK 不能因为不翻案就丢掉）")
	}

	// 对照组：结构性故障（unreachable）仍必须被探测恢复 —— 既有语义不得被改坏。
	m2, _ := newTestManager(t)
	m2.MarkFailure("b", "unreachable", "connection refused")
	m2.ApplyProbe([]ProbeResult{{Model: "b", OK: true, State: StateHealthy}})
	if h := healthOf(t, m2, "b"); h.State != StateHealthy || h.CooldownTo != 0 || h.ConsecFail != 0 {
		t.Fatalf("结构性故障应被探测恢复，实际 %+v", h)
	}
}

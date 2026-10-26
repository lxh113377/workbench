package modelpool

import "testing"

// TestNotServedOneStrikeCooldownAndProbeImmunity 钉住 `not_served` 的两条口径（T17②-b）：
//
//  1. 一击即冷却，且冷却长度是「出口级事实」量级（≥1500s，不是偶发失败的 120s）；
//  2. **探测成功不得把它治回** —— round 33 实测：按 model_unavailable 记时，45s 后探测周期就把三条 combo 车道
//     从 down 治回 healthy，归因被抵消。本用例守住「出口级事实不会被端点可达性翻案」。
func TestNotServedOneStrikeCooldownAndProbeImmunity(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", notServedKind, `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`)
	h := healthOf(t, m, "a")
	if h.State != StateDown || h.CooldownTo <= nowMs() {
		t.Fatalf("一次 not_served 就应转 down + 冷却，实际 state=%s cooldownTo=%d", h.State, h.CooldownTo)
	}
	if left := h.CooldownTo - nowMs(); left < 1_500_000 {
		t.Fatalf("not_served 冷却应为出口级量级（≥1500s），实际剩余 %dms", left)
	}
	consecBefore := h.ConsecFail

	// 探测成功（端点可达）不得清它。
	m.ApplyProbe([]ProbeResult{{Model: "a", OK: true, State: StateHealthy}})
	after := healthOf(t, m, "a")
	if after.ConsecFail != consecBefore || after.ConsecFail == 0 {
		t.Fatalf("探测成功不得清 not_served 的连续失败（前 %d，后 %d）", consecBefore, after.ConsecFail)
	}
	if after.CooldownTo <= nowMs() {
		t.Fatalf("探测成功不得解除 not_served 的冷却，实际 cooldownTo=%d", after.CooldownTo)
	}
	if after.ProbeOK == 0 {
		t.Fatal("探测成功仍应记 ProbeOK（可达性信息不能丢）")
	}

	// 对照组：结构性故障（unreachable）仍必须能被探测恢复（既有语义不得被改坏）。
	m2, _ := newTestManager(t)
	m2.MarkFailure("b", "unreachable", "connection refused")
	m2.ApplyProbe([]ProbeResult{{Model: "b", OK: true, State: StateHealthy}})
	if h2 := healthOf(t, m2, "b"); h2.State != StateHealthy || h2.CooldownTo != 0 || h2.ConsecFail != 0 {
		t.Fatalf("结构性故障应被探测恢复，实际 %+v", h2)
	}
}

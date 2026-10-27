package modelpool

import "testing"

// TestNotServedNotRevivedByCooldownExpiryPlusProbe 守住本缺陷的根因：出口级事实 not_served
// 不得被「冷却到期半开 + 下一轮探测成功」洗成 healthy。
//
// 实测由来（round 51，pool.json sha ab85e4d9adc4c3e5 / model-status.jsonl）：combo:auto-vision 在
// 2026-10-27T11:45:37 被 healHalfOpenLocked 半开成 unknown 并清掉 LastErrKind，11:45:39 探测
// 随即写 healthy，11:50 又被派活并 boot_failed —— 566 次尝试里 293 次 boot_failed 的主体。
func TestNotServedNotRevivedByCooldownExpiryPlusProbe(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", notServedKind, "dsh: SERVER: our free model does not serve \"combo:auto-vision\" on this egress")
	before := healthOf(t, m, "a")
	if before.State != StateDown || before.LastErrKind != notServedKind {
		t.Fatalf("前置不成立：not_served 首击应 down 且留痕，实际 %+v", before)
	}
	// 把冷却强行拨到「已过期」：这正是线上每 30~60 分钟出现一次的现场。
	m.mu.Lock()
	m.health["a"].CooldownTo = nowMs() - 1000
	m.mu.Unlock()

	// Routes() 触发 healHalfOpenLocked：出口级事实不许在这里被半开清掉。
	for _, rt := range m.Routes() {
		if rt.Model == "a" {
			t.Fatal("not_served 车道在冷却到期后不得被半开放回路由表")
		}
	}
	mid := healthOf(t, m, "a")
	if mid.State != StateDown || mid.LastErrKind != notServedKind {
		t.Fatalf("冷却到期不得清掉出口级印记（否则下一轮探测立刻翻案），实际 %+v", mid)
	}

	// 紧接一轮探测成功：依旧不得 healthy。
	m.ApplyProbe([]ProbeResult{{Model: "a", OK: true, State: StateHealthy, LatencyMs: 40}})
	after := healthOf(t, m, "a")
	if after.State == StateHealthy {
		t.Fatal("探针成功不得把 not_served 车道标回 healthy")
	}
	if after.LastErrKind != notServedKind {
		t.Fatalf("探测不得抹掉 not_served 印记，实际 %q", after.LastErrKind)
	}
	if after.ProbeOK <= mid.ProbeOK {
		t.Fatal("可达性读数仍应记录（ProbeOK 必须增长）")
	}
	for _, rt := range m.Routes() {
		if rt.Model == "a" {
			t.Fatal("探测后 not_served 车道仍不得进入路由表")
		}
	}

	// 真实执行成功是唯一自动恢复路径。
	m.MarkSuccess("a", 500)
	if h := healthOf(t, m, "a"); h.State != StateHealthy || h.LastErrKind != "" {
		t.Fatalf("真实执行成功应清除出口级印记并恢复 healthy，实际 %+v", h)
	}

	// 对照组：普通偶发失败（timeout）仍走半开恢复 —— 既有语义不许被改坏。
	m2, _ := newTestManager(t)
	for i := 0; i < 3; i++ {
		m2.MarkFailure("b", "timeout", "boom")
	}
	m2.mu.Lock()
	m2.health["b"].CooldownTo = nowMs() - 1000
	m2.mu.Unlock()
	found := false
	for _, rt := range m2.Routes() {
		if rt.Model == "b" {
			found = true
		}
	}
	if !found {
		t.Fatal("普通偶发失败在冷却到期后仍应半开恢复（对照）")
	}
}

// TestProbeHealthyDoesNotFlipUserDisabledLane 守住「探测只证端点可达，不得把用户停用车道翻成 healthy」。
//
// 实测由来（round 51，model-status.jsonl）：14:20:11 面板停用 combo:auto-vision 后，
// 同一毫秒的探测写出 disabled -> healthy，面板出现「已停用却健康」假象，且 re-enable 时
// State 已不是 disabled，SetEnabled 的复位分支不会生效。
func TestProbeHealthyDoesNotFlipUserDisabledLane(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.SetEnabled("b", false, "user"); err != nil {
		t.Fatal(err)
	}
	if h := healthOf(t, m, "b"); h.State != StateDisabled {
		t.Fatalf("前置不成立：手动停用应 disabled，实际 %s", h.State)
	}
	m.ApplyProbe([]ProbeResult{{Model: "b", OK: true, State: StateHealthy}})
	after := healthOf(t, m, "b")
	if after.State != StateDisabled {
		t.Fatalf("探测成功不得把用户停用车道标回 %s", after.State)
	}
	if after.ProbeOK == 0 {
		t.Fatal("可达性读数仍应记录（ProbeOK 不能丢）")
	}
	// 对照组：启用中的 unknown 车道仍应被探测标为 healthy。
	m.ApplyProbe([]ProbeResult{{Model: "a", OK: true, State: StateHealthy}})
	if h := healthOf(t, m, "a"); h.State != StateHealthy {
		t.Fatalf("启用中的车道探测成功应 healthy，实际 %s", h.State)
	}
}

// TestGatewaySyncDoesNotOverwriteNotServedHealth 是「根因不是网关清单同步」的反证：
// SyncFromGateway / UpsertPreserve 只动静态字段，从不写健康度（③ 假设不成立）。
// 这条同时防止将来有人把同步改成「重建 spec/health」，把出口级事实清零。
func TestGatewaySyncDoesNotOverwriteNotServedHealth(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", notServedKind, "dsh: SERVER: our free model does not serve \"combo:auto-vision\" on this egress")
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k",
		Models: []string{"our-free-model/a", "our-free-model/b"}}
	SyncFromGateway(m, gw, "periodic")
	h := healthOf(t, m, "a")
	if h.State != StateDown || h.LastErrKind != notServedKind || h.CooldownTo <= 0 {
		t.Fatalf("网关清单同步不得覆盖/清零 not_served 健康度，实际 %+v", h)
	}
}

package modelpool

import "testing"

// TestApplyProbeDoesNotClearExecutionFailures 钉住「探测成功不得抹掉执行侧失败」。
//
// 实测由来（2026-10-26 round 15）：deepseek-ai/deepseek-v4.1-flash 在多次**零产出**失败
// （stalled / bad_exit）之后，健康账本仍是 `succ=17 fail=3 consec=0 healthy` —— 每轮探测把它复活，
// 于是每个新任务的首个路由永远落在实测跑不出东西的模型上，白烧一次尝试。
func TestApplyProbeDoesNotClearExecutionFailures(t *testing.T) {
	m, _ := newTestManager(t)
	for i := 0; i < 3; i++ {
		m.MarkFailure("a", "stalled", "启动后零进展")
	}
	before := healthOf(t, m, "a")
	if before.State != StateDown || before.CooldownTo <= nowMs() {
		t.Fatalf("前置不成立：连续 3 次 stalled 应转 down + 冷却，实际 %+v", before)
	}

	// 一轮**探测成功**（端点可达，但没有任何执行成功）
	m.ApplyProbe([]ProbeResult{{Model: "a", OK: true, State: StateHealthy, LatencyMs: 90}})
	after := healthOf(t, m, "a")
	if after.ConsecFail == 0 {
		t.Fatalf("探测成功不得清零执行侧连续失败（实测缺陷形态），实际 consec=%d", after.ConsecFail)
	}
	if after.CooldownTo <= nowMs() {
		t.Fatalf("探测成功不得解除执行侧冷却，实际 cooldownTo=%d", after.CooldownTo)
	}
	if after.ProbeOK == 0 {
		t.Fatalf("探测成功仍应记 ProbeOK（可达性信息不能丢）")
	}
	if after.LatencyMs != 90 {
		t.Fatalf("探测延迟仍应更新，实际 %d", after.LatencyMs)
	}
}

// TestApplyProbeStillRecoversStructuralFailures 是对照组：结构性故障仍由探测恢复
// （原有语义不许被改坏 —— 端点真的修好了必须能回到调度队列）。
func TestApplyProbeStillRecoversStructuralFailures(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("b", "unreachable", "connection refused")
	m.ApplyProbe([]ProbeResult{{Model: "b", OK: true, State: StateHealthy}})
	h := healthOf(t, m, "b")
	if h.State != StateHealthy || h.CooldownTo != 0 || h.ConsecFail != 0 {
		t.Fatalf("结构性故障应被探测成功恢复（healthy / cooldown=0 / consec=0），实际 %+v", h)
	}
}

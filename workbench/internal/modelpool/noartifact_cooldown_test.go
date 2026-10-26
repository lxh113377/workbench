package modelpool

import "testing"

// TestNoArtifactOneStrikeCooldown 钉住「协议不合规一击即冷却」（T18⑦）。
//
// 为什么（round 22/25 实测）：no_artifact 是**能力性**失败（该车道不认产物协议），与偶发超时不同 ——
// 反复试它只会重复被判 no_artifact；同一次尝试内连续 3 条车道因此被拒。给它一击即冷却、更长冷却期，
// 才能把执行阶段的尝试机会让给可能合规的车道。
func TestNoArtifactOneStrikeCooldown(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", noArtifactKind, "回答不含产物块（不符合 ARTIFACTS 协议）")
	h := healthOf(t, m, "a")
	if h.State != StateDown || h.CooldownTo <= nowMs() {
		t.Fatalf("一次 no_artifact 就应转 down + 冷却，实际 state=%s cooldownTo=%d", h.State, h.CooldownTo)
	}
	if h.ConsecFail != 1 {
		t.Fatalf("首击 consec 应为 1，实际 %d", h.ConsecFail)
	}
	// 能力性失败的冷却要明显长于默认 120s（让重复车道真正退出候选）。
	if left := h.CooldownTo - nowMs(); left < 300_000 {
		t.Fatalf("no_artifact 冷却应 ≥300s，实际剩余 %dms", left)
	}

	// 对照组：偶发类（timeout）仍按既有阈值 —— 首次失败不得直接 down（分层不得被改坏）。
	m2, _ := newTestManager(t)
	m2.MarkFailure("b", "timeout", "boom")
	if h2 := healthOf(t, m2, "b"); h2.State == StateDown || h2.CooldownTo > nowMs() {
		t.Fatalf("timeout 首击不应直接进入冷却，实际 %+v", h2)
	}
}

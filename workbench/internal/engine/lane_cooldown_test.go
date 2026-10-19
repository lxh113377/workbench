package engine

// lane_cooldown_test.go 守护「429 只冷却出问题的车道，不整池停摆」。
//
// 现场实测（2026-10-19 09:05）：our-free-model 匿名免费车道 429 期间，
// 同 provider 下的 Kilo 免费池（kilo-auto/free、reflection/beam-free）直连探测
// 完全可用。但引擎收到 429 就 setPoolCooldown（整池闸门），把健康的 Kilo 车道
// 也一起停掉 —— 直接违反目标要求 4「自动切换其他正常可用的模型兜底」。
//
// 正确语义：429 只冷却出问题的 provider；只有全部 provider 都不可用时才整池闸门。

import (
	"context"
	"errors"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// seedMultiLaneRoutes 造两条车道的路由表：ofm（会 429）与 kilo（健康）。
func seedMultiLaneRoutes(t *testing.T, e *Engine) {
	t.Helper()
	routes := []config.ModelRoute{
		{Model: "ofm-model-a", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "ofm-model-b", Provider: "our-free-model", Tier: "fast", Enabled: true, Weight: 60, MaxFail: 3, CooldownM: 30},
		{Model: "kilo-model-a", Provider: "our-free-model", Tier: "fast", Enabled: true, Weight: 40, MaxFail: 3, CooldownM: 30},
	}
	e.router.UpdateRoutes(routes)
}

// TestProviderCooldownKeepsOtherLanes 守护 Provider 级冷却的隔离性。
func TestProviderCooldownKeepsOtherLanes(t *testing.T) {
	e, _, _ := newTestEngine(t)
	// 两条真正不同的 provider（kilo 车道在 OFM 里是同一 provider，
	// 但真实部署里可以并存自定义端点；这里用不同 provider 模拟"另一条车道"）
	routes := []config.ModelRoute{
		{Model: "ofm-a", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "ofm-b", Provider: "our-free-model", Tier: "fast", Enabled: true, Weight: 60, MaxFail: 3, CooldownM: 30},
		{Model: "kilo-a", Provider: "kilo-lane", Tier: "fast", Enabled: true, Weight: 40, MaxFail: 3, CooldownM: 30},
	}
	e.router.UpdateRoutes(routes)

	// 冷却 our-free-model 这条车道
	n := e.router.CooldownByProvider("our-free-model", 5*time.Minute, "rate_limited", "429")
	if n != 2 {
		t.Fatalf("应冷却该 provider 下 2 个模型，实际 %d", n)
	}

	// kilo 车道必须仍可用
	provs := e.router.AvailableProviders()
	if len(provs) != 1 || provs[0] != "kilo-lane" {
		t.Fatalf("应只剩 kilo-lane 可用，实际 %v", provs)
	}
	if _, err := e.router.Pick("fast", nil); err != nil {
		t.Fatalf("kilo 车道仍应能选到模型: %v", err)
	}

	// 再把 kilo 也冷却 ⇒ 此时才应该"全部车道不可用"
	e.router.CooldownByProvider("kilo-lane", 5*time.Minute, "rate_limited", "429")
	if got := e.router.AvailableProviders(); len(got) != 0 {
		t.Fatalf("全部车道冷却后应没有可用 provider，实际 %v", got)
	}
}

// TestProviderOfResolvesRoute 守护 ProviderOf 的基本行为。
func TestProviderOfResolvesRoute(t *testing.T) {
	e, _, _ := newTestEngine(t)
	routes := []config.ModelRoute{
		{Model: "m1", Provider: "prov-a", Tier: "balanced", Enabled: true, Weight: 100},
		{Model: "m2", Provider: "prov-b", Tier: "fast", Enabled: true, Weight: 50},
	}
	e.router.UpdateRoutes(routes)
	if got := e.router.ProviderOf("m1"); got != "prov-a" {
		t.Fatalf("ProviderOf(m1) = %q, want prov-a", got)
	}
	if got := e.router.ProviderOf("unknown"); got != "" {
		t.Fatalf("未知模型应返回空串，实际 %q", got)
	}
}

// TestRateLimitDoesNotStopOtherLanes 端到端守护：429 发生后任务应退回队列，
// 且**不设置整池闸门**（因为还有其它车道可用）。
func TestRateLimitDoesNotStopOtherLanes(t *testing.T) {
	e, st, _ := newTestEngine(t)
	routes := []config.ModelRoute{
		{Model: "ofm-a", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "kilo-a", Provider: "kilo-lane", Tier: "fast", Enabled: true, Weight: 40, MaxFail: 3, CooldownM: 30},
	}
	e.router.UpdateRoutes(routes)
	e.SetRunner(&fakeRateLimitRunner{})

	// 造一条 queued 任务并放进队列
	id := "task-lane-cooldown"
	task := &model.Task{ID: id, Title: "车道冷却测试", State: model.StateCandidate, Domain: "code"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(id, model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(id, model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	q, _ := st.Get(id)
	e.q.Push(q)

	e.runOne(t.Context(), q)

	// 关键断言：还有别的车道可用时，**不得**设置整池闸门
	if until := e.poolWaitUntil.Load(); until > model.Now() {
		t.Fatalf("仍有可用车道时不应设置整池冷却闸门（until=%d now=%d）", until, model.Now())
	}
	// 出问题车道的模型必须不再被选中。
	//
	// 注意 Pick 会回落到任意 tier，所以不能只断言 err != nil —— 必须按模型名校验。
	for i := 0; i < 30; i++ {
		picked, err := e.router.Pick("balanced", nil)
		if err != nil {
			break // 没有可用模型也算通过（说明冷却生效）
		}
		if picked == "ofm-a" {
			t.Fatal("出问题车道（our-free-model）的模型应被冷却，不应再被选中")
		}
	}
	// 另一条车道仍可选
	picked, err := e.router.Pick("fast", nil)
	if err != nil {
		t.Fatalf("另一条车道应仍可用: %v", err)
	}
	if picked != "kilo-a" {
		t.Fatalf("应调度到健康车道 kilo-a，实际 %q", picked)
	}
	// 任务退回队列（不是判死）
	got, _ := st.Get(id)
	if got.State != model.StateQueued {
		t.Fatalf("限流时任务应退回队列，实际 %s", got.State)
	}
}

// fakeRateLimitRunner 永远返回 429。
type fakeRateLimitRunner struct{}

func (f *fakeRateLimitRunner) UpdateConfig(config.ExecutorConfig) {}

func (f *fakeRateLimitRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{ErrorKind: string(executor.KindRateLimited)}, errors.New("dsh: RATE_LIMIT")
}

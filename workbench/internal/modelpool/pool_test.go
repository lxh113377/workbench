package modelpool

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// ── 调度器 ──────────────────────────────────────────────────────

func spec(id string, pri, weight int, state State) Spec {
	return Spec{ID: id, Model: id, Provider: "our-free-model", Lane: LaneFree,
		Tier: "balanced", Priority: pri, Weight: weight, Enabled: true, Routable: true,
		Format: FormatChat, Runtime: &specRuntime{State: state}}
}

func TestSchedulerPriorityOrder(t *testing.T) {
	sch := NewScheduler(StrategyPriority)
	specs := []Spec{
		spec("c", 30, 10, StateHealthy),
		spec("a", 10, 10, StateHealthy),
		spec("b", 20, 10, StateHealthy),
	}
	for i := 0; i < 5; i++ {
		got, strategy := sch.Pick(specs, nil, nowMs())
		if got != "a" {
			t.Fatalf("priority 策略应稳定选 a，实际 %q（%s）", got, strategy)
		}
	}
}

func TestSchedulerSkipsDownAndExcluded(t *testing.T) {
	sch := NewScheduler(StrategyPriority)
	specs := []Spec{
		spec("a", 10, 10, StateDown),
		spec("b", 20, 10, StateHealthy),
		spec("c", 30, 10, StateHealthy),
	}
	got, _ := sch.Pick(specs, nil, nowMs())
	if got != "b" {
		t.Fatalf("应跳过 down 的 a，实际 %q", got)
	}
	got, _ = sch.Pick(specs, map[string]bool{"b": true}, nowMs())
	if got != "c" {
		t.Fatalf("应跳过 excluded 的 b，实际 %q", got)
	}
}

func TestSchedulerRoundRobinRotates(t *testing.T) {
	sch := NewScheduler(StrategyRoundRobin)
	specs := []Spec{
		spec("a", 10, 10, StateHealthy),
		spec("b", 20, 10, StateHealthy),
		spec("c", 30, 10, StateHealthy),
	}
	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		got, _ := sch.Pick(specs, nil, nowMs())
		seen[got]++
	}
	for _, id := range []string{"a", "b", "c"} {
		if seen[id] != 2 {
			t.Fatalf("轮询应平均分配，%s 出现 %d 次（期望 2）seen=%v", id, seen[id], seen)
		}
	}
}

func TestSchedulerWeightedProportional(t *testing.T) {
	sch := NewScheduler(StrategyWeighted)
	specs := []Spec{
		spec("heavy", 10, 90, StateHealthy),
		spec("light", 20, 10, StateHealthy),
	}
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		got, _ := sch.Pick(specs, nil, nowMs())
		counts[got]++
	}
	if counts["heavy"] < 80 || counts["light"] > 20 {
		t.Fatalf("加权应按 90:10 分配，实际 %v", counts)
	}
}

func TestSchedulerPickChainOrder(t *testing.T) {
	sch := NewScheduler(StrategyPriority)
	specs := []Spec{
		spec("b", 20, 10, StateHealthy),
		spec("a", 10, 10, StateHealthy),
		spec("dead", 1, 10, StateDown),
	}
	chain := sch.PickChain(specs, nowMs())
	if len(chain) != 2 || chain[0] != "a" || chain[1] != "b" {
		t.Fatalf("兜底链应为 [a b]（dead 跳过），实际 %v", chain)
	}
}

// ── Manager：失败冷却 / 成功恢复 / 路由投影 ──────────────────────

func newTestManager(t *testing.T) (*Manager, *Store) {
	t.Helper()
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{
		spec("a", 10, 100, StateUnknown),
		spec("b", 20, 50, StateUnknown),
	})
	return m, store
}

func TestManagerFailureCooldownAndSuccessRecovery(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", "timeout", "boom")
	if h := healthOf(t, m, "a"); h.State != StateDegraded {
		t.Fatalf("首次失败应为 degraded，实际 %s", h.State)
	}
	m.MarkFailure("a", "timeout", "boom")
	m.MarkFailure("a", "timeout", "boom")
	h := healthOf(t, m, "a")
	if h.State != StateDown || h.CooldownTo <= nowMs() {
		t.Fatalf("连续失败 3 次应转 down 且冷却，实际 state=%s cooldownTo=%d", h.State, h.CooldownTo)
	}
	// down 的模型不得出现在路由表里
	for _, rt := range m.Routes() {
		if rt.Model == "a" {
			t.Fatal("down 的模型不应进入路由表")
		}
	}
	m.MarkSuccess("a", 1200)
	h = healthOf(t, m, "a")
	if h.State != StateHealthy || h.CooldownTo != 0 || h.ConsecFail != 0 {
		t.Fatalf("成功后应恢复 healthy，实际 state=%s cooldown=%d consec=%d", h.State, h.CooldownTo, h.ConsecFail)
	}
	found := false
	for _, rt := range m.Routes() {
		if rt.Model == "a" {
			found = true
		}
	}
	if !found {
		t.Fatal("恢复后应重新进入路由表")
	}
}

func TestManagerStructuralErrorLongCooldown(t *testing.T) {
	m, _ := newTestManager(t)
	m.MarkFailure("a", "model_unavailable", "EOL")
	h := healthOf(t, m, "a")
	if h.State != StateDown {
		t.Fatalf("结构性错误应立即 down，实际 %s", h.State)
	}
	if h.CooldownTo-nowMs() < 60_000 {
		t.Fatalf("结构性错误应长冷却，实际剩余 %d ms", h.CooldownTo-nowMs())
	}
}

func healthOf(t *testing.T, m *Manager, id string) Health {
	t.Helper()
	for _, h := range m.HealthSnapshot() {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("找不到健康度 %s", id)
	return Health{}
}

// ── Store 持久化 ────────────────────────────────────────────────

func TestStorePersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	f := &file{Specs: []Spec{spec("x", 1, 10, StateUnknown)}, Health: map[string]*Health{"x": {ID: "x", State: StateUnknown}}}
	if err := s.Save(f); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Specs) != 1 || loaded.Specs[0].ID != "x" {
		t.Fatalf("落盘/读取不一致: %+v", loaded.Specs)
	}
	s.AppendLog(logRecord{Kind: "probe", Model: "x", Reason: "hello"})
	lines := s.TailLog(10)
	if len(lines) != 1 {
		t.Fatalf("状态日志应有 1 行，实际 %d", len(lines))
	}
}

func TestManagerPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	m1 := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	m1.MarkFailure("a", "timeout", "boom")
	if err := m1.SetEnabled("b", false, "test"); err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(NewStore(dir), nil)
	if h := healthOf(t, m2, "a"); h.Failure == 0 {
		t.Fatal("重启后失败计数应保留")
	}
	var b Spec
	for _, s := range m2.Specs() {
		if s.ID == "b" {
			b = s
		}
	}
	if b.Enabled {
		t.Fatal("重启后停用状态应保留")
	}
}

// ── 探测 ────────────────────────────────────────────────────────

func TestProbeHealthyAndDown(t *testing.T) {
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer okSrv.Close()
	// 注意：本机回环地址会被识别为「本机网关」，401 按环境态处理；
	// 这里用 127.0.0.2 构造一个非本机网关（仍可连通），验证真正的鉴权失败判定。
	badSrv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	ln, lerr := net.Listen("tcp", "127.0.0.2:0")
	if lerr != nil {
		t.Skipf("无法绑定 127.0.0.2（%v），跳过远程端点鉴权用例", lerr)
	}
	badSrv.Listener.Close()
	badSrv.Listener = ln
	badSrv.Start()
	defer badSrv.Close()
	p := NewProber(3 * time.Second)
	good := p.Probe(Spec{ID: "m1", Model: "m1", BaseURL: okSrv.URL, APIKey: "k"})
	if !good.OK || good.State != StateHealthy {
		t.Fatalf("健康端点应判 healthy，实际 %+v", good)
	}
	bad := p.Probe(Spec{ID: "m2", Model: "m2", BaseURL: badSrv.URL, APIKey: "k"})
	if bad.OK || bad.State != StateDown || bad.Kind != "missing_credential" {
		t.Fatalf("401 应判 down/missing_credential，实际 %+v", bad)
	}
}

func TestApplyProbeStateMachine(t *testing.T) {
	m, _ := newTestManager(t)
	// 第一轮：a 成功，b 失败
	sum := m.ApplyProbe([]ProbeResult{
		{Model: "a", OK: true, State: StateHealthy, LatencyMs: 120},
		{Model: "b", OK: false, State: StateDown, Kind: "unreachable", Error: "refused"},
	})
	if sum.OK != 1 || sum.Down != 1 {
		t.Fatalf("摘要统计错误: %+v", sum)
	}
	if h := healthOf(t, m, "a"); h.State != StateHealthy || h.LatencyMs != 120 {
		t.Fatalf("a 应 healthy 且记录延迟，实际 %+v", h)
	}
	if h := healthOf(t, m, "b"); h.State != StateDown || h.CooldownTo <= nowMs() {
		t.Fatalf("b 应 down 且冷却，实际 %+v", h)
	}
	// 第二轮：b 恢复
	m.ApplyProbe([]ProbeResult{{Model: "b", OK: true, State: StateHealthy}})
	if h := healthOf(t, m, "b"); h.State != StateHealthy || h.CooldownTo != 0 {
		t.Fatalf("b 恢复探测后应 healthy，实际 %+v", h)
	}
}

func TestProbeGatewayOffNotKillModel(t *testing.T) {
	p := NewProber(2 * time.Second)
	// 127.0.0.1:1 是几乎不可能有监听的端口，且属「本机网关」判定分支。
	res := p.Probe(Spec{ID: "x", Model: "x", BaseURL: "http://127.0.0.1:1/v1"})
	if res.State != StateUnknown || res.Kind != "gateway_off" {
		t.Fatalf("本机网关未开应判 unknown/gateway_off（不误杀模型），实际 %+v", res)
	}
}

// ── 故障转移 ────────────────────────────────────────────────────

type fakeRunner struct {
	calls []string
	fail  map[string]bool
	err   error
}

func (f *fakeRunner) UpdateConfig(config.ExecutorConfig) {}

func (f *fakeRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	f.calls = append(f.calls, modelID)
	if f.fail[modelID] {
		return &executor.Result{ErrorKind: string(executor.KindUpstream)}, f.err
	}
	return &executor.Result{FinalText: "产出正常", Elapsed: 1500 * time.Millisecond}, nil
}

func TestFailoverSwitchesWithoutUserVisibleFailure(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{
		spec("a", 10, 100, StateUnknown),
		spec("b", 20, 50, StateUnknown),
	})
	sch := NewScheduler(StrategyPriority)
	inner := &fakeRunner{fail: map[string]bool{"a": true}, err: errors.New("upstream 502")}
	fr := NewFailoverRunner(inner, m, sch)
	task := &model.Task{ID: "task-1", Title: "t"}
	res, err := fr.Run(context.Background(), task, "a")
	if err != nil || res == nil || res.FinalText == "" {
		t.Fatalf("故障转移后应返回成功结果，err=%v res=%+v", err, res)
	}
	if len(inner.calls) != 2 || inner.calls[0] != "a" || inner.calls[1] != "b" {
		t.Fatalf("应先试 a 再切 b，实际调用 %v", inner.calls)
	}
	if h := healthOf(t, m, "a"); h.Failure == 0 {
		t.Fatal("a 的失败应被记录")
	}
	if h := healthOf(t, m, "b"); h.Success == 0 {
		t.Fatal("b 的成功应被记录")
	}
	// 状态日志里必须有 switch 记录（无感切换的可追溯证据）
	logs := store.TailLog(50)
	found := false
	for _, l := range logs {
		if contains(l, "\"kind\":\"switch\"") && contains(l, "\"from\":\"a\"") && contains(l, "\"to\":\"b\"") {
			found = true
		}
	}
	if !found {
		t.Fatalf("状态日志应记录 a→b 切换，实际日志: %v", logs)
	}
}

func TestFailoverAllFailedReturnsLastError(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	inner := &fakeRunner{fail: map[string]bool{"a": true, "b": true}, err: errors.New("boom")}
	fr := NewFailoverRunner(inner, m, sch)
	_, err := fr.Run(context.Background(), &model.Task{ID: "t"}, "a")
	if err == nil {
		t.Fatal("整条链失败时应返回错误")
	}
	if len(inner.calls) != 2 {
		t.Fatalf("应尝试 2 个模型，实际 %v", inner.calls)
	}
}

func TestFailoverCancelledStopsSwitching(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	inner := &fakeRunner{fail: map[string]bool{"a": true, "b": true}, err: context.Canceled}
	fr := NewFailoverRunner(inner, m, sch)
	_, _ = fr.Run(ctx, &model.Task{ID: "t"}, "a")
	if len(inner.calls) != 1 {
		t.Fatalf("取消后不应继续换模型，实际 %v", inner.calls)
	}
}

// ── 网关清单同步 ────────────────────────────────────────────────

func TestSpecFromGatewayModelClassification(t *testing.T) {
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k"}
	cases := []struct {
		in       string
		wantID   string
		routable bool
		enabled  bool
	}{
		{"our-free-model/z-ai/glm-5.3", "z-ai/glm-5.3", true, true},
		{"our-free-model-region/beam-free", "beam-free", true, false},
		{"deepseek-official/deepseek-flash", "deepseek-official/deepseek-flash", false, false},
	}
	for _, c := range cases {
		s, ok := SpecFromGatewayModel(gw, c.in)
		if !ok {
			t.Fatalf("%s 应能转换", c.in)
		}
		if s.ID != c.wantID || s.Routable != c.routable || s.Enabled != c.enabled {
			t.Fatalf("%s -> %+v（期望 id=%s routable=%v enabled=%v）", c.in, s, c.wantID, c.routable, c.enabled)
		}
		if s.BaseURL != gw.BaseURL || s.APIKey != "k" {
			t.Fatalf("应继承网关地址与密钥: %+v", s)
		}
	}
}

func TestSyncFromGatewayPreservesUserSettings(t *testing.T) {
	m, _ := newTestManager(t)
	// 用户把 a 调成高优先级、关掉 b
	if _, err := m.Upsert(Spec{ID: "a", Model: "a", Priority: 1, Weight: 99, Enabled: true,
		BaseURL: "http://127.0.0.1:8326/v1", Format: FormatChat, Tier: "quality", Group: "自定义"}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled("b", false, "test"); err != nil {
		t.Fatal(err)
	}
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k",
		Models: []string{"our-free-model/a", "our-free-model/b", "our-free-model/c"}}
	res := SyncFromGateway(m, gw, "probe")
	if res.Added != 1 || res.Updated != 2 {
		t.Fatalf("同步统计应为新增 1 / 更新 2，实际 %+v", res)
	}
	var a Spec
	for _, s := range m.Specs() {
		if s.ID == "a" {
			a = s
		}
	}
	if a.Priority != 1 || a.Weight != 99 {
		t.Fatalf("同步不应覆盖用户设置的优先级/权重，实际 %+v", a)
	}
	if h := healthOf(t, m, "b"); h.State != StateDisabled {
		t.Fatalf("同步不应重新启用用户停用的模型，实际 %s", h.State)
	}
}

func TestHalfOpenRecoveryAfterCooldown(t *testing.T) {
	m, _ := newTestManager(t)
	// 直接把 a 置为「冷却已过期」的 down
	m.mu.Lock()
	h := m.health["a"]
	h.State = StateDown
	h.CooldownTo = nowMs() - 1000
	h.ConsecFail = 3
	m.mu.Unlock()
	// Routes() 会触发半开恢复
	routes := m.Routes()
	found := false
	for _, rt := range routes {
		if rt.Model == "a" {
			found = true
		}
	}
	if !found {
		t.Fatal("冷却到期的模型应通过半开恢复重新进入路由表")
	}
	if h := healthOf(t, m, "a"); h.State != StateUnknown || h.CooldownTo != 0 || h.ConsecFail != 0 {
		t.Fatalf("半开恢复后应为 unknown 且清空冷却，实际 %+v", h)
	}
}

func TestModelAttributableKinds(t *testing.T) {
	cases := map[string]bool{
		"model_unavailable": true, "missing_credential": true, "upstream_failed": true,
		"timeout": true, "stalled": true, "unreachable": true,
		"bad_exit": false, "boot_failed": false, "cancelled": false, "unknown": false,
	}
	for kind, want := range cases {
		if got := modelAttributable(kind); got != want {
			t.Fatalf("modelAttributable(%q)=%v，期望 %v", kind, got, want)
		}
	}
}

func TestFailoverDoesNotPenalizeModelForTaskLevelFailure(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	inner := &taskFailRunner{}
	fr := NewFailoverRunner(inner, m, sch)
	_, _ = fr.Run(context.Background(), &model.Task{ID: "t"}, "a")
	// bad_exit 属任务级失败：不得计入模型健康度
	if h := healthOf(t, m, "a"); h.Failure != 0 || h.ConsecFail != 0 {
		t.Fatalf("bad_exit 不应计入模型失败，实际 %+v", h)
	}
}

// taskFailRunner 模拟「模型能跑但任务本身没产出」的执行器。
type taskFailRunner struct{ calls int }

func (f *taskFailRunner) UpdateConfig(config.ExecutorConfig) {}
func (f *taskFailRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	f.calls++
	return &executor.Result{ErrorKind: string(executor.KindExit)}, nil
}




// taskFailRunner 模拟「模型能跑但任务本身没产出」的执行器。

func TestSyncAutoRevivesAutoDisabledModel(t *testing.T) {
	m, _ := newTestManager(t)
	// 模拟「上游判死 -> 自动停用」的模型
	if _, err := m.Upsert(Spec{ID: "recovered", Model: "recovered", Enabled: false, AutoDisabled: true,
		BaseURL: "http://127.0.0.1:8326/v1", Format: FormatChat, Tier: "fast", Priority: 5, Weight: 10}, "test"); err != nil {
		t.Fatal(err)
	}
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k", Models: []string{"our-free-model/recovered"}}
	res := SyncFromGateway(m, gw, "probe")
	if len(res.Revived) != 1 || res.Revived[0] != "recovered" {
		t.Fatalf("网关重新广播后应自动恢复，实际 revived=%v res=%+v", res.Revived, res)
	}
	var got Spec
	for _, s := range m.Specs() {
		if s.ID == "recovered" {
			got = s
		}
	}
	if !got.Enabled || got.AutoDisabled {
		t.Fatalf("恢复后应 enabled=true autoDisabled=false，实际 %+v", got)
	}
}

func TestSyncDoesNotReviveUserDisabledModel(t *testing.T) {
	m, _ := newTestManager(t)
	// 用户手动停用（AutoDisabled=false）
	if err := m.SetEnabled("b", false, "user"); err != nil {
		t.Fatal(err)
	}
	cur, _, _ := m.Get("b")
	cur.AutoDisabled = false
	if _, err := m.UpsertPreserve(cur, "test"); err != nil {
		t.Fatal(err)
	}
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k", Models: []string{"our-free-model/b"}}
	res := SyncFromGateway(m, gw, "probe")
	if len(res.Revived) != 0 {
		t.Fatalf("用户手动停用不应被自动恢复，实际 %v", res.Revived)
	}
	if h := healthOf(t, m, "b"); h.State != StateDisabled {
		t.Fatalf("b 应保持停用，实际 %s", h.State)
	}
}

func TestDiscoverCandidatesAreRoutable(t *testing.T) {
	dir := t.TempDir()
	ofm := filepath.Join(dir, "our-free-model")
	if err := os.MkdirAll(ofm, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ofm, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("catalog.json", `{"version":1,"at":1,"entries":["free-a"],"sealIds":["seal-x"],"kiloRows":[]}`)
	write("availability.json", `{"version":1,"at":2,"results":{"free-a":{"state":"unavailable"}}}`)
	write("settings.json", `{"hiddenModelIds":[],"forward":{"enabled":false,"port":18899}}`)
	d := Discover(dir)
	for _, c := range d.Candidates {
		if !c.Routable {
			t.Fatalf("catalog 候选应可调度（Routable），实际 %+v", c)
		}
	}
	// 判死的那个应标记为自动停用（可被网关广播恢复）
	for _, c := range d.Candidates {
		if c.ID == "free-a" && (!c.AutoDisabled || c.Enabled) {
			t.Fatalf("availability 判死的模型应 enabled=false + autoDisabled=true，实际 %+v", c)
		}
	}
}




func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ── 发现（真实文件形状 + 临时目录） ─────────────────────────────

func TestDiscoverFromFixture(t *testing.T) {
	dir := t.TempDir()
	ofm := filepath.Join(dir, "our-free-model")
	if err := os.MkdirAll(ofm, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ofm, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("catalog.json", `{"version":1,"at":1,"entries":["free-a","free-b"],
		"sealIds":["seal-x"],"kiloRows":[{"id":"kilo-auto/free","name":"Kilo Auto","contextWindow":256000}]}`)
	write("availability.json", `{"version":1,"at":2,"results":{"free-b":{"state":"unavailable","detail":"EOL"}}}`)
	write("settings.json", `{"hiddenModelIds":[],"forward":{"enabled":false,"host":"127.0.0.1","port":18899}}`)
	d := Discover(filepath.Join(dir, ""))
	if d.Dir != ofm {
		t.Fatalf("应定位到 our-free-model 目录，实际 %s", d.Dir)
	}
	if len(d.Candidates) != 4 {
		t.Fatalf("应有 4 个候选（2 free + 1 seal + 1 kilo），实际 %d: %+v", len(d.Candidates), d.Candidates)
	}
	byID := map[string]Spec{}
	for _, c := range d.Candidates {
		byID[c.ID] = c
	}
	if byID["free-b"].Enabled {
		t.Fatal("availability 判死的模型应默认停用")
	}
	if !byID["free-a"].Enabled {
		t.Fatal("未判死的模型应默认启用")
	}
	if lane := byID["kilo-auto/free"].Lane; lane != LaneKilo {
		t.Fatalf("kilo 行应归 LaneKilo，实际 %s", lane)
	}
}

var _ = fmt.Sprintf

package modelroute

// 本文件补齐剩余 0% 函数（AllInCooldown / routeOfLocked）与 recordFailure / recordSuccess /
// trimMsg / Save / UpdateRoutes 的未覆盖分支。
//
// 这一组的落点是熔断与热更新，都是「静默出错最难查」的面：
// AllInCooldown 是整池闸门的判据，把「禁用」「空路由表」当成容量，现场就是任务被误判死；
// routeOfLocked 是 RecordFailure 取 MaxFail/CooldownM 的唯一入口，查不到却回退错值时，
// 一次拼错的模型 id 会让冷却阈值悄悄变成默认值；UpdateRoutes 保留健康度是热更新不丢冷却的前提。

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
)

// enabled 是构造启用路由的简写，避免每个用例重复一遍 Enabled/Weight 样板。
func enabled(model, provider, lane, tier string) config.ModelRoute {
	return config.ModelRoute{Model: model, Provider: provider, Lane: lane, Tier: tier, Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30}
}

// TestAllInCooldownTruthTable 表驱动钉熔断判据的每个分支。
// 关键取舍：Enabled=false 不是容量（跳过），健康条目缺失按「可用」处理（保守：宁可不熔断，
// 也不因为一次热更新窗口就把整池判死）—— 这两条都必须可证伪。
func TestAllInCooldownTruthTable(t *testing.T) {
	cases := []struct {
		name   string
		routes []config.ModelRoute
		setup  func(r *Router)
		want   bool
	}{
		{
			// 退化态（brief 点名）：空路由表没有任何「启用且冷却」的条目，循环体不进 => true。
			// 这条必须钉死语义：上层熔断据此「整池不可用」，与「有车道但全冷却」是同一结论。
			name:   "空路由表：退化判为整池不可用",
			routes: []config.ModelRoute{},
			want:   true,
		},
		{
			name:   "全部健康：不熔断",
			routes: []config.ModelRoute{enabled("a", "p1", "l1", "balanced"), enabled("b", "p1", "l2", "fast")},
			want:   false,
		},
		{
			name:   "一条冷却一条健康：不熔断（这就是要求 4 的分流点）",
			routes: []config.ModelRoute{enabled("a", "p1", "l1", "balanced"), enabled("b", "p1", "l2", "fast")},
			setup:  func(r *Router) { r.CooldownByLane("lane:l1", time.Minute, "rate_limited", "429") },
			want:   false,
		},
		{
			name:   "全部启用道冷却：熔断",
			routes: []config.ModelRoute{enabled("a", "p1", "l1", "balanced"), enabled("b", "p1", "l2", "fast")},
			setup: func(r *Router) {
				r.CooldownByLane("lane:l1", time.Minute, "rate_limited", "429")
				r.CooldownByLane("lane:l2", time.Minute, "upstream", "502")
			},
			want: true,
		},
		{
			// 负向：禁用条目即使被显式置冷却，也不能淹没健康的启用道 —— 「人为关掉」不是「故障」。
			// setup 直接写私有字段来构造「禁用条目被外部置冷」这一罕见态（CooldownBy* 本身不冷禁用条目）。
			name:   "禁用道冷却 + 启用道健康：不熔断",
			routes: []config.ModelRoute{{Model: "off", Provider: "p1", Lane: "l2", Tier: "fast", Enabled: false, Weight: 1}, enabled("a", "p1", "l1", "balanced")},
			setup: func(r *Router) {
				r.health["off"].CooldownTo = time.Now().Add(time.Hour).UnixMilli()
			},
			want: false,
		},
		{
			// 启用道全冷却、禁用道无关：仍判熔断（禁用条目不得被算成可用容量而放行）。
			name:   "启用道全冷却 + 禁用道无关：熔断",
			routes: []config.ModelRoute{enabled("a", "p1", "l1", "balanced"), {Model: "off", Provider: "p1", Lane: "l2", Tier: "fast", Enabled: false, Weight: 1}},
			setup: func(r *Router) {
				r.CooldownByLane("lane:l1", time.Minute, "rate_limited", "429")
				r.health["off"].CooldownTo = time.Now().Add(time.Hour).UnixMilli()
			},
			want: true,
		},
		{
			// 全是禁用条目 = 池内零可用容量，必须与「全冷却」同样判熔断。
			// 这条才是给 Enabled 过滤分支真正上牙的：去掉该 continue 后，两条「健康但禁用」
			// 的条目会被当成可用容量而返回 false —— 闸门不开，任务反复撞已关闭的车道。
			name: "全部条目禁用（零可用容量）：熔断",
			routes: []config.ModelRoute{
				{Model: "off1", Provider: "p1", Lane: "l1", Tier: "fast", Enabled: false, Weight: 1},
				{Model: "off2", Provider: "p1", Lane: "l2", Tier: "balanced", Enabled: false, Weight: 1},
			},
			want: true,
		},
		{
			// 健康条目缺失（热更新窗口）按可用处理：不得因缺条目而熔断。
			name:   "启用道但健康条目缺失：保守判为可用",
			routes: []config.ModelRoute{enabled("a", "p1", "l1", "balanced")},
			setup:  func(r *Router) { delete(r.health, "a") },
			want:   false,
		},
	}
	for _, c := range cases {
		r := New(c.routes)
		// setup 省略 = 不制造任何冷却的基线态，因此必须判空再调用。
		if c.setup != nil {
			c.setup(r)
		}
		if got := r.AllInCooldown(); got != c.want {
			t.Errorf("%s: AllInCooldown() = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAllInCooldownConsistentWithEarliestAvailable 交叉校验两个熔断读数必须自洽：
// AllInCooldown=true 时 EarliestAvailable 必须给出未来时刻（闸门要有时长来源）；
// =false 时必须返回 0（有可用道就不许让闸门空转）。
// 这是 engine.go:1730 那条「waitUntil <= now 就补 15s」兜底能不被触发的前提。
func TestAllInCooldownConsistentWithEarliestAvailable(t *testing.T) {
	r := New([]config.ModelRoute{enabled("a", "p1", "l1", "balanced"), enabled("b", "p1", "l2", "fast")})
	if r.AllInCooldown() {
		t.Fatal("全健康时不得判熔断")
	}
	if got := r.EarliestAvailable(); got != 0 {
		t.Fatalf("有可用道时 EarliestAvailable 应为 0，实际 %d", got)
	}
	r.CooldownByLane("lane:l1", 2*time.Minute, "rate_limited", "429")
	r.CooldownByLane("lane:l2", time.Minute, "upstream", "502")
	if !r.AllInCooldown() {
		t.Fatal("两条启用道全冷却时应判熔断")
	}
	now := time.Now().UnixMilli()
	got := r.EarliestAvailable()
	if got <= now {
		t.Fatalf("熔断时 EarliestAvailable 必须是未来时刻，实际 %d（now=%d）", got, now)
	}
	// 取最早解禁的那条：l2 只冷 1 分钟，必须早于 l1 的 2 分钟。
	if got >= time.Now().Add(90*time.Second).UnixMilli() {
		t.Fatalf("EarliestAvailable 应返回最早解禁时刻（约 1 分钟内），实际距今 %dms", got-now)
	}
	// 解禁一条后两个读数必须同时翻转（不能一个说熔断、一个说有可用道）。
	r.ResetCooldown("b")
	if r.AllInCooldown() {
		t.Error("解除一条冷却后不得仍判熔断")
	}
	if got := r.EarliestAvailable(); got != 0 {
		t.Errorf("解除一条冷却后 EarliestAvailable 应回到 0，实际 %d", got)
	}
}

// TestRouteOfLockedFallbackAndHit 钉私有查表函数的两个分支：命中返回真实条目、未命中返回零值。
// 不是「调用一下不 panic」—— routeOfLocked 的输出直接决定 RecordFailure 用哪套 MaxFail/CooldownM，
// 所以这里通过「查不到 => 默认 3 次 / 30 分钟」的可观测后果来断言它。
func TestRouteOfLockedFallbackAndHit(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "cfg-2", Provider: "p1", Tier: "balanced", Enabled: true, Weight: 10, MaxFail: 2, CooldownM: 5},
	})
	// 命中分支：按路由表自己的阈值冷却（2 次即冷，5 分钟）。
	r.RecordFailure("cfg-2", "upstream", "boom")
	r.RecordFailure("cfg-2", "upstream", "boom")
	h := snapshotOf(t, r, "cfg-2")
	if !cooling(h) {
		t.Fatal("MaxFail=2 时第二次失败必须进冷却")
	}
	// 区间断言而不是「不小于 4 分钟」：漏读 rt.CooldownM 会退化成默认 30 分钟，
	// 那正好落在「> 4 分钟」一侧 —— 原判据会把实现缺陷判绿，所以这里两侧都卡。
	left := h.CooldownTo - time.Now().UnixMilli()
	if left < int64(4*time.Minute/time.Millisecond) || left > int64(6*time.Minute/time.Millisecond) {
		t.Fatalf("MaxFail=2/CooldownM=5 应冷却约 5 分钟（既不更短，也不因漏读 CooldownM 退化成默认 30 分钟），实际剩余 %dms", left)
	}
	// 未命中分支：路由表里查不到 => 用内置默认 3 次阈值，而不是「一次失败就冷却」。
	r.RecordFailure("ghost", "upstream", "boom")
	r.RecordFailure("ghost", "upstream", "boom")
	gh := snapshotOf(t, r, "ghost")
	if cooling(gh) {
		t.Fatal("未知模型按默认 MaxFail=3，两次失败不应冷却")
	}
	r.RecordFailure("ghost", "upstream", "boom")
	if h := snapshotOf(t, r, "ghost"); !cooling(h) {
		t.Fatal("第三次失败应按默认阈值进入冷却")
	}
	// ProviderOf / LaneOf 走的也是同一个查表函数，未命中都必须给零值而非猜测值。
	if got := r.ProviderOf("ghost"); got != "" {
		t.Errorf("未命中模型 ProviderOf 应为空串，实际 %q", got)
	}
	if got := r.LaneOf("ghost"); got != "" {
		t.Errorf("未命中模型 LaneOf 应为空串，实际 %q", got)
	}
}

// TestRecordFailureDefaultCooldownMinutes 覆盖 routeOfLocked 命中但 CooldownM<=0 的分支：
// 必须回退默认 30 分钟（配置文件里漏填 cooldownM 是常态，不能退化成「冷却 0 分钟 = 不冷却」）。
func TestRecordFailureDefaultCooldownMinutes(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "nocfg", Provider: "p1", Tier: "fast", Enabled: true, Weight: 10, MaxFail: 1},
	})
	r.RecordFailure("nocfg", "upstream", "boom")
	h := snapshotOf(t, r, "nocfg")
	if !cooling(h) {
		t.Fatal("MaxFail=1 时首次失败即应冷却")
	}
	left := h.CooldownTo - time.Now().UnixMilli()
	if left < int64(25*time.Minute/time.Millisecond) || left > int64(31*time.Minute/time.Millisecond) {
		t.Fatalf("CooldownM 缺省时应回退 30 分钟，实际剩余 %dms", left)
	}
	// MaxFail<=0 同理回退 3：单次失败不得直接熔断一条车道。
	r2 := New([]config.ModelRoute{
		{Model: "zerofail", Provider: "p1", Tier: "fast", Enabled: true, Weight: 10},
	})
	r2.RecordFailure("zerofail", "upstream", "boom")
	if h := snapshotOf(t, r2, "zerofail"); cooling(h) {
		t.Fatal("MaxFail 缺省时应按 3 次才冷却")
	}
}

// TestRecordFailureStructuralKindCaseInsensitive 覆盖 errKind 归一化分支：
// 上游回传的类别大小写混杂（MODEL_UNAVAILABLE / Missing_Credential），必须先 ToLower 再判，
// 否则结构性故障只拿到 30 分钟普通冷却，模型被反复放回池里撞墙。
func TestRecordFailureStructuralKindCaseInsensitive(t *testing.T) {
	cases := []struct {
		name    string
		errKind string
	}{
		{"小写 model_unavailable", "model_unavailable"},
		{"大写 MODEL_UNAVAILABLE", "MODEL_UNAVAILABLE"},
		{"混合大小写 Missing_Credential", "Missing_Credential"},
		{"带前缀的 upstream_model_unavailable", "upstream_model_unavailable"},
	}
	for _, c := range cases {
		r := New([]config.ModelRoute{enabled("m", "p1", "l1", "balanced")})
		r.RecordFailure("m", c.errKind, "detail")
		h := snapshotOf(t, r, "m")
		if h.CooldownTo < time.Now().Add(6*time.Hour).UnixMilli() {
			t.Errorf("%s: 结构性错误应拿长冷却（>=6h），实际距今 %dms", c.name, h.CooldownTo-time.Now().UnixMilli())
		}
	}
	// 负向对照：普通 upstream 错误走阈值冷却，绝不能吃 12 小时。
	r := New([]config.ModelRoute{enabled("m", "p1", "l1", "balanced")})
	r.RecordFailure("m", "upstream", "502")
	if h := snapshotOf(t, r, "m"); h.CooldownTo != 0 {
		t.Fatalf("未达阈值的普通失败不应冷却，实际 CooldownTo=%d", h.CooldownTo)
	}
	r.RecordFailure("m", "upstream", "502")
	r.RecordFailure("m", "upstream", "502")
	if h := snapshotOf(t, r, "m"); h.CooldownTo >= time.Now().Add(6*time.Hour).UnixMilli() {
		t.Fatal("普通失败达阈值只应按 CooldownM 冷却，不得判成结构性长冷却")
	}
}

// TestRecordSuccessBranches 覆盖 RecordSuccess 的两条分支与副作用清零：
// ① 首次成功 AvgMs 直接取样本（无历史平均可加权）；
// ② 后续成功按 0.7/0.3 EWMA（慢模型要被拉开、但不是一刀切零）；
// ③ 未知模型补建条目；④ LastErr 清空 + ConsecFail 归零（成功必须能洗掉失败印记）。
func TestRecordSuccessBranches(t *testing.T) {
	r := New([]config.ModelRoute{enabled("m", "p1", "l1", "balanced")})
	r.RecordFailure("m", "upstream", "boom")
	r.RecordSuccess("m", 1000)
	h := snapshotOf(t, r, "m")
	if h.AvgMs != 1000 {
		t.Fatalf("首次成功 AvgMs 应直接取样本，实际 %.1f", h.AvgMs)
	}
	if h.ConsecFail != 0 || h.LastErr != "" {
		t.Fatalf("成功必须清掉失败印记，实际 consecFail=%d lastErr=%q", h.ConsecFail, h.LastErr)
	}
	r.RecordSuccess("m", 2000)
	h = snapshotOf(t, r, "m")
	want := 1000*0.7 + 2000*0.3
	if diff := h.AvgMs - want; diff > 0.01 || diff < -0.01 {
		t.Fatalf("后续成功应按 EWMA 0.7/0.3 更新：want %.1f got %.1f", want, h.AvgMs)
	}
	if h.Success != 2 || h.Failure != 1 {
		t.Fatalf("计数应累加不清零，实际 success=%d failure=%d", h.Success, h.Failure)
	}
	if h.LastUsedAt <= 0 {
		t.Error("成功必须记 LastUsedAt（面板靠它分辨「冷模型」与「没人用的健康模型」）")
	}
	// 未知模型：补建条目而不是丢弃这次成功。
	r.RecordSuccess("ghost", 500)
	if h := snapshotOf(t, r, "ghost"); h.Success != 1 || h.AvgMs != 500 {
		t.Fatalf("未知模型的成功应被补建记录，实际 %+v", h)
	}
}

// TestRecordFailureBranches 覆盖 RecordFailure 的计数与阈值分支（未达阈值只计数不冷却）。
func TestRecordFailureBranches(t *testing.T) {
	r := New([]config.ModelRoute{{Model: "m", Provider: "p1", Tier: "balanced", Enabled: true, Weight: 10, MaxFail: 3, CooldownM: 10}})
	r.RecordFailure("m", "upstream", "boom")
	h := snapshotOf(t, r, "m")
	if h.Failure != 1 || h.ConsecFail != 1 || cooling(h) {
		t.Fatalf("未达阈值应只计数不冷却，实际 %+v", h)
	}
	r.RecordSuccess("m", 100)
	r.RecordFailure("m", "upstream", "boom")
	r.RecordFailure("m", "upstream", "boom")
	if h := snapshotOf(t, r, "m"); cooling(h) {
		t.Fatal("成功已把 ConsecFail 归零，之后两次失败不应达阈值 3")
	}
	r.RecordFailure("m", "upstream", "boom")
	h = snapshotOf(t, r, "m")
	if !cooling(h) {
		t.Fatal("连续第三次失败应达阈值进冷却")
	}
	left := h.CooldownTo - time.Now().UnixMilli()
	if left < int64(8*time.Minute/time.Millisecond) || left > int64(11*time.Minute/time.Millisecond) {
		t.Fatalf("CooldownM=10 应冷却约 10 分钟，实际剩余 %dms", left)
	}
	if h.LastErr != "upstream: boom" {
		t.Errorf("LastErr = %q, want %q", h.LastErr, "upstream: boom")
	}
	// 未知模型补建条目。
	r.RecordFailure("ghost", "upstream", "x")
	if h := snapshotOf(t, r, "ghost"); h.Failure != 1 || h.ConsecFail != 1 {
		t.Fatalf("未知模型的失败应被补建记录，实际 %+v", h)
	}
}

// TestTrimMsgBranches 表驱动钉 trimMsg 四分支：有类别加冒号、无类别不加、
// 恰好在 300 不截、超长必须截到 300（含中文按字节截的行为也钉住，防止改成 rune 截后长度变化没人发现）。
// 为什么要截断：errMsg 来自上游响应体，不截会把整条 JSON 塞进健康度文件与事件流。
func TestTrimMsgBranches(t *testing.T) {
	cases := []struct {
		name string
		kind string
		msg  string
		want string
	}{
		{"有类别带前缀", "upstream", "boom", "upstream: boom"},
		{"无类别不加冒号", "", "boom", "boom"},
		{"两者皆空得空串", "", "", ""},
		{"恰好300不截", "", strings.Repeat("x", 300), strings.Repeat("x", 300)},
		{"超长截到300", "", strings.Repeat("y", 301), strings.Repeat("y", 300)},
		// 前缀占的 14 字节也计入 300 上限，所以尾部只能留 286 个 z。
		{"前缀占位一起算长度", "rate_limited", strings.Repeat("z", 400), "rate_limited: " + strings.Repeat("z", 286)},
		{"无类别超长也截", "", strings.Repeat("中", 400), strings.Repeat("中", 400)[:300]},
	}
	for _, c := range cases {
		got := trimMsg(c.kind, c.msg)
		if got != c.want {
			t.Errorf("%s: trimMsg(%q, msg) 长度 %d 前缀 %q，want 长度 %d 前缀 %q", c.name, c.kind, len(got), first(got, 24), len(c.want), first(c.want, 24))
		}
		if len(got) > 300 {
			t.Errorf("%s: 结果长度 %d 超过 300 上限", c.name, len(got))
		}
	}
}

// first 取前 n 个字节用于失败信息展示，避免把 300 字符全打进日志。
func first(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TestUpdateRoutesPreservesHealth 覆盖 UpdateRoutes 两个分支：
// ① 新模型建条目；② 已有模型只换 provider/tier/disabled，**保留 Success/Failure/CooldownTo**
// —— 热更新（engine.go:363/723/2117）每次都整体替换路由表，若健康度被重置，
// 一次改配置就会把已知死模型放回池里，正是 2026-10-19 事故的配置面版本。
func TestUpdateRoutesPreservesHealth(t *testing.T) {
	r := New([]config.ModelRoute{enabled("keep", "p1", "l1", "balanced")})
	r.RecordFailure("keep", "upstream", "boom")
	r.RecordSuccess("keep", 1500)
	before := snapshotOf(t, r, "keep")
	r.UpdateRoutes([]config.ModelRoute{
		{Model: "keep", Provider: "p2", Lane: "l9", Tier: "quality", Enabled: true, Weight: 7, MaxFail: 1, CooldownM: 3},
		{Model: "fresh", Provider: "p3", Tier: "fast", Enabled: false, Weight: 5},
	})
	after := snapshotOf(t, r, "keep")
	if after.Success != before.Success || after.Failure != before.Failure || after.AvgMs != before.AvgMs {
		t.Fatalf("热更新必须保留健康统计：before %+v after %+v", before, after)
	}
	if after.Provider != "p2" || after.Tier != "quality" {
		t.Fatalf("热更新必须刷新 provider/tier，实际 %+v", after)
	}
	if after.Disabled {
		t.Error("仍启用的条目不得被置 Disabled")
	}
	// 新条目按 Enabled 反推 Disabled（禁用条目不得被 Pick 选中）。
	if h := snapshotOf(t, r, "fresh"); !h.Disabled {
		t.Error("Enabled=false 的新条目应落成 Disabled=true")
	}
	// 车道归属随热更新改变（routeOfLocked 命中新条目 => LaneOf 给新键）。
	if got := r.LaneOf("keep"); got != "lane:l9" {
		t.Errorf("LaneOf 应跟随最新路由表，实际 %q", got)
	}
	// 整体替换语义：被删掉的模型不再参与调度，但它的冷却不会被误算进熔断。
	if got, err := r.PickStrict("quality", nil); err != nil || got != "keep" {
		t.Fatalf("quality 档只剩 keep，PickStrict=(%q,%v)", got, err)
	}
}

// TestSaveNoopWithoutPathAndWritesWithPath 覆盖 Save 的三条分支：
// path 为空返回 nil（纯内存路由器不得碰文件系统）；有 path 时落盘且能被新实例读回；
// 落盘内容含被 CooldownByProvider 写进去的冷却（0% 函数链路的持久化闭环）。
func TestSaveNoopWithoutPathAndWritesWithPath(t *testing.T) {
	if err := New(laneRoutes()).Save(); err != nil {
		t.Fatalf("纯内存路由器 Save 应静默返回 nil，实际 %v", err)
	}
	path := filepath.Join(t.TempDir(), "health.json")
	r := NewWithPersistence(laneRoutes(), path)
	r.CooldownByProvider("our-free-model", 30*time.Minute, "rate_limited", "429")
	if err := r.Save(); err != nil {
		t.Fatalf("Save 落盘失败: %v", err)
	}
	r2 := NewWithPersistence(laneRoutes(), path)
	for _, m := range []string{"ofm-a", "kilo-a"} {
		if h := snapshotOf(t, r2, m); !cooling(h) {
			t.Fatalf("重启后 %s 应仍在冷却（持久化闭环），实际 %+v", m, h)
		}
	}
	if h := snapshotOf(t, r2, "direct-a"); cooling(h) {
		t.Error("邻居 provider 不得因持久化被误冷")
	}
	if got := len(r2.AvailableLanes()); got != 1 {
		t.Fatalf("重启后可用车道应仍是 1 条，实际 %d（%v）", got, r2.AvailableLanes())
	}
}

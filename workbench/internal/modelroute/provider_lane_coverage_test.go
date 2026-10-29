package modelroute

// 本文件补齐 provider / lane 两个「故障域维度」的逐分支判据（CI 口径 modelroute 52.6%，
// 其中 router.go 的 8 个函数 0.0% 覆盖，就是那 44% 缺口的主体）。
//
// 为什么值得逐分支钉死，而不是「调用一下不 panic」：engine 的车道级冷却修正链把判据
// 全落在这些方法上 —— engine.go:1168 用 LaneOf 取故障域、1170 用 CooldownByLane 冷却、
// 1185 与 1724 用 AvailableLanes 决定「能否立刻重调度」，plan_stage.go:102 用 ProviderOf
// 非空当「模型在不在路由表里」的哨兵。这些语义一旦退回上一版（把 provider 当故障域），
// 现场表现就是 2026-10-19 那次事故：our-free-model 的 OFM 车道 429，同 provider 的 Kilo
// 车道直连探测明明可用却被一起停掉，直接违反目标要求 4「自动切换到队列中其他正常可用
// 的模型兜底」。

import (
	"testing"
	"time"

	"workbench/internal/config"
)

// laneRoutes 构造「一个 provider 挂三条独立上游车道」的最小仿真路由表。
// 这就是 2026-10-19 实测的真实形态：our-free-model 插件把 OFM 免费 / Kilo 免费池 /
// EAC 协付三条独立线路统一注册成同一个 provider，所以按 provider 分组必然误伤；
// direct-a 故意不配 Lane，用来走「回退 provider」那条分支。
func laneRoutes() []config.ModelRoute {
	return []config.ModelRoute{
		{Model: "ofm-a", Provider: "our-free-model", Lane: "ofm", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "ofm-b", Provider: "our-free-model", Lane: "OFM", Tier: "fast", Enabled: true, Weight: 50, MaxFail: 3, CooldownM: 30},
		{Model: "kilo-a", Provider: "our-free-model", Lane: "kilo", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "eac-a", Provider: "our-free-model", Lane: "eac", Tier: "fast", Enabled: true, Weight: 30, MaxFail: 2, CooldownM: 30},
		{Model: "direct-a", Provider: "kilo-lane", Lane: "", Tier: "balanced", Enabled: true, Weight: 80, MaxFail: 3, CooldownM: 30},
	}
}

// snapshotOf 取某个模型的健康度快照。测试侧一律走导出的 Health() 读：
// 断言的是面板与台账实际看到的那份数据，而不是私有字段。
func snapshotOf(t *testing.T, r *Router, model string) Health {
	t.Helper()
	for _, h := range r.Health() {
		if h.Model == model {
			return h
		}
	}
	t.Fatalf("健康度快照里没有 %s（说明该条目没被建出来）", model)
	return Health{}
}

// cooling 判断某条健康度当前是否处于冷却。CooldownTo 是绝对毫秒，
// 因此这里按「now 之后」判，不比较时长，避免依赖计时精度。
func cooling(h Health) bool {
	return h.CooldownTo > time.Now().UnixMilli()
}

// has 是字符串切片包含判定（本包测试不引入外部依赖）。
func has(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestLaneOfPrefersLaneThenProvider 钉 laneOf 的归一化契约：Lane 优先、小写化、去空格，
// Lane 未配（含纯空格）才回退 Provider。
// 证伪面：实现若退回「只看 Provider」，前三条用例全红；去掉 TrimSpace，第 2、3 条红。
func TestLaneOfPrefersLaneThenProvider(t *testing.T) {
	cases := []struct {
		name string
		rt   config.ModelRoute
		want string
	}{
		{"显式 lane 归一为小写", config.ModelRoute{Lane: "OFM"}, "lane:ofm"},
		{"lane 两侧空格必须去掉", config.ModelRoute{Lane: "  Kilo-Auto  "}, "lane:kilo-auto"},
		{"未配 lane 回退 provider", config.ModelRoute{Provider: "Our-Free-Model"}, "provider:our-free-model"},
		{"纯空格 lane 视为未配", config.ModelRoute{Lane: "   ", Provider: "P"}, "provider:p"},
		{"lane 优先于 provider", config.ModelRoute{Lane: "eac", Provider: "our-free-model"}, "lane:eac"},
		{"两者皆空置为 provider 空键", config.ModelRoute{}, "provider:"},
	}
	for _, c := range cases {
		if got := laneOf(c.rt); got != c.want {
			t.Errorf("%s: laneOf(%+v) = %q, want %q", c.name, c.rt, got, c.want)
		}
	}
	// 核心不变量（要求 4 的根）：同一个 provider 下的两条车道必须是两个不同故障域键。
	a := laneOf(config.ModelRoute{Provider: "our-free-model", Lane: "ofm"})
	b := laneOf(config.ModelRoute{Provider: "our-free-model", Lane: "kilo"})
	if a == b {
		t.Fatalf("同 provider 的不同 lane 必须解析成不同故障域，实际都是 %q（会把健康车道一起冷却）", a)
	}
}

// TestLaneOfIsScopedToRouteTable 钉 LaneOf 的两个调用方契约。
// engine.go:1169 靠「未知必须空串」决定要不要发起冷却：若未知模型回退成 provider: 幻影键，
// 就会去冷却一条不存在的路由；且 LaneOf 必须按路由表解析（不是健康表），
// 否则热更新删掉的车道还能被当成有效故障域继续冷却。
func TestLaneOfIsScopedToRouteTable(t *testing.T) {
	r := New(laneRoutes())
	if got := r.LaneOf("ofm-a"); got != "lane:ofm" {
		t.Errorf("LaneOf(ofm-a) = %q, want lane:ofm", got)
	}
	if got := r.LaneOf("direct-a"); got != "provider:kilo-lane" {
		t.Errorf("未配 lane 的模型应回退 provider，实际 %q", got)
	}
	if got := r.LaneOf("not-in-table"); got != "" {
		t.Errorf("未知模型必须返回空串，实际 %q（engine 会照这个值去冷却幻影车道）", got)
	}
	if r.LaneOf("ofm-a") == r.LaneOf("kilo-a") {
		t.Fatalf("ofm-a 与 kilo-a 必须解析成不同故障域，实际同为 %q", r.LaneOf("ofm-a"))
	}
	// 同 lane 的两条模型（ofm-a / ofm-b 大小写不同）必须归到同一故障域，
	// 否则一条 429 只冷却一半，剩下的会继续撞同一面上游。
	if r.LaneOf("ofm-a") != r.LaneOf("ofm-b") {
		t.Fatalf("同 lane 不同大小写必须归一，实际 %q vs %q", r.LaneOf("ofm-a"), r.LaneOf("ofm-b"))
	}
	// 整体替换路由表后，被删模型立刻不再是有效车道。
	r.UpdateRoutes([]config.ModelRoute{
		{Model: "kilo-a", Provider: "our-free-model", Lane: "kilo", Tier: "balanced", Enabled: true, Weight: 100},
	})
	if got := r.LaneOf("ofm-a"); got != "" {
		t.Errorf("已从路由表删除的模型必须返回空串，实际 %q", got)
	}
	if got := r.LaneOf("kilo-a"); got != "lane:kilo" {
		t.Errorf("保留模型应仍能解析车道，实际 %q", got)
	}
}

// TestProviderOfResolvesRouteTable 钉 ProviderOf 作为「表内哨兵」的用法
// （plan_stage.go:102 用它判断点名模型在不在路由表里）。未知必须空串，命中要拿到真实 provider。
func TestProviderOfResolvesRouteTable(t *testing.T) {
	r := New(laneRoutes())
	cases := []struct {
		model string
		want  string
	}{
		{"ofm-a", "our-free-model"},
		{"direct-a", "kilo-lane"},
		{"ghost", ""},
	}
	for _, c := range cases {
		if got := r.ProviderOf(c.model); got != c.want {
			t.Errorf("ProviderOf(%s) = %q, want %q", c.model, got, c.want)
		}
	}
	// 表内但没配 provider 的模型：哨兵返回空串，调用方会把它当「不在表里」。
	// 这是该 API 的已知限制，钉住它是为了防止有人改成从健康表回填别的值。
	r.UpdateRoutes([]config.ModelRoute{
		{Model: "noprov", Tier: "balanced", Enabled: true, Weight: 10},
	})
	if got := r.ProviderOf("noprov"); got != "" {
		t.Errorf("未配 provider 的表内模型 ProviderOf 应为空串，实际 %q", got)
	}
}

// TestCooldownByProviderCoolsWholeProvider 正向：一个 provider 下的多条启用路由全部进冷却，
// 邻居 provider 完全不受影响（这正是「429 只冷却出问题的车道，其它车道照常调度」）。
// 返回计数必须等于被冷却条数，engine 侧靠它写台账。
func TestCooldownByProviderCoolsWholeProvider(t *testing.T) {
	r := New(laneRoutes())
	n := r.CooldownByProvider("our-free-model", 5*time.Minute, "rate_limited", "429 upstream")
	if n != 4 {
		t.Fatalf("our-free-model 下应有 4 条启用车道被冷却，实际 n=%d", n)
	}
	for _, m := range []string{"ofm-a", "ofm-b", "kilo-a", "eac-a"} {
		if h := snapshotOf(t, r, m); !cooling(h) {
			t.Errorf("%s 应处于冷却，CooldownTo=%d", m, h.CooldownTo)
		}
	}
	if h := snapshotOf(t, r, "direct-a"); cooling(h) {
		t.Errorf("kilo-lane 这条独立 provider 不得被 our-free-model 的 429 冷却")
	}
	if got := r.AvailableProviders(); len(got) != 1 || got[0] != "kilo-lane" {
		t.Fatalf("AvailableProviders 应只剩 kilo-lane，实际 %v", got)
	}
	// LastErr 必须带错误类别前缀，否则面板只知道「冷却中」不知道为什么。
	if got := snapshotOf(t, r, "ofm-a").LastErr; got != "rate_limited: 429 upstream" {
		t.Errorf("LastErr = %q, want %q", got, "rate_limited: 429 upstream")
	}
}

// TestCooldownByProviderIsCaseInsensitive 钉 EqualFold 语义：provider 名大小写不一致时
// （配置手写、上游返回体大小写漂移）仍要命中同一批车道。证伪面：EqualFold 改成 == 即红。
func TestCooldownByProviderIsCaseInsensitive(t *testing.T) {
	r := New(laneRoutes())
	if n := r.CooldownByProvider("OUR-FREE-MODEL", time.Minute, "rate_limited", "429"); n != 4 {
		t.Fatalf("provider 匹配应大小写不敏感，实际 n=%d", n)
	}
}

// TestCooldownByProviderNonPositiveDurationIsNoop 边界：d<=0 必须直接返回 0 且不动任何状态。
// 为什么单独钉：调用方若误传 0（配置缺省、秒数算成 0），绝不能被当成「取消冷却」或
// 「按 now 冷却」来用 —— 那会让 429 完全不生效，或把 LastErr/LastUsedAt 写脏。
func TestCooldownByProviderNonPositiveDurationIsNoop(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Minute} {
		r := New(laneRoutes())
		before := snapshotOf(t, r, "ofm-a")
		if n := r.CooldownByProvider("our-free-model", d, "rate_limited", "429"); n != 0 {
			t.Fatalf("d=%v 时不得冷却任何车道，实际 n=%d", d, n)
		}
		after := snapshotOf(t, r, "ofm-a")
		if after.CooldownTo != before.CooldownTo || after.LastErr != "" || after.LastUsedAt != 0 {
			t.Fatalf("d=%v 时健康度必须一字不动，实际 %+v", d, after)
		}
	}
}

// TestCooldownByProviderUnknownDoesNotCoolEverything 负向：provider 名打错时必须命中 0 条，
// 而且绝不能退化成冷却全部车道。
// 证伪面：实现若把「没匹配上」当成「空 provider = 全池」，AvailableLanes 会掉到 0 而红。
func TestCooldownByProviderUnknownDoesNotCoolEverything(t *testing.T) {
	r := New(laneRoutes())
	before := len(r.AvailableLanes())
	if n := r.CooldownByProvider("no-such-provider", 5*time.Minute, "rate_limited", "429"); n != 0 {
		t.Fatalf("不存在的 provider 应命中 0 条，实际 n=%d", n)
	}
	if got := len(r.AvailableLanes()); got != before {
		t.Fatalf("不存在的 provider 却减少了可用车道：%d -> %d（不得误冷所有车道）", before, got)
	}
	if r.AllInCooldown() {
		t.Fatal("不存在的 provider 冷却后不得判整池不可用")
	}
}

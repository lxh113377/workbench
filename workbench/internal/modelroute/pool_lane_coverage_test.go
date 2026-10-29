package modelroute

// 本文件补齐「可用性判定」与「车道级冷却 / 整池熔断」这一组的逐分支判据。
//
// 为什么必须逐分支钉：engine 的兜底链路（目标要求 4）完全靠这几个读数分流 ——
// engine.go:1185 用 AvailableLanes 决定「冷却后还剩哪些道」并写进事件流；
// engine.go:1724 用 AvailableLanes 是否为空决定「立刻重调度」还是「推闸门等冷却」；
// AllInCooldown 是熔断依据，EarliestAvailable 是闸门时长来源。任一分支把「禁用」
// 「空路由表」「未知键」误判成容量，现场表现就是调度停摆或任务被误判死，且日志看不出成因。

import (
	"testing"
	"time"

	"workbench/internal/config"
)

// TestAvailableProvidersThreeStates 覆盖三态：全健康 / 部分冷却 / provider 级联动冷却。
// 每态同时断「有哪些」和「排序与去重」，防止只断长度把误伤洗绿。
func TestAvailableProvidersThreeStates(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *Router)
		want  []string
	}{
		{
			name:  "全健康：两个 provider 都在册（按字典序去重）",
			setup: func(r *Router) {},
			want:  []string{"kilo-lane", "our-free-model"},
		},
		{
			// 部分冷却：ofm 车道 429 后，our-free-model 内仍有 kilo/eac 健康，
			// provider 级读数必须仍然报出该 provider（这就是要求 4 的「切到其他可用模型」）。
			name:  "同 provider 内只冷一条车道时该 provider 仍在册",
			setup: func(r *Router) { r.CooldownByLane("lane:ofm", time.Minute, "rate_limited", "429") },
			want:  []string{"kilo-lane", "our-free-model"},
		},
		{
			// provider 级联动：整条 provider 冷却后必须从可用性里消失，邻居 provider 必须留着
			// （留着的那条就是「不许洗绿」的另一面：整池并没有全可用）。
			name:  "整条 provider 冷却后从可用性里消失",
			setup: func(r *Router) { r.CooldownByProvider("our-free-model", time.Minute, "rate_limited", "429") },
			want:  []string{"kilo-lane"},
		},
		{
			name: "两条 provider 都冷却后可用列表为空",
			setup: func(r *Router) {
				r.CooldownByProvider("our-free-model", time.Hour, "rate_limited", "429")
				r.CooldownByProvider("kilo-lane", time.Hour, "rate_limited", "429")
			},
			want: []string{},
		},
	}
	for _, c := range cases {
		r := New(laneRoutes())
		c.setup(r)
		got := r.AvailableProviders()
		if len(got) != len(c.want) {
			t.Errorf("%s: AvailableProviders() = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: AvailableProviders() = %v（须字典序去重），want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// TestAvailableProvidersSkipsUnusableSources 负向：三类「不是容量」的来源都不得进可用性读数
// —— 禁用路由、provider 名为空的路由、健康条目缺失（热更新窗口内的竞态面）。
// 证伪面：删掉任一 continue，对应用例即红（把幻影容量报给调用方 = 「有可用道」的假绿）。
func TestAvailableProvidersSkipsUnusableSources(t *testing.T) {
	routes := []config.ModelRoute{
		{Model: "off", Provider: "prov-off", Tier: "fast", Enabled: false, Weight: 10},
		{Model: "noprov", Provider: "", Tier: "fast", Enabled: true, Weight: 10},
		{Model: "real", Provider: "prov-real", Tier: "balanced", Enabled: true, Weight: 10},
	}
	r := New(routes)
	if got := r.AvailableProviders(); len(got) != 1 || got[0] != "prov-real" {
		t.Fatalf("只有既启用又配了 provider 的路由才算容量，实际 %v", got)
	}
	// 健康条目被删（模拟热更新窗口）后该模型不再贡献可用性，也不能 panic。
	delete(r.health, "real")
	if got := r.AvailableProviders(); len(got) != 0 {
		t.Fatalf("健康条目缺失时不得报出可用 provider，实际 %v", got)
	}
	// Disabled 由 Enabled 反推：热更新把车道关掉后必须立刻从可用性里消失。
	r2 := New(laneRoutes())
	r2.UpdateRoutes([]config.ModelRoute{
		{Model: "ofm-a", Provider: "our-free-model", Lane: "ofm", Tier: "balanced", Enabled: false, Weight: 100},
		{Model: "direct-a", Provider: "kilo-lane", Tier: "balanced", Enabled: true, Weight: 80},
	})
	if got := r2.AvailableProviders(); len(got) != 1 || got[0] != "kilo-lane" {
		t.Fatalf("禁用条目不得贡献可用性，实际 %v", got)
	}
}

// TestAvailableLanesThreeStates 覆盖需求点名的三态：全健康 / 部分冷却 / provider 级联动。
// 车道键必须带 lane: 或 provider: 前缀 —— engine 把它原样写进台账与事件流
// （engine.go:1182 的 "| lane=" + lane），读日志的人靠前缀分辨两种故障域。
func TestAvailableLanesThreeStates(t *testing.T) {
	cases := []struct {
		name  string
		setup func(r *Router)
		want  []string
	}{
		{
			name:  "全健康：四条车道（含 provider 回退道）都在",
			setup: func(r *Router) {},
			want:  []string{"lane:eac", "lane:kilo", "lane:ofm", "provider:kilo-lane"},
		},
		{
			name:  "部分冷却：只摘掉出问题的那条",
			setup: func(r *Router) { r.CooldownByLane("lane:ofm", time.Minute, "rate_limited", "429") },
			want:  []string{"lane:eac", "lane:kilo", "provider:kilo-lane"},
		},
		{
			// provider 级联动：整条 our-free-model 冷却后其三条 lane 一起消失，只剩邻居 provider；
			// 此时 engine.go:1724 仍判「有可用道、立刻重调度」，不得推整池闸门。
			name:  "provider 级冷却联动：该 provider 下三条车道一起消失",
			setup: func(r *Router) { r.CooldownByProvider("our-free-model", time.Minute, "rate_limited", "429") },
			want:  []string{"provider:kilo-lane"},
		},
		{
			name: "两条车道分别冷却后只剩两条",
			setup: func(r *Router) {
				r.CooldownByLane("lane:ofm", time.Minute, "rate_limited", "429")
				r.CooldownByLane("lane:kilo", time.Minute, "upstream", "502")
			},
			want: []string{"lane:eac", "provider:kilo-lane"},
		},
		{
			name: "全池冷却后可用车道为空（熔断面的前提）",
			setup: func(r *Router) {
				r.CooldownByProvider("our-free-model", time.Hour, "rate_limited", "429")
				r.CooldownByProvider("kilo-lane", time.Hour, "rate_limited", "429")
			},
			want: []string{},
		},
	}
	for _, c := range cases {
		r := New(laneRoutes())
		c.setup(r)
		got := r.AvailableLanes()
		if len(got) != len(c.want) {
			t.Errorf("%s: AvailableLanes() = %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: AvailableLanes() = %v（须字典序去重），want %v", c.name, got, c.want)
				break
			}
		}
	}
}

// TestAvailableLanesVsProvidersAsymmetry 钉住两个读数在「空 provider」上的不对称：
// AvailableProviders 要求 Provider 非空，AvailableLanes 不要求（空 provider 归到 "provider:" 键）。
// 这条不是断言「哪个更好」，而是防止有人单方面改一个语义而调用方毫不知情
// （engine.go:1185/1724 读的是 lane 面，面板读的是 provider 面，两者会给出不同答案）。
func TestAvailableLanesVsProvidersAsymmetry(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "bare", Provider: "", Lane: "", Tier: "fast", Enabled: true, Weight: 10},
	})
	if got := r.AvailableProviders(); len(got) != 0 {
		t.Errorf("AvailableProviders 应跳过空 provider，实际 %v", got)
	}
	if got := r.AvailableLanes(); len(got) != 1 || got[0] != "provider:" {
		t.Errorf("AvailableLanes 按现实现把空 provider 归到 provider: 键，实际 %v", got)
	}
}

// TestCooldownByLaneHitsOnlyThatLane 正向（要求 4 的核心判据）：同 provider 下只冷却出问题的那条车道。
// 对应 2026-10-19 实测：OFM 429 期间 Kilo 车道直连探测可用，这里就是钉住「Kilo 仍可选」。
// 证伪面：实现若退回 provider 级冷却（把 laneKey 当 provider 比），kilo-a 被一起冷却 => 红。
func TestCooldownByLaneHitsOnlyThatLane(t *testing.T) {
	r := New(laneRoutes())
	n := r.CooldownByLane("lane:ofm", 5*time.Minute, "rate_limited", "429 rate limited")
	if n != 2 {
		t.Fatalf("lane:ofm 下只有 ofm-a / ofm-b 两条，实际 n=%d", n)
	}
	for _, m := range []string{"ofm-a", "ofm-b"} {
		if h := snapshotOf(t, r, m); !cooling(h) {
			t.Errorf("%s 应被本车道冷却，CooldownTo=%d", m, h.CooldownTo)
		}
	}
	for _, m := range []string{"kilo-a", "eac-a", "direct-a"} {
		if h := snapshotOf(t, r, m); cooling(h) {
			t.Errorf("%s 属另一条故障域，不得被 lane:ofm 的 429 冷却", m)
		}
	}
	// 兜底必须仍然成立：仍能选出健康车道，且绝不会选到被冷却的模型。
	// 固定 20 轮而不是 1 轮，是因为 Pick 是加权随机 —— 单轮通过说明不了排除逻辑生效。
	for i := 0; i < 20; i++ {
		got, err := r.Pick("", nil)
		if err != nil {
			t.Fatalf("还有健康车道时不得报「没有可用模型」: %v", err)
		}
		if got == "ofm-a" || got == "ofm-b" {
			t.Fatalf("冷却中的 %s 仍被选中（排除分支失效）", got)
		}
	}
	if got := r.AvailableLanes(); has(got, "lane:ofm") {
		t.Errorf("lane:ofm 已冷却却仍在可用列表，实际 %v", got)
	}
	if got := snapshotOf(t, r, "ofm-a").LastErr; got != "rate_limited: 429 rate limited" {
		t.Errorf("LastErr = %q，须带错误类别前缀（面板要靠它分辨 429 与 5xx）", got)
	}
}

// TestCooldownByLaneUsesProviderFallback 边界：未配 Lane 的模型只能通过 provider: 回退键命中。
// laneOf 的回退分支在此变成可观测行为 —— 证伪面：删回退分支则 n=0 即红。
func TestCooldownByLaneUsesProviderFallback(t *testing.T) {
	r := New(laneRoutes())
	if n := r.CooldownByLane("provider:kilo-lane", time.Minute, "rate_limited", "429"); n != 1 {
		t.Fatalf("provider 回退键应命中 direct-a 一条，实际 n=%d", n)
	}
	if h := snapshotOf(t, r, "direct-a"); !cooling(h) {
		t.Error("direct-a 应被 provider 回退键冷却")
	}
	// 键前缀必须区分两种故障域：provider:kilo-lane 不得命中名为 kilo-a 的 lane:kilo 模型。
	if h := snapshotOf(t, r, "kilo-a"); cooling(h) {
		t.Error("kilo-a 属 lane:kilo，不得被 provider:kilo-lane 命中")
	}
}

// TestCooldownByLaneUnknownKeyIsNoop 负向：拼错的车道键命中 0 条，且不误伤任何健康车道。
// 「严禁猜标识符」在这条路径上有真实后果：若把「不相等」写成前缀/包含匹配，
// 一个猜出来的键就能把整池洗成冷却。证伪面：!= 改成 strings.HasPrefix 即红。
func TestCooldownByLaneUnknownKeyIsNoop(t *testing.T) {
	r := New(laneRoutes())
	for _, key := range []string{"lane:ofm-x", "ofm", "provider:our-free-model", ""} {
		if n := r.CooldownByLane(key, time.Minute, "rate_limited", "429"); n != 0 {
			t.Fatalf("车道键 %q 不该命中却命中 %d 条", key, n)
		}
	}
	if got := len(r.AvailableLanes()); got != 4 {
		t.Fatalf("未知键不得减少可用车道，期望 4 实际 %d", got)
	}
	if r.AllInCooldown() {
		t.Fatal("未知键是空操作，不得判整池不可用")
	}
}

// TestCooldownByLaneNonPositiveDurationAndNoShortening 两条边界合一：
// ① d<=0 是空操作（返回 0、不动状态、不判熔断）；
// ② 已有长冷却不被一次短 429 洗短 —— 现场成因：一条车道已因 model_unavailable 被判 12h，
// 随后一次普通 429 只给 1 分钟，无条件覆盖 CooldownTo 会把结构性故障的长冷却洗短，
// 模型被提前放回池里反复撞同一面墙。
func TestCooldownByLaneNonPositiveDurationAndNoShortening(t *testing.T) {
	r := New(laneRoutes())
	if n := r.CooldownByLane("lane:ofm", 0, "rate_limited", "429"); n != 0 {
		t.Fatalf("d=0 时不得冷却任何车道，实际 n=%d", n)
	}
	if h := snapshotOf(t, r, "ofm-a"); h.CooldownTo != 0 || h.LastErr != "" {
		t.Fatalf("d=0 时健康度必须一字不动，实际 %+v", h)
	}
	if n := r.CooldownByLane("lane:ofm", -time.Minute, "rate_limited", "429"); n != 0 {
		t.Fatalf("d<0 时不得冷却任何车道，实际 n=%d", n)
	}
	r.RecordFailure("ofm-a", "model_unavailable", "EOL")
	long := snapshotOf(t, r, "ofm-a").CooldownTo
	if long <= time.Now().Add(6*time.Hour).UnixMilli() {
		t.Fatalf("前置条件不成立：ofm-a 应先拿到长冷却，实际 %d", long)
	}
	if n := r.CooldownByLane("lane:ofm", time.Minute, "rate_limited", "429"); n != 2 {
		t.Fatalf("n = %d, want 2", n)
	}
	if got := snapshotOf(t, r, "ofm-a").CooldownTo; got != long {
		t.Fatalf("短冷却不得覆盖长冷却：%d -> %d", long, got)
	}
	// 冷却时刻不变，LastErr 仍要更新：「什么时候解禁」与「最近一次为什么失败」是两条独立事实。
	if got := snapshotOf(t, r, "ofm-a").LastErr; got != "rate_limited: 429" {
		t.Errorf("LastErr 应更新为最近一次错误，实际 %q", got)
	}
}

// TestCooldownByLaneCreatesMissingHealthEntry 边界：健康表缺条目时补建并冷却，不 panic。
// 证伪面：删掉补建分支 —— nil 解引用 panic，或该模型漏冷却（断言红），两种都拦得住。
func TestCooldownByLaneCreatesMissingHealthEntry(t *testing.T) {
	r := New(laneRoutes())
	delete(r.health, "ofm-b")
	if n := r.CooldownByLane("lane:ofm", time.Minute, "rate_limited", "429"); n != 2 {
		t.Fatalf("缺健康条目也要计数，期望 2 实际 %d", n)
	}
	if h := snapshotOf(t, r, "ofm-b"); !cooling(h) {
		t.Fatalf("ofm-b 的冷却条目应被补建，实际 %+v", h)
	}
}

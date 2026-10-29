package modelroute

// 本文件补齐两处「静默出错」分支面：
// ① provider 级冷却的四个边界（空参数通配、禁用条目、短冷却不得洗长冷却、缺条目补建）；
// ② 选取与降权路径上的零权重 / 慢模型惩罚 / 热更新窗口缺条目。
//
// 为什么这些分支值得单独钉：CooldownByProvider 的 429 冷却是「按故障域收敛」的设计核心
// （见 2026-10-19 事故说明），空 provider 参数若被当成通配就会把整池洗成冷却；
// Weight<=0 是配置文件手填的常态（漏填即 0），若被当「永不选取」则新增车道静默不上岗；
// AvgMs 惩罚分支是「同档内慢模型要让位」的唯一实现，缺它则 200s 与 2s 的模型同权。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
)

// TestCooldownByProviderEmptyArgNeverCoolsHealthyLanes 负向：传空 provider 参数时，
// 只允许命中「同样没配 provider」的条目，所有配了 provider 的健康车道必须不受影响。
// 证伪面：把 EqualFold 换成 Contains（或任何前缀语义）后，空串会通配全部 provider => n 变大、
// ofm-a 被误冷，两条断言同时红。
func TestCooldownByProviderEmptyArgNeverCoolsHealthyLanes(t *testing.T) {
	routes := append(laneRoutes(), config.ModelRoute{
		Model: "noprov", Provider: "", Lane: "misc", Tier: "fast", Enabled: true, Weight: 10, MaxFail: 3, CooldownM: 30,
	})
	r := New(routes)
	n := r.CooldownByProvider("", 5*time.Minute, "rate_limited", "429")
	if n != 1 {
		t.Fatalf("空 provider 参数只应命中未配 provider 的那 1 条，实际 n=%d", n)
	}
	if h := snapshotOf(t, r, "noprov"); !cooling(h) {
		t.Error("Provider 为空的路由本应被空串精确命中")
	}
	for _, m := range []string{"ofm-a", "kilo-a", "direct-a"} {
		if h := snapshotOf(t, r, m); cooling(h) {
			t.Errorf("配了 provider 的 %s 不得被空 provider 参数冷却", m)
		}
	}
}

// TestCooldownByProviderSkipsDisabledRoutes 负向：Enabled=false 的车道不是容量，
// 既不该计数，也不该被写健康度（面板上「禁用」与「冷却」是两种事实，混了就查不清成因）。
func TestCooldownByProviderSkipsDisabledRoutes(t *testing.T) {
	routes := append(laneRoutes(), config.ModelRoute{
		Model: "off-a", Provider: "our-free-model", Lane: "ofm", Tier: "fast", Enabled: false, Weight: 10,
	})
	r := New(routes)
	n := r.CooldownByProvider("our-free-model", 5*time.Minute, "rate_limited", "429")
	if n != 4 {
		t.Fatalf("禁用路由不得计入冷却条数，期望 4 实际 %d", n)
	}
	h := snapshotOf(t, r, "off-a")
	if h.CooldownTo != 0 {
		t.Errorf("禁用路由的冷却时刻必须保持 0，实际 %d", h.CooldownTo)
	}
	if !h.Disabled {
		t.Error("Enabled=false 应落成 Disabled=true（否则 Pick 兜底会选到禁用道）")
	}
}

// TestCooldownByProviderNeverShortensExistingCooldown 边界：一次普通 429 不得把
// model_unavailable 的 12 小时长冷却洗短（现场成因：结构性故障被提前放回池里反复撞墙）。
// 证伪面：去掉 until > h.CooldownTo 的比较改为无条件覆盖 => 冷却时刻变小而红。
func TestCooldownByProviderNeverShortensExistingCooldown(t *testing.T) {
	r := New(laneRoutes())
	r.RecordFailure("ofm-a", "model_unavailable", "EOL")
	long := snapshotOf(t, r, "ofm-a").CooldownTo
	if long <= time.Now().Add(6*time.Hour).UnixMilli() {
		t.Fatalf("前置条件不成立：ofm-a 应先拿到长冷却，实际 %d", long)
	}
	if n := r.CooldownByProvider("our-free-model", time.Minute, "rate_limited", "429"); n != 4 {
		t.Fatalf("n = %d, want 4", n)
	}
	if got := snapshotOf(t, r, "ofm-a").CooldownTo; got != long {
		t.Fatalf("短冷却不得覆盖长冷却：%d -> %d", long, got)
	}
	// 冷却时刻不变，LastErr 仍要更新：「何时解禁」与「最近一次为什么失败」是两条独立事实。
	if got := snapshotOf(t, r, "ofm-a").LastErr; got != "rate_limited: 429" {
		t.Errorf("LastErr 应更新为最近一次错误，实际 %q", got)
	}
	// 同一批里原本没有长冷却的条目必须被这次 429 冷却到（不能因为「有条目已长冷」就整批跳过）。
	if h := snapshotOf(t, r, "kilo-a"); !cooling(h) {
		t.Error("kilo-a 应被本次 provider 级冷却覆盖")
	}
}

// TestCooldownByProviderCreatesMissingHealthEntry 边界：健康表里缺条目时补建并冷却，
// 既不 panic 也不静默跳过（热更新窗口内新加的车道可能还没建条目）。
func TestCooldownByProviderCreatesMissingHealthEntry(t *testing.T) {
	r := New(laneRoutes())
	delete(r.health, "eac-a")
	n := r.CooldownByProvider("our-free-model", 5*time.Minute, "rate_limited", "429")
	if n != 4 {
		t.Fatalf("缺健康条目也要计数，期望 4 实际 %d", n)
	}
	if h := snapshotOf(t, r, "eac-a"); !cooling(h) {
		t.Fatalf("eac-a 的冷却条目应被补建，实际 %+v", h)
	}
	if got := snapshotOf(t, r, "eac-a").Provider; got != "our-free-model" {
		t.Errorf("补建条目要带上 provider（面板按 provider 归组），实际 %q", got)
	}
	if has(r.AvailableLanes(), "lane:eac") {
		t.Error("刚被补建冷却的车道不得出现在可用车道列表里")
	}
}

// TestAvailableLanesSkipsDisabledRoute 边界：AvailableLanes 的 Enabled 过滤分支。
// 禁用道即使健康也绝不能报成「可用车道」—— engine.go:1724 拿这个读数决定
// 「不推闸门、立刻重调度」，报出去就是一条永远选不中的空转车道。
func TestAvailableLanesSkipsDisabledRoute(t *testing.T) {
	r := New([]config.ModelRoute{
		enabled("live", "p1", "l1", "balanced"),
		{Model: "off", Provider: "p1", Lane: "l2", Tier: "fast", Enabled: false, Weight: 100},
	})
	got := r.AvailableLanes()
	if len(got) != 1 || got[0] != "lane:l1" {
		t.Fatalf("禁用道不得进可用列表，实际 %v", got)
	}
	// 热更新把唯一可用道关掉后：可用列表转空且熔断成立（面板与闸门必须同步翻转）。
	r.UpdateRoutes([]config.ModelRoute{
		{Model: "live", Provider: "p1", Lane: "l1", Tier: "balanced", Enabled: false, Weight: 100},
	})
	if got := r.AvailableLanes(); len(got) != 0 {
		t.Fatalf("热更新关掉车道后可用列表应为空，实际 %v", got)
	}
	if !r.AllInCooldown() {
		t.Error("唯一车道被关掉时应判零可用容量")
	}
}

// TestEarliestAvailableSkipsUnusableRoutes 覆盖 EarliestAvailable 的两个 continue 分支：
// 禁用条目不参与（否则一个「人为关掉且被外部置冷」的条目会把闸门时长拉错），
// Disabled 或缺健康条目也不参与（否则热更新窗口里会算出一个无人能用的等待时刻）。
func TestEarliestAvailableSkipsUnusableRoutes(t *testing.T) {
	r := New([]config.ModelRoute{
		enabled("live", "p1", "l1", "balanced"),
		{Model: "off", Provider: "p1", Lane: "l2", Tier: "fast", Enabled: false, Weight: 100},
	})
	r.CooldownByLane("lane:l1", 2*time.Minute, "rate_limited", "429")
	// 给禁用条目一个更晚的冷却时刻：它必须被忽略， earliest 只能来自 live。
	r.health["off"].CooldownTo = time.Now().Add(10 * time.Minute).UnixMilli()
	got := r.EarliestAvailable()
	if got <= 0 {
		t.Fatalf("全冷却时应返回未来时刻，实际 %d", got)
	}
	if got > time.Now().Add(3*time.Minute).UnixMilli() {
		t.Fatalf("禁用条目的 10 分钟冷却不得进入闸门时长，实际距今 %dms", got-time.Now().UnixMilli())
	}
	left := got - time.Now().UnixMilli()
	if left < int64(60*time.Second/time.Millisecond) {
		t.Fatalf("live 的 2 分钟冷却应给出约 2 分钟的等待，实际剩余 %dms", left)
	}
	// Disabled 条目（热更新关掉但保留在表里）同样不得贡献等待时刻。
	r2 := New([]config.ModelRoute{enabled("live", "p1", "l1", "balanced"), enabled("slow", "p1", "l2", "fast")})
	r2.CooldownByLane("lane:l1", time.Minute, "rate_limited", "429")
	r2.CooldownByLane("lane:l2", 9*time.Minute, "rate_limited", "429")
	r2.UpdateRoutes([]config.ModelRoute{
		enabled("live", "p1", "l1", "balanced"),
		{Model: "slow", Provider: "p1", Lane: "l2", Tier: "fast", Enabled: true, Weight: 100},
	})
	if got := r2.EarliestAvailable(); got > time.Now().Add(3*time.Minute).UnixMilli() {
		t.Errorf("两条都冷却时应取最早解禁（约 1 分钟），实际距今 %dms", got-time.Now().UnixMilli())
	}
	// 缺健康条目（热更新竞态面）：跳过而不 panic，且不得把它当「已可用」返回 0。
	delete(r2.health, "live")
	if got := r2.EarliestAvailable(); got <= time.Now().UnixMilli() {
		t.Errorf("缺条目时不得谎报 0（表示已有可用道），实际 %d", got)
	}
}

// TestPickTreatsZeroWeightAsOne 覆盖 pickLocked 的 Weight<=0 分支。
// 语义必须钉成「漏填权重 = 普通车道」而不是「永不选取」：配置文件手填时 0 是常态，
// 若 0 被当成 0 概率，新加的车道会静默不上岗（且没有任何报错面）。
func TestPickTreatsZeroWeightAsOne(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "zerow", Provider: "p1", Tier: "balanced", Enabled: true, MaxFail: 3, CooldownM: 30},
	})
	for i := 0; i < 10; i++ {
		got, err := r.Pick("balanced", nil)
		if err != nil {
			t.Fatalf("Weight=0 的启用车道必须仍可被选中: %v", err)
		}
		if got != "zerow" {
			t.Fatalf("只有一条车道时选中了 %q", got)
		}
	}
	// 负权重同样按 1 处理，且不得把「权重低」变成「永不选取」：反复取样必须能命中。
	r2 := New([]config.ModelRoute{
		{Model: "negw", Provider: "p1", Tier: "balanced", Enabled: true, Weight: -5, MaxFail: 3, CooldownM: 30},
		enabled("heavy", "p1", "l2", "balanced"),
	})
	r2.RecordSuccess("heavy", 100)
	// 加权随机不能靠单次或少量取样定判据（既有假红也有假绿），这里做双向判定：
	// 下界证明「Weight<=0 不被当成永不选取」，上界证明「不被归一成超大权重」。
	// 期望命中率 = 70/(70+10000) ≈ 0.7%，取 20000 次让下界在概率上稳定成立。
	samples := 20000
	hit := 0
	for i := 0; i < samples; i++ {
		got, err := r2.Pick("balanced", nil)
		if err != nil {
			t.Fatalf("Pick 报错: %v", err)
		}
		if got == "negw" {
			hit++
		}
	}
	if hit == 0 {
		t.Fatalf("%d 次取样都没选中 Weight=-5 的车道（0 权重被当成永不选取）", samples)
	}
	if hit*20 > samples {
		t.Fatalf("Weight<=0 应按 1 处理，命中率却达 %.1f%%（疑似被当成超大权重）", float64(hit*100)/float64(samples))
	}
}

// TestScoreSlowModelPenalty 覆盖 Score 的 AvgMs 惩罚分支（L45-47）：
// >120s 才起罚、按每 20s 扣 5 分。这是「同档内慢模型让位」的唯一实现，
// 缺它则实测 200s 的车道与 2s 的车道同权，池轮换会反复把任务交给慢道。
func TestScoreSlowModelPenalty(t *testing.T) {
	fast := Health{Model: "fast", Success: 10, AvgMs: 1000}
	mid := Health{Model: "mid", Success: 10, AvgMs: 120_000}
	slow := Health{Model: "slow", Success: 10, AvgMs: 220_000}
	if fast.Score() <= 99 {
		t.Errorf("未超时的健康模型应接近满分，实际 %.1f", fast.Score())
	}
	if mid.Score() <= 99 {
		t.Errorf("恰好 120s 不起罚（阈值是严格大于），实际 %.1f", mid.Score())
	}
	if got := slow.Score(); got != 75 {
		t.Errorf("220s 平均耗时应扣 (220000-120000)/20000*5 = 25 分 => 75，实际 %.1f", got)
	}
	// 慢模型的分数必须严格低于同成功率的快模型（加权选取的实际后果）。
	if slow.Score() >= fast.Score() {
		t.Fatalf("慢模型不得与快模型同权：slow=%.1f fast=%.1f", slow.Score(), fast.Score())
	}
	// 连续失败重罚也必须可见（每次 -12），且触底不低于 1。
	failing := Health{Model: "f", Success: 0, Failure: 10, ConsecFail: 10}
	if got := failing.Score(); got != 1 {
		t.Errorf("重罚后应触底为 1（不得为 0，否则该模型永远不会再被探索），实际 %.1f", got)
	}
	if got := (Health{}).Score(); got != 70 {
		t.Errorf("未探索模型应给 70 分保底以被试到，实际 %.1f", got)
	}
}

// TestLoadIgnoresMalformedEntries 覆盖 load 的 h==nil / id=="" 跳过分支：
// 健康度文件是人可编辑的落盘产物，半截 JSON 或 null 值不得阻断启动，
// 也不得把合法条目冲掉（改造前这里一旦解析异常就整池丢掉冷却记录）。
func TestLoadIgnoresMalformedEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "health.json")
	raw := strings.Join([]string{
		"{",
		"  \"savedAt\": 1700000000000,",
		"  \"health\": {",
		"    \"\": {\"model\":\"\",\"success\":5},",
		"    \"ghost\": null,",
		"    \"live\": {\"model\":\"live\",\"success\":3,\"failure\":1,\"consecFail\":0,\"avgMs\":800,\"cooldownTo\":0}",
		"  }",
		"}",
	}, "\n")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("写入畸形健康度文件失败: %v", err)
	}
	r := NewWithPersistence([]config.ModelRoute{enabled("live", "p1", "l1", "balanced")}, path)
	h := snapshotOf(t, r, "live")
	if h.Success != 3 || h.Failure != 1 || h.AvgMs != 800 {
		t.Fatalf("合法条目必须被正常恢复，实际 %+v", h)
	}
	// 空 key 与 null 条目被跳过后不得凭空造出模型条目（否则面板出现幽灵车道）。
	for _, hh := range r.Health() {
		if hh.Model == "" || hh.Model == "ghost" {
			t.Errorf("畸形条目不得被恢复成健康记录: %+v", hh)
		}
	}
	if got := len(r.AvailableLanes()); got != 1 {
		t.Fatalf("可用车道应只有 lane:l1 一条，实际 %d（%v）", got, r.AvailableLanes())
	}
}

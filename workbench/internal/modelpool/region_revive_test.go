package modelpool

import (
	"testing"
)

// TestSyncDoesNotReviveRegionLimited 钉住一条边界：地区受限模型不能被自动恢复。
//
// 现场（2026-10-19 容器 e2e）：网关把地区受限的模型广播成
// our-free-model-region/<id>，所以「网关仍在广播」这个恢复信号对它是恒真的 ——
// 一旦按「还在广播 ⇒ 上游判定过期」去 revive，地区受限模型会在下一次同步被
// 自动启用，"默认停用"形同虚设（e2e 断言 enabled=false 实测翻成 true）。
//
// 判据必须看广播形态：只有裸 our-free-model/<id>（可路由车道）的重新出现
// 才算"上游判定过期"；带 -region 前缀的出现恰恰是"仍然受限"的复证。
func TestSyncDoesNotReviveRegionLimited(t *testing.T) {
	m, _ := newTestManager(t)
	// 模拟首次同步：地区受限 → 自动停用 + 打 autoDisabled 标记
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k",
		Models: []string{"our-free-model-region/beam-limited"}}
	if res := SyncFromGateway(m, gw, "probe"); res.Added != 1 {
		t.Fatalf("首次同步应新增 1 个，实际 %+v", res)
	}
	sp, _, okf := m.Get("beam-limited")
	if !okf {
		t.Fatal("模型应已入池")
	}
	if sp.Enabled || !sp.AutoDisabled {
		t.Fatalf("地区受限模型应 enabled=false + autoDisabled=true，实际 %+v", sp)
	}
	// 第二次同步：网关依旧以 -region 形态广播 ⇒ 不得自动恢复
	res2 := SyncFromGateway(m, gw, "probe")
	if len(res2.Revived) != 0 {
		t.Fatalf("地区受限模型不能被自动恢复，实际 revived=%v", res2.Revived)
	}
	sp2, _, _ := m.Get("beam-limited")
	if sp2.Enabled {
		t.Fatalf("第二次同步后仍必须停用，实际 %+v", sp2)
	}
}

// TestSyncRevivesRoutableAutoDisabled 是上一条的正例：可路由车道重新出现，
// 说明 availability.json 的判死已过期，必须自动恢复（否则模型永久缺席）。
func TestSyncRevivesRoutableAutoDisabled(t *testing.T) {
	m, _ := newTestManager(t)
	if _, err := m.Upsert(Spec{ID: "came-back", Model: "came-back", Enabled: false, AutoDisabled: true,
		BaseURL: "http://127.0.0.1:8326/v1", Format: FormatChat, Tier: "fast", Priority: 5, Weight: 10}, "test"); err != nil {
		t.Fatal(err)
	}
	gw := Gateway{BaseURL: "http://127.0.0.1:8326/v1", Key: "k", Models: []string{"our-free-model/came-back"}}
	res := SyncFromGateway(m, gw, "probe")
	if len(res.Revived) != 1 || res.Revived[0] != "came-back" {
		t.Fatalf("可路由车道重新广播应自动恢复，实际 %+v", res)
	}
}

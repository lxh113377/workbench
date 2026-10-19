package modelpool

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStatusCarriesAggregateView 锁住面板可见性：/api/pool 必须带聚合车道视图，
// 且**绝不回显密钥明文**（只给指纹）。
func TestStatusCarriesAggregateView(t *testing.T) {
	store := NewStore(t.TempDir())
	svc := NewService(store, []Spec{
		{ID: "open_router/openrouter/free", Model: "openrouter/free", Label: "自建端点",
			Lane: LaneCustom, BaseURL: "http://127.0.0.1:18432/v1", APIKey: "sk-secret-9876",
			Format: FormatChat, Tier: "balanced", Priority: 1, Weight: 10,
			Enabled: true, Routable: true, MaxFail: 3, CooldownSec: 120},
	}, ServiceConfig{Enabled: false, DSHHome: t.TempDir()})

	st := svc.Status()
	raw, err := json.Marshal(st["aggregate"])
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "registryLoaded") {
		t.Fatalf("aggregate 视图缺少清单加载状态：%s", body)
	}
	if strings.Contains(body, "sk-secret-9876") {
		t.Fatal("聚合车道视图泄漏密钥明文")
	}
	view, okf := st["aggregate"].(AggregateStatus)
	if !okf {
		t.Fatalf("aggregate 类型不对：%T", st["aggregate"])
	}
	if view.RegistryCount == 0 {
		t.Skipf("本机无 OFM 插件，跳过投影断言：%s", view.Error)
	}
	if !view.Enabled {
		t.Fatalf("真实清单下 open_router 应可投影，实际拒绝 %+v", view.Rejections)
	}
	if len(view.Providers) != 1 || view.Providers[0].ProviderID != "open_router" {
		t.Fatalf("应投影出 open_router 一个条目，实际 %+v", view.Providers)
	}
	if view.Providers[0].KeyHint != "…9876" || !view.Providers[0].HasKey {
		t.Fatalf("密钥应只回显指纹，实际 %+v", view.Providers[0])
	}
}

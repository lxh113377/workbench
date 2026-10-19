package modelpool

import (
	"testing"
)

// TestLoadProviderRegistryRealPlugin 用真实安装的 OFM 插件清单做冒烟：
// 读不到就跳过（CI/容器无插件），读到就必须是可用索引。
func TestLoadProviderRegistryRealPlugin(t *testing.T) {
	registry, path, err := LoadProviderRegistry("")
	if err != nil {
		t.Skipf("本机未安装 OFM 插件，跳过真实清单冒烟: %v", err)
	}
	t.Logf("registry 来源: %s（%d 个 provider）", path, len(registry))
	if len(registry) < 50 {
		t.Fatalf("真实 registry 应有数十个 provider，实际 %d", len(registry))
	}
	for _, id := range []string{"open_router", "siliconflow", "anthropic", "ollama"} {
		info, okf := registry[id]
		if !okf {
			t.Fatalf("真实清单缺少已知 provider %q", id)
		}
		if info.Format == "" || info.Auth == "" {
			t.Fatalf("provider %q 约束不完整: %+v", id, info)
		}
	}
	if got := registry["openai"].Auth; got != "oauth" {
		t.Fatalf("openai 应为 oauth（决定聚合车道不服务它），实际 %q", got)
	}
	if got := registry["ollama"].Auth; got != "none" {
		t.Fatalf("ollama 应为 auth=none 的本地端点，实际 %q", got)
	}
}

// TestAggregateForDSHHomeFallsBackClosed 锁住「清单读不到时与改造前等价」：
// 返回 enabled:false 且映射为空，绝不静默改道。
func TestAggregateForDSHHomeFallsBackClosed(t *testing.T) {
	section, modelIDs, err := AggregateForDSHHome(t.TempDir()+"/no-such-home", []Spec{
		{ID: "zzz-not-real/m", Model: "m", Lane: LaneCustom, BaseURL: "http://127.0.0.1:1/v1", APIKey: "k", Enabled: true, Routable: true},
	})
	if err != nil {
		t.Logf("预期内的清单缺失错误: %v", err)
	}
	if section["enabled"] != false {
		t.Fatalf("清单不可用时 aggregate 必须保持关闭，实际 %v", section["enabled"])
	}
	if len(modelIDs) != 0 {
		t.Fatalf("清单不可用时不得有任何可路由映射，实际 %v", modelIDs)
	}
}

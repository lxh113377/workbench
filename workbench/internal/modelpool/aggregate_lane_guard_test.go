package modelpool

import "testing"

// TestProjectAggregateIgnoresOFMLaneSpecs 锁住上一条教训：
// 免费/Kilo/EAC 车道的模型（带网关 BaseURL 且 Routable=true）**绝不能**被
// 当成聚合车道候选 —— 否则会把网关密钥投影进 aggregate，并让模型从
// 免费车道改道到 aggregate 路由（绕过车道隔离）。
func TestProjectAggregateIgnoresOFMLaneSpecs(t *testing.T) {
	gw := "http://127.0.0.1:8326/v1"
	mk := func(id string, lane Lane) Spec {
		return Spec{ID: id, Model: id, Lane: lane, BaseURL: gw, APIKey: "gateway-secret",
			Format: FormatChat, Tier: "balanced", Priority: 5, Weight: 10,
			Enabled: true, Routable: true, Source: "gateway-sync"}
	}
	specs := []Spec{
		mk("z-ai/glm-5.3", LaneFree),
		mk("cohere/north-mini-code:free", LaneKilo),
		mk("poolside/laguna-s-2.1:free", LaneKilo),
		mk("openai/gpt-oss-20b", LaneEAC),
	}
	// 关键：registry 里必须**真的包含**这些 provider，否则宽松版也会因为
	// 「未知 providerId」被拒 —— 那测试就成了假通过（拒绝原因不对）。
	registry := reg()
	for _, info := range []ProviderInfo{
		{ID: "cohere", Name: "Cohere", Auth: "apiKey", Format: "openai-chat", BaseURL: "https://api.cohere.ai/compatibility/v1"},
		{ID: "poolside", Name: "Poolside", Auth: "apiKey", Format: "openai-chat", BaseURL: "https://inference.poolside.ai/v1"},
		{ID: "z-ai", Name: "Z.AI", Auth: "apiKey", Format: "openai-chat", BaseURL: "https://api.z.ai/v1"},
	} {
		registry[info.ID] = info
	}
	// 先自证前提：这些模型具备宽松判据会放行的全部特征
	// （Enabled + 带 BaseURL + Routable）。否则本测试只是在验证
	// 「不完整的 Spec 被忽略」，锁不住真正的教训。
	for _, sp := range specs {
		if !sp.Enabled || sp.BaseURL == "" || !sp.Routable {
			t.Fatalf("前提不成立：%s 不满足宽松判据的特征组合，测试无法验证拒绝路径", sp.ID)
		}
		if sp.APIKey == "" {
			t.Fatalf("前提不成立：%s 应带网关密钥，才能验证密钥不被投影出去", sp.ID)
		}
	}
	// 反向自证：aggregateCandidate 对 OFM 车道必须返回 false（收紧后的判据）。
	for _, sp := range specs {
		if aggregateCandidate(sp) {
			t.Fatalf("aggregateCandidate 仍把 OFM 车道 %s 当候选，收紧未生效", sp.ID)
		}
	}
	p := ProjectAggregate(specs, registry)
	if p.Enabled || len(p.Providers) != 0 {
		t.Fatalf("OFM 车道模型不得进 aggregate，实际 enabled=%v providers=%+v", p.Enabled, p.Providers)
	}
	if len(p.ModelIDs) != 0 {
		t.Fatalf("不得产生任何可路由映射：%+v", p.ModelIDs)
	}
	if len(p.Rejections) != 0 {
		t.Fatalf("非候选模型应被静默忽略（不是拒绝），实际 %+v", p.Rejections)
	}
}

// TestProjectAggregateCustomLaneStillWorks 收紧判据后，用户自填端点仍要能投影。
func TestProjectAggregateCustomLaneStillWorks(t *testing.T) {
	sp := customSpec("open_router/openrouter/free", "http://127.0.0.1:18432/v1", "sk-user", LaneCustom)
	sp.Source = "user"
	p := ProjectAggregate([]Spec{sp}, reg())
	if !p.Enabled || len(p.Providers) != 1 {
		t.Fatalf("custom 车道自填端点应可投影：%+v rej=%+v", p, p.Rejections)
	}
}

// TestProjectAggregateIgnoresGatewaySyncedAggregateLane 锁住第二道判据：
// 网关专属车道（lane=aggregate 但 source=gateway-sync）不是用户自填端点，
// 不得进投影，否则每次面板刷新都会刷出「providerId 不在 registry」的噪声拒绝。
func TestProjectAggregateIgnoresGatewaySyncedAggregateLane(t *testing.T) {
	specs := []Spec{
		{ID: "deepseek-official/deepseek-flash", Model: "deepseek-flash", Lane: LaneAggregate,
			Source: "gateway-sync", BaseURL: "http://127.0.0.1:8326/v1", APIKey: "gateway-key",
			Format: FormatChat, Enabled: true, Routable: false},
		{ID: "deepseek-official/deepseek-v4-pro", Model: "deepseek-v4-pro", Lane: LaneAggregate,
			Source: "ofm-settings", BaseURL: "http://127.0.0.1:8326/v1", APIKey: "gateway-key",
			Format: FormatChat, Enabled: true, Routable: false},
	}
	p := ProjectAggregate(specs, reg())
	if p.Enabled || len(p.Providers) != 0 {
		t.Fatalf("网关同步条目不得进 aggregate：%+v", p.Providers)
	}
	if len(p.Rejections) != 0 || len(p.Warnings) != 0 {
		t.Fatalf("网关同步条目应被静默忽略，不该产生噪声 rej=%+v warn=%+v", p.Rejections, p.Warnings)
	}
}

// TestProjectAggregateUserSourcedCustomStillWorks 收紧来源判据后，人工添加的端点必须照常投影。
func TestProjectAggregateUserSourcedCustomStillWorks(t *testing.T) {
	for _, src := range []string{"user", "panel", ""} {
		sp := customSpec("open_router/openrouter/free", "http://127.0.0.1:18432/v1", "sk-u", LaneCustom)
		sp.Source = src
		p := ProjectAggregate([]Spec{sp}, reg())
		if !p.Enabled {
			t.Fatalf("source=%q 的人工端点应可投影：%+v", src, p.Rejections)
		}
	}
}

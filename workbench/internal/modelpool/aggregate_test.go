package modelpool

import (
	"encoding/json"
	"strings"
	"testing"
)

func reg() map[string]ProviderInfo {
	return map[string]ProviderInfo{
		"open_router": {ID: "open_router", Name: "OpenRouter", Auth: "apiKey",
			Format: "openai-chat", BaseURL: "https://openrouter.ai/api/v1"},
		"anthropic": {ID: "anthropic", Name: "Anthropic", Auth: "apiKey",
			Format: "anthropic-messages", BaseURL: "https://api.anthropic.com/v1"},
		"ollama": {ID: "ollama", Name: "Ollama", Auth: "none", Format: "openai-chat", BaseURL: "http://localhost:11434/v1"},
		"openai": {ID: "openai", Name: "OpenAI", Auth: "oauth", Format: "openai-responses", BaseURL: "https://chatgpt.com/backend-api/codex"},
		"az":     {ID: "az", Name: "Azure", Auth: "adc", Format: "openai-chat"},
		"vertex": {ID: "vertex", Name: "Vertex", Auth: "apiKey", Format: "openai-chat", BaseURL: "https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{location}/"},
	}
}

func customSpec(id, base, key string, lane Lane) Spec {
	return Spec{ID: id, Model: id, Lane: lane, BaseURL: base, APIKey: key,
		Format: FormatChat, Enabled: true, Routable: true, Priority: 10, Weight: 10,
		UpdatedAt: 1700000000000}
}

// TestProjectAggregateHappyPath 验证正常投影的形状与 settings 契约一致。
func TestProjectAggregateHappyPath(t *testing.T) {
	specs := []Spec{customSpec("open_router/openrouter/free", "http://127.0.0.1:18432/v1", "sk-mock", LaneCustom)}
	p := ProjectAggregate(specs, reg())
	if !p.Enabled {
		t.Fatalf("应启用聚合车道，实际 %v（拒绝 %+v）", p.Enabled, p.Rejections)
	}
	entry, okf := p.Providers["open_router"]
	if !okf {
		t.Fatalf("providers 应含 open_router，实际 %+v", p.Providers)
	}
	if entry["apiKey"] != "sk-mock" || entry["baseUrl"] != "http://127.0.0.1:18432/v1" || entry["enabled"] != true {
		t.Fatalf("条目字段不符 OFM 契约：%+v", entry)
	}
	models, _ := entry["models"].([]string)
	if len(models) != 1 || models[0] != "openrouter/free" {
		t.Fatalf("models 应为 [openrouter/free]，实际 %v", models)
	}
	if p.ModelIDs["open_router/openrouter/free"] != "open_router/openrouter/free" {
		t.Fatalf("ModelIDs 应映射出 overlay 可写的复合 id，实际 %+v", p.ModelIDs)
	}
	// settings.json 段落形状必须是 {enabled, providers}
	section := p.AggregateSection()
	b, err := json.Marshal(section)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back["enabled"] != true {
		t.Fatalf("aggregate.enabled 应为 true，实际 %v", back["enabled"])
	}
	provs, _ := back["providers"].(map[string]any)
	if _, has := provs["open_router"]; !has {
		t.Fatalf("序列化后 providers 丢失，实际 %s", string(b))
	}
}

// TestProjectAggregateRejectsUnknownProvider 锁住反假绿：registry 外的 providerId
// 必须被拒绝并给出原因，**绝不允许静默回落到免费车道**。
func TestProjectAggregateRejectsUnknownProvider(t *testing.T) {
	p := ProjectAggregate([]Spec{customSpec("probe/vendor-x", "http://127.0.0.1:1/v1", "k", LaneCustom)}, reg())
	if p.Enabled {
		t.Fatal("未知 provider 不得启用车道")
	}
	if len(p.ModelIDs) != 0 {
		t.Fatalf("未知 provider 不得出现在可路由映射里：%+v", p.ModelIDs)
	}
	if len(p.Rejections) != 1 || !strings.Contains(p.Rejections[0].Reason, "registry") {
		t.Fatalf("应给出一条 registry 相关拒绝原因，实际 %+v", p.Rejections)
	}
}

// TestProjectAggregateRejectsOAuthAndAdc 对应 authUsable：oauth/adc 不服务。
func TestProjectAggregateRejectsOAuthAndAdc(t *testing.T) {
	for _, id := range []string{"openai/gpt-5", "az/deployment-1"} {
		p := ProjectAggregate([]Spec{customSpec(id, "https://example.com/v1", "k", LaneAggregate)}, reg())
		if p.Enabled || len(p.Rejections) != 1 {
			t.Fatalf("%s 应被拒绝，实际 enabled=%v rej=%+v", id, p.Enabled, p.Rejections)
		}
		if !strings.Contains(p.Rejections[0].Reason, "登录") {
			t.Fatalf("%s 拒绝原因应说明登录类型，实际 %q", id, p.Rejections[0].Reason)
		}
	}
}

// TestProjectAggregateRequiresKeyForApiKeyAuth 对应 resolveProvider 的缺密钥分支。
func TestProjectAggregateRequiresKeyForApiKeyAuth(t *testing.T) {
	p := ProjectAggregate([]Spec{customSpec("open_router/openrouter/free", "https://openrouter.ai/api/v1", "", LaneCustom)}, reg())
	if p.Enabled {
		t.Fatal("apiKey 型 provider 缺密钥不得启用")
	}
	if len(p.Rejections) != 1 || !strings.Contains(p.Rejections[0].Reason, "API Key") {
		t.Fatalf("应报缺 API Key，实际 %+v", p.Rejections)
	}
	// auth=none 的本地端点（ollama/lmstudio）无密钥也必须能投影
	p2 := ProjectAggregate([]Spec{customSpec("ollama/qwen2.5", "http://localhost:11434/v1", "", LaneCustom)}, reg())
	if !p2.Enabled {
		t.Fatalf("auth=none 的本地端点应无需密钥：%+v", p2.Rejections)
	}
}

// TestProjectAggregateRejectsPlaceholderURL 对应 hasPlaceholder：含 {} 的 base 会被 OFM 丢行。
func TestProjectAggregateRejectsPlaceholderURL(t *testing.T) {
	p := ProjectAggregate([]Spec{customSpec("vertex/gemini", "https://aiplatform.googleapis.com/v1/projects/{project_id}/locations/{location}/", "k", LaneCustom)}, reg())
	if p.Enabled {
		t.Fatal("含占位符的 BaseURL 不得投影")
	}
	if len(p.Rejections) != 1 || !strings.Contains(p.Rejections[0].Reason, "占位符") {
		t.Fatalf("应报占位符原因，实际 %+v", p.Rejections)
	}
}

// TestProjectAggregateWarnsFormatMismatch 锁住「线协议以 registry 为准」这条易踩语义：
// 用户填 anthropic-messages 但 provider 实际服务 openai-chat 时，必须如实告警。
func TestProjectAggregateWarnsFormatMismatch(t *testing.T) {
	sp := customSpec("open_router/openrouter/free", "http://127.0.0.1:18432/v1", "k", LaneCustom)
	sp.Format = FormatMessages
	p := ProjectAggregate([]Spec{sp}, reg())
	if !p.Enabled {
		t.Fatalf("协议不符仍可投影（OFM 按 registry 走），但必须告警：%+v", p.Rejections)
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0].Reason, "registry 为准") {
		t.Fatalf("应有一条协议告警，实际 %+v", p.Warnings)
	}
}

// TestProjectAggregateMergesModelsSameEndpoint 同 provider 同端点的多模型必须并进一个条目。
func TestProjectAggregateMergesModelsSameEndpoint(t *testing.T) {
	base := "http://127.0.0.1:18432/v1"
	p := ProjectAggregate([]Spec{
		customSpec("open_router/z-model", base, "k", LaneCustom),
		customSpec("open_router/a-model", base, "k", LaneCustom),
	}, reg())
	entry := p.Providers["open_router"]
	models, _ := entry["models"].([]string)
	if len(models) != 2 || models[0] != "a-model" || models[1] != "z-model" {
		t.Fatalf("应合并并排序为 [a-model z-model]，实际 %v", models)
	}
	if len(p.ModelIDs) != 2 {
		t.Fatalf("两个模型都应可路由，实际 %+v", p.ModelIDs)
	}
}

// TestProjectAggregateRejectsConflictingEndpoint 同 provider 配两个不同端点时，
// 后到者必须按不可路由处理并告警 —— 不得静默覆盖先到者。
func TestProjectAggregateRejectsConflictingEndpoint(t *testing.T) {
	p := ProjectAggregate([]Spec{
		customSpec("open_router/first", "http://127.0.0.1:18432/v1", "k1", LaneCustom),
		customSpec("open_router/second", "http://127.0.0.1:2/v1", "k2", LaneCustom),
	}, reg())
	entry := p.Providers["open_router"]
	if entry["baseUrl"] != "http://127.0.0.1:18432/v1" {
		t.Fatalf("先到者不得被覆盖，实际 %v", entry["baseUrl"])
	}
	if _, has := p.ModelIDs["open_router/second"]; has {
		t.Fatal("冲突端点的模型必须按不可路由处理")
	}
	if len(p.Warnings) != 1 || !strings.Contains(p.Warnings[0].Reason, "只服务一份") {
		t.Fatalf("应告警端点冲突，实际 %+v", p.Warnings)
	}
}

// TestProjectAggregateIgnoresNonCandidates 免费/Kilo/EAC 车道不得被误投影。
func TestProjectAggregateIgnoresNonCandidates(t *testing.T) {
	free := Spec{ID: "z-ai/glm-5.3", Model: "z-ai/glm-5.3", Lane: LaneFree,
		Format: FormatChat, Enabled: true, Routable: true} // BaseURL 空 → 非候选
	kilo := customSpec("kilo-auto/free", "http://127.0.0.1:8326/v1", "k", LaneKilo)
	kilo.Routable = false // 非 custom/aggregate 且不可路由 → 非候选
	p := ProjectAggregate([]Spec{free, kilo}, reg())
	if p.Enabled || len(p.Rejections) != 0 || len(p.Warnings) != 0 {
		t.Fatalf("非候选模型应被完全忽略，实际 enabled=%v rej=%+v warn=%+v", p.Enabled, p.Rejections, p.Warnings)
	}
}

// TestProjectAggregateWithoutRegistry 缺 registry 时必须整体拒绝（宁可不通）。
func TestProjectAggregateWithoutRegistry(t *testing.T) {
	p := ProjectAggregate([]Spec{customSpec("open_router/openrouter/free", "http://x/v1", "k", LaneCustom)}, nil)
	if p.Enabled {
		t.Fatal("无 registry 时不得启用车道")
	}
	if len(p.Rejections) != 1 || !strings.Contains(p.Rejections[0].Reason, "registry") {
		t.Fatalf("应给出无法校验的原因，实际 %+v", p.Rejections)
	}
}

// TestSplitCompositeID 对齐 OFM parseProviderModelId 的边界。
func TestSplitCompositeID(t *testing.T) {
	cases := []struct {
		in              string
		ok              bool
		provider, model string
	}{
		{"open_router/openrouter/free", true, "open_router", "openrouter/free"},
		{" vendor/x ", true, "vendor", "x"},
		{"/leading", false, "", ""},
		{"no-slash", false, "", ""},
		{"trailing/", false, "", ""},
		{"", false, "", ""},
	}
	for _, c := range cases {
		p, m, ok := splitCompositeID(c.in)
		if ok != c.ok || p != c.provider || m != c.model {
			t.Fatalf("splitCompositeID(%q)=(%q,%q,%v)，期望 (%q,%q,%v)", c.in, p, m, ok, c.provider, c.model, c.ok)
		}
	}
}

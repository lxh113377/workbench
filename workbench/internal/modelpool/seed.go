package modelpool

// SeedSpecs 返回内置种子模型清单：首次启动（或 OFM 数据面不可读）时的兜底池。
//
// 取值依据（2026-10-18 实测团队记录 + OFM catalog.sealIds）：
//   - z-ai/glm-5.3 为历史首选（短任务 55.6s 稳定产出）；
//   - openai/gpt-oss-20b 为快速备选（9.6s ping）；
//   - deepseek-ai/deepseek-v4.1-flash / moonshotai/kimi-k2.6 为 EAC 协付车道候选。
//
// 种子只保证「有模型可调度」，真实清单由 Discover() 从 OFM 数据面刷新覆盖。
func SeedSpecs() []Spec {
	const gw = "http://127.0.0.1:8326/v1"
	return []Spec{
		{
			ID: "z-ai/glm-5.3", Label: "OFM z-ai/glm-5.3", Provider: "our-free-model",
			Lane: LaneFree, Model: "z-ai/glm-5.3", Format: FormatChat, Tier: "balanced",
			Group: "OFM 免费", Priority: 1, Weight: 100, MaxFail: 3, CooldownSec: 120,
			BaseURL: gw, Source: "builtin", Enabled: true, Routable: true,
			Note: "历史首选：短任务实测稳定（55.6s）",
		},
		{
			ID: "openai/gpt-oss-20b", Label: "OFM openai/gpt-oss-20b", Provider: "our-free-model",
			Lane: LaneFree, Model: "openai/gpt-oss-20b", Format: FormatChat, Tier: "fast",
			Group: "OFM 免费", Priority: 2, Weight: 60, MaxFail: 3, CooldownSec: 120,
			BaseURL: gw, Source: "builtin", Enabled: true, Routable: true,
			Note: "快速备选：ping 实测 9.6s",
		},
		{
			ID: "z-ai/glm-5.3-flash", Label: "OFM z-ai/glm-5.3-flash", Provider: "our-free-model",
			Lane: LaneFree, Model: "z-ai/glm-5.3-flash", Format: FormatChat, Tier: "fast",
			Group: "OFM 免费", Priority: 3, Weight: 40, MaxFail: 2, CooldownSec: 180,
			BaseURL: gw, Source: "builtin", Enabled: true, Routable: true,
			Note: "慢速但可兜底（实测 82.3s）",
		},
		{
			ID: "deepseek-ai/deepseek-v4.1-flash", Label: "EAC DeepSeek V4.1 Flash",
			Provider: "our-free-model", Lane: LaneEAC, Model: "deepseek-ai/deepseek-v4.1-flash",
			Format: FormatChat, Tier: "quality", Group: "EAC 协付", Priority: 4, Weight: 50,
			MaxFail: 3, CooldownSec: 120, BaseURL: gw, Source: "builtin", Enabled: true, Routable: true,
			Note: "EAC 协付车道（需 GitHub 授权后才放行）",
		},
	}
}

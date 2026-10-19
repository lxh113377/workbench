package modelpool

// RedactSpecs 把 Spec 列表转成可安全输出的 map 列表（密钥只留指纹）。
//
// 为什么在 modelpool 而不是复用 web.redactSpec：CLI（cmd/workbench）与任何
// 未来的导出面都要同一口径，而 web 依赖 modelpool，反向引用会成环。
// 两处字段清单必须同步 —— 新增 Spec 字段时忘了在这里输出，面板与 CLI
// 会各自缺字段；由 redact_test.go 用反射钉住。
func RedactSpecs(specs []Spec) []map[string]any {
	out := make([]map[string]any, 0, len(specs))
	for _, sp := range specs {
		out = append(out, map[string]any{
			"id": sp.ID, "label": sp.Label, "provider": sp.Provider, "lane": sp.Lane,
			"baseUrl": sp.BaseURL, "model": sp.Model, "format": sp.Format, "tier": sp.Tier,
			"group": sp.Group, "priority": sp.Priority, "weight": sp.Weight,
			"enabled": sp.Enabled, "vision": sp.Vision, "contextWindow": sp.ContextWindow,
			"maxOutput": sp.MaxOutput, "maxFail": sp.MaxFail, "cooldownSec": sp.CooldownSec,
			"source": sp.Source, "note": sp.Note, "routable": sp.Routable,
			"autoDisabled": sp.AutoDisabled,
			"hasKey":       sp.APIKey != "", "keyHint": keyFingerprint(sp.APIKey),
			"updatedAt": sp.UpdatedAt,
			"state":     sp.HealthState(), "cooldownTo": sp.HealthCooldown(),
		})
	}
	return out
}

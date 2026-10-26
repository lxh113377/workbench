package modelpool

import "testing"

// TestUpstreamVerdictState 钉住「上游判定的映射口径」。
//
// 由来（round 17 实测）：availability.json 实测 1 个 region-blocked + **7 个 throttled**，
// 而原实现只认 unavailable/region-blocked/deprecated ⇒ 额度已耗尽的免费车道被当成健康，
// 每个任务都挑中它们，每次尝试烧 120–150 s 却**零 token 产出**（子会话 tokenUsage 全 0）。
func TestUpstreamVerdictState(t *testing.T) {
	cases := []struct {
		verdict  string
		wantDown bool
		wantKind string
	}{
		{"unavailable", true, "model_unavailable"},
		{"region-blocked", true, "model_unavailable"},
		{"Region-Blocked", true, "model_unavailable"}, // 大小写与首尾空格不敏感
		{" deprecated ", true, "model_unavailable"},
		{"throttled", true, "model_throttled"},
		{"RATE-LIMITED", true, "model_throttled"},
		{"quota_exceeded", true, "model_throttled"},
		{"available", false, ""},
		{"ok", false, ""},
		{"", false, ""},
		{"unknown-state", false, ""},
	}
	for _, c := range cases {
		st, kind, reason := upstreamVerdictState(c.verdict)
		if c.wantDown {
			if st != StateDown || kind != c.wantKind || reason == "" {
				t.Errorf("%q: 期望 down/%s 且带原因，实得 %s/%s/%q", c.verdict, c.wantKind, st, kind, reason)
			}
			continue
		}
		if st != "" || kind != "" || reason != "" {
			t.Errorf("%q: 上游无判定时不得判 down，实得 %s/%s/%q", c.verdict, st, kind, reason)
		}
	}
}

// TestModelCore 钉住同族核的归一化口径。
func TestModelCore(t *testing.T) {
	cases := []struct{ in, want string }{
		{"nvidia/nemotron-3.5-lightning:free", "nemotron-3.5-lightning"},
		{"nemotron-3.5-lightning-free", "nemotron-3.5-lightning"},
		{"opencode/deepseek-v4.1-flash-free", "deepseek-v4.1-flash"},
		{"deepseek-ai/deepseek-v4.1-flash", "deepseek-v4.1-flash"},
		{" Z-AI/GLM-5.3-Flash ", "glm-5.3-flash"},
		{"recursive/flint-v1.0:free", "flint-v1.0"},
		{"flint-40b-free", "flint-40b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := modelCore(c.in); got != c.want {
			t.Errorf("modelCore(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestVerdictForSpec 钉住「同族合并」的保守口径（round 17 遗留 T16-②）。
//
// 实测缺口：availability 键是 OFM 车道 id（`*-free`），池里 36 条路由是另一套写法，
// 只按字面相等查 ⇒ 只有 8 条能被判到，其余 28 条「healthy」里混着同族孪生 id。
func TestVerdictForSpec(t *testing.T) {
	avail := map[string]string{
		"nemotron-3.5-lightning-free": "throttled",
		"deepseek-v4.1-flash-free":    "throttled",
		"flint-40b-free":              "region-blocked",
		"glm-5.3-flash-free":          "throttled",
		"beam-free":                   "throttled",
	}
	cases := []struct {
		specID  string
		want    string
		wantHit bool
	}{
		{"nemotron-3.5-lightning-free", "throttled", true},        // 精确
		{"nvidia/nemotron-3.5-lightning:free", "throttled", true}, // 同族别名
		{"opencode/deepseek-v4.1-flash-free", "throttled", true},  // 同族别名
		{"deepseek-ai/deepseek-v4.1-flash", "throttled", true},    // 同族别名（去掉 provider 前缀）
		{"z-ai/glm-5.3-flash", "throttled", true},                 // 同族别名
		{"reflection/beam-free", "throttled", true},               // 同族别名
		{"recursive/flint-v1.0:free", "", false},                  // 不同族：不得误并到 flint-40b
		{"z-ai/glm-5.3", "", false},                               // 核 glm-5.3 ≠ glm-5.3-flash：不得误并
		{"openai/gpt-oss-20b", "", false},                         // availability 里没有：不判
	}
	for _, c := range cases {
		got, hit := verdictForSpec(c.specID, avail)
		if hit != c.wantHit || got != c.want {
			t.Errorf("verdictForSpec(%q) = (%q,%v), want (%q,%v)", c.specID, got, hit, c.want, c.wantHit)
		}
	}

	// 歧义保护：两条同核 ⇒ 不判。
	amb := map[string]string{"a/dup-free": "throttled", "b/dup:free": "region-blocked"}
	if _, hit := verdictForSpec("x/dup", amb); hit {
		t.Error("同核 ≥2 条时不得下判定（避免误杀）")
	}
}

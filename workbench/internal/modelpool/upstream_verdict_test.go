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

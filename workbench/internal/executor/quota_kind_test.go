package executor

import "testing"

// TestQuotaExhaustedText 钉住「额度耗尽」的判据边界（round 91 · T76）。
//
// 现场（2026-10-29，近两小时 409 次 rate_limited，elapsedMs 中位 2.9s、333/409 落在 2.5–4s 桶）：
// 引擎在**原地空转** —— 冷却窗口一开就再试、每次秒失败、再冷却，同一分钟最多撞 13 次。
// 根因是把「免费额度耗尽」与「瞬时 429」混成一个 kind，于是两者共用秒级冷却。
//
// ⚠️ 判据边界由**既有测试**当场纠正过一次（TestKindFromStreamError）：`RATE_LIMIT: Provider returned error`
// 的 message 里没有任何关键词、只能靠 code 定性 ⇒ **不能**把 `provider returned error` 当额度证据，
// 否则真正的瞬时限流会被判成小时级冷却、免费车道被无故雪藏。
func TestQuotaExhaustedText(t *testing.T) {
	mustHit := []string{
		`RATE_LIMIT: Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`,
		`RATE_LIMIT: Free usage limit reached. Free models work in OpenCode (https://opencode.ai/zen)`,
	}
	for _, s := range mustHit {
		if !quotaExhaustedText(lowerASCII(s)) {
			t.Errorf("额度耗尽原文必须命中：%q", s)
		}
	}
	mustMiss := []string{
		`RATE_LIMIT: too many requests; retry later`,
		`RATE_LIMIT: Provider returned error`,
		`RATE_LIMIT: Rate limit exceeded: cohere/north-mini-code-20260617/d3a18412`,
		``,
	}
	for _, s := range mustMiss {
		if quotaExhaustedText(lowerASCII(s)) {
			t.Errorf("瞬时限流原文不得判成额度耗尽（会被小时级冷却雪藏）：%q", s)
		}
	}
}

// lowerASCII 只做 ASCII 小写（判据函数接收的已是 lower 文本，这里保持与 classify 的调用口径一致）。
func lowerASCII(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if b[i] >= 65 && b[i] <= 90 {
			b[i] += 32
		}
	}
	return string(b)
}

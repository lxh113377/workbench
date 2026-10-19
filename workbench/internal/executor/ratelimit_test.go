package executor

import (
	"testing"
)

// TestClassifyRateLimit 覆盖现场真实故障：429 的 stderr 形如
//   dsh: RATE_LIMIT: too many requests; retry later
// 旧实现先被 "upstream"/"timeout" 之外的 "dsh:" 兜底规则吃掉，判成
// boot_failed（不可重试）→ 引擎立刻换模型 → 整池各挨一次 429 → 加剧限流。
func TestClassifyRateLimit(t *testing.T) {
	cases := []string{
		"dsh: RATE_LIMIT: too many requests; retry later",
		"RATE_LIMIT",
		"too many requests",
		"HTTP 429 Too Many Requests",
		"{\"code\":\"RATE_LIMIT\",\"status\":429}",
	}
	for _, c := range cases {
		got := classify(c, &Result{ExitCode: 1})
		if got != KindRateLimited {
			t.Fatalf("classify(%q) = %s, want %s", c, got, KindRateLimited)
		}
	}
}

// TestRateLimitedRetryableButNotModelDead 保证限流不会被当成模型下线：
// 模型下线要长冷却 12h，限流只需按 Retry-After 冷却。
func TestRateLimitedRetryableButNotModelDead(t *testing.T) {
	if !KindRateLimited.Retryable() {
		t.Fatal("限流应可重试（整池冷却后重试）")
	}
	if KindRateLimited == KindModelDead {
		t.Fatal("限流不能等同模型下线")
	}
}

// TestClassifyBootNotShadowed 保证真正的启动失败仍归 boot_failed。
func TestClassifyBootNotShadowed(t *testing.T) {
	if got := classify("dsh: failed to start packaged dsh: boom", &Result{ExitCode: 1}); got != KindBoot {
		t.Fatalf("classify = %s, want %s", got, KindBoot)
	}
}

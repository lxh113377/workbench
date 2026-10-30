package executor

import "testing"

// TestQuotaTextWinsOverStructuredRateLimited 钉住 T100（round 144）：
// 额度耗尽的**专有措辞**必须优先于「结构化字段已判成 rate_limited」的短路。
//
// 现场量化（agent 根全量扫 attempts.jsonl，2109 行）：**114 行** kind=rate_limited 的 error 正文其实是
//
//	`RATE_LIMIT: Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`
//
// —— 带 RATE_LIMIT 前缀 ⇒ 上游结构化字段先定性成瞬时限流，而旧实现的结构化短路又在额度原文判定之前 return，
// 于是账号级事实被降级：短冷却 + 计入「这个模型不行」的可归因失败。
func TestQuotaTextWinsOverStructuredRateLimited(t *testing.T) {
	// 上游给免费额度用完时的原文（executor.go 常量注释里登记的两条之一）。
	const quotaMsg = `RATE_LIMIT: Sign up for free to continue and explore 500 other models. Takes 2 minutes to sign up.`
	const quotaMsg2 = `RATE_LIMIT: Free usage limit reached. Free models work in OpenCode.`

	// ① 结构化字段已是 rate_limited，正文是额度原文 ⇒ 必须升级为额度耗尽
	if got := classify(quotaMsg, &Result{ErrorKind: string(KindRateLimited)}); got != KindQuotaExhausted {
		t.Fatalf("正文是额度原文时应判 quota_exhausted，实得 %q ⇒ 账号级事实被降级成瞬时限流（T100 未修）", got)
	}
	if got := classify(quotaMsg2, &Result{ErrorKind: string(KindRateLimited)}); got != KindQuotaExhausted {
		t.Fatalf("第二条额度原文同理，实得 %q", got)
	}
	// ② 正文只落在 LastError（stderr 是通用 429）⇒ 也要升级
	if got := classify("dsh: RATE_LIMIT: too many requests; retry later",
		&Result{ErrorKind: string(KindRateLimited), LastError: quotaMsg}); got != KindQuotaExhausted {
		t.Fatalf("LastError 带额度原文时应判 quota_exhausted，实得 %q（纯 stderr 分类会漏这一侧）", got)
	}
	// ③ 保守面：真正的瞬时限流必须保持 rate_limited（否则免费车道会被无故雪藏小时级）
	if got := classify(`dsh: RATE_LIMIT: too many requests; retry later`, &Result{ErrorKind: string(KindRateLimited)}); got != KindRateLimited {
		t.Fatalf("瞬时限流必须保持 rate_limited，实得 %q", got)
	}
	// ④ 保守面：429 的通用外壳不算额度（判据纪律：`provider returned error` 不是额度措辞）
	if got := classify(`RATE_LIMIT: Provider returned error`, &Result{ErrorKind: string(KindRateLimited)}); got != KindRateLimited {
		t.Fatalf("通用外壳不得升成额度耗尽，实得 %q", got)
	}
}

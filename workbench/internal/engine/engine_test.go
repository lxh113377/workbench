package engine

import (
	"context"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/retry"
)

func TestBuildRetryPolicyLegacyCompat(t *testing.T) {
	// 旧配置只写 maxRetries + retryBackoffMs：必须原样生效
	cfg := config.ExecutorConfig{
		MaxRetries:     3,
		RetryBackoffMs: []int{5000, 20000, 60000},
	}
	p := buildRetryPolicy(cfg)
	got := []time.Duration{
		p.Decide(retry.Timeout, 0, "").Delay,
		p.Decide(retry.Timeout, 1, "").Delay,
		p.Decide(retry.Timeout, 2, "").Delay,
	}
	want := []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("legacy backoff[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	if d := p.Decide(retry.Timeout, 3, ""); d.Action != retry.ActionFail {
		t.Fatalf("attempt beyond max should fail, got %s", d.Action)
	}
}

func TestBuildRetryPolicyNewConfig(t *testing.T) {
	// 新配置：空列表 → 指数退避 + 抖动 + 白名单类别
	cfg := config.ExecutorConfig{
		MaxRetries: 2,
		RetryPolicy: config.RetryPolicyConfig{
			BaseDelayMs:    1000,
			MaxDelayMs:     4000,
			Jitter:         0,
			RetryableKinds: []string{"timeout", "upstream_failed"},
		},
	}
	p := buildRetryPolicy(cfg)
	if d := p.Decide(retry.Timeout, 0, "").Delay; d != time.Second {
		t.Fatalf("delay = %v, want 1s", d)
	}
	if d := p.Decide(retry.Timeout, 1, "").Delay; d != 2*time.Second {
		t.Fatalf("delay = %v, want 2s", d)
	}
	// 不在白名单里的类别直接失败
	if d := p.Decide(retry.Exit, 0, ""); d.Action != retry.ActionFail {
		t.Fatalf("exit not in whitelist should fail, got %s", d.Action)
	}
}

func TestBuildRetryPolicyNonRetryableByKind(t *testing.T) {
	p := buildRetryPolicy(config.DefaultConfig().Executor)
	for _, kind := range []retry.Kind{retry.MissingKey, retry.ModelDead, retry.Boot} {
		if d := p.Decide(kind, 0, "x"); d.Action != retry.ActionFail {
			t.Fatalf("kind %s should fail immediately, got %s", kind, d.Action)
		}
	}
}

func TestCtxCancelText(t *testing.T) {
	if got := ctxCancelText(nil); got != "" {
		t.Fatalf("nil err text = %q, want empty", got)
	}
	if got := ctxCancelText(errFake{}); got != "" {
		t.Fatalf("non-ctx err text = %q, want empty", got)
	}
}

type errFake struct{}

func (errFake) Error() string { return "boom" }

func TestErrText(t *testing.T) {
	if got := errText(nil); got != "" {
		t.Fatalf("nil err text = %q, want empty", got)
	}
	if got := errText(errFake{}); got != "boom" {
		t.Fatalf("err text = %q, want boom", got)
	}
}

func TestCtxCancelTextTranslation(t *testing.T) {
	if got := ctxCancelText(context.Canceled); got == "" {
		t.Fatal("context.Canceled 应翻译成中文说明")
	}
	if got := ctxCancelText(context.DeadlineExceeded); got == "" {
		t.Fatal("context.DeadlineExceeded 应翻译成中文说明")
	}
}

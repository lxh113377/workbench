package engine

import (
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/retry"
)

func TestBuildRetryPolicyDefaultConfigJittered(t *testing.T) {
	cfg := config.DefaultConfig().Executor
	p := buildRetryPolicy(cfg)
	base := time.Duration(cfg.RetryBackoffMs[0]) * time.Millisecond
	got := p.Decide(retry.Timeout, 0, "").Delay
	lower := time.Duration(float64(base) * 0.8)
	if got < lower || got > base {
		t.Fatalf("delay = %v, want in [%v, %v]", got, lower, base)
	}
}

func TestErrTextHelpers(t *testing.T) {
	if got := errText(nil); got != "" {
		t.Fatalf("nil err text = %q, want empty", got)
	}
	if got := errText(errFake{}); got != "boom" {
		t.Fatalf("err text = %q, want boom", got)
	}
	if got := ctxCancelText(nil); got != "" {
		t.Fatalf("nil ctx err = %q, want empty", got)
	}
	if got := ctxCancelText(errFake{}); got != "" {
		t.Fatalf("non-ctx err = %q, want empty", got)
	}
}

package retry

import (
	"testing"
	"time"
)

func TestKindRetryable(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"timeout", true},
		{"upstream_failed", true},
		{"bad_exit", true},
		{"stalled", true},
		{"unknown", true},
		{"missing_credential", false},
		{"model_unavailable", false},
		{"boot_failed", false},
		{"cancelled", false},
	}
	for _, c := range cases {
		got := ParseKind(c.kind).Retryable()
		if got != c.want {
			t.Fatalf("kind %q retryable = %v, want %v", c.kind, got, c.want)
		}
	}
}

func TestParseKindNormalizes(t *testing.T) {
	if got := ParseKind(""); got != Unknown {
		t.Fatalf("ParseKind(empty) = %q, want unknown", got)
	}
	if got := ParseKind("  weird_kind  "); got != Unknown {
		t.Fatalf("ParseKind(weird) = %q, want unknown", got)
	}
	if got := ParseKind("timeout"); got != Timeout {
		t.Fatalf("ParseKind(timeout) = %q, want timeout", got)
	}
}

func TestDecideNonRetryableFailsImmediately(t *testing.T) {
	p := New(Policy{MaxRetries: 5, BaseDelay: time.Millisecond})
	for _, kind := range []Kind{MissingKey, ModelDead, Boot} {
		got := p.Decide(kind, 0, "boom")
		if got.Action != ActionFail {
			t.Fatalf("kind %s: action = %s, want fail", kind, got.Action)
		}
		if got.Delay != 0 {
			t.Fatalf("kind %s: delay = %v, want 0", kind, got.Delay)
		}
	}
}

func TestDecideCancelled(t *testing.T) {
	p := Default()
	got := p.Decide(Cancelled, 0, "user cancel")
	if got.Action != ActionCancel {
		t.Fatalf("action = %s, want cancel", got.Action)
	}
}

func TestDecideAttemptsExhausted(t *testing.T) {
	p := New(Policy{MaxRetries: 2, BaseDelay: time.Millisecond})
	if got := p.Decide(Timeout, 0, ""); got.Action != ActionRetry {
		t.Fatalf("attempt 0 should retry, got %s", got.Action)
	}
	if got := p.Decide(Timeout, 1, ""); got.Action != ActionRetry {
		t.Fatalf("attempt 1 should retry, got %s", got.Action)
	}
	if got := p.Decide(Timeout, 2, ""); got.Action != ActionFail {
		t.Fatalf("attempt 2 should fail, got %s", got.Action)
	}
}

func TestDelayFromLegacyBackoffList(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BackoffMs: []int{5000, 20000, 60000}, Jitter: 0})
	want := []time.Duration{5 * time.Second, 20 * time.Second, 60 * time.Second}
	for i, w := range want {
		if got := p.Decide(Timeout, i, "").Delay; got != w {
			t.Fatalf("attempt %d delay = %v, want %v", i, got, w)
		}
	}
}

func TestDelayExponentialCapped(t *testing.T) {
	p := New(Policy{MaxRetries: 10, BaseDelay: time.Second, MaxDelay: 8 * time.Second, Jitter: 0})
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := p.Decide(Timeout, i, "").Delay; got != w {
			t.Fatalf("attempt %d delay = %v, want %v", i, got, w)
		}
	}
}

func TestJitterStaysWithinBounds(t *testing.T) {
	p := New(Policy{MaxRetries: 5, BaseDelay: time.Second, Jitter: 0.2})
	for i := 0; i < 200; i++ {
		d := p.Decide(Timeout, 0, "").Delay
		if d <= 0 || d > time.Second {
			t.Fatalf("jittered delay out of bounds: %v", d)
		}
	}
}

func TestRetryableKindsWhitelist(t *testing.T) {
	p := New(Policy{MaxRetries: 3, BaseDelay: time.Millisecond, RetryableKinds: []Kind{Timeout}})
	if got := p.Decide(Timeout, 0, ""); got.Action != ActionRetry {
		t.Fatalf("whitelisted timeout should retry, got %s", got.Action)
	}
	if got := p.Decide(Upstream, 0, ""); got.Action != ActionFail {
		t.Fatalf("non-whitelisted upstream should fail, got %s", got.Action)
	}
}

func TestIsRetryablePackageHelper(t *testing.T) {
	if !IsRetryable("timeout") {
		t.Fatal("timeout should be retryable")
	}
	if IsRetryable("missing_credential") {
		t.Fatal("missing_credential must not be retryable")
	}
	if !IsRetryable("something_new") {
		t.Fatal("unknown kinds default to retryable")
	}
}

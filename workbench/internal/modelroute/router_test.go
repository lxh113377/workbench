package modelroute

import (
	"testing"
	"time"

	"workbench/internal/config"
)

func TestPickAvoidsExcluded(t *testing.T) {
	routes := []config.ModelRoute{
		{Model: "a", Tier: "balanced", Enabled: true, Weight: 100},
		{Model: "b", Tier: "balanced", Enabled: true, Weight: 100},
	}
	r := New(routes)
	got, err := r.Pick("balanced", map[string]bool{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Fatalf("expected b, got %s", got)
	}
}

func TestCooldownAfterFailures(t *testing.T) {
	routes := []config.ModelRoute{{Model: "x", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 2, CooldownM: 30}}
	r := New(routes)
	r.RecordFailure("x", "upstream", "boom")
	r.RecordFailure("x", "upstream", "boom")
	if _, err := r.Pick("balanced", nil); err == nil {
		t.Fatal("model should be in cooldown")
	}
	r.ResetCooldown("x")
	if _, err := r.Pick("balanced", nil); err != nil {
		t.Fatalf("after reset should be pickable: %v", err)
	}
}

func TestModelUnavailableLongCooldown(t *testing.T) {
	routes := []config.ModelRoute{{Model: "dead", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30}}
	r := New(routes)
	r.RecordFailure("dead", "model_unavailable", "EOL")
	h := r.Health()
	if len(h) != 1 {
		t.Fatal("expected one health entry")
	}
	if h[0].CooldownTo < time.Now().Add(6*time.Hour).UnixMilli() {
		t.Fatalf("model_unavailable should get long cooldown, got %d", h[0].CooldownTo)
	}
}

func TestSuccessResetsConsecFail(t *testing.T) {
	routes := []config.ModelRoute{{Model: "y", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30}}
	r := New(routes)
	r.RecordFailure("y", "upstream", "x")
	r.RecordSuccess("y", 1000)
	h := r.Health()[0]
	if h.ConsecFail != 0 {
		t.Fatalf("consecFail = %d, want 0", h.ConsecFail)
	}
}

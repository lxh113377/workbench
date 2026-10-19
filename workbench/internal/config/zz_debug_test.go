package config

import (
	"encoding/json"
	"testing"
)

func TestDebugNormalizeDiff(t *testing.T) {
	c := DefaultConfig()
	before, _ := json.MarshalIndent(c, "", "  ")
	c.Normalize()
	after, _ := json.MarshalIndent(c, "", "  ")
	if string(before) == string(after) {
		t.Log("identical")
		return
	}
	var bm, am map[string]any
	_ = json.Unmarshal(before, &bm)
	_ = json.Unmarshal(after, &am)
	for k := range am {
		b, _ := json.Marshal(bm[k])
		a, _ := json.Marshal(am[k])
		if string(b) != string(a) {
			t.Logf("DIFF top-level key=%s\n  before=%s\n  after=%s", k, string(b), string(a))
		}
	}
}

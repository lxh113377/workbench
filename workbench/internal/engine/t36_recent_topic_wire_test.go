package engine

import (
	"context"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestSupplyCandidatesExcludesRecentTopics 是 T36 接线判据：评估器窗口的近期主题不得被 supplyCandidates 复现；负向对照见答复第 ④ 节。
func TestSupplyCandidatesExcludesRecentTopics(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Autonomy.Enabled, c.Eval.DailyCap = false, 100000
	}); err != nil {
		t.Fatal(err)
	}
	tpl := e.reg.Enabled()[0]
	excluded := map[string]bool{}
	for i := 0; i < 5000 && len(excluded) < recentTopicWindow; i++ {
		excluded[topicFromTitle(e.gen.RootTask(tpl, 1700000000).Title)] = true
	}
	for topic := range excluded {
		e.eval.Observe(&model.Task{ID: "t36-" + topic, Domain: tpl.Domain,
			Title: tpl.Name + " · " + topic}, 9, tpl.Domain, 400)
	}
	for i := 0; i < 60; i++ {
		e.supplyCandidates(context.Background(), e.Config())
	}
	for _, tk := range e.Store().List() {
		if tk.Domain == tpl.Domain && excluded[topicFromTitle(tk.Title)] {
			t.Fatalf("近期已用主题 %q 被重复产出（%q）⇒ 接线失效", topicFromTitle(tk.Title), tk.Title)
		}
	}
}

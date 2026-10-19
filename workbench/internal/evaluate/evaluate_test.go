package evaluate

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func TestWeightsNormalize(t *testing.T) {
	cases := []struct {
		in   model.Weights
		want float64
	}{
		{model.Weights{Value: 1, Cost: 1, Feasibility: 1}, 1.0},
		{model.Weights{Value: 0, Cost: 0, Feasibility: 0}, 1.0},
		{model.Weights{Value: -1, Cost: 2, Feasibility: 0}, 1.0},
	}
	for _, c := range cases {
		got := c.in.Normalized()
		sum := got.Value + got.Cost + got.Feasibility
		if diff := sum - c.want; diff > 1e-9 || diff < -1e-9 {
			t.Fatalf("Normalized() sum = %v, want %v", sum, c.want)
		}
	}
}

func TestTransitionAllowed(t *testing.T) {
	allowed := [][2]model.TaskState{
		{model.StateCandidate, model.StateEvaluating},
		{model.StateEvaluating, model.StateApproved},
		{model.StateApproved, model.StateQueued},
		{model.StateQueued, model.StateRunning},
		{model.StateRunning, model.StateDone},
	}
	for _, pair := range allowed {
		if !model.TransitionAllowed(pair[0], pair[1]) {
			t.Fatalf("expected %s -> %s allowed", pair[0], pair[1])
		}
	}
	denied := [][2]model.TaskState{
		{model.StateDone, model.StateRunning},
		{model.StateQueued, model.StateDone},
		{model.StateRejected, model.StateRunning},
	}
	for _, pair := range denied {
		if model.TransitionAllowed(pair[0], pair[1]) {
			t.Fatalf("expected %s -> %s denied", pair[0], pair[1])
		}
	}
}

func TestEvaluateRejectsDuplicate(t *testing.T) {
	cfg := config.DefaultConfig().Eval
	ev := New(cfg)
	done := &model.Task{ID: "t1", Title: "深度长文 · AI 效率", Prompt: "请写一篇关于AI效率的深度文章", State: model.StateDone, CreatedAt: model.Now()}
	ev.Observe(done, 8, "content", 100)

	dup := &model.Task{ID: "t2", Title: "深度长文 · AI 效率", Prompt: "请写一篇关于AI效率的深度文章", CreatedAt: model.Now()}
	scores := ev.Evaluate(Input{Task: dup, Now: model.Now()})
	if len(scores.Gates) == 0 {
		t.Fatal("expected duplicate gate to fire")
	}
	if Approve(scores) {
		t.Fatal("duplicate task must not be approved")
	}
}

func TestEvaluateThreshold(t *testing.T) {
	cfg := config.DefaultConfig().Eval
	cfg.Threshold = 101 // 不可能达到
	ev := New(cfg)
	task := &model.Task{ID: "t3", Title: "全新主题 · 量子计算入门", Prompt: "请写量子计算入门手册", CreatedAt: model.Now()}
	scores := ev.Evaluate(Input{Task: task, Now: model.Now()})
	if Approve(scores) {
		t.Fatalf("expected rejection with threshold 101, total=%v", scores.Total)
	}
}

func TestNoveltyIncreasesForNewTopic(t *testing.T) {
	cfg := config.DefaultConfig().Eval
	ev := New(cfg)
	ev.Observe(&model.Task{ID: "a", Title: "深度长文 · 个人效率", Prompt: "写个人效率文章", State: model.StateDone, CreatedAt: model.Now()}, 8, "content", 100)
	fresh := &model.Task{ID: "b", Title: "知识地图 · 分布式系统", Prompt: "整理分布式系统知识地图", CreatedAt: model.Now()}
	n := ev.novelty(fresh)
	if n < 50 {
		t.Fatalf("expected high novelty for unrelated topic, got %v", n)
	}
}

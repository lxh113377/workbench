package model

import "testing"

func TestTransitionTable(t *testing.T) {
	// 正向：主链必须通
	positive := [][2]TaskState{
		{StateCandidate, StateEvaluating},
		{StateEvaluating, StateApproved},
		{StateApproved, StateQueued},
		{StateQueued, StateRunning},
		{StateRunning, StateDone},
		{StateRunning, StateInterrupted},
		{StateInterrupted, StateQueued},
		{StateRunning, StateFailed},
		{StateFailed, StateQueued},
		{StateTimeout, StateQueued},
		{StateRejected, StateQueued}, // 人工重开被拒任务（回归：以前非法）
	}
	for _, p := range positive {
		if !TransitionAllowed(p[0], p[1]) {
			t.Fatalf("%s -> %s 应被允许", p[0], p[1])
		}
	}

	// 反向：跨级/回退必须被拒
	negative := [][2]TaskState{
		{StateCandidate, StateRunning},
		{StateQueued, StateDone},
		{StateDone, StateQueued},
		{StateCancelled, StateQueued},
		{StateRejected, StateRunning},
		{StateApproved, StateDone},
	}
	for _, n := range negative {
		if TransitionAllowed(n[0], n[1]) {
			t.Fatalf("%s -> %s 应被拒绝", n[0], n[1])
		}
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := []TaskState{StateDone, StateFailed, StateRejected, StateCancelled}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Fatalf("%s 应为终态", s)
		}
	}
	nonTerminal := []TaskState{StateCandidate, StateQueued, StateRunning, StateTimeout, StateInterrupted, StateApproved, StateEvaluating}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Fatalf("%s 不应为终态", s)
		}
	}
}

func TestIsActive(t *testing.T) {
	if !StateRunning.IsActive() || !StateEvaluating.IsActive() {
		t.Fatal("running/evaluating 应为活跃态")
	}
	if StateQueued.IsActive() {
		t.Fatal("queued 不应为活跃态")
	}
}

func TestWeightsNormalized(t *testing.T) {
	w := Weights{Value: 5, Cost: 3, Feasibility: 2}.Normalized()
	sum := w.Value + w.Cost + w.Feasibility
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("归一化后权重和 = %v, want 1", sum)
	}
	// 全零/负数必须回退默认权重
	z := Weights{}.Normalized()
	if z.Value != 0.5 || z.Cost != 0.2 || z.Feasibility != 0.3 {
		t.Fatalf("零权重回退错误: %+v", z)
	}
	neg := Weights{Value: -1, Cost: -1, Feasibility: -1}.Normalized()
	if neg.Value != 0.5 {
		t.Fatalf("负权重回退错误: %+v", neg)
	}
}

func TestTruncateUTF8Safe(t *testing.T) {
	s := "中文测试内容"
	got := Truncate(s, 4) // 4 字节会切断中文，必须回退到合法边界
	for _, r := range got {
		if r == 0xFFFD {
			t.Fatalf("Truncate 产生了非法 UTF-8: %q", got)
		}
	}
	if len(got) > 4 {
		t.Fatalf("Truncate 超出上限: %d bytes", len(got))
	}
	if Truncate("abc", 0) != "abc" || Truncate("abc", 10) != "abc" {
		t.Fatal("边界截断行为错误")
	}
}

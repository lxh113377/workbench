package queue

import (
	"testing"

	"workbench/internal/model"
)

func mk(id, chain string, priority int, pinned bool, created int64) *model.Task {
	return &model.Task{ID: id, ChainID: chain, Priority: priority, Pinned: pinned,
		CreatedAt: created, State: model.StateQueued, Title: id}
}

func TestPopRespectsPriorityAndFIFO(t *testing.T) {
	q := New()
	q.Push(mk("low", "", 1, false, 100))
	q.Push(mk("high", "", 5, false, 200))
	q.Push(mk("same-prio-earlier", "", 3, false, 50))
	q.Push(mk("same-prio-later", "", 3, false, 60))
	q.Push(mk("pinned", "", 1, true, 999))

	order := []string{}
	for {
		got := q.Pop()
		if got == nil {
			break
		}
		order = append(order, got.ID)
	}
	want := []string{"pinned", "high", "same-prio-earlier", "same-prio-later", "low"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

func TestPopSkipsNonQueued(t *testing.T) {
	q := New()
	task := mk("a", "", 1, false, 1)
	task.State = model.StateRunning
	q.Push(task)
	if got := q.Pop(); got != nil {
		t.Fatalf("非 queued 状态不应被取出，got %s", got.ID)
	}
	if q.Len() != 1 {
		t.Fatalf("len = %d, want 1", q.Len())
	}
}

func TestChainSerialization(t *testing.T) {
	q := New()
	q.Push(mk("c1", "chain-A", 5, false, 1))
	q.Push(mk("c2", "chain-A", 4, false, 2))
	q.Push(mk("other", "chain-B", 1, false, 3))

	first := q.Pop()
	if first == nil || first.ID != "c1" {
		t.Fatalf("first = %v, want c1", first)
	}
	second := q.Pop()
	if second == nil || second.ID != "other" {
		t.Fatalf("second = %v, want other（同链必须串行）", second)
	}
	if third := q.Pop(); third != nil {
		t.Fatalf("第三条不该被取出（chain-A 仍占用），got %s", third.ID)
	}
	q.ReleaseChain("chain-A")
	third := q.Pop()
	if third == nil || third.ID != "c2" {
		t.Fatalf("释放链后应取到 c2, got %v", third)
	}
}

func TestReleaseEmptyChainIsNoop(t *testing.T) {
	q := New()
	q.ReleaseChain("")
	q.ReleaseChain("never-seen")
	if q.BusyChains() != 0 {
		t.Fatalf("busy chains = %d, want 0", q.BusyChains())
	}
}

func TestRemoveAndReset(t *testing.T) {
	q := New()
	q.Push(mk("a", "", 1, false, 1))
	q.Push(mk("b", "", 1, false, 2))
	q.Remove("a")
	if q.Len() != 1 {
		t.Fatalf("len = %d, want 1", q.Len())
	}
	q.Reset([]*model.Task{mk("x", "", 1, false, 1), mk("y", "", 1, false, 2), nil})
	if q.Len() != 2 {
		t.Fatalf("reset len = %d, want 2（nil 必须被忽略）", q.Len())
	}
}

func TestPushIgnoresEmptyID(t *testing.T) {
	q := New()
	q.Push(nil)
	q.Push(&model.Task{})
	if q.Len() != 0 {
		t.Fatalf("len = %d, want 0", q.Len())
	}
}

func TestReorder(t *testing.T) {
	q := New()
	q.Push(mk("a", "", 1, false, 1))
	q.Push(mk("b", "", 2, false, 2))
	if err := q.Reorder("a", 9, true); err != nil {
		t.Fatal(err)
	}
	got := q.Pop()
	if got == nil || got.ID != "a" {
		t.Fatalf("reorder 后应先取 a, got %v", got)
	}
	if err := q.Reorder("missing", 1, false); err == nil {
		t.Fatal("重排不存在的任务必须报错")
	}
}

func TestSnapshotCopyNotAliased(t *testing.T) {
	q := New()
	q.Push(mk("a", "", 1, false, 1))
	snap := q.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	snap[0].Priority = 99
	again := q.Snapshot()
	if again[0].Priority == 99 {
		t.Fatal("Snapshot 必须返回副本，外部修改不得污染队列")
	}
}

package queue

import (
	"testing"

	"workbench/internal/model"
)

// TestPopPrunesTerminalResidue 钉住 T95（round 139，由独立复核的代码面取证指出）。
//
// 病灶机制：Len() 计全部条目（queue.go:28），而 Pop 对"非 queued"只 continue 不删除，
// 且 Remove 全仓只有 CancelTask 一处调用（engine.go:2059）⇒ 已入队后又被改成终态的任务
// 会永久留在 items 里：取不走、却让 q.Len() 恒 > 0，把 engine.go:835 的供料门**永久关死**
// （自主规划与模板补货同时停摆，直到重启）。
//
// 判据分两半：① 终态残留被剔（修好的那一半）；② 非终态的中间态不许被剔（保守的那一半，
// 删了会让 approved→queued 路上的任务永远不被派发）。
func TestPopPrunesTerminalResidue(t *testing.T) {
	q := New()
	q.Push(&model.Task{ID: "dead", State: model.StateCancelled, Priority: 5})
	q.Push(&model.Task{ID: "live", State: model.StateQueued, Priority: 1})
	if got := q.Len(); got != 2 {
		t.Fatalf("前置不成立：Len 应计全部条目=2（含终态残留），实得 %d —— 若语义已变，本判据需重写", got)
	}
	first := q.Pop()
	if first == nil || first.ID != "live" {
		t.Fatalf("应取到 live（终态不可派发），实得 %v", first)
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("终态残留未被剔除：Pop 后 Len=%d，want 0 ⇒ 供料门会被永久关死（T95 未修）", got)
	}
	if again := q.Pop(); again != nil {
		t.Fatalf("残留剔掉后不该还能取到东西，实得 %v", again.ID)
	}
}

func TestPopKeepsNonTerminalNonQueued(t *testing.T) {
	q := New()
	q.Push(&model.Task{ID: "mid", State: model.StateApproved, Priority: 5})
	if got := q.Pop(); got != nil {
		t.Fatalf("非 queued 不该被派发，实得 %v", got.ID)
	}
	if got := q.Len(); got != 1 {
		t.Fatalf("非终态的中间态不许被剔除（删了它永远不被派发），实得 Len=%d", got)
	}
}

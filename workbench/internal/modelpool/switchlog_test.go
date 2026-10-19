package modelpool

import (
	"context"
	"encoding/json"
	"testing"

	"workbench/internal/model"
)

// TestSwitchLogRecordsActualPredecessor 锁住需求4 的「保留模型状态日志便于排查问题」：
// 多跳接力时，每条 switch 记录的 from 必须是**真正上一个失败让位的模型**，
// 不能恒等于本次尝试的首发模型。
//
// 现场缺陷（2026-10-19 活体审计发现）：Run 里 from := order[0] 之后从不更新，
// 于是 a 失败、b 也失败、c 成功时，日志记成两条 from=a（a→b、a→c）。
// 拿这份日志排查会以为 b 从没被调用过、或 a 直接跳到了 c —— 恰好是「便于排查」的反面。
// 更糟的是它看起来完全正常：切换确实发生了，功能没坏，只有留痕是错的。
func TestSwitchLogRecordsActualPredecessor(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{
		laneSpec("a", 1, 100, "free"),
		laneSpec("b", 2, 90, "free"),
		laneSpec("c", 3, 80, "kilo"),
	})
	sch := NewScheduler(StrategyPriority)
	// a、b 失败，c 成功：应产生两条切换记录 a→b、b→c。
	rec := &recordingRunner{failFor: map[string]bool{"a": true, "b": true}}
	fr := NewFailoverRunner(rec, mgr, sch)

	task := &model.Task{ID: "t-log", Title: "切换日志准确性", State: model.StateQueued}
	// context.Background() 无 deadline ⇒ hasTimeLeft 恒真，接力不会被时间闸门提前掐断。
	res, err := fr.Run(context.Background(), task, "a")
	if err != nil || res == nil || res.FinalText == "" {
		t.Fatalf("c 应顶上成功，err=%v res=%+v", err, res)
	}
	if seen := rec.seen(); len(seen) != 3 || seen[0] != "a" || seen[1] != "b" || seen[2] != "c" {
		t.Fatalf("调用顺序应为 a,b,c，实际 %v", seen)
	}

	type sw struct {
		Kind string `json:"kind"`
		From string `json:"from"`
		To   string `json:"to"`
	}
	var got []sw
	for _, line := range store.TailLog(200) {
		var s sw
		if json.Unmarshal([]byte(line), &s) != nil {
			continue
		}
		if s.Kind == "switch" {
			got = append(got, s)
		}
	}
	if len(got) != 2 {
		t.Fatalf("应有 2 条切换记录（a→b、b→c），实际 %d：%+v", len(got), got)
	}
	if got[0].From != "a" || got[0].To != "b" {
		t.Fatalf("第一条应为 a→b，实际 %s→%s", got[0].From, got[0].To)
	}
	// 关键断言：第二条的 from 必须是 b（真正让位的那个），不是首发 a。
	if got[1].From != "b" || got[1].To != "c" {
		t.Fatalf("第二条应为 b→c，实际 %s→%s —— from 恒为首发会把排查者带偏", got[1].From, got[1].To)
	}
}

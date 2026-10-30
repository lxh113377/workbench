package engine

import (
	"context"
	"testing"
	"time"

	"workbench/internal/model"
)

// TestSupplyBlockedObservationRunsAfterDispatch 钉住 T96（round 140）：
// 供料门的观测点必须在**取件尝试之后** —— 否则队列里明明有可取任务，也会先报一条
// 「队列有货但无在跑任务」（旧位置读到的 inflight 只是"本 tick 还没 Pop"那一瞬）。
//
// 这条判据在修复前必然是红的：旧实现于取件循环之前观测，此处 Len=1、inflight=0 ⇒ 必报。
//
// 为什么每次断言前都 WaitInflight：tick 会**异步**起 runOne goroutine，日志与状态都在变动中；
// 不等就既不确定、也让 `-race`（CI 用 race 跑）更难判。
func TestSupplyBlockedObservationRunsAfterDispatch(t *testing.T) {
	e, st, _ := newTestEngine(t)
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))

	const id = "t-dispatchable"
	if err := st.Create(&model.Task{ID: id, Title: "可取件", State: model.StateCandidate, MaxAttempts: 3, OutputFormat: "code"}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	got, ok := st.Get(id)
	if !ok {
		t.Fatal("任务不见了")
	}
	e.q.Push(got)

	e.tick(context.Background())
	if !e.WaitInflight(5 * time.Second) {
		t.Log("在途后台工作 5s 未排空（继续断言）")
	}

	// 前置断言（防空洞）：这一 tick 必须真的把它取走了 —— 否则下面的"没报警"可能只是 tick 提前返回。
	if n := e.q.Len(); n != 0 {
		t.Fatalf("前置不成立：可取件任务仍留在队列（Len=%d）⇒ 本判据没测到取件路径，需先修测试", n)
	}
	if hits := cap.find("供料门被挡"); len(hits) != 0 {
		t.Fatalf("队列里有可取件任务时不该报「供料门被挡」（观测点仍在取件之前 ⇒ T96 未修）；实际 %d 条：\n%s",
			len(hits), cap.text())
	}
}

// 另一半（防我把门拆了）：队列里只剩**取不走**的东西、且没有任何在跑任务 ⇒ 必须报。
// 这里用 approved 态：Pop 会跳过它（非 queued），且它**非终态**、不许被剔除（与 T95 的边界一致）。
func TestSupplyBlockedObservationFiresWhenNothingDispatchable(t *testing.T) {
	e, st, _ := newTestEngine(t)
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))

	const id = "t-midstate"
	if err := st.Create(&model.Task{ID: id, Title: "中间态", State: model.StateCandidate, MaxAttempts: 3, OutputFormat: "code"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(id, model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get(id)
	e.q.Push(got)

	e.tick(context.Background())
	if !e.WaitInflight(5 * time.Second) {
		t.Log("在途后台工作 5s 未排空（继续断言）")
	}

	if n := e.q.Len(); n != 1 {
		t.Fatalf("前置不成立：非终态中间态不该被剔除，Len=%d want 1（若为 0 说明 T95 的保守面被破坏）", n)
	}
	if hits := cap.find("供料门被挡"); len(hits) != 1 {
		t.Fatalf("取不走任何任务且无在跑任务时应报一条「供料门被挡」，实得 %d 条：\n%s", len(hits), cap.text())
	}
}

// 空队列 = 门开着（下一 tick 自然会补货）：不该报。
func TestSupplyBlockedObservationSilentOnEmptyQueue(t *testing.T) {
	e, _, _ := newTestEngine(t)
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))

	e.tick(context.Background())
	if !e.WaitInflight(5 * time.Second) {
		t.Log("在途后台工作 5s 未排空（继续断言）")
	}

	if hits := cap.find("供料门被挡"); len(hits) != 0 {
		t.Fatalf("空队列不该报「供料门被挡」，实得 %d 条：\n%s", len(hits), cap.text())
	}
}

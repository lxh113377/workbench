package engine

import (
	"testing"

	"workbench/internal/model"
	"workbench/internal/store"
)

func TestFailureBreakdownEmpty(t *testing.T) {
	e, _, _ := newTestEngine(t)
	got := e.FailureBreakdown()
	if len(got) != 0 {
		t.Fatalf("无失败任务时应返回空切片，got %+v", got)
	}
	if got == nil {
		t.Fatal("应返回空切片而非 nil（面板依赖空数组）")
	}
}

func TestFailureBreakdownAggregates(t *testing.T) {
	e, st, _ := newTestEngine(t)
	seedFailedTask(t, st, "f1", "timeout")
	seedFailedTask(t, st, "f2", "timeout")
	seedFailedTask(t, st, "f3", "stalled")
	seedFailedTask(t, st, "f4", "")
	got := e.FailureBreakdown()
	if len(got) != 3 {
		t.Fatalf("buckets = %d, want 3: %+v", len(got), got)
	}
	if got[0].Kind != "timeout" || got[0].Count != 2 {
		t.Fatalf("首个桶应按数量降序: %+v", got[0])
	}
	if got[0].LastAt == 0 {
		t.Fatalf("LastAt 应被填充: %+v", got[0])
	}
	if len(got[0].Samples) == 0 {
		t.Fatalf("Samples 应列出任务 ID: %+v", got[0])
	}
	found := false
	for _, b := range got {
		if b.Kind == "unknown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("空 ErrorKind 应归入 unknown: %+v", got)
	}
}

func TestAttemptSummaryLimit(t *testing.T) {
	e, st, _ := newTestEngine(t)
	if got := e.AttemptSummary(5); len(got) != 0 {
		t.Fatalf("无台账时应为空，got %d", len(got))
	}
	task := &model.Task{ID: "task-att", Title: "台账", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		st.AppendAttempt(store.AttemptRecord{TaskID: "task-att", Attempt: i, Kind: "timeout", Action: "retry"})
	}
	if got := e.AttemptSummary(3); len(got) != 3 {
		t.Fatalf("limit=3 应返回 3 条，got %d", len(got))
	}
	if got := e.AttemptSummary(0); len(got) != 4 {
		t.Fatalf("limit=0 应回退默认并返回全部 4 条，got %d", len(got))
	}
}

func seedFailedTask(t *testing.T, st *store.Store, id, kind string) {
	t.Helper()
	task := &model.Task{ID: id, Title: id, State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateEvaluating, model.StateApproved, model.StateQueued, model.StateRunning, model.StateFailed} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s -> %s: %v", id, to, err)
		}
	}
	if _, err := st.Update(id, func(x *model.Task) error {
		x.ErrorKind = kind
		x.FinishedAt = model.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

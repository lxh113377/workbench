package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/templates"
)

func newTestEngine(t *testing.T) (*Engine, *store.Store, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := templates.Open(layout.DataRoot + string(os.PathSeparator) + "templates.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(layout, cstore, st, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	// WAL 文件是长期打开的句柄：测试结束时必须先关闭，否则 t.TempDir 的
	// RemoveAll 在 Windows 上会因文件被占用而失败（现场 28 个用例集体假红）。
	t.Cleanup(func() { _ = st.Close() })
	return e, st, layout
}

func TestEnqueueManualAndStats(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task, err := e.EnqueueManual("测试标题", "写一篇关于测试的文章，要求结构完整、有结论。", "content", 3)
	if err == nil {
		if task.State != model.StateQueued {
			t.Fatalf("手动任务状态 = %s, want queued", task.State)
		}
		stats := e.Stats()
		if stats.TotalTasks != 1 {
			t.Fatalf("totalTasks = %d, want 1", stats.TotalTasks)
		}
	} else {
		// 未过阈值是被允许的结果（评估器按规则打分），但必须留下 rejected 记录
		list := st.List()
		if len(list) != 1 || list[0].State != model.StateRejected {
			t.Fatalf("评估拒绝应留下 rejected 记录，实际: %+v", list)
		}
	}
}

func TestRateValidation(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-rate", Title: "评分", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := e.Rate("task-rate", 9); err == nil {
		t.Fatal("越界评分必须报错")
	}
	if err := e.Rate("task-rate", 4); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("task-rate")
	if got.Rating != 4 {
		t.Fatalf("rating = %d, want 4", got.Rating)
	}
}

func TestSetPriorityClamp(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-prio", Title: "优先级", State: model.StateQueued}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := e.SetPriority("task-prio", 99, true); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("task-prio")
	if got.Priority != 5 {
		t.Fatalf("priority = %d, want 5（越界钳制）", got.Priority)
	}
	if !got.Pinned {
		t.Fatal("pinned 未生效")
	}
	if err := e.SetPriority("missing", 1, false); err == nil {
		t.Fatal("不存在的任务必须报错")
	}
}

func TestCancelQueuedTask(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-cancel", Title: "取消", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-cancel", model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-cancel", model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelTask("task-cancel"); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Get("task-cancel")
	if got.State != model.StateCancelled {
		t.Fatalf("state = %s, want cancelled", got.State)
	}
}

func TestCancelTerminalTaskErrors(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-done", Title: "已完成", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-done", model.StateRejected, "test", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.CancelTask("task-done"); err == nil {
		t.Fatal("终态任务取消必须报错")
	}
}

func TestRetryOnlyForRetryableStates(t *testing.T) {
	e, st, _ := newTestEngine(t)
	task := &model.Task{ID: "task-retry", Title: "重试", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if err := e.RetryTask("task-retry"); err == nil {
		t.Fatal("candidate 状态不可重试")
	}
	if _, err := st.Transition("task-retry", model.StateRejected, "test", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.RetryTask("task-retry"); err != nil {
		t.Fatalf("rejected 状态应可重试: %v", err)
	}
	got, _ := st.Get("task-retry")
	if got.State != model.StateQueued {
		t.Fatalf("state = %s, want queued", got.State)
	}
}

func TestRecoverRebuildsQueue(t *testing.T) {
	e, st, _ := newTestEngine(t)
	// 模拟崩溃前遗留的 running + queued
	a := &model.Task{ID: "task-recover-a", Title: "运行中", State: model.StateCandidate}
	b := &model.Task{ID: "task-recover-b", Title: "排队中", State: model.StateCandidate}
	for _, task := range []*model.Task{a, b} {
		if err := st.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Transition("task-recover-a", model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-recover-a", model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-recover-a", model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-recover-b", model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition("task-recover-b", model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Recover(); err != nil {
		t.Fatal(err)
	}
	if got := e.QueueSnapshot(); len(got) != 2 {
		t.Fatalf("恢复后队列长度 = %d, want 2（running 收敛为 interrupted 后再入队）", len(got))
	}
	a2, _ := st.Get("task-recover-a")
	if a2.State != model.StateQueued {
		t.Fatalf("悬挂任务恢复后状态 = %s, want queued", a2.State)
	}
}

func TestPolicyHotReload(t *testing.T) {
	e, _, _ := newTestEngine(t)
	before := e.policy().MaxRetries
	if _, err := e.cstore.Update(func(c *config.Config) { c.Executor.MaxRetries = before + 2 }); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	if got := e.policy().MaxRetries; got != before+2 {
		t.Fatalf("热更新后 maxRetries = %d, want %d", got, before+2)
	}
}

func TestCancelAllRunningNoop(t *testing.T) {
	e, _, _ := newTestEngine(t)
	done := make(chan struct{})
	go func() {
		e.cancelAllRunning()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelAllRunning 在无运行任务时不应阻塞")
	}
}

func TestFinishCancelledOnUnknownTask(t *testing.T) {
	e, _, _ := newTestEngine(t)
	// 不存在的任务：只记审计，不 panic
	e.finishCancelled("no-such-task", context.Canceled)
}

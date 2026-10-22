package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
	"workbench/internal/ratelimit"
	"workbench/internal/store"
	"workbench/internal/templates"
)

// noopTestRunner 是空执行器：返回空文本，让语义层走 fail-open（不建任务目录、
// 不拉子进程）。判据见 newTestEngine 的注释。
//
// 它同时实现 SetLimiter/LimiterSnapshot，**故意与真实 *executor.Runner 的能力面对齐**：
// 测试桩缺能力面时，依赖该能力面的判据会拿零值 —— 而历史上 limiter_snapshot_test.go
// 对不支持的桩直接 t.Skip，把"闸门从未接线"（D9）洗成"本用例不适用"。
// 桩具备能力面后，装不上就是真装不上，判红可信、不得跳过。
type noopTestRunner struct {
	lim *ratelimit.Limiter
}

func (s *noopTestRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{}, nil
}
func (s *noopTestRunner) UpdateConfig(config.ExecutorConfig)  {}
func (s *noopTestRunner) SetLimiter(l *ratelimit.Limiter)     { s.lim = l }
func (s *noopTestRunner) LimiterSnapshot() ratelimit.Snapshot { return s.lim.Snapshot() }
func (s *noopTestRunner) AttachHooks(h *executor.Hooks)       {}

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
	// 默认注入空执行器（D7 的第二半，2026-10-20）：
	// 引擎构造期的 runner 是**真实**执行器，而 completeTask→postProcess 的语义子任务
	// （summary-*/critique-*）现在与引擎共用同一个执行器（judge 不再自造）。
	// 于是凡是直接调 completeTask 的用例都会真的拉起 DSH 子进程 —— 实测取证：
	// t.TempDir 残留 tasks/summary-task-done-e2e/events.jsonl，Windows 下
	// TempDir RemoveAll 报 "used by another process"，engine 包判红。
	// 需要真实执行路径的用例自己 SetRunner 覆盖即可。
	e.SetRunner(&noopTestRunner{})
	e.ReloadConfig()
	// 先等后台在途工作（postProcess 的自评/摘要/链式后继，engine.go 用 e.inflight 记账）
	// 全部落地，再关 store —— t.Cleanup 后进先出：本条注册在前，故 st.Close 先跑、
	// 这条后跑、t.TempDir 的自动清理最后跑，顺序正好。
	//
	// 为什么必须有（2026-10-22 CI 实测假红）：TestPlanModelDiffersFromExecutorRole 判红
	// 「TempDir RemoveAll cleanup: ... workbench-output: The directory is not empty」——
	// runOne 的成功路径会把 postProcess 丢给 goroutine 异步跑（engine.go:1244），
	// 它继续往 layout.TaskDir 里写 events.jsonl / 产物；测试函数返回时它还没写完，
	// RemoveAll 删到一半被并发新增击穿。Windows 上表现为「目录非空」，
	// Linux 容器里表现为「文件被占用」—— 同一根因，两种报错。
	// 有界等待（5s）与 stop() 的宽限同一形态：等待本身不许把测试挂死。
	// WAL 文件是长期打开的句柄：测试结束时必须先关闭，否则 t.TempDir 的
	// RemoveAll 在 Windows 上会因文件被占用而失败（现场 28 个用例集体假红）。
	t.Cleanup(func() { _ = st.Close() })
	// 再等后台在途工作（postProcess 的自评/摘要/链式后继，engine.go 用 e.inflight 记账）
	// 全部落地 —— t.Cleanup 后进先出：这条注册在 st.Close **之后**，所以它先跑：
	// 排空在途 → 关 store → t.TempDir 自动清理，顺序正好。
	//
	// 为什么必须有（2026-10-22 远端 CI 实测假红）：TestPlanModelDiffersFromExecutorRole 判红
	// 「TempDir RemoveAll cleanup: ... workbench-output: The directory is not empty」——
	// runOne 成功路径把 postProcess 丢给 goroutine 异步跑（engine.go:1244），
	// 它继续往 layout.TaskDir 写 events.jsonl / 产物；测试函数返回时它还没写完，
	// RemoveAll 删到一半被并发新增击穿。慢 runner 上窗口才张得开
	// （本机 20 连跑不复现，CI 上判红 —— 与 2026-10-19 那轮「CI 独有残留」同型）。
	fin := make(chan struct{})
	go func() { e.inflight.Wait(); close(fin) }()
	t.Cleanup(func() {
		select {
		case <-fin:
		case <-time.After(5 * time.Second):
			t.Log("在途后台工作 5s 未排空（继续收尾，不等死）")
		}
	})
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

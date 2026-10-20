package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
	"workbench/internal/ratelimit"
)

// semSpyRunner 记录每次调用的任务 id（语义子任务是否走到注入的执行器）。
type semSpyRunner struct {
	calls []string
	fake  bool
}

func (s *semSpyRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	s.calls = append(s.calls, task.ID)
	// 合法自评 JSON：让 postProcess 的 Critique 解析成功，快速收尾。
	return &executor.Result{FinalText: "{\"quality\":8,\"tags\":[\"守护\"]}", Elapsed: time.Second}, nil
}
func (s *semSpyRunner) UpdateConfig(config.ExecutorConfig) {}

// TestPostProcessUsesInjectedRunner 端到端守护缺陷 D7（2026-10-20 实测）：
// 改造前 judge.run() 自造执行器，engine.SetRunner 的桩管不到语义子任务，
// 于是「单测拉起真实 DSH 子进程」+「绕过模型池与限速闸门」两件事同时发生。
// 本用例锁两件事：
//  1. completeTask -> postProcess 的 summary/critique 必须命中注入的执行器；
//  2. 语义子任务不得再产生任何任务目录（真子进程会建 tasks/<语义id>/）。
func TestPostProcessUsesInjectedRunner(t *testing.T) {
	e, st, layout := newTestEngine(t)
	spy := &semSpyRunner{fake: true}
	e.SetRunner(spy)

	taskID := "task-d7-guard"
	task := &model.Task{ID: taskID, Title: "D7 守护", State: model.StateCandidate, OutputFormat: "md", MaxAttempts: 1, Domain: "content"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(taskID, to, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	running, _ := st.Get(taskID)
	res := &executor.Result{FinalText: strings.Repeat("正文内容", 100)}
	e.completeTask(task, running, res, "m", config.DefaultConfig())

	if !e.WaitInflight(10 * time.Second) {
		t.Fatal("后处理未在 10 秒内收尾（语义层可能又在等真实子进程 —— D7 回归）")
	}
	found := map[string]bool{}
	for _, c := range spy.calls {
		found[c] = true
	}
	if !found["summary-"+taskID] || !found["critique-"+taskID] {
		t.Fatalf("语义子任务未命中注入执行器: %v（D7 回归：judge 又自造 Runner 了）", spy.calls)
	}
	// 语义子任务不应建立真实任务目录（真执行器才会建；桩执行器只记录调用）。
	for _, id := range []string{"summary-" + taskID, "critique-" + taskID} {
		if _, err := os.Stat(filepath.Join(layout.TaskDir(id))); err == nil {
			t.Fatalf("语义子任务 %s 建了任务目录 = 真子进程被拉起（D7 回归）", id)
		}
	}
}

// TestRateLimiterWiredByEngine 守护缺陷 D9（2026-10-20 实测）：
// ratelimit 包与 executor.SetLimiter 早就存在、面板也有读数，但生产从未接线，
// requestsPerMinute=20 实际是装饰品。本用例锁死「引擎装配/热更新会把闸门装上去」。
func TestRateLimiterWiredByEngine(t *testing.T) {
	e, _, _ := newTestEngine(t)
	// 默认配置 requestsPerMinute=20 → rate=1/3 每秒。
	snap := e.RateLimiterSnapshot()
	if snap.RatePerSecond <= 0 {
		t.Fatalf("新建引擎的限速快照 = %+v，必须非零（D9 回归：闸门又没接上）", snap)
	}
	if got := snap.RatePerSecond; got < 0.30 || got > 0.35 {
		t.Fatalf("RatePerSecond = %v, 期望 20/分钟≈0.333", got)
	}

	// 改配置后热更新必须重装闸门（读数跟着变，不是只改面板显示）。
	if _, err := e.cstore.Update(func(c *config.Config) { c.Executor.RequestsPerMinute = 60 }); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	snap = e.RateLimiterSnapshot()
	if snap.RatePerSecond < 0.95 || snap.RatePerSecond > 1.05 {
		t.Fatalf("热更新后 RatePerSecond = %v, 期望 60/分钟≈1.0（配置改了但闸门没重装）", snap.RatePerSecond)
	}
}

// innerLimiterRunner 是支持 SetLimiter 的假执行器（模拟装饰器链最内层）。
type innerLimiterRunner struct {
	l *ratelimit.Limiter
}

func (r *innerLimiterRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{}, nil
}
func (r *innerLimiterRunner) UpdateConfig(config.ExecutorConfig)  {}
func (r *innerLimiterRunner) SetLimiter(l *ratelimit.Limiter)     { r.l = l }
func (r *innerLimiterRunner) LimiterSnapshot() ratelimit.Snapshot { return r.l.Snapshot() }

// decoratorRunner 模拟 FailoverRunner：Inner() 暴露内层。
type decoratorRunner struct{ inner executor.RunnerContract }

func (d *decoratorRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return d.inner.Run(ctx, task, modelID)
}
func (d *decoratorRunner) UpdateConfig(c config.ExecutorConfig) { d.inner.UpdateConfig(c) }
func (d *decoratorRunner) Inner() executor.RunnerContract       { return d.inner }

// TestRateLimiterFoundThroughDecoratorChain 守护下探逻辑：闸门装在 Inner 上时
// 也必须被装配并被快照读到（历史上 main 先装闸门再包装饰器，直接断言会拿零值）。
func TestRateLimiterFoundThroughDecoratorChain(t *testing.T) {
	e, _, _ := newTestEngine(t)
	inner := &innerLimiterRunner{}
	e.SetRunner(&decoratorRunner{inner: inner})
	if inner.l == nil {
		t.Fatal("SetRunner 后未沿 Inner 链重装闸门（面板显示限速、实际不限速的形态）")
	}
	if snap := e.RateLimiterSnapshot(); snap.RatePerSecond <= 0 {
		t.Fatalf("装饰器链上的闸门读数 = %+v，必须非零", snap)
	}
}

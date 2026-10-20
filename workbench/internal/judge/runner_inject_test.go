package judge

import (
	"context"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// recordingRunner 记录语义层到底把请求发给了谁（D7 守护）。
type recordingRunner struct {
	calls []string
	res   *executor.Result
}

func (r *recordingRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.calls = append(r.calls, task.ID)
	return r.res, nil
}
func (r *recordingRunner) UpdateConfig(config.ExecutorConfig) {}

// TestSemanticUsesInjectedRunner 锁死缺陷 D7（2026-10-20 实测）：
// 改造前 run() 每次自己 executor.New(cfg, layout)，于是
//   - engine.SetRunner 注入的假执行器管不到语义层：go test 会真的拉起 DSH 子进程
//     （取证：t.TempDir 残留 tasks/summary-task-done-e2e 与 tasks/critique-task-done-e2e，
//     events.jsonl 长度 0；本机同时堆积 20+ 个 DSH Desktop 进程）；
//   - 生产里装在 e.runner 上的 FailoverRunner（故障转移/车道健康度）与全局限速闸门
//     被每个任务的 2-3 次语义子请求全部绕开。
//
// 判据要能证不误报：不注入时仍走自建执行器（本机无 DSH 安装时应启动失败而非静默成功）。
func TestSemanticUsesInjectedRunner(t *testing.T) {
	layout, err := config.BuildLayout(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	stub := &recordingRunner{res: &executor.Result{FinalText: `{"quality":7,"tags":["闭环"]}`}}
	c := New(layout, config.DefaultConfig().Executor)
	c.SetRunner(stub)

	if _, err := c.Summarize(context.Background(), &model.Task{ID: "task-1", Title: "标题"}, "正文"); err != nil {
		t.Fatalf("Summarize 失败: %v", err)
	}
	if _, err := c.Critique(context.Background(), &model.Task{ID: "task-1", Title: "标题"}, "正文", 100); err != nil {
		t.Fatalf("Critique 失败: %v", err)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("语义层命中注入执行器的次数 = %d, want 2（为 0 即 D7 回归：语义层又在自造执行器）", len(stub.calls))
	}
	if stub.calls[0] != "summary-task-1" || stub.calls[1] != "critique-task-1" {
		t.Fatalf("语义子任务标识不符: %v", stub.calls)
	}
}

// TestSemanticTimeoutPositive 守护超时约束不因注入而丢失：
// 改造前 timeoutMin 只写进 cfg.DefaultTimeout 供自建 Runner 用，注入后若 task.TimeoutMin
// 仍为 0，执行器就拿不到超时约束（FailoverRunner 的 hasTimeLeft 也失去依据）。
func TestSemanticTimeoutPositive(t *testing.T) {
	layout, err := config.BuildLayout(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	var got []int
	stub := &rootSpyRunner{onRun: func(task *model.Task) { got = append(got, task.TimeoutMin) }}
	c := New(layout, config.DefaultConfig().Executor)
	c.SetRunner(stub)
	if _, err := c.Critique(context.Background(), &model.Task{ID: "t2", Title: "x"}, strings.Repeat("正文", 200), 100); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] <= 0 {
		t.Fatalf("语义子任务 TimeoutMin = %v，必须 >0", got)
	}
}

type rootSpyRunner struct{ onRun func(*model.Task) }

func (s *rootSpyRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	s.onRun(task)
	return &executor.Result{FinalText: "{\"quality\":7,\"tags\":[\"a\"]}"}, nil
}
func (s *rootSpyRunner) UpdateConfig(config.ExecutorConfig) {}

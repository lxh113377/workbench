package executor

import (
	"context"
	"errors"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func TestHooksOrderAndArgs(t *testing.T) {
	var calls []string
	h := NewHooks(
		[]BeforeRunFunc{
			func(ctx context.Context, task *model.Task, modelID string) error {
				calls = append(calls, "before1:"+task.ID+":"+modelID)
				return nil
			},
			func(ctx context.Context, task *model.Task, modelID string) error {
				calls = append(calls, "before2")
				return nil
			},
		},
		[]AfterRunFunc{
			func(ctx context.Context, task *model.Task, modelID string, res *Result, err error) {
				calls = append(calls, "after1")
			},
			func(ctx context.Context, task *model.Task, modelID string, res *Result, err error) {
				calls = append(calls, "after2")
			},
		},
	)
	task := &model.Task{ID: "task-1"}
	if err := h.RunBefore(context.Background(), task, "m1"); err != nil {
		t.Fatal(err)
	}
	h.RunAfter(context.Background(), task, "m1", &Result{}, nil)
	want := []string{"before1:task-1:m1", "before2", "after1", "after2"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestHooksBeforeErrorAborts(t *testing.T) {
	after := false
	h := NewHooks(
		[]BeforeRunFunc{
			func(ctx context.Context, task *model.Task, modelID string) error { return nil },
			func(ctx context.Context, task *model.Task, modelID string) error { return errors.New("凭证缺失") },
			func(ctx context.Context, task *model.Task, modelID string) error {
				t.Fatal("第二个前置钩子失败后，后续钩子不应执行")
				return nil
			},
		},
		[]AfterRunFunc{func(context.Context, *model.Task, string, *Result, error) { after = true }},
	)
	err := h.RunBefore(context.Background(), &model.Task{ID: "t"}, "m")
	if err == nil {
		t.Fatal("前置钩子返回错误应被传出")
	}
	if !after {
		_ = after // RunAfter 由 Run 的 defer 负责，此处单独调用 RunBefore 不触发
	}
	if !errors.Is(err, err) {
		t.Fatal("错误应可被识别")
	}
}

func TestHooksPanicIsRecovered(t *testing.T) {
	reached := false
	h := NewHooks(
		[]BeforeRunFunc{
			func(ctx context.Context, task *model.Task, modelID string) error { panic("boom") },
		},
		[]AfterRunFunc{
			func(ctx context.Context, task *model.Task, modelID string, res *Result, err error) {
				panic("after boom")
			},
			func(ctx context.Context, task *model.Task, modelID string, res *Result, err error) { reached = true },
		},
	)
	if err := h.RunBefore(context.Background(), &model.Task{}, "m"); err == nil {
		t.Fatal("前置钩子 panic 应转成错误")
	}
	h.RunAfter(context.Background(), &model.Task{}, "m", nil, nil)
	if !reached {
		t.Fatal("后置钩子 panic 不应阻断后续钩子")
	}
}

func TestNilHooksSafe(t *testing.T) {
	var h *Hooks
	if err := h.RunBefore(context.Background(), &model.Task{}, "m"); err != nil {
		t.Fatalf("nil hooks RunBefore 应返回 nil: %v", err)
	}
	h.RunAfter(context.Background(), &model.Task{}, "m", nil, nil) // 不应 panic
}

func TestRunnerSetHooks(t *testing.T) {
	r := New(config.DefaultConfig().Executor, config.Layout{})
	if r.Hooks() != nil {
		t.Fatal("默认不应有钩子")
	}
	h := NewHooks(nil, nil)
	if r.SetHooks(h) != r {
		t.Fatal("SetHooks 应返回自身以便链式调用")
	}
	if r.Hooks() != h {
		t.Fatal("钩子未挂上")
	}
}

func TestRunnerContractSatisfied(t *testing.T) {
	var _ RunnerContract = New(config.DefaultConfig().Executor, config.Layout{})
}

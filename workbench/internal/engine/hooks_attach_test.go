package engine

import (
	"context"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/logging"
	"workbench/internal/model"
)

// hookableRunner 是实现 RunnerContract + AttachHooks 的最小执行器。
type hookableRunner struct {
	hooks *executor.Hooks
}

func (h *hookableRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{}, nil
}
func (h *hookableRunner) UpdateConfig(config.ExecutorConfig) {}
func (h *hookableRunner) AttachHooks(x *executor.Hooks)      { h.hooks = x }

// chainOuter 只实现 Inner()（模拟模型池装饰器）：钩子必须沿链下探到内层。
type chainOuter struct{ inner executor.RunnerContract }

func (c *chainOuter) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return c.inner.Run(ctx, task, modelID)
}
func (c *chainOuter) UpdateConfig(cfg config.ExecutorConfig) { c.inner.UpdateConfig(cfg) }
func (c *chainOuter) Inner() executor.RunnerContract         { return c.inner }

// TestHooksAttachThroughDecoratorChain 守护 L5 收敛（2026-10-20）：
// 改造前 engine.SetLogger 断言 e.runner.(*executor.Runner)，执行器被装饰后
// 断言不匹配 → 「尝试开始/尝试结束」日志静默失效（功能正常、只是没日志，
// 运行期发现不了）。现在必须沿 Inner() 链找到支持 AttachHooks 的那一层。
func TestHooksAttachThroughDecoratorChain(t *testing.T) {
	e, _, _ := newTestEngine(t)
	inner := &hookableRunner{}
	e.SetRunner(&chainOuter{inner: inner})
	e.SetLogger(logging.Nop())
	if inner.hooks == nil {
		t.Fatal("钩子未沿 Inner() 链装到内层（L5 回归：装饰器路径没有尝试日志）")
	}
}

// TestHooksAttachOnPlainRunner 正向对照组：未装饰的执行器同样要装上。
func TestHooksAttachOnPlainRunner(t *testing.T) {
	e, _, _ := newTestEngine(t)
	plain := &hookableRunner{}
	e.SetRunner(plain)
	e.SetLogger(logging.Nop())
	if plain.hooks == nil {
		t.Fatal("钩子未装到普通执行器")
	}
}

// TestHooksAttachSurvivesNilChain 负面用例：链上有 nil 不得 panic，
// 也不得因此把任务执行判失败（钩子是观测面）。
func TestHooksAttachSurvivesNilChain(t *testing.T) {
	attachHooksToRunner(nil, executor.NewHooks(nil, nil))
	attachHooksToRunner(&chainOuter{inner: nil}, executor.NewHooks(nil, nil))
}

// TestHookBeforeRunAbortPropagates 守护钩子语义本身：BeforeRun 返回错误必须
// 中止本次尝试并归为 boot_failed（对标 river HookWorkBegin 的中止语义，L5 完成判据）。
func TestHookBeforeRunAbortPropagates(t *testing.T) {
	r := executor.New(config.DefaultConfig().Executor, config.Layout{})
	h := executor.NewHooks([]executor.BeforeRunFunc{
		func(ctx context.Context, task *model.Task, modelID string) error {
			return errHookAbort
		},
	}, nil)
	r.AttachHooks(h)
	res, err := r.Run(context.Background(), &model.Task{ID: "task-hook", Title: "钩子中止"}, "")
	if err == nil {
		t.Fatal("BeforeRun 返回错误时 Run 必须报错（否则钩子无法拦截执行）")
	}
	if res == nil || res.ErrorKind != string(executor.KindBoot) {
		t.Fatalf("中止应归为 boot_failed，实际 %+v", res)
	}
}

var errHookAbort = hookAbortError{}

type hookAbortError struct{}

func (hookAbortError) Error() string { return "测试钩子主动中止" }

// 执行钩子与运行器接口（P1-2）。
//
// 对标 riverqueue/river 的插件钩子（HookWorkBegin / HookWorkEnd / MiddlewareWorker）
// 与 asynq 的 middleware：把"执行前准备"和"执行后观察"从 Run 实现里抽出来，
// 让引擎可以按需插入横切逻辑（日志、指标、审计、未来多执行通道），不必改 executor 源码。
package executor

import (
	"context"
	"fmt"

	"workbench/internal/config"
	"workbench/internal/model"
)

// Hooks 是一次执行前后的钩子集合。
//
// 约定：
//   - 任何钩子 panic 都会被 recover，绝不影响任务本身；
//   - BeforeRun 返回错误 ⇒ 本次尝试不执行，错误按 boot_failed 类别回传；
//   - AfterRun 在 Run 返回后必然被调用（含失败路径），用于记账与观测。
type Hooks struct {
	before []BeforeRunFunc
	after  []AfterRunFunc
}

// BeforeRunFunc 在执行前调用；返回错误即中止本次尝试。
type BeforeRunFunc func(ctx context.Context, task *model.Task, modelID string) error

// AfterRunFunc 在执行后调用；res 可能为 nil（启动失败等），err 为执行错误。
type AfterRunFunc func(ctx context.Context, task *model.Task, modelID string, res *Result, err error)

// NewHooks 构造钩子集合（按注册顺序执行）。
func NewHooks(before []BeforeRunFunc, after []AfterRunFunc) *Hooks {
	return &Hooks{before: before, after: after}
}

// AddBefore 追加一个前置钩子。
func (h *Hooks) AddBefore(fn BeforeRunFunc) { h.before = append(h.before, fn) }

// AddAfter 追加一个后置钩子。
func (h *Hooks) AddAfter(fn AfterRunFunc) { h.after = append(h.after, fn) }

// RunBefore 依次执行前置钩子；首个错误即返回。
func (h *Hooks) RunBefore(ctx context.Context, task *model.Task, modelID string) error {
	if h == nil {
		return nil
	}
	for i, fn := range h.before {
		if fn == nil {
			continue
		}
		if herr := callBefore(fn, ctx, task, modelID); herr != nil {
			return fmt.Errorf("前置钩子 #%d 失败: %w", i, herr)
		}
	}
	return nil
}

// RunAfter 依次执行后置钩子；单个钩子 panic 不影响其余钩子。
func (h *Hooks) RunAfter(ctx context.Context, task *model.Task, modelID string, res *Result, err error) {
	if h == nil {
		return
	}
	for _, fn := range h.after {
		if fn == nil {
			continue
		}
		callAfter(fn, ctx, task, modelID, res, err)
	}
}

func callBefore(fn BeforeRunFunc, ctx context.Context, task *model.Task, modelID string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("钩子 panic: %v", r)
		}
	}()
	return fn(ctx, task, modelID)
}

func callAfter(fn AfterRunFunc, ctx context.Context, task *model.Task, modelID string, res *Result, runErr error) {
	defer func() {
		_ = recover() // 后置钩子永不影响主流程
	}()
	fn(ctx, task, modelID, res, runErr)
}

// RunnerContract 是引擎依赖的执行器契约（P1-2 抽象）。
//
// 抽出接口的目的：引擎不再绑死具体结构体，未来可替换为本地进程 / 远程队列 / 测试桩；
// 同时让 engine 的单测可以注入假执行器，不必真的拉起 DSH 子进程。
//
// 注：用 Contract 后缀而非 Runner，避免与既有具体类型 Runner 重名（保持向后兼容）。
type RunnerContract interface {
	Run(ctx context.Context, task *model.Task, modelID string) (*Result, error)
	UpdateConfig(cfg config.ExecutorConfig)
}

// SetHooks 给具体 Runner 挂上钩子集合，并返回自身以便链式装配。
func (r *Runner) SetHooks(h *Hooks) *Runner {
	r.hooks = h
	return r
}

// Hooks 返回当前钩子集合（可能为 nil）。
func (r *Runner) Hooks() *Hooks { return r.hooks }

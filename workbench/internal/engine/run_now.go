package engine

// 本文件提供"同步跑一个任务"的受控入口，供离线自检与诊断使用。
//
// 为什么需要它：
//   双角色闭环的自检必须同步看到"方案调用 -> 执行调用 -> 验收回灌"的完整结果，
//   而正常调度是异步的（queue -> loop -> goroutine）。测试可以用同包私有函数，
//   但 CLI 自检（--plan-selftest）在另一个包里，只能走导出面。
//
// 为什么不直接导出 runOne：
//   runOne 的语义是"调度器已经把这个任务取出来了"，它假设任务已处于 running 态、
//   并发槽已占用、取消函数已注册。直接把这种内部状态暴露出去，调用方很容易用错。
//   这里显式收口成一个语义明确的入口，并在文档里写清前置条件。

import (
	"context"
	"errors"

	"workbench/internal/model"
)

// ErrTaskNotRunning 表示调用 RunOneNow 时任务不处于 running 态。
// 返回它而不是静默继续：状态不对时跑一遍会污染审计流水（running 迁移重复）。
var ErrTaskNotRunning = errors.New("任务不处于 running 态，不能直接执行")

// RunOneNow 同步执行一个已处于 running 态的任务（诊断/自检用）。
//
// 前置条件（调用方负责）：任务已 Create 并迁移到 running，且没有并发执行者。
// 生产调度路径**不使用**本方法 —— 它走 loop/tick 的异步链路（见 engine.loop）。
func (e *Engine) RunOneNow(ctx context.Context, task *model.Task) error {
	if task == nil {
		return errors.New("任务为空")
	}
	if task.State != model.StateRunning {
		return ErrTaskNotRunning
	}
	e.runOne(ctx, task)
	return nil
}

package executor

import (
	"errors"
	"fmt"

	"workbench/internal/model"
)

// FinalizeResult 用的哨兵错误（便于调用方 errors.Is 判定，且消息对用户可读）。
var (
	errResultNil = errors.New("执行器返回空结果")
	errStalled   = errors.New("任务长时间无任何进展，已判定卡死并回收进程")
	errTimedOut  = errors.New("任务超时")
	errCancelled = errors.New("任务已被取消")
	errNoOutput  = errors.New("任务未产生最终答案")
)

// newExitError 构造带退出码与 stderr 摘要的错误。
func newExitError(code int, stderr string) error {
	return fmt.Errorf("DSH 退出码 %d: %s", code, model.Truncate(stderrSummary(stderr, 400), 400))
}

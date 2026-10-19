package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/retry"
)

// 本文件收纳不依赖 Engine 接收者的纯函数与策略装配逻辑。
//
// 单独成文件的目的：这些是"失败之后怎么办"的核心判定，必须能被单元测试
// 直接覆盖（engine.go 里大量逻辑与 DSH 子进程强耦合，不适合硬测）。
// 对标 riverqueue/river 把 ClientRetryPolicy 抽成独立接口的做法。

// buildRetryPolicy 把配置翻译成重试策略对象。
//
// 兼容历史配置：只写 maxRetries / retryBackoffMs 的旧 config.json 仍然生效
// （列表优先），新增的 retryPolicy 段只在列表缺失时作为指数退避参数使用。
func buildRetryPolicy(cfg config.ExecutorConfig) *retry.Policy {
	p := retry.Policy{
		MaxRetries: cfg.MaxRetries,
		BackoffMs:  append([]int(nil), cfg.RetryBackoffMs...),
		BaseDelay:  time.Duration(cfg.RetryPolicy.BaseDelayMs) * time.Millisecond,
		MaxDelay:   time.Duration(cfg.RetryPolicy.MaxDelayMs) * time.Millisecond,
		Jitter:     cfg.RetryPolicy.Jitter,
	}
	for _, k := range cfg.RetryPolicy.RetryableKinds {
		p.RetryableKinds = append(p.RetryableKinds, retry.ParseKind(k))
	}
	return retry.New(p)
}

// errText 安全取错误文本。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ctxCancelText 把 ctx 取消错误翻译成中文（其余错误返回空串）。
func ctxCancelText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "用户取消（执行上下文已取消）"
	}
	return ""
}

// filepathExt / baseName 是 filepath.Ext / filepath.Base 的薄封装，
// 让纯函数测试不必关心路径分隔符在 Windows 上的差异。
func filepathExt(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		p = p[i+1:]
	}
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i:]
	}
	return ""
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// truncateForAudit 截断审计文本并回退到合法 UTF-8。
func truncateForAudit(s string, n int) string { return model.Truncate(s, n) }

var _ = fmt.Sprintf

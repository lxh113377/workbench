package modelpool

import (
	"context"
	"errors"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// rateLimitRunner 模拟"所有模型都返回 429"的执行器。
type rateLimitRunner struct {
	calls []string
}

func (r *rateLimitRunner) UpdateConfig(cfg config.ExecutorConfig) {}

func (r *rateLimitRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.calls = append(r.calls, modelID)
	return &executor.Result{ErrorKind: string(executor.KindRateLimited)},
		errors.New("dsh: RATE_LIMIT: too many requests; retry later")
}

// TestFailoverStopsOnRateLimit 是本轮现场缺陷的回归测试。
//
// 现场实测（2026-10-19 08:08，隔离实例）：限流时 FailoverRunner 连试 8 个模型
// （每个约 3 秒），24 秒内打空整池并全部记失败。限流是通道级/账号级的，
// 换模型毫无意义，只会延长限流窗口。
//
// 期望：遇到 rate_limited 立刻停止换模型，只调用 1 次，并把 429 原样上抛，
// 交给 engine 做整池冷却。
func TestFailoverStopsOnRateLimit(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{
		spec("a", 10, 100, StateUnknown),
		spec("b", 20, 50, StateUnknown),
		spec("c", 30, 10, StateUnknown),
	})
	sch := NewScheduler(StrategyPriority)
	inner := &rateLimitRunner{}
	fr := NewFailoverRunner(inner, m, sch)

	res, err := fr.Run(context.Background(), &model.Task{ID: "task-429"}, "a")
	if len(inner.calls) != 1 {
		t.Fatalf("限流时必须立即停止换模型，实际调用 %v（%d 次）", inner.calls, len(inner.calls))
	}
	if err == nil {
		t.Fatal("限流应把错误上抛给引擎，而不是当成成功")
	}
	if res == nil || res.ErrorKind != string(executor.KindRateLimited) {
		t.Fatalf("应原样返回 429 结果，实际 %+v", res)
	}
}

// TestFailoverStillSwitchesOnOtherFailures 保证修复没有误伤正常的故障转移。
func TestFailoverStillSwitchesOnOtherFailures(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{
		spec("a", 10, 100, StateUnknown),
		spec("b", 20, 50, StateUnknown),
	})
	sch := NewScheduler(StrategyPriority)
	inner := &fakeRunner{fail: map[string]bool{"a": true}, err: errors.New("upstream 502")}
	fr := NewFailoverRunner(inner, m, sch)
	res, err := fr.Run(context.Background(), &model.Task{ID: "t"}, "a")
	if err != nil || res == nil || res.FinalText == "" {
		t.Fatalf("非限流失败仍应故障转移: err=%v res=%+v", err, res)
	}
	if len(inner.calls) != 2 {
		t.Fatalf("应切换一次，实际 %v", inner.calls)
	}
}

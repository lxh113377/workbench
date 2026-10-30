package modelpool

import (
	"context"
	"errors"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// bootTextRunner 复现线上形态（round 144 · T98 取证）：上游把类别预置成通用类别 boot_failed，
// 而**错误正文**写明"这条出口不服务该模型"。
//
// 现场依据（agent 根全量扫 attempts.jsonl）：kind=boot_failed 共 191 行，其中 190 行的 error 正文是
//
//	dsh: SERVER: our free model does not serve "combo:auto-free" on this egress
//
// ⇒ 99.5% 的「启动失败」桶装的是"出口不服务"，行为侧早按 not_served 处理（MarkFailure），但台账仍显示 boot_failed。
type bootTextRunner struct {
	detail string
	calls  []string
}

func (r *bootTextRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *bootTextRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.calls = append(r.calls, modelID)
	return &executor.Result{ErrorKind: string(executor.KindBoot)}, errors.New(r.detail)
}

// TestLedgerRecordsAttributedKind 钉住：补偿把类别改准之后，**台账也必须改**。
//
// 为什么这不是"只改显示"：台账与面板是人与报告读它做判断的唯一入口 —— 本会话我自己就据「启动失败」桶
// 得出过错误结论。行为侧（车道冷却）早已正确，缺的是同一份事实在**记录面**的一致性。
func TestLedgerRecordsAttributedKind(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	inner := &bootTextRunner{detail: `dsh: SERVER: our free model does not serve "combo:auto-free" on this egress`}
	fr := NewFailoverRunner(inner, m, sch)

	res, err := fr.Run(context.Background(), &model.Task{ID: "t-doesnotserve"}, "a")
	if err == nil {
		t.Fatal("应失败")
	}
	if res == nil {
		t.Fatal("失败时也应返回 res（否则上层拿不到类别）")
	}
	if got := res.ErrorKind; got != notServedKind {
		t.Fatalf("台账类别应为 %q（出口不服务该模型），实得 %q ⇒ 面板会继续显示「启动失败」而真实成因是出口不服务",
			notServedKind, got)
	}
}

// 保守面：正文**没有**"不服务该模型"这类确定性描述时，不许把 boot_failed 改写成别的类别
// （否则一次泛化的启动失败会被归到某条车道上，误杀健康车道 —— 判据刻意取窄的原意）。
func TestLedgerKeepsBootForGenericFailure(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	inner := &bootTextRunner{detail: "dsh: 启动失败: spawn ENOENT"}
	fr := NewFailoverRunner(inner, m, sch)

	res, err := fr.Run(context.Background(), &model.Task{ID: "t-genericboot"}, "a")
	if err == nil {
		t.Fatal("应失败")
	}
	if res == nil {
		t.Fatal("失败时也应返回 res")
	}
	if got := res.ErrorKind; got != string(executor.KindBoot) {
		t.Fatalf("泛化启动失败必须保持 %q，实得 %q ⇒ 判据放宽了（会误杀健康车道）", string(executor.KindBoot), got)
	}
}

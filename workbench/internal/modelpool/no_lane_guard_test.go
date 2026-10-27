package modelpool

import (
	"context"
	"errors"
	"testing"

	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestFailoverNoLaneDoesNotRunCooledModel 钉住 T19 判据的**位置**（round 37 实测教训）。
//
// 第一版判据写成 `len(order) == 0`，而 `order` 开头就被引擎传进来的 modelID 占位 ⇒ 判据永不成立，
// 引擎带着刚冷却的车道硬跑，任务继续被烧成 boot_failed（与修复前的现场现象完全一致）。
// 本用例：唯一车道已冷却 + 引擎仍给出 modelID ⇒ 必须返回 ErrNoLane，且**不得调用内层 runner**。
func TestFailoverNoLaneDoesNotRunCooledModel(t *testing.T) {
	store := NewStore(t.TempDir())
	mgr := NewManager(store, []Spec{laneSpec("m1", 1, 100, "free")})
	sch := NewScheduler(StrategyPriority)
	// 结构性故障一击即冷却：让唯一车道进入冷却（前置条件，先自证）。
	mgr.MarkFailure("m1", "model_unavailable", "端点不可达（模拟）")
	if chain := sch.PickChain(mgr.Specs(), model.Now()); len(chain) != 0 {
		t.Fatalf("前置条件不成立：冷却中的车道仍进了候选链 %v", chain)
	}
	inner := &recordingRunner{}
	fr := NewFailoverRunner(inner, mgr, sch)
	res, err := fr.Run(context.Background(), &model.Task{ID: "t1", Prompt: "x"}, "m1")
	if !errors.Is(err, executor.ErrNoLane) {
		t.Fatalf("应返回 ErrNoLane（整池无可用车道），实得 res=%+v err=%v", res, err)
	}
	if got := inner.seen(); len(got) != 0 {
		t.Fatalf("不得调用内层 runner，实际调用 %v", got)
	}
}

package engine

import (
	"errors"
	"fmt"
	"testing"

	"workbench/internal/executor"
	"workbench/internal/retry"
)

// TestIsNoLane 钉住「整池无可用车道」的识别口径（T19）。
//
// 由来（round 35 实测）：全池只剩 3 条 not_served 车道时，引擎仍开跑并把两个任务烧成 boot_failed 任务失败；
// 正确处置是「设池闸门 + 任务退回队列」（与限流同路），所以引擎必须先能识别这个形态。
func TestIsNoLane(t *testing.T) {
	if !isNoLane(nil, executor.ErrNoLane) {
		t.Fatal("哨兵错误应被识别为整池无可用车道")
	}
	wrapped := fmt.Errorf("包一层: %w", executor.ErrNoLane)
	if !isNoLane(nil, wrapped) {
		t.Fatal("包装后的哨兵错误（errors.Is）也应被识别")
	}
	if !isNoLane(&executor.Result{ErrorKind: string(executor.KindNoLane)}, nil) {
		t.Fatal("结果类别为 no_lane 时应被识别")
	}
	if isNoLane(nil, nil) {
		t.Fatal("既无错误也无结果时不得判为整池无可用车道")
	}
	other := &executor.Result{ErrorKind: string(executor.KindBoot)}
	if isNoLane(other, errors.New("dsh: SERVER: does not serve")) {
		t.Fatal("普通 boot_failed 不得被判为整池无可用车道（那是单车道问题）")
	}
	if string(executor.KindNoLane) != "no_lane" {
		t.Fatalf("executor.KindNoLane = %q，对外契约应为 no_lane", executor.KindNoLane)
	}
	if retry.ParseKind("no_lane") != retry.NoLane {
		t.Fatalf("retry.ParseKind(no_lane) = %q，应为 %q", retry.ParseKind("no_lane"), retry.NoLane)
	}
}

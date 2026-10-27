package engine

import (
	"fmt"
	"testing"

	"workbench/internal/model"
)

// TestRejectionPressureHasTimeWindow 钉住熔断判据的时间窗（round 47 · T35，live 铁证）。
//
// 现场：09:02 最后一次成功生成后，每次 tick 都 `自主规划熔断 streak=5` ⇒ 不再产草案 ⇒
// 永远无法靠「被受理」冲掉尾巴（死锁：熔断依据自己把自己锁住）。
func TestRejectionPressureHasTimeWindow(t *testing.T) {
	e, st, _ := newTestEngine(t)
	goal := "goal-t35"
	now := model.Now()
	makeRejected := func(id string, at int64) {
		t.Helper()
		tk := &model.Task{ID: id, Title: id, Source: "autonomy", GoalID: goal, State: model.StateRejected, CreatedAt: at}
		if err := st.Create(tk); err != nil {
			t.Fatalf("创建拒绝任务失败: %v", err)
		}
	}

	// ① 5 条**陈旧**拒绝（2 小时前）⇒ 不应构成压力。
	for i := 0; i < 5; i++ {
		makeRejected(fmt.Sprintf("old-%d", i), now-2*3600*1000)
	}
	if streak, tripped := e.rejectionPressure(goal, 10); tripped || streak != 0 {
		t.Fatalf("陈旧拒绝不得构成熔断压力，实得 streak=%d tripped=%v", streak, tripped)
	}

	// ② 3 条**新近**拒绝 ⇒ 未达阈值（<5）。
	for i := 0; i < 3; i++ {
		makeRejected(fmt.Sprintf("new-%d", i), now-60_000)
	}
	if streak, tripped := e.rejectionPressure(goal, 10); tripped || streak != 3 {
		t.Fatalf("3 条新近拒绝未达阈值，实得 streak=%d tripped=%v", streak, tripped)
	}

	// ③ 再补 2 条 ⇒ 达阈值 5 ⇒ 熔断保护仍在（活风暴照样叫停）。
	for i := 0; i < 2; i++ {
		makeRejected(fmt.Sprintf("new2-%d", i), now-30_000)
	}
	if streak, tripped := e.rejectionPressure(goal, 10); !tripped || streak != 5 {
		t.Fatalf("5 条新近拒绝应触发熔断，实得 streak=%d tripped=%v", streak, tripped)
	}

	// ④ 别的目标不受影响（判据只对本目标计数）。
	if streak, tripped := e.rejectionPressure("goal-other", 10); tripped || streak != 0 {
		t.Fatalf("别的目标不应继承压力，实得 streak=%d tripped=%v", streak, tripped)
	}
}

package engine

// output_zero_test.go 守护「面板显示运行中但今日产出为 0」的根因链（2026-10-21 现场）。
//
// 现场事实（一笔一笔可从 dist 数据核对，不是推断）：
//   - 当日 00:00:02 - 01:39:59 产出 2000 条自主草案，**标题与 Prompt 逐字节相同**；
//   - 2000 条全部被评估器拒绝（综合分 48.0 / 54.0 < 阈值 55），无一入队；
//   - 01:39:59 之后当天再无任何新任务 —— 因为 rejected 也计入 dailyCap，
//     配额被"从未执行过的草案"吃满，两条供货链同时停机；
//   - 面板 phase=running、todayDone=0，日志里除"模型路由表已更新"外一片安静。
//
// 本文件守的是引擎侧两条口径：
//   1) 被拒草案**不得**计入每日上限（否则一次空转能把一整天打掉）；
//   2) 同一目标连续被拒到达阈值后必须熔断并留痕（不得静默空转）。

import (
	"context"
	"testing"

	"workbench/internal/model"
)

// seedRejected 造一条"今日产生的被拒自主草案"。
func seedRejected(t *testing.T, e *Engine, id, goalID string) {
	t.Helper()
	tk := &model.Task{
		ID: id, Title: "同一条草案", Prompt: "p", GoalID: goalID,
		Source: "autonomy", State: model.StateRejected,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}
	if err := e.store.Create(tk); err != nil {
		t.Fatal(err)
	}
}

// TestRejectedDraftsDoNotConsumeDailyCap 断言：当日 2000 条被拒草案不得把配额吃满。
//
// 这是现场"运行中但产出 0"的直接成因：配额本应只统计真正的工作量。
func TestRejectedDraftsDoNotConsumeDailyCap(t *testing.T) {
	e, _, _ := newTestEngine(t)
	goal := "goal-cap"
	for i := 0; i < 30; i++ {
		seedRejected(t, e, "task-rej-"+string(rune('a'+i)), goal)
	}
	if n := e.tasksToday(); n != 0 {
		t.Fatalf("被拒草案不得计入当日工作量：期望 0，实际 %d", n)
	}
	// 对照：入队/完成的任务必须计入（否则闸门口径过松会让系统无限跑）。
	if err := e.store.Create(&model.Task{
		ID: "task-real", Title: "真任务", Prompt: "p", State: model.StateDone,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if n := e.tasksToday(); n != 1 {
		t.Fatalf("已完成任务必须计入当日工作量：期望 1，实际 %d", n)
	}
}

// TestTasksTodayIgnoresPreviousDays 断言口径仍按"今日"切分，不因上面那条改动放宽。
func TestTasksTodayIgnoresPreviousDays(t *testing.T) {
	e, _, _ := newTestEngine(t)
	if err := e.store.Create(&model.Task{
		ID: "task-old", Title: "昨天的任务", Prompt: "p", State: model.StateDone,
		CreatedAt: startOfToday() - 60_000, UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if n := e.tasksToday(); n != 0 {
		t.Fatalf("昨天的任务不得计入今日工作量：期望 0，实际 %d", n)
	}
}

// TestRejectStreakTripsBreaker 断言：同一目标连续被拒达阈值即熔断。
func TestRejectStreakTripsBreaker(t *testing.T) {
	e, _, _ := newTestEngine(t)
	goal := "goal-streak"
	for i := 0; i < rejectStreakLimit; i++ {
		seedRejected(t, e, "task-streak-"+string(rune('a'+i)), goal)
	}
	streak, tripped := e.rejectionPressure(goal, 0)
	if !tripped {
		t.Fatalf("连续 %d 条被拒必须熔断，实际 streak=%d", rejectStreakLimit, streak)
	}
	if streak != rejectStreakLimit {
		t.Fatalf("熔断时的 streak 读数应为 %d，实际 %d", rejectStreakLimit, streak)
	}
}

// TestRejectStreakResetsOnAnyAcceptedTask 断言：只要出现一条非拒绝任务（说明链路恢复），
// 熔断必须立即解除 —— 否则修好之后还得人工重启才能恢复产出。
func TestRejectStreakResetsOnAnyAcceptedTask(t *testing.T) {
	e, _, _ := newTestEngine(t)
	goal := "goal-reset"
	for i := 0; i < rejectStreakLimit+2; i++ {
		seedRejected(t, e, "task-reset-rej-"+string(rune('a'+i)), goal)
	}
	if _, tripped := e.rejectionPressure(goal, 0); !tripped {
		t.Fatal("前置条件不成立：应先熔断")
	}
	// 出现一条被受理的任务（入队即算恢复）。
	if err := e.store.Create(&model.Task{
		ID: "task-reset-ok", Title: "恢复后的任务", Prompt: "p", GoalID: goal,
		Source: "autonomy", State: model.StateQueued,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if streak, tripped := e.rejectionPressure(goal, 0); tripped {
		t.Fatalf("出现被受理任务后熔断必须解除，实际仍 tripped（streak=%d）", streak)
	}
}

// TestRejectBreakerStopsPlanning 断言：熔断生效时 supplyAutonomous 不再产出新草案
// （即不再往台账刷拒绝）。
func TestRejectBreakerStopsPlanning(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	if !cfg.Autonomy.Enabled {
		t.Skip("自主未启用")
	}
	// 先让引擎建立活跃目标。
	if _, err := e.autonomy.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	g, ok := e.autonomy.Active()
	if !ok || g == nil {
		t.Fatal("应存在活跃目标")
	}
	for i := 0; i < rejectStreakLimit; i++ {
		seedRejected(t, e, "task-breaker-"+string(rune('a'+i)), g.ID)
	}
	before := len(st.List())
	if e.supplyAutonomous(context.Background(), cfg) {
		t.Fatal("熔断期间 supplyAutonomous 应返回 false（把机会让给模板链路）")
	}
	if after := len(st.List()); after != before {
		t.Fatalf("熔断期间不得新增任何任务：%d -> %d", before, after)
	}
}

// TestRejectBreakerOnlyCountsSameGoal 断言：熔断按目标隔离，别的目标的拒绝不牵连本目标。
func TestRejectBreakerOnlyCountsSameGoal(t *testing.T) {
	e, _, _ := newTestEngine(t)
	for i := 0; i < rejectStreakLimit+2; i++ {
		seedRejected(t, e, "task-other-"+string(rune('a'+i)), "goal-other")
	}
	if streak, tripped := e.rejectionPressure("goal-mine", 0); tripped {
		t.Fatalf("别的目标的连续拒绝不得牵连本目标（streak=%d）", streak)
	}
}

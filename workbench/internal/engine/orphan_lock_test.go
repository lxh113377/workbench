package engine

// orphan_lock_test.go 守护「孤儿 done 任务不得永久锁死自主链路」（2026-10-19 现场）。
//
// 现场（真实运行数据，时间线铁证）：
//   21:11:43  任务完成 [task-...173184-c87c]   <- 任务变 done，验收尚未写入 stages
//   21:12:11  引擎已启动                     <- **运维重启（换二进制）打断异步验收**
//   => 任务成了「孤儿」：state=done 但 goal.stages 里没有它的 taskId
//   => pendingAcceptance 命中「有 done 但未记录」的分支 => 恒返回 true
//   => supplyAutonomous 永久不规划，进度卡在 90%（36.5 分钟零活动）
//
// 机理：pendingAcceptance 的语义是「已完成但尚未验收的任务同样视为在途」，
//   用于防止「done 到写入 stages 的窗口期内重复规划」。
//   但它**没有时间上界**：一旦验收被中断，这个「在途」就成了永远。
//
// 为什么必须修：
//   - 7x24 下重启是常态（换二进制/自愈/宕机），不可能要求「永不在窗口内重启」；
//   - 一旦命中，自主链路永久死锁 —— 比「偶发重复规划」严重得多；
//   - 与被拒任务不同：这个没有任何日志说明卡住了（只有 plan 归零）。
//
// 判据：孤儿的「在途」判定必须有时间上界；超过窗口即视为已失去验收机会，
//   不再阻塞规划（宁可回到队列重做，也不能永久停摆）。

import (
	"context"
	"testing"

	"workbench/internal/model"
)

// TestStaleOrphanDoneTaskDoesNotBlockForever 断言：陈旧孤儿不再阻塞规划。
func TestStaleOrphanDoneTaskDoesNotBlockForever(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	// 先让自主链路 bootstrap 出一个活动目标（与生产一致）。
	e.supplyAutonomous(context.Background(), e.Config())
	goal, ok := e.autonomy.Active()
	if !ok || goal == nil {
		t.Skip("没有活动目标")
	}

	// 造一个「已完成但未记入 stages」的孤儿任务，且**完成时间远早于现在**。
	stale := &model.Task{
		ID: "task-orphan-stale", Title: "孤儿任务", Prompt: "p",
		State: model.StateDone, Source: "autonomy", GoalID: goal.ID,
		CreatedAt:  model.Now() - 30*60*1000, // 30 分钟前
		FinishedAt: model.Now() - 30*60*1000,
	}
	if err := st.Create(stale); err != nil {
		t.Fatal(err)
	}
	// 确认它确实是孤儿（stages 里没有）
	for _, s := range goal.Stages {
		if s.TaskID == stale.ID {
			t.Skip("该 ID 已在 stages 里，构造失败")
		}
	}

	all := st.List()
	if pendingAcceptance(all, goal) {
		t.Fatal("陈旧孤儿（30 分钟前 done 且未记录）不应再被视为「在途」——否则永久锁死自主链路")
	}
}

// TestFreshOrphanDoneTaskStillGuards 断言：窗口期内的孤儿**仍然**阻塞（防重复规划）。
//
// 这条是回归保护：修「陈旧」时不能把「新鲜」的保护也去掉。
func TestFreshOrphanDoneTaskStillGuards(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	e.supplyAutonomous(context.Background(), e.Config())
	goal, ok := e.autonomy.Active()
	if !ok || goal == nil {
		t.Skip("没有活动目标")
	}
	fresh := &model.Task{
		ID: "task-orphan-fresh", Title: "刚完成的任务", Prompt: "p",
		State: model.StateDone, Source: "autonomy", GoalID: goal.ID,
		CreatedAt:  model.Now(),
		FinishedAt: model.Now(),
	}
	if err := st.Create(fresh); err != nil {
		t.Fatal(err)
	}
	if !pendingAcceptance(st.List(), goal) {
		t.Fatal("刚完成且未记录的任务仍应视为在途（防止窗口期内重复规划）")
	}
}

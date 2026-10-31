package engine

// dailycap_spin_test.go 守护「每日上限命中后不得空转刷台账」（2026-10-19 现场）。
//
// 现场（6.8 小时真实运行数据，decisions.jsonl 1408 条）：
//   plan 1360 条 vs accept 只有 33 条（41:1）；
//   其中 980 条 evidence 是「已达每日任务上限」；
//   连续 766 次同一拒绝；拒绝中位间隔 = 3000ms —— 正是 tick 周期。
//
// 机理：tick 每 3 秒跑一次 -> supplyCandidates -> supplyAutonomous ->
//   NextTask 生成草案 -> eval.Evaluate 命中 DailyCap 硬门禁 -> Approve=false ->
//   任务落 rejected、**不入队** -> autonomyQueued 仍是 0 -> 下一个 tick 重复。
//   于是形成「规划 -> 拒绝 -> 3 秒后再规划」的空转热循环：
//   零产出，但 decisions.jsonl 以每 3 秒 1 条的速度膨胀（7x24 下会写爆磁盘）。
//
// 判据：DailyCap 是**终止条件**（今天不再生成新任务），
//   命中后必须让自主链路停下来，而不是每 3 秒重试一次。

import (
	"context"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestDailyCapStopsAutonomySupply 断言：当日任务数已达上限时，
// supplyAutonomous 不得再产生新的 plan 决策（不刷台账）。
func TestDailyCapStopsAutonomySupply(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	if !cfg.Autonomy.Enabled {
		t.Skip("自主未启用")
	}

	// 造一条今天的任务，让 tasksToday() >= DailyCap。
	seed := &model.Task{
		ID: "task-dailycap-seed", Title: "占位任务", Prompt: "p",
		State: model.StateDone, CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}
	if err := st.Create(seed); err != nil {
		t.Fatal(err)
	}

	// 把每日上限压到 1：现在「今天已有 1 条」即达上限。
	if _, err := e.cstore.Update(func(c *config.Config) { c.Eval.DailyCap = 1 }); err != nil {
		t.Fatal(err)
	}
	cfg = e.Config()

	before := len(e.autonomy.Decisions(2000))
	// 连跑三个周期，模拟现场空转。
	for i := 0; i < 3; i++ {
		e.supplyAutonomous(context.Background(), cfg)
		time.Sleep(5 * time.Millisecond)
	}
	after := len(e.autonomy.Decisions(2000))
	if after > before {
		t.Fatalf("每日上限命中后不应继续规划刷台账：决策数 %d -> %d（多出 %d 条）", before, after, after-before)
	}
}

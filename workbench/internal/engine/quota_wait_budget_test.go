package engine

import (
	"testing"

	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestQuotaWaitBudgetOutlivesDefault 钉住「额度耗尽用更长等待预算」（round 101-113 · T78）。
//
// 现场（2026-10-29/30）：36-38 条车道同时冷却、十余名任务 park；额度耗尽的原文写着要账号侧动作，
// 恢复是**小时级**。沿用 poolWaitBudget=4（约 20 分钟）⇒ 额度未恢复前每条任务都被判死，7×24 下一条不留。
//
// 判据（两条都要钉）：额度耗尽超过默认预算仍活着；普通种类到默认预算仍判死（分级不是取消封顶）。
func TestQuotaWaitBudgetOutlivesDefault(t *testing.T) {
	if quotaWaitBudget <= poolWaitBudget {
		t.Fatalf("分级预算必须严格大于默认预算（quota=%d default=%d）", quotaWaitBudget, poolWaitBudget)
	}
	e, st, _ := newTestEngine(t)
	id := "task-quota-wait"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)
	for i := 1; i <= poolWaitBudget; i++ {
		if e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindQuotaExhausted)) {
			t.Fatalf("第 %d 次额度等待就被判死（默认预算 %d），分级没生效", i, poolWaitBudget)
		}
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	if cur, ok := st.Get(id); !ok || cur.State != model.StateRunning {
		t.Fatalf("超过默认预算后应仍存活且在跑，实际 ok=%v", ok)
	}
}

// TestQuotaWaitBudgetStillCaps 证明分级没有取消封顶：额度耗尽到 quotaWaitBudget 次仍判死。
func TestQuotaWaitBudgetStillCaps(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-quota-cap"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)
	// 实测口径：实现是 `waits >= budget` ⇒ 第 budget 次调用即判死（不是第 budget+1 次）。
	// 所以循环只跑到 budget-1 次仍存活，第 budget 次必须判死。
	for i := 1; i < quotaWaitBudget; i++ {
		if e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindQuotaExhausted)) {
			t.Fatalf("第 %d 次就判死（期望第 %d 次到顶）", i, quotaWaitBudget)
		}
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	if !e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindQuotaExhausted)) {
		t.Fatalf("额度等待到 %d 次仍未判死 —— 分级不得退化成无限等待（僵尸任务会回来）", quotaWaitBudget)
	}
}

// TestDefaultBudgetStillApplies 反向对照：非额度类仍走 poolWaitBudget。
func TestDefaultBudgetStillApplies(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-default-wait"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)
	// 同口径：第 poolWaitBudget 次即判死。
	for i := 1; i < poolWaitBudget; i++ {
		if e.requeueForPoolWaitKind(id, before.ChainID, "") {
			t.Fatalf("第 %d 次普通等待就判死（默认预算 %d）", i, poolWaitBudget)
		}
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	if !e.requeueForPoolWaitKind(id, before.ChainID, "") {
		t.Fatalf("普通等待到 %d 次应判死（分级不得放宽别的种类）", poolWaitBudget)
	}
}

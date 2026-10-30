package engine

import (
	"testing"

	"workbench/internal/executor"
	"workbench/internal/model"
)

// 测试节奏说明（本轮实测踩过）：requeueForPoolWaitKind 会把任务从 running 退回 queued，
// 所以循环必须是「先 Transition 到 running，再 requeue」并在每轮开头建立 running 态 ——
// 反过来写会让末轮结束时停在 queued，判据就把"正常退回队列"误读成"没活着"。

// TestGrantQuotaBudgetOutlivesMixedRateLimits 钉住 T85：长预算**授予后不回收**。
//
// 线上实测（2026-10-30 10:42:22 → 10:52:09，同一个 task-1793327951210-75bc，逐秒日志可查）：
//
//	10:42:22 尝试结束 kind=quota_exhausted ⇒「免费额度耗尽：任务改走长等待预算 budget=48」
//	10:42:51 / 10:52:09 后续几轮只是 rate_limited ⇒ 重算得 budget=4 ⇒「重投次数到顶…budget=4」判死
//
// ⇒ 长预算曾被授予、又被收回，等于没授予：额度耗尽的恢复是小时级，4 次等待跨不过去。
//
//	根因是"每次重新判断该给多少"，而不是"一旦按更长尺度授予过就一直按那个尺度活着"。
func TestGrantQuotaBudgetOutlivesMixedRateLimits(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-grant-mixed"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)
	// 第 1 轮：额度耗尽 ⇒ 授予长预算
	if e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindQuotaExhausted)) {
		t.Fatal("第 1 次额度等待就被判死")
	}
	if cur, ok := st.Get(id); !ok || cur.PoolWaitCeil != quotaWaitBudget {
		t.Fatalf("额度耗尽应把预算授予为 %d，实际 PoolWaitCeil=%d（ok=%v）", quotaWaitBudget, cur.PoolWaitCeil, ok)
	}
	// 后续若干轮只有瞬时限流（线上真实形态）：跨过默认预算仍必须活着
	for i := 2; i <= poolWaitBudget+3; i++ {
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
		if e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindRateLimited)) {
			t.Fatalf("第 %d 次就被判死：授予过的长预算被收回了（T85 的病灶：budget 每次重算）", i)
		}
	}
	// 末轮结束后任务在 queued（退回队列）——这是"活着"的正确形态，不是终态
	if cur, ok := st.Get(id); !ok {
		t.Fatal("任务应仍存在")
	} else if cur.State != model.StateQueued {
		t.Fatalf("授予长预算后应仍活在队列里，实际 state=%v", cur.State)
	}
	// 但授予不等于取消封顶：一路退回到 quotaWaitBudget 次必须判死（防"授予后永生"）
	for i := poolWaitBudget + 4; i < quotaWaitBudget; i++ {
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
		if e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindRateLimited)) {
			t.Fatalf("第 %d 次提前判死（应到 %d 次）", i, quotaWaitBudget)
		}
	}
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	if !e.requeueForPoolWaitKind(id, before.ChainID, string(executor.KindRateLimited)) {
		t.Fatalf("到 quotaWaitBudget=%d 次必须判死（授予长预算不是无限存活）", quotaWaitBudget)
	}
}

// TestDefaultBudgetNotInflatedByGrant 钉另一侧：从未遇到额度耗尽的任务不该沾别人的授予 ——
// 预算是**按任务**授予的，不是全局开关；否则限流风暴期间所有任务都变僵尸，占住在途位与供料门。
func TestDefaultBudgetNotInflatedByGrant(t *testing.T) {
	e, st, _ := newTestEngine(t)
	granted := "task-granted"
	plain := "task-plain"
	seedQueuedForPoolTest(t, e, granted)
	seedQueuedForPoolTest(t, e, plain)
	gb, _ := st.Get(granted)
	pb, _ := st.Get(plain)
	allCooling(t, e)
	if e.requeueForPoolWaitKind(granted, gb.ChainID, string(executor.KindQuotaExhausted)) {
		t.Fatal("granted 第 1 次不该判死")
	}
	for i := 1; i < poolWaitBudget; i++ {
		if _, err := st.Transition(plain, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
		if e.requeueForPoolWaitKind(plain, pb.ChainID, string(executor.KindRateLimited)) {
			t.Fatalf("plain 第 %d 次提前判死", i)
		}
	}
	if _, err := st.Transition(plain, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	if !e.requeueForPoolWaitKind(plain, pb.ChainID, string(executor.KindRateLimited)) {
		t.Fatalf("plain 应在默认预算 %d 次判死（授予是按任务的）", poolWaitBudget)
	}
	if cur, ok := st.Get(plain); ok && cur.PoolWaitCeil > poolWaitBudget {
		t.Fatalf("plain 从未遇到额度耗尽，PoolWaitCeil 不该被抬高（实际 %d）", cur.PoolWaitCeil)
	}
}

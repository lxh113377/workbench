package engine

import (
	"testing"

	"workbench/internal/model"
)

// TestPoolWaitBudgetCapsZombie 钉住「环境退回封顶判死」（round 60 · T54）。
//
// 现场：唯一活任务 5134 被重投 47 次仍 attempts=2/3，僵尸任务永久占住自主供料门
// （inFlightTasksOfGoal 把 queued 也算在途）⇒ 7×24 下不再产生任何新任务。
func TestPoolWaitBudgetCapsZombie(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-poolwait-cap"
	seedQueuedForPoolTest(t, e, id)
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)

	// 前 budget-1 次：仍然走合法迁移回 queued，Attempts 不被环境等待消耗（既有不变量）。
	for i := 1; i < poolWaitBudget; i++ {
		e.requeueForPoolWait(id, before.ChainID)
		cur, ok := st.Get(id)
		if !ok {
			t.Fatalf("第 %d 次重投后任务应仍存在", i)
		}
		if cur.State != model.StateQueued {
			t.Fatalf("第 %d 次重投应仍退回队列，实际 %s", i, cur.State)
		}
		if cur.PoolWaits != i {
			t.Fatalf("第 %d 次重投后 PoolWaits = %d, want %d（环境等待必须可累计）", i, cur.PoolWaits, i)
		}
		if cur.Attempts != before.Attempts {
			t.Fatalf("环境等待不得计入 Attempts：应为 %d，实际 %d", before.Attempts, cur.Attempts)
		}
		if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
			t.Fatal(err)
		}
	}

	// 第 budget 次：到顶 ⇒ 判死（failed 是终态，自主供料门就此放行新任务）。
	e.requeueForPoolWait(id, before.ChainID)
	after, ok := st.Get(id)
	if !ok {
		t.Fatal("任务应仍存在（判死不删任务）")
	}
	if after.State != model.StateFailed {
		t.Fatalf("到顶后应落 failed（终态、解锁自主供料），实际 %s", after.State)
	}
	if after.ErrorKind == "" {
		t.Error("判死必须留真实错误类别，不能空着（面板/统计要能区分环境问题）")
	}
}

// TestPoolWaitsNotConsumedByRetryPath 钉住计数隔离：正常重试路径不污染 PoolWaits。
// 若两条预算互相污染，要么限流风暴会误杀任务、要么环境等待偷偷吃掉了重试额度。
func TestPoolWaitsNotConsumedByRetryPath(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-poolwait-isolation"
	task := &model.Task{ID: id, Title: "隔离测试", State: model.StateCandidate, Domain: "code"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.Update(id, func(x *model.Task) error { x.Attempts = 1; return nil }); err != nil {
		t.Fatal(err)
	}
	cur, _ := st.Get(id)
	if cur.PoolWaits != 0 {
		t.Fatalf("未走环境退回时 PoolWaits 应为 0，实际 %d", cur.PoolWaits)
	}
	e.requeueForPoolWait(id, cur.ChainID)
	if got, _ := st.Get(id); got.Attempts != 1 {
		t.Fatalf("环境退回不应改动 Attempts：应仍为 1，实际 %d", got.Attempts)
	}
}

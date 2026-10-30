package engine

import (
	"testing"

	"workbench/internal/model"
	"workbench/internal/retry"
)

// TestRetryTaskResetsPoolWaitBudget 钉住 round 139 的修法（由独立复核的代码面取证指出）：
// 任务因「模型池重投次数到顶，判死以解锁自主供料」失败时，事件里明写「可在面板重试」；
// 而旧实现只清 Attempts/Error ⇒ PoolWaits 仍停在封顶值、PoolWaitCeil 也仍是那个值，
// 于是下一次池不可用事件里 `waits >= budget` **立刻**成立 ⇒ 用户按提示重试，什么也修不好。
//
// 判据分两层：① 状态与计数复位（可观测）；② 复位后「封顶判死」这个条件不许立刻成立（行为层）。
func TestRetryTaskResetsPoolWaitBudget(t *testing.T) {
	e, st, _ := newTestEngine(t)
	const id = "task-retry-poolwait"
	task := &model.Task{ID: id, Title: "池等待封顶后重试", State: model.StateCandidate, MaxAttempts: 3, OutputFormat: "code"}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning, model.StateFailed} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s: %v", to, err)
		}
	}
	// 真实死亡形态：判死那一刻 PoolWaits 已封顶、PoolWaitCeil 也已授予（engine.go 里 Ceil 只升不降）
	if _, err := st.Update(id, func(x *model.Task) error {
		x.PoolWaits = 48
		x.PoolWaitCeil = 48
		x.Attempts = 3
		x.Error = "模型池连续 48 次不可用/限流，环境等待封顶判死"
		x.ErrorKind = string(retry.PoolWaitCapped)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.RetryTask(id); err != nil {
		t.Fatalf("重试应成功：%v", err)
	}
	got, ok := st.Get(id)
	if !ok {
		t.Fatal("重试后任务不见了")
	}
	if got.State != model.StateQueued {
		t.Fatalf("重试后状态 = %s, want queued", got.State)
	}
	if got.PoolWaits != 0 || got.PoolWaitCeil != 0 {
		t.Fatalf("重试后池等待预算未复位：PoolWaits=%d PoolWaitCeil=%d ⇒ 下一次池不可用会立刻再次判死（而事件明写「可在面板重试」）",
			got.PoolWaits, got.PoolWaitCeil)
	}
	if got.Attempts != 0 || got.Error != "" || got.ErrorKind != "" {
		t.Fatalf("重试后旧字段未清：attempts=%d error=%q kind=%q", got.Attempts, got.Error, got.ErrorKind)
	}
	// ② 行为层：复位之后，同一个"封顶判死"条件不许立刻成立（否则重试等于空转）
	budget := got.PoolWaitCeil
	if budget <= 0 {
		budget = 48 // 自动重投的默认尺度（engine.go 里 budget 的下界来自配置）
	}
	if got.PoolWaits >= budget {
		t.Fatalf("复位后 waits=%d 仍 >= budget=%d ⇒ 重试无效（病灶未除）", got.PoolWaits, budget)
	}
}

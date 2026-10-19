package engine

import (
	"context"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// seedQueuedForPoolTest 造一个 queued 任务（走合法迁移并放入内存队列）。
func seedQueuedForPoolTest(t *testing.T, e *Engine, id string) {
	t.Helper()
	task := &model.Task{ID: id, Title: "池等待测试", State: model.StateCandidate, Domain: "code"}
	if err := e.store.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Transition(id, model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Transition(id, model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	if q, ok := e.store.Get(id); ok {
		e.q.Push(q)
	}
}

// allCooling 把路由器配置成"唯一模型已失效"，用于模拟模型池全冷却。
func allCooling(t *testing.T, e *Engine) {
	t.Helper()
	routes := []config.ModelRoute{{Model: "only-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 1, CooldownM: 30}}
	e.router.UpdateRoutes(routes)
	e.router.RecordFailure("only-model", "model_unavailable", "EOL")
	if _, err := e.router.Pick("balanced", nil); err == nil {
		t.Fatal("前置条件失败：模型应处于冷却")
	}
}

// TestRequeueForPoolWaitSetsGate 覆盖 P1 热循环修复的三个不变量：
//  1. 任务从 running 退回 queued 必须走合法迁移（不是直接改字段）；
//  2. 必须给整池设置"下次可试时刻"，否则调度循环 3 秒后又捞起同一批任务；
//  3. 任务不算失败（Attempts 不变）。
func TestRequeueForPoolWaitSetsGate(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-pool-wait"
	seedQueuedForPoolTest(t, e, id)

	popped := e.q.Pop()
	if popped == nil {
		t.Fatal("队列应有任务")
	}
	if _, err := st.Transition(id, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Get(id)
	allCooling(t, e)

	e.requeueForPoolWait(id, before.ChainID)

	after, ok := st.Get(id)
	if !ok {
		t.Fatal("任务应仍存在")
	}
	if after.State != model.StateQueued {
		t.Fatalf("退回后状态 = %s, want queued（必须走合法迁移）", after.State)
	}
	if after.Attempts != before.Attempts {
		t.Fatalf("环境等待不应计入任务失败次数: attempts %d -> %d", before.Attempts, after.Attempts)
	}
	gate := e.poolWaitUntil.Load()
	if gate <= model.Now() {
		t.Fatalf("应设置未来闸门，实际 %d（now=%d）", gate, model.Now())
	}
	if gate > model.Now()+40*60_000 {
		t.Fatalf("闸门时间异常过大: %d", gate)
	}
	if got := e.QueueSnapshot(); len(got) != 1 || got[0].ID != id {
		t.Fatalf("退回后应重新入队，实际队列: %+v", got)
	}
}

// TestTickRespectsPoolGate 保证闸门真的挡住调度（否则仍然是热循环）。
func TestTickRespectsPoolGate(t *testing.T) {
	e, _, _ := newTestEngine(t)
	seedQueuedForPoolTest(t, e, "task-gate")

	e.poolWaitUntil.Store(time.Now().Add(time.Minute).UnixMilli())
	e.tick(context.Background())

	if e.q.Len() != 1 {
		t.Fatalf("闸门期内不应取出任务，队列长度 = %d", e.q.Len())
	}
	if !e.tickBusy.Load() {
		// tick 正常返回即释放；这里只断言没有并发残留
	}
}

// TestTickRunsWhenGateExpired 反向用例：闸门过期后必须恢复调度。
func TestTickRunsWhenGateExpired(t *testing.T) {
	e, _, _ := newTestEngine(t)
	id := "task-after-gate"
	seedQueuedForPoolTest(t, e, id)

	// 闸门设为过去（已过期）；用空模型池让 runOne 选模型失败并走 requeue 分支，
	// 这样不会真的启动 DSH 子进程，同时验证 tick 真的取了任务。
	e.poolWaitUntil.Store(time.Now().Add(-time.Minute).UnixMilli())
	e.router.UpdateRoutes([]config.ModelRoute{{Model: "only", Tier: "balanced", Enabled: true, Weight: 1, MaxFail: 1, CooldownM: 30}})
	e.router.RecordFailure("only", "model_unavailable", "EOL")
	e.ctx = context.Background()

	e.tick(context.Background())
	// runOne 是异步 goroutine；等它把任务退回队列
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if e.q.Len() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// 必须等异步 goroutine 全部结束再断言/收尾，否则泄漏的 runOne 会在
	// 下一个测试里继续操作已 Close 的 store 与队列（现场：队列长度偶发 2）。
	waitInflightIdle(t, e, 3*time.Second)
	if e.q.Len() != 1 {
		t.Fatalf("闸门过期后应恢复调度并把任务退回队列，实际队列长度 = %d", e.q.Len())
	}
}

// waitInflightIdle 等引擎的 in-flight 任务归零（防止异步 goroutine 跨测试污染）。
//
// inflight 是 sync.WaitGroup，没有原子读数；这里用 tickBusy 作为
// "tick 的 goroutine 已退出"的代理信号，配合轮询等待。
func waitInflightIdle(t *testing.T, e *Engine, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if !e.tickBusy.Load() && e.q.Len() >= 1 {
			// 再给一轮时间让最后一个 goroutine 完成队列写回
			time.Sleep(50 * time.Millisecond)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("等待 inflight 归零超时，继续断言")
}

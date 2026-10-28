package engine

// StallReasonWith 的等价性与「真的用传入快照」判据（2026-10-28 性能修复配套）。
//
// 被优化的事实：一次面板 /api/status 里全量 store.List()（生产 5001 条按值拷贝 + 排序，
// 实测单价 2.0-2.75ms / 2.9MB 垃圾 / 5004 allocs，见 store/bench_perf_probe_test.go）
// 被重复付 5~6 次：handleStatus 自己算一次 Stats()，startedAt 字段又字面重算一次，
// StallReason 内部还自带三次（自身 + upstreamFailingStall + queuedWaitingForLane）。
//
// 判据钉两件事，缺一不可：
//   一、等价：StallReasonWith(e.Stats()) 与 StallReason() 逐字段相同，对外结果不变；
//   二、真的消费传入快照：给它一份与实况不同的 stats，结果必须随之改变。
//       若它仍然自己重算，注入的假快照会被忽略、输出与真实一致，本条判红。
//       这条同时是负向对照：证明「省掉的那几次 List()」确实存在，不是改了个没人用的参数。

import (
	"testing"

	"workbench/internal/model"
)

// gateQueuedScenario 复刻在册现场（2026-10-21 22:3x）：有任务排队、一条都没在跑、
// 整池闸门仍按住调度。这是 queuedWaitingForLane 唯一的命中路径，且该判据只读 stats
// 里的 QueuedTasks/RunningTasks 两项，因此最适合验「传入快照是否被真实消费」。
func gateQueuedScenario(t *testing.T, e *Engine, id string) {
	t.Helper()
	startRunning(t, e)
	if err := e.store.Create(&model.Task{
		ID: id, Title: "排队中的任务 " + id, Prompt: "p", State: model.StateQueued,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	e.poolWaitUntil.Store(model.Now() + 20*60*1000)
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 3
	e.rateLimit.lastAt = model.Now() - 60_000
	e.rateLimitMu.Unlock()
}

func TestStallReasonWithEqualsLegacyEntry(t *testing.T) {
	e, _, _ := newTestEngine(t)
	gateQueuedScenario(t, e, "task-simcache-equiv")

	legacy := e.StallReason()
	injected := e.StallReasonWith(e.Stats())
	if legacy != injected {
		t.Fatalf("等价性破裂: 兼容入口=%+v 注入入口=%+v", legacy, injected)
	}
	if legacy.Code != StallQueuedNoLane {
		t.Fatalf("前置未成立：该形态应命中 %s，实得 %q（判据会空转）", StallQueuedNoLane, legacy.Code)
	}
}

// TestStallReasonWithActuallyUsesInjectedStats 负向对照：传入的快照必须被真实消费。
func TestStallReasonWithActuallyUsesInjectedStats(t *testing.T) {
	e, _, _ := newTestEngine(t)
	gateQueuedScenario(t, e, "task-simcache-consume")

	real := e.Stats()
	if real.QueuedTasks == 0 {
		t.Fatal("前置未成立：队列应有任务，实得 0（负向对照会假绿）")
	}
	if got := e.StallReasonWith(real).Code; got != StallQueuedNoLane {
		t.Fatalf("真实快照下应命中 %s，实得 %q", StallQueuedNoLane, got)
	}

	// 注入「零排队、零在跑、无产出」的快照：真用它就走到后面的分支，不再给 QueuedNoLane；
	// 若实现忽略参数、自己重算 Stats()，则仍然给 QueuedNoLane ⇒ 本条判红。
	got := e.StallReasonWith(model.Stats{}).Code
	if got == StallQueuedNoLane {
		t.Fatal("注入空快照仍报 QueuedNoLane，说明实现自行重算了 Stats()，" +
			"本轮宣称省下的 List() 并不存在（假收益）")
	}
}

// TestStallReasonWithEquivalenceOnProducingState 覆盖另一条早退路径（有产出不报停摆），
// 防「只在一条分支上等价」。
func TestStallReasonWithEquivalenceOnProducingState(t *testing.T) {
	e, _, _ := newTestEngine(t)
	startRunning(t, e)
	if err := e.store.Create(&model.Task{
		ID: "task-simcache-done", Title: "今天做完的任务", Prompt: "p", State: model.StateDone,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if a, b := e.StallReason(), e.StallReasonWith(e.Stats()); a != b {
		t.Fatalf("有产出态不等价: legacy=%+v injected=%+v", a, b)
	}
	if e.StallReason().Code != "" {
		t.Fatal("有产出时不得报停摆")
	}
}

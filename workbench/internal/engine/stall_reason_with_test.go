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
	"strings"
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

// maskClockDigits 把十进制数字串归一为单个 # 占位符。
//
// 为什么判据需要它（本轮一手实踩，不是设计时想清的）：queuedWaitingForLane 的文案里带
// 「剩余约 N 秒」= (poolWaitUntil - model.Now())/1000，而两次调用之间墙钟必然前进 ⇒
// 我原先用整结构相等（legacy != injected）比较，实测在秒边界上随机失败
// （第一次抓到的是 1200 秒 vs 1199 秒）。整轮 go test 恰好落在同一秒内才绿 ——
// 那是**时间边界 flaky**，CI 上必然随机红。
//
// 归一的只是数字：Code 与 Since 要求逐位相等（这两项不含墙钟，不放水）。
//
// ⚠️ 本判据**抓不到文案字面漂移**，别把它当文案锁（本轮一手实测，不是我写注释时的设想）：
// 我拿 MUT1 把 queuedWaitingForLane 的「一个都没在跑」改成「一条都没在跑」，
// 本等价判据**仍然绿** —— 因为两个入口走的是同一条实现，两边文案恒等，
// 比较的是"自己 vs 自己"。⇒ 文案的锁在 TestStallQueuedNoLaneTextTemplateIsExact，
// 那里把归一后的完整模板与字面常量比对，改一个字必红。
func maskClockDigits(s string) string {
	out := make([]rune, 0, len(s))
	inDigits := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if !inDigits {
				out = append(out, '#')
				inDigits = true
			}
			continue
		}
		inDigits = false
		out = append(out, r)
	}
	return string(out)
}

// assertStallEquivalent 钉「两个入口给出同一件事」：机器码与起点严格相等，文案模板逐字相等。
func assertStallEquivalent(t *testing.T, tag string, a, b StallReason) {
	t.Helper()
	if a.Code != b.Code {
		t.Fatalf("%s 原因码不等: %q vs %q", tag, a.Code, b.Code)
	}
	if a.Since != b.Since {
		t.Fatalf("%s Since 不等: %d vs %d（这一项不含墙钟倒计时，必须逐位相等）", tag, a.Since, b.Since)
	}
	if maskClockDigits(a.Text) != maskClockDigits(b.Text) {
		t.Fatalf("%s 文案模板不等:\n a=%s\n b=%s", tag, a.Text, b.Text)
	}
	if maskClockDigits(a.Detail) != maskClockDigits(b.Detail) {
		t.Fatalf("%s 证据模板不等:\n a=%s\n b=%s", tag, a.Detail, b.Detail)
	}
}

func TestStallReasonWithEqualsLegacyEntry(t *testing.T) {
	e, _, _ := newTestEngine(t)
	gateQueuedScenario(t, e, "task-simcache-equiv")

	legacy := e.StallReason()
	injected := e.StallReasonWith(e.Stats())
	assertStallEquivalent(t, "命中分支", legacy, injected)
	if legacy.Code != StallQueuedNoLane {
		t.Fatalf("前置未成立：该形态应命中 %s，实得 %q（判据会空转）", StallQueuedNoLane, legacy.Code)
	}
	if legacy.Text == "" || legacy.Detail == "" {
		t.Fatal("前置未成立：文案为空时「模板逐字相等」会退化成恒真（判据空转）")
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
	assertStallEquivalent(t, "有产出早退分支", e.StallReason(), e.StallReasonWith(e.Stats()))
	if e.StallReason().Code != "" {
		t.Fatal("有产出时不得报停摆")
	}
}

// TestStallQueuedNoLaneTextTemplateIsExact 把文案钉成完整模板（数字位归一为 #）。
//
// 为什么必须另开这一条：上面的等价判据比较的是同一条实现的两次调用，对文案漂移是
// **结构性空判据**（MUT1 实测：改「一个」为「一条」它照样绿）。既有 stall 用例只用
// strings.Contains 取片段，同样抓不到量词级别的变化。完整模板逐字比对才抓得到。
//
// 负控（本轮实测，非推定）：把实现里的「一个都没在跑」改成「一条都没在跑」⇒ 本条判红，
// 而**同一次运行**里 TestStallReasonQueuedButNoLane（Contains 取片段）与上面的等价判据
// 都仍然绿 ⇒ 三条判据的覆盖面差异是被验证过的事实，不是我对文案的想象。
// 还原后用 git diff --exit-code 判定与已部署提交逐字节相同（负控副本用完即删）。
func TestStallQueuedNoLaneTextTemplateIsExact(t *testing.T) {
	e, _, _ := newTestEngine(t)
	gateQueuedScenario(t, e, "task-simcache-text")

	r := e.StallReason()
	if r.Code != StallQueuedNoLane {
		t.Fatalf("前置未成立：应命中 %s，实得 %q", StallQueuedNoLane, r.Code)
	}
	const wantText = "模型池冷却中：# 个任务在排队，一个都没在跑；约 # 秒后自动重试"
	if got := maskClockDigits(r.Text); got != wantText {
		t.Fatalf("text 模板漂移:\n want=%s\n got =%s", wantText, got)
	}
	// detail 前段是固定句式（后半段会带上游原因文本，随环境变化，只钉到句号为止）。
	const wantDetailHead = "整池冷却第 # 次，剩余约 # 秒后自动重试（闸门到点即恢复调度）。"
	if got := maskClockDigits(r.Detail); !strings.HasPrefix(got, wantDetailHead) {
		t.Fatalf("detail 前缀模板漂移:\n want=%s\n got =%s", wantDetailHead, got)
	}
}

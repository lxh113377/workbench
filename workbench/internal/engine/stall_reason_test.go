package engine

// stall_reason_test.go 守护「运行中但产出 0」的自证逻辑（2026-10-21 现场）。
//
// 现场事实：面板 phase=running、todayDone=0、queued=0、running=0，
// 日志里除"模型路由表已更新"外没有一行说明为什么。用户只能看到三个互相矛盾的读数。
//
// 自证必须满足两条：①正常产出时**不得**报停摆（否则变成常驻噪音）；
// ②每种能解释"为什么 0 产出"的情形都要有稳定原因码 + 人话文案。

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// startRunning 把引擎切到 running（StallReason 只在运行阶段判定）。
func startRunning(t *testing.T, e *Engine) {
	t.Helper()
	e.mu.Lock()
	e.phase = model.PhaseRunning
	e.mu.Unlock()
}

// TestStallReasonEmptyWhenProducing 断言正常产出时不得报停摆。
func TestStallReasonEmptyWhenProducing(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	// 造一条今日已完成任务 => todayDone > 0
	if err := st.Create(&model.Task{
		ID: "task-done-today", Title: "今天做完的任务", Prompt: "p", State: model.StateDone,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := e.StallReason(); r.Code != "" {
		t.Fatalf("有产出时不得报停摆，实际 %s: %s", r.Code, r.Text)
	}
}

// TestStallReasonEmptyWhenQueued 断言有排队任务时不算停摆（只是还没轮到执行）。
func TestStallReasonEmptyWhenQueued(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	if err := st.Create(&model.Task{
		ID: "task-queued", Title: "排队中的任务", Prompt: "p", State: model.StateQueued,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := e.StallReason(); r.Code != "" {
		t.Fatalf("有排队任务时不算停摆，实际 %s: %s", r.Code, r.Text)
	}
}

// TestStallReasonDailyCap 断言配额耗尽必须能自证（本轮"整天零产出"的直接成因）。
func TestStallReasonDailyCap(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	if _, err := e.cstore.Update(func(c *config.Config) { c.Eval.DailyCap = 2 }); err != nil {
		t.Fatal(err)
	}
	// 用**失败**任务吃满配额：这类任务真的消耗了执行额度却没产出，
	// 是"配额耗尽但今日产出仍为 0"的真实成因（与现场 2000 条被拒草案同构，
	// 区别只是它们走了执行而非被评估拦下）。
	for _, id := range []string{"task-cap-1", "task-cap-2"} {
		if err := st.Create(&model.Task{
			ID: id, Title: "执行失败的任务", Prompt: "p", State: model.StateFailed,
			CreatedAt: model.Now(), UpdatedAt: model.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	r := e.StallReason()
	if r.Code != StallDailyCap {
		t.Fatalf("配额耗尽应报 %s，实际 %q（text=%s）", StallDailyCap, r.Code, r.Text)
	}
	if r.Text == "" || r.Detail == "" {
		t.Fatal("配额耗尽必须给出计数与人话解释")
	}
}

// TestStallReasonRejectBreaker 断言熔断必须能自证（本轮空转的表征）。
func TestStallReasonRejectBreaker(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	startRunning(t, e)
	if _, err := e.autonomy.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	g, ok := e.autonomy.Active()
	if !ok || g == nil {
		t.Fatal("应存在活跃目标")
	}
	for i := 0; i < rejectStreakLimit; i++ {
		seedRejected(t, e, "task-stall-rej-"+string(rune('a'+i)), g.ID)
	}
	r := e.StallReason()
	if r.Code != StallRejectBreaker {
		t.Fatalf("连续被拒应报 %s，实际 %q（text=%s）", StallRejectBreaker, r.Code, r.Text)
	}
	if r.Text == "" {
		t.Fatal("熔断必须给出人话解释")
	}
}

// TestStallReasonRateLimited 断言限流冷却必须能自证。
func TestStallReasonRateLimited(t *testing.T) {
	e, _, _ := newTestEngine(t)
	startRunning(t, e)
	e.poolWaitUntil.Store(model.Now() + 30_000)
	r := e.StallReason()
	if r.Code != StallRateLimited {
		t.Fatalf("限流冷却应报 %s，实际 %q（text=%s）", StallRateLimited, r.Code, r.Text)
	}
}

// TestStallReasonEmptyWhenStopped 断言引擎不在运行时不得报停摆
// （停机是用户意图，不该被描述成故障）。
func TestStallReasonEmptyWhenStopped(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.mu.Lock()
	e.phase = model.PhaseStopped
	e.mu.Unlock()
	if r := e.StallReason(); r.Code != "" {
		t.Fatalf("停机状态不该报停摆，实际 %s: %s", r.Code, r.Text)
	}
}

// TestStallReasonUpstreamRejectingWhileRunning 是用户所报现象的正解回归。
//
// 现场形态：running=6 / queued=3（看起来在干活）但 todayDone=0，
// 每轮 turn 都被上游拒（no eligible anonymous exit）。
// 这是**唯一**会让面板同时显示"运行中"和"产出 0"的形态，
// 因此 StallReason 必须优先识别它，而不是因为"有任务在跑"就早退成"正常"。
func TestStallReasonUpstreamRejectingWhileRunning(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	// 有任务在跑（running > 0），但今天零完成。
	if err := st.Create(&model.Task{
		ID: "task-running-up", Title: "在跑但被上游拒", Prompt: "p", State: model.StateRunning,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 引擎已记录上游拒绝原文（failTask 会写 lastErr）。
	e.mu.Lock()
	e.lastErr = "dsh: EAC_UPSTREAM_FAILED: no eligible anonymous exit; retry after cooldown"
	e.mu.Unlock()

	r := e.StallReason()
	if r.Code != StallUpstreamRejecting {
		t.Fatalf("「在跑但上游系统性拒答」必须报 %s，实际 %q（text=%s）", StallUpstreamRejecting, r.Code, r.Text)
	}
	if r.Text == "" || r.Detail == "" {
		t.Fatal("必须给出人话原因与证据（用户看到的就是这句话）")
	}
}

// TestStallReasonRunningWithoutUpstreamErrorIsNormal 断言不能误报。
//
// 仅仅"有任务在跑 + 今天还没完成"不等于故障（可能刚启动几分钟）。
// 只有拿到上游拒绝证据才报 —— 否则横幅会变成每次都亮的噪音。
func TestStallReasonRunningWithoutUpstreamErrorIsNormal(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	if err := st.Create(&model.Task{
		ID: "task-running-ok", Title: "正常运行中的任务", Prompt: "p", State: model.StateRunning,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if r := e.StallReason(); r.Code != "" {
		t.Fatalf("没有上游拒绝证据时不得误报停摆，实际 %s: %s", r.Code, r.Text)
	}
}

// TestStallReasonQueuedButNoLane 是「排队 14 但是 0 正在执行」这句用户报障的回归。
//
// 现场（2026-10-21 22:31~22:39 活实例实测，watcher 每 10 秒一条）：
//
//	queuedTasks=14 runningTasks=0 rateLimit.limited=true remainingSeconds=442
//	stallReason={}  ← 这就是缺陷：服务端握着两个能说清的字段，却什么都没说
//
// 判据：队列非空 + 一条都没在跑 + 整池闸门仍按住调度 ⇒ 必须给出可读原因与等待时长。
func TestStallReasonQueuedButNoLane(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	if err := st.Create(&model.Task{
		ID: "task-queued-gated", Title: "排队中的任务", Prompt: "p", State: model.StateQueued,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 复刻现场：闸门推到 20 分钟后，退避已到第 3 档。
	e.poolWaitUntil.Store(model.Now() + 20*60*1000)
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 3
	e.rateLimit.lastAt = model.Now() - 60_000
	e.rateLimitMu.Unlock()

	r := e.StallReason()
	if r.Code != StallQueuedNoLane {
		t.Fatalf("有排队任务但闸门按住调度时必须报 %s，实际 %q（text=%s）",
			StallQueuedNoLane, r.Code, r.Text)
	}
	if r.Text == "" || r.Detail == "" {
		t.Fatal("必须同时给出人话原因与证据（用户看到的就是 text/detail 两句）")
	}
	// text 里必须带出排队条数与等待时长，否则用户还是不知道要等多久。
	if !strings.Contains(r.Text, "1 个任务在排队") || !strings.Contains(r.Text, "秒后自动重试") {
		t.Fatalf("text 必须给出排队数与等待时长，实际：%s", r.Text)
	}
	if !strings.Contains(r.Detail, "第 3 次") {
		t.Fatalf("detail 必须给出退避档位（解释为什么等这么久），实际：%s", r.Detail)
	}
}

// TestStallReasonQueuedWithoutGateIsNormal 保证新判据不制造噪音。
//
// 有任务排队、闸门没关（还在正常轮转）时，绝不能报停摆 —— 那只是"还没轮到执行"。
func TestStallReasonQueuedWithoutGateIsNormal(t *testing.T) {
	e, st, _ := newTestEngine(t)
	startRunning(t, e)
	if err := st.Create(&model.Task{
		ID: "task-queued-normal", Title: "普通排队任务", Prompt: "p", State: model.StateQueued,
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	// 闸门已过期（0 值即"没有闸门"）。
	if r := e.StallReason(); r.Code != "" {
		t.Fatalf("闸门没关时不得报停摆，实际 %s: %s", r.Code, r.Text)
	}
}

// TestUpstreamRejectKind 断言各类上游拒绝文案都能被识别。
func TestUpstreamRejectKind(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"no eligible anonymous exit; retry after cooldown", true},
		{"dsh: EAC_UPSTREAM_FAILED: Function 'x': Not found for account 'y'", true},
		{"provider \"our-free-model\" model \"combo:auto-main\" does not support reasoning effort \"high\"", true},
		{"UNSUPPORTED_REASONING_EFFORT", true},
		{"任务超时（30m0s）", false},
		{"", false},
	}
	for _, c := range cases {
		got := upstreamRejectKind(c.msg) != ""
		if got != c.want {
			t.Fatalf("upstreamRejectKind(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}

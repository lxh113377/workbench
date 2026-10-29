package engine

// cancel_cause_contract_test.go —— 取消归因的三条契约判据（issue #11）。
//
// 现场（生产实测，2026-10-29，tasks/*/attempts.jsonl）：2 行 kind=unknown / action=cancel /
//   error="启动 DSH 失败: context canceled"。成因：executor 用 fmt.Errorf("启动 DSH 失败: %w", err)
//   包装 ctx 错误时，包装层不认识 context 错误 ⇒ 具名 KindCancelled 根本没机会被赋上，
//   类别只能落 unknown。同时 RecordFailure 原先排在取消判定**之前** ⇒ 用户点一次「终止」就把
//   那条无辜车道打成一次失败冷却。
// 对标：river 用 context.Cause 做第二维（job_executor.go:183-185 在写库前把原因换成真相、
//   :590-597 用 cause 把停机从失败里摘出）；asynq 停机走 requeue，既不写 ErrorMsg 也不吃重试
//   次数（processor.go:238-241 + rdb.go:598-619）。本项目 7×24 无人值守 ⇒ 取消与失败的边界
//   必须机读可分，否则失败率与车道健康度都是脏的。

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
	"workbench/internal/retry"
)

// ── 判据①：cause 分类表（同一段代码只改 cause ⇒ 结论随之变，自带负向对照）──────
func TestClassifyCancellationByCause(t *testing.T) {
	cases := []struct {
		name      string
		cause     error
		userReq   bool
		kindIn    retry.Kind
		wantYes   bool
		wantKind  retry.Kind
		wantInMsg string
	}{
		{"停机具名 cause ⇒ 判取消且文案点名停机", causeEngineShutdown, false, retry.Unknown, true, retry.Cancelled, "引擎停机"},
		{"用户取消具名 cause ⇒ 判取消且文案点名用户", causeUserCancel, false, retry.Unknown, true, retry.Cancelled, "用户取消"},
		// 关键边界：超时不是取消。判成取消会吞掉一次真实失败（车道不被归因、失败率不涨）。
		{"DeadlineExceeded 不得算取消", context.DeadlineExceeded, false, retry.Timeout, false, retry.Timeout, ""},
		// 本 issue 的原始形态：cause=Canceled 而 executor 类别是 unknown ⇒ 以 cause 为准
		{"泛错顶掉的 unknown 被 cause 纠正", context.Canceled, false, retry.Unknown, true, retry.Cancelled, "取消"},
		// 竞态兜底：cause 还没落到但用户意图标记在 ⇒ 以意图为准（不再落 unknown）
		{"无 cause 但有用户意图标记", nil, true, retry.Boot, true, retry.Cancelled, "用户取消"},
		// 都没命中 ⇒ 真失败原样交回，类别不得被改写
		{"非取消路径保持原类别", nil, false, retry.Upstream, false, retry.Upstream, ""},
		// executor 自己已判具名取消（res.ErrorKind=cancelled）：认下
		{"executor 具名 cancelled 被认下", nil, false, retry.Cancelled, true, retry.Cancelled, "取消"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			// cause 只记第一次 ⇒ 这里兜底 cancel(nil) 不会覆盖上面已写入的具名原因。
			defer cancel(nil)
			if tc.cause != nil {
				cancel(tc.cause)
			}
			gotKind, gotText, yes := classifyCancellation(ctx, tc.userReq, tc.kindIn)
			if yes != tc.wantYes {
				t.Fatalf("是否取消 = %v，期望 %v（kind=%s text=%q）", yes, tc.wantYes, gotKind, gotText)
			}
			if gotKind != tc.wantKind {
				t.Fatalf("类别 = %s，期望 %s（text=%q）", gotKind, tc.wantKind, gotText)
			}
			if !tc.wantYes {
				return
			}
			if !strings.Contains(gotText, tc.wantInMsg) {
				t.Fatalf("归因文案必须含 %q，实得 %q", tc.wantInMsg, gotText)
			}
			// 取消行的文案不得是裸 context 错误 —— 那正是「读台账仍查不动」的旧形态。
			if gotText == "context canceled" || gotText == "unknown" {
				t.Fatalf("不得把裸 ctx 错误当归因文案，实得 %q", gotText)
			}
		})
	}
}

// 尾注保留取证线索（原始错误有信息量），但 nil / 空白不得产生噪声括号。
func TestCauseSuffixKeepsEvidence(t *testing.T) {
	if got := causeSuffix(nil); got != "" {
		t.Fatalf("nil 必须空串，实得 %q", got)
	}
	if got := causeSuffix(errors.New("   ")); got != "" {
		t.Fatalf("空白错误必须空串，实得 %q", got)
	}
	got := causeSuffix(errors.New("启动 DSH 失败: context canceled"))
	if !strings.Contains(got, "context canceled") || !strings.HasPrefix(got, "（") {
		t.Fatalf("尾注须成括号形态并保留原文，实得 %q", got)
	}
}

// ── 判据②：taskCancels 存取类型一致（源文本不变量）──────────────────────
//
// 为什么钉文本而不是行为：sync.Map 的类型断言失败时 ok=false，而调用点是
// `if cancel, ok := v.(T); ok { ... }` ⇒ 静默跳过、零日志零报错，「终止」按钮当场退化成
// 空操作（本仓在册缺陷形态：P0-3 之前的 CancelTask 正是空操作）。这种失效无法用行为测试
// 稳定捕获（它表现得像「什么都没发生」），只能对账文本 —— 与 daily_count_source_test.go 同族。
func TestTaskCancelsTypeConsistency(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读 engine.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "e.taskCancels.Store(task.ID, taskCanceler{cause: cancel})") {
		t.Fatal("runOne 必须以 taskCanceler{cause: cancel} 形状注册取消入口（改形状请同步本判据）")
	}
	// 两个消费点（CancelTask / cancelAllRunning）必须断言同一类型。
	if got := strings.Count(body, "v.(taskCanceler)"); got != 2 {
		t.Fatalf("taskCancels 的 taskCanceler 断言应有 2 处，实得 %d —— 类型不一致时取消会静默失效", got)
	}
	// 禁止退回裸 func 存储：那样取消时无处写入具名 cause（issue #11 修掉的正是这个）。
	if strings.Contains(body, "e.taskCancels.Store(task.ID, cancel)") {
		t.Fatal("不得再存裸 cancel 函数：取消将无处写入具名 cause")
	}
	if strings.Contains(body, "v.(context.CancelFunc)") {
		t.Fatal("仍有 context.CancelFunc 断言点：与 Store 的 taskCanceler 不一致 ⇒ 静默不取消")
	}
	// 引擎级 cancel 也必须带 cause（停机归因的源头）。
	if !strings.Contains(body, "e.ctx, e.cancel = context.WithCancelCause(context.Background())") {
		t.Fatal("引擎根 ctx 必须用 WithCancelCause：否则停机路径无 cause 可写")
	}
	if !strings.Contains(body, "cancel(causeEngineShutdown)") {
		t.Fatal("stop() 必须以 causeEngineShutdown 取消根 ctx（与用户终止区分开）")
	}
}

// ── 判据③：取消优先于车道记账（行为判据，三态对照）──────────────────

// cancelProbeRunner 逐字复刻生产那 2 行的形态（forensics 定位）：
//
//	res.ErrorKind="unknown"（modelpool/failover.go 的兜底类别）+ runErr 是包装泛错
//	"启动 DSH 失败: context canceled" ⇒ 旧代码里 ParseKind 判不出取消、
//	且 RecordFailure 排在取消判定**之前** ⇒ 台账 kind=unknown 且无辜车道白吃一次失败。
type cancelProbeRunner struct {
	// resKind 传给 Result.ErrorKind；wrapErr 决定是否带包装泛错。
	resKind string
}

func (r *cancelProbeRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	switch r.resKind {
	case "upstream":
		// 真失败且具名 ⇒ 车道必须被归因（证明"取消不记账"那条断言的 0 不是恒真）。
		return &executor.Result{ErrorKind: string(executor.KindUpstream)}, errors.New("上游 5xx")
	default:
		return &executor.Result{ErrorKind: "unknown"}, errors.New("启动 DSH 失败: " + context.Canceled.Error())
	}
}

func (r *cancelProbeRunner) UpdateConfig(config.ExecutorConfig) {}

// laneFailureOf 读某条车道的累计失败数（车道记账的可见面；对照面丢失一律判红，不 Skip）。
func laneFailureOf(t *testing.T, e *Engine, modelID string) int {
	t.Helper()
	for _, h := range e.router.Health() {
		if h.Model == modelID {
			return h.Failure
		}
	}
	t.Fatalf("路由表里没有车道 %s（判据失去对照面，判红而非跳过）", modelID)
	return -1
}

// runCancelProbe 跑一次 runOne 并回传（终态, 台账最后一行的 kind, action）。
// 参数 signalUserCancel = 是否等价"用户点了终止"（CancelTask 会先置意图标记再取消 ctx）。
// 车道注册必须走 config.Models 而不是 UpdateRoutes：后者会被随后的
// e.ReloadConfig() 用 cfg.Models **整体替换**掉 ⇒  RecordFailure 打到另一条 modelID 上，
// "车道失败数"恒为 0（本 probe 第一版就是这么错的：连"取消不记账"那条 PASS 都是假绿，
// 因为它量的那条车道从头到尾没人记过账）。改法与真实启动路径同构。
func runCancelProbe(t *testing.T, resKind string, signalUserCancel bool) (model.TaskState, string, string, int) {
	t.Helper()
	const lane = "executor/cancel-probe-model"
	e, st, _ := newTestEngine(t)
	if _, err := e.cstore.Update(func(c *config.Config) {
		// 单一车道 = 本判据的对照面；tier 必须是 balanced（runOne 无模板时的默认档位）。
		c.Models = []config.ModelRoute{{
			Model: lane, Provider: "our-free-model", Tier: "balanced",
			Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30,
		}}
		c.Plan.Enabled = false    // 只测执行段分流，不掺方案阶段
		c.Executor.MaxRetries = 0 // 单次尝试即收敛 ⇒ 判据不依赖重试次数（与 T69 消抖同口径）
	}); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	e.SetRunner(&cancelProbeRunner{resKind: resKind})
	task := &model.Task{ID: "task-probe-" + resKind, Title: "取消归因探针", Prompt: "探针",
		State: model.StateCandidate, MaxAttempts: 1}
	running := prepareRunning(t, st, task)
	if signalUserCancel {
		// 与 CancelTask 对 running 任务的处置同形：先置意图标记，再具名取消该任务的 ctx。
		// 这里不真去调 CancelTask —— 它的 taskCancels 查找需要 runOne 已注册（并发时序），
		// 而标记 + ctx 两路信号都被 classifyCancellation 覆盖（②③），单测要的是确定性。
		e.cancelling.Store(task.ID, struct{}{})
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.runOne(context.Background(), running)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("runOne 未在 20s 内收敛")
	}
	got, ok := st.Get(task.ID)
	if !ok {
		t.Fatal("任务应仍存在")
	}
	recs, err := st.ListAttempts(task.ID)
	if err != nil || len(recs) == 0 {
		t.Fatalf("应有台账行，实得 %d 行 err=%v", len(recs), err)
	}
	last := recs[len(recs)-1]
	// 对照面自证：台账行必须落在**那条**车道上，否则"失败数 0"只是量错了对象。
	// 本 probe 第一版缺这条自证 ⇒ 三条断言里"车道失败数"全部无效（其中一条还 PASS 了，是假绿）。
	// 加上这行后，对照面一旦漂移会立刻判红并说出为什么读数不可信。
	if last.Model != lane {
		t.Fatalf("台账行的 model = %q，期望 %s ⇒ 对照面已漂移（多半是路由表被别处覆盖），失败数读数不可信", last.Model, lane)
	}
	return got.State, last.Kind, last.Action, laneFailureOf(t, e, lane)
}

// 三态：① 生产形态 + 用户取消 ⇒ 具名 cancelled、车道零失败；
//
//	② 同一条错误但没有任何取消信号 ⇒ 保持 unknown（不许凭空判取消）、车道照记；
//	③ 具名真失败 ⇒ 车道照记一次（②③ 共同证明 ① 的 0 不是恒真）。
func TestCancellationDoesNotPenalizeLane(t *testing.T) {
	state, kind, action, laneFail := runCancelProbe(t, "unknown", true)
	if state != model.StateCancelled {
		t.Fatalf("用户取消的终态应为 cancelled，实得 %s", state)
	}
	if kind != string(retry.Cancelled) || action != "cancel" {
		t.Fatalf("取消台账应为 cancelled/cancel，实得 %s/%s（落 unknown 即 issue #11 的原始缺陷）", kind, action)
	}
	if laneFail != 0 {
		t.Fatalf("取消不得污染车道健康度，实得 failure=%d（旧代码 RecordFailure 排在取消判定之前 ⇒ 无辜车道白吃一次冷却）", laneFail)
	}
}

func TestNoCancelSignalKeepsHonestUnknown(t *testing.T) {
	state, kind, action, laneFail := runCancelProbe(t, "unknown", false)
	if state != model.StateFailed {
		t.Fatalf("无取消信号时不得判成 cancelled（那会吞掉真实失败），实得 %s", state)
	}
	if action != "fail" {
		t.Fatalf("应走失败路径，实得 action=%s", action)
	}
	if kind != string(retry.Unknown) {
		t.Fatalf("没有任何 cause/标记时保持诚实的 unknown，不得凭空命名，实得 %s", kind)
	}
	if laneFail != 1 {
		t.Fatalf("真失败必须归因到车道，实得 %d", laneFail)
	}
}

func TestNamedFailureStillPenalizesLane(t *testing.T) {
	state, kind, _, laneFail := runCancelProbe(t, "upstream", false)
	if state != model.StateFailed {
		t.Fatalf("终态应为 failed，实得 %s", state)
	}
	if kind != string(retry.Upstream) {
		t.Fatalf("类别应保持具名 upstream_failed，实得 %s", kind)
	}
	if laneFail != 1 {
		t.Fatalf("具名真失败必须记一次车道失败，实得 %d", laneFail)
	}
}

// Package engine 是工作台的大脑：把“生成 -> 评估 -> 批准 -> 队列 -> 执行 ->
// 归档 -> 自评 -> 生成后继”串成无人干预的闭环，并负责断点续跑与自愈。
package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workbench/internal/autonomy"
	"workbench/internal/blueprint"
	"workbench/internal/chain"
	"workbench/internal/config"
	"workbench/internal/evaluate"
	"workbench/internal/executor"
	"workbench/internal/judge"
	"workbench/internal/logging"
	"workbench/internal/model"
	"workbench/internal/modelroute"
	"workbench/internal/queue"
	"workbench/internal/ratelimit"
	"workbench/internal/retry"
	"workbench/internal/store"
	"workbench/internal/templates"
	"workbench/internal/workspace"
)

// Emitter 是事件广播接口（SSE 与日志共用）。
type Emitter interface {
	Emit(ev model.Event)
}

// Engine 是工作台引擎。
type Engine struct {
	layout config.Layout
	cstore *config.Store
	store  *store.Store
	reg    *templates.Registry
	eval   *evaluate.Evaluator
	gen    *chain.Generator
	router *modelroute.Router
	sem    *judge.Client
	// runner 是执行器契约而不是具体结构体（P1-2）：便于替换实现与注入测试桩。
	runner executor.RunnerContract
	q      *queue.Queue
	emit   Emitter
	log    *logging.Logger

	mu    sync.Mutex
	phase model.EnginePhase
	ctx   context.Context
	// cancel 带 cause（issue #11）：停机时把「引擎停机」写进上下文，
	// 运行中任务经 context.Cause 继承同一原因 ⇒ 不再被下游泛错顶成 unknown。
	cancel  context.CancelCauseFunc
	wg      sync.WaitGroup
	slots   chan struct{}
	started int64
	lastErr string

	inflight   sync.WaitGroup
	runningIDs sync.Map
	stopping   atomic.Bool
	tickBusy   atomic.Bool
	// rescueSweepAt 是持续卡死救援的节流时刻（毫秒，见 runtime_rescue.go）。
	// 0 表示从未扫过。用 atomic 而非加锁：它只在 tick 单线程路径里读写，
	// 但需与 tick 的并发早退（tickBusy）语义一致地做到无锁。
	rescueSweepAt atomic.Int64
	// orphanSeen 记录「首次被判定为孤儿候选」的时刻（taskID -> ms），用于两阶段确认。
	// 由 orphanMu 保护；tick 单线程调用，但测试与未来并行调度都会并发访问。
	orphanMu   sync.Mutex
	orphanSeen map[string]int64

	// taskCancels 保存每个运行中任务的取消函数（P0-3）。
	// 改造前 CancelTask 对 running 任务只写审计、不取消子进程，等于空操作。
	taskCancels sync.Map // taskID -> taskCanceler（带 cause 的取消入口，见 cancel_cause.go）
	// cancelling 记录"按用户意图正在取消"的任务，用于区分取消与真实失败。
	cancelling sync.Map // taskID -> struct{}
	// retryPol 是"失败之后怎么办"的策略对象（P0-2），由配置热更新。
	retryPolMu sync.RWMutex
	retryPol   *retry.Policy
	// autonomy 是自主任务规划与闭环执行的状态机（目标要求 1-3）。
	// 为 nil 或配置关闭时，引擎行为与改造前完全一致（固定模板链路）。
	autonomy *autonomy.Manager
	// ws 是「AI 开发工作目录」管理器：project 模式（默认）下行为与改造前完全一致，
	// external 模式下为任务提供用户选定的真实工作目录与越界防护。
	ws *workspace.Manager
	// rateLimit 记录连续限流次数（用于整池冷却的指数退避），rateLimitMu 保护它。
	rateLimitMu sync.Mutex
	rateLimit   rateLimitState
	// pendingWarn 缓存**构造期**的降级原因（L6 接线收口，2026-10-20 实测缺口）。
	//
	// 问题形态：New() 里 e.log 先是 Nop，而 main.go 是在 New() **返回之后**才
	// SetLogger(logger)。于是构造期所有"失败只告警不阻断"的告警（审计文件打不开、
	// 自主子系统初始化失败）写进了 io.Discard —— 永久消失。注释承诺"只告警"，
	// 实际是"零可观测"：现场表现为自主链路没起来，日志里一条原因都查不到。
	// 这里先缓冲，SetLogger 时一次性冲刷；未注入日志器时保持零输出（库默认语义不变）。
	pendingWarnMu sync.Mutex
	pendingWarn   []pendingWarnRecord
	// probeSuppressor 是"限流时暂停模型探测"的回调（由 main 注入，可为 nil）。
	probeSuppressorMu sync.RWMutex
	probeSuppressor   func(seconds int, reason string)
	// poolRoutesFn 读取模型池的实时路由表（可选；未注入时用配置里的静态 models）。
	poolRoutesMu sync.RWMutex
	poolRoutesFn func() []config.ModelRoute
	// poolWaitUntil 是"模型池全部冷却"时的下次可试时刻（毫秒）。
	// 现场缺陷：池不可用时 runOne 直接把任务改回 queued 再 Push，调度循环 3 秒后
	// 又把它捞起来，形成 queued→running→queued 热循环 —— 实测 400 行审计里 394 次
	// running 迁移、0 产出，audit.jsonl 一夜涨到 2.3MB。
	poolWaitUntil atomic.Int64
	// routeLogMu/routeFingerprint/routeLogAt 是「模型路由表已更新」日志的去抖状态：
	// 模型池按模型逐个回调，不去抖会把一轮同步写成 20-30 行噪音。
	routeLogMu       sync.Mutex
	routeFingerprint string
	routeLogAt       time.Time
	// rejectBreakerAt 记录每个目标最近一次"拒绝熔断"告警时刻（按目标去抖）。
	//
	// 现场（2026-10-21）：同一目标连续 2000 条草案被评估拒绝、连续 12 小时零产出，
	// 期间**没有任何日志**说明正在空转，只能靠事后翻 queue.json 才发现。
	// 静默空转是 24/7 系统最坏的失效形态：熔断必须自己发声。
	rejectBreakerMu sync.Mutex
	rejectBreakerAt map[string]int64
}

// New 构造引擎。
func New(layout config.Layout, cstore *config.Store, st *store.Store, reg *templates.Registry, emit Emitter) (*Engine, error) {
	cfg := cstore.Get()
	e := &Engine{
		layout: layout,
		cstore: cstore,
		store:  st,
		reg:    reg,
		eval:   evaluate.New(cfg.Eval),
		gen:    chain.New(reg, cfg.Chain),
		router: modelroute.NewWithPersistence(cfg.Models, layout.RuntimeFile("models-health.json")),
		sem:    judge.New(layout, cfg.Executor),
		runner: executor.New(cfg.Executor, layout),
		q:      queue.New(),
		emit:   emit,
		phase:  model.PhaseIdle,
		slots:  make(chan struct{}, cfg.Executor.Concurrency),
		log:    logging.Nop(),
		ws:     workspace.New(layout.DataRoot, layout.Root, cfg.Workspace),

		rejectBreakerAt: map[string]int64{},
		orphanSeen:      map[string]int64{},
	}
	// 工作目录审计：允许/拒绝都落盘，越权尝试可事后复核（打开失败只告警不阻断）。
	// L6 接线收口：改造前这里 aerr 被直接丢掉，且紧随其后的 e.log 又被重设成 Nop，
	// 于是"越权审计没开"这件事在现场完全不可见 —— 面板显示 external 模式正常，
	// 但 workspace-audit.jsonl 一直是空的，事后复核无从谈起。
	if audit, aerr := workspace.OpenAudit(layout.WorkspaceAuditFile); aerr == nil {
		e.ws.SetAudit(audit)
	} else {
		e.warnDeferred("工作目录越权审计未启用（external 模式的写越界将无留痕）", "err", aerr.Error(), "file", layout.WorkspaceAuditFile)
	}
	// 外部目录模式在构造期就要把运行层沙箱放行位同步给执行器（热更新见 ReloadConfig），
	// 并把"采集工作目录上下文"的钩子挂上去（external 模式下注入 prompt）。
	e.wireWorkspaceRunner()
	e.applyWorkspaceMode()
	// 语义层（自评/摘要/判分）必须与引擎用同一个执行器（2026-10-20 实测缺陷 D7）：
	// 改造前 judge.run() 每次自己 executor.New(...)，于是语义子任务绕过引擎的
	// FailoverRunner（模型故障转移 + 车道健康度）与全局限速闸门，单测里还绕过
	// SetRunner 注入的假执行器 —— 直接后果是 go test 会真的拉起 DSH 子进程。
	// 详细取证见 judge.SetRunner 的注释。
	e.sem.SetRunner(e.runner)
	// 闸门必须在**构造期**就装上（不能只等 ReloadConfig）：
	// cmd/workbench/main.go 有第二个引擎入口（--diagnose 类一次性 CLI，只 engine.New 后直接
	// SelfTestArtifactPipeline，全程不调 ReloadConfig），那里若不装就等于该路径不限速。
	// 生产路径随后还会 ReloadConfig 再装一次：applyRateLimit 幂等（替换令牌桶而非叠加），
	// 且 New→SetLogger→ReloadConfig 之间不发请求，不会丢已在途的令牌状态。
	e.applyRateLimit(cfg.Executor)
	e.retryPol = buildRetryPolicy(cfg.Executor)
	logRetryPolicyFixes(e.log, e.retryPol)
	// 自主子系统：初始化失败只降级，不阻断引擎构造。
	e.initAutonomy()
	return e, nil
}

// SetLogger 注入日志器（P1-1）。未注入时用 Nop，保证零配置可用。
//
// 同时把日志挂成执行器钩子（P1-2）：每次尝试的开始/结束都会留一行结构化日志，
// 这就是"接口 + 钩子"的直接收益——不需要改 executor 内部实现。
func (e *Engine) SetLogger(lg *logging.Logger) {
	if lg == nil {
		lg = logging.Nop()
	}
	// 统一打上 component=engine：面板与采集侧可据此过滤，不必逐条猜来源。
	e.log = lg.At("engine")
	e.flushPendingWarn()
	e.attachAttemptLogging()
}

// pendingWarnRecord 是一条构造期降级原因。
type pendingWarnRecord struct {
	msg string
	kv  []any
}

// warnDeferred 记录"不阻断但必须可见"的降级原因。
//
// 用法边界：只用于 New() 及其被调用的构造期函数。运行期直接用 e.log.Warn ——
// 那时日志器已经装好，缓冲反而会推迟可见性。
func (e *Engine) warnDeferred(msg string, kv ...any) {
	e.pendingWarnMu.Lock()
	e.pendingWarn = append(e.pendingWarn, pendingWarnRecord{msg: msg, kv: kv})
	e.pendingWarnMu.Unlock()
}

// flushPendingWarn 把构造期缓冲的降级原因写进真实日志器（SetLogger 时调用一次）。
// 冲刷后清空：重复调用不得把同一条原因写两遍。
func (e *Engine) flushPendingWarn() {
	e.pendingWarnMu.Lock()
	pending := e.pendingWarn
	e.pendingWarn = nil
	e.pendingWarnMu.Unlock()
	for _, p := range pending {
		e.log.Warn(p.msg, p.kv...)
	}
}

// attachAttemptLogging 把「尝试开始/尝试结束」日志挂成执行器钩子（L5 收敛）。
//
// 改造前的形态是 e.runner.(*executor.Runner) —— 接口签名里带具体类型，等于没脱离
// 结构体：main.go 用 FailoverRunner 包住真实执行器后再 SetRunner，这条断言就不匹配了，
// 钩子只留在构造期那个对象上（碰巧还是同一个内层对象才没出事）。
// 现在沿 Inner() 链找 AttachHooks 能力，装不上也不报错（钩子是观测面，不影响执行）。
func (e *Engine) attachAttemptLogging() {
	hooks := executor.NewHooks(
		[]executor.BeforeRunFunc{func(ctx context.Context, task *model.Task, modelID string) error {
			e.log.Info("尝试开始", logging.Redact([]any{
				"taskId", task.ID, "model", modelID, "attempt", task.Attempts, "title", model.Truncate(task.Title, 60),
			})...)
			return nil
		}},
		[]executor.AfterRunFunc{func(ctx context.Context, task *model.Task, modelID string, res *executor.Result, err error) {
			kind := "ok"
			if err != nil {
				kind = "error"
			}
			elapsed := int64(0)
			stalled := false
			if res != nil {
				elapsed = res.Elapsed.Milliseconds()
				stalled = res.Stalled
				if res.ErrorKind != "" {
					kind = res.ErrorKind
				}
			}
			// 失败原因必须随日志走（2026-10-26 round 9 · T10）：此前只记 kind/elapsed，
			// 一次真跑的 bad_exit 事后完全无法归因 —— 现在把 stderr 摘要（或执行器错误）一并落进日志，
			// 并指向已落盘的 stderr.txt。日志是 7×24 无人值守下唯一的事实来源。
			reason := ""
			stderrPath := ""
			if res != nil {
				stderrPath = res.StderrPath
				reason = model.Truncate(res.LastError, 300)
			}
			if strings.TrimSpace(reason) == "" && err != nil {
				reason = model.Truncate(err.Error(), 300)
			}
			// 日志同样记实际执行模型（T27）：这是 7×24 无人值守下唯一的事实来源，记 selected 会误导排查方向。
			e.log.Info("尝试结束", logging.Redact([]any{
				"taskId", task.ID, "model", attemptedModel(res, modelID), "kind", kind,
				"elapsedMs", elapsed, "stalled", stalled,
				"reason", reason, "stderrPath", stderrPath,
			})...)
		}},
	)
	attachHooksToRunner(e.runner, hooks)
}

// SetRunner 替换执行器实现（P1-2 接口化的直接收益：可注入假执行器做单测）。
func (e *Engine) SetRunner(r executor.RunnerContract) {
	if r != nil {
		e.runner = r
		// 换执行器后必须重新挂工作目录钩子与沙箱开关，否则 external 模式会静默失效。
		e.wireWorkspaceRunner()
		e.applyWorkspaceMode()
		// 语义层跟着换（D7）：main.go 在这里才把 FailoverRunner 装上来，
		// 不同步的话构造期注入的裸执行器会继续留在语义层上，限速与故障转移照样绕开。
		e.sem.SetRunner(r)
		// 换执行器后重装限速闸门（D9）：闸门挂在装饰器链最内层，只换外层不重装时，
		// 新链若没有已装闸门的内层就退化成「面板显示限速、实际不限速」。
		e.applyRateLimit(e.cstore.Get().Executor)
	}
}

// SetSemanticRunner 单独替换语义层（自评/摘要/判分）使用的执行器。
//
// 与 SetRunner 的区别：SetRunner 换的是任务主链路执行器，会同时重挂工作目录钩子与
// 沙箱开关；这里只影响语义子任务。两个用途：
//  1. 生产：语义层可与主链路用不同车道（如更便宜/更短超时的模型）；
//  2. 测试：注入桩执行器，防止单元测试把真实 DSH 子进程拉起来（实测一次
//     go test 遗留 20+ 个子进程、并把 t.TempDir 的文件占用成 engine 包假红）。
func (e *Engine) SetSemanticRunner(r executor.RunnerContract) { e.sem.SetRunner(r) }

// Runner 返回当前执行器实现（模型池装配故障转移装饰器时需要拿到底层实例）。
func (e *Engine) Runner() executor.RunnerContract { return e.runner }

// RateLimiterSnapshot 返回全局请求速率闸门的状态（面板/诊断用）。
//
// 2026-10-19 事故后新增：闸门本身也必须可观测 —— 否则「限速是否真的在工作」
// 又一次变成靠日志猜的黑盒。返回零值表示未装闸门（不限速）。
func (e *Engine) RateLimiterSnapshot() ratelimit.Snapshot {
	// 注意装饰器链（2026-10-19 实测踩到）：main 里先把限流器装在内层
	// *executor.Runner 上，随后用 FailoverRunner 包住它再 SetRunner。
	// 于是引擎持有的是外层装饰器，直接断言 limiter 接口会拿到零值，
	// 表现为"配置 20/分钟但快照全零"——闸门其实在工作，只是读不到。
	// 这里按"自身 -> 内层 Runner"两级查找。
	if ls, ok := e.runner.(interface {
		LimiterSnapshot() ratelimit.Snapshot
	}); ok {
		if s := ls.LimiterSnapshot(); s.RatePerSecond > 0 {
			return s
		}
	}
	// 内层执行器（FailoverRunner.Inner 暴露的契约）。
	if inner, ok := e.runner.(interface {
		Inner() executor.RunnerContract
	}); ok {
		if ls, ok2 := inner.Inner().(interface {
			LimiterSnapshot() ratelimit.Snapshot
		}); ok2 {
			return ls.LimiterSnapshot()
		}
	}
	return ratelimit.Snapshot{}
}

// SetPoolRoutes 注入「当前模型池路由表」读取器（由 main 装配，模型池为可选子系统）。
func (e *Engine) SetPoolRoutes(fn func() []config.ModelRoute) {
	e.poolRoutesMu.Lock()
	e.poolRoutesFn = fn
	e.poolRoutesMu.Unlock()
}

// poolRoutes 返回模型池当前路由表（未接入时为空，回落静态配置）。
func (e *Engine) poolRoutes() []config.ModelRoute {
	e.poolRoutesMu.RLock()
	fn := e.poolRoutesFn
	e.poolRoutesMu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

// ReloadModelRoutes 用模型池投影出的路由表热替换调度器的模型清单。
//
// 与 ReloadConfig 的分工：ReloadConfig 读的是「用户手写在 config.Models 里的模型」；
// 本方法是「模型池探测/同步后的实时清单」，优先级更高（池子是把所有接口统一纳管的账本）。
// 池子为空时保持原配置不变，避免探测启动早期把调度器清空。
func (e *Engine) ReloadModelRoutes(routes []config.ModelRoute) {
	if len(routes) == 0 {
		return
	}
	e.router.UpdateRoutes(routes)
	// 去抖（现场实测）：模型池每同步/探测一个模型就回调一次路由表，启动阶段单轮能触发
	// 20-30 条"路由表已更新"，把真正有用的任务/切换日志挤出可读窗口（24h 运行更严重）。
	// 这里只在"清单指纹变化"或距上次记录超过 2 秒时记一行，信息量不减、噪音归零。
	fp := routeFingerprint(routes)
	e.routeLogMu.Lock()
	changed := fp != e.routeFingerprint
	quiet := time.Since(e.routeLogAt) < 2*time.Second
	if changed || !quiet {
		e.log.Info("模型路由表已更新", "source", "modelpool", "routes", len(routes), "changed", changed)
		e.routeFingerprint = fp
		e.routeLogAt = time.Now()
	}
	e.routeLogMu.Unlock()
}

// routeFingerprint 生成路由表指纹（模型 ID + tier + 权重），用于"变了才记日志"。
func routeFingerprint(routes []config.ModelRoute) string {
	var b strings.Builder
	for _, r := range routes {
		b.WriteString(r.Model)
		b.WriteByte('|')
		b.WriteString(r.Tier)
		b.WriteByte('|')
		b.WriteString(fmt.Sprint(r.Weight))
		b.WriteByte(';')
	}
	return b.String()
}

// Logger 返回当前日志器。
func (e *Engine) Logger() *logging.Logger { return e.log }

// policy 返回当前重试策略（读锁保护，支持热更新）。
func (e *Engine) policy() *retry.Policy {
	e.retryPolMu.RLock()
	defer e.retryPolMu.RUnlock()
	if e.retryPol == nil {
		return retry.Default()
	}
	return e.retryPol
}

// ── 生命周期 ─────────────────────────────────────────────────────

// Start 启动引擎主循环。phase 变为 running。
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.phase == model.PhaseRunning {
		// 幂等分支也必须**对齐磁盘**（2026-10-19 现场缺陷）。
		//
		// 现场：老大要 7x24，我为换二进制执行了 --stop（CLI 在**另一个进程**里自开 store、
		// 盲写 stopped，不知道内存里引擎仍在 running），随后用面板 POST /api/engine/start
		// 让内存回到 running。结果：
		//   磁盘 state.json = stopped（被 CLI 写的）
		//   内存 phase      = running
		// 而这条 early-return **不落盘**，于是分歧**永久化**、再也纠正不过来。
		//
		// 为什么对 7x24 致命：重启后的接续判据 shouldAutoStart(cfg, phase, resumeMarked)
		// 读的是**磁盘** state.json。磁盘说 stopped => 重启后不自启 => 无人值守断在这一步。
		//
		// 修法：幂等分支也把磁盘对齐到 running。磁盘可能被别的进程写坏，
		// 这里以「内存真实相位」为准做自愈。
		if st := e.store.State(); st.Phase != model.PhaseRunning {
			st.Phase = model.PhaseRunning
			st.StartedAt = e.started
			st.StoppedAt = 0
			_ = e.store.SaveState(st)
		}
		// 幂等分支同样要清「用户主动退出」标记：
		// 用户点「开始」的语义是「我要它跑」，与内存是否已是 running 无关。
		// （测试实测：只在正常分支清会被这条 early-return 跳过，标记残留。）
		e.clearUserQuit()
		return nil
	}
	// 用户点「开始」= 恢复运行 = 恢复正常守护语义（清掉主动退出标记）。
	e.clearUserQuit()
	e.ctx, e.cancel = context.WithCancelCause(context.Background())
	e.phase = model.PhaseRunning
	e.started = model.Now()
	e.stopping.Store(false)
	st := e.store.State()
	st.Phase = model.PhaseRunning
	st.StartedAt = e.started
	st.StoppedAt = 0
	if err := e.store.SaveState(st); err != nil {
		return err
	}
	e.bootstrapAutonomy()
	e.wg.Add(1)
	go e.loop(e.ctx)
	e.log.Info("引擎已启动", "concurrency", e.cstore.Get().Executor.Concurrency, "phase", string(e.phase), "autonomy", e.autonomyStatusLine())
	e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: "引擎已启动"})
	return nil
}

// MarkUserQuit 记录「用户主动退出」（--quit / 面板退出）。
//
// 为什么必须有这个显式标记（2026-10-19 现场）：
//
//	外部守护者（tests/keepalive.ps1）的分支是「stopped 或 idle 都拉回 running」，
//	它**区分不出**「意外停机」与「用户主动退出」——
//	于是用户 --quit 后 60 秒内又被拉起来，--quit 等于不生效。
//
// 产品侧先把这件事表达出来，外部才有据可依；同时落一个 flag 文件，
// 让 shell/PS 一行即可判断（不必解析 JSON）。
//
// 清除点：Start()（用户点「开始」= 恢复运行 = 恢复守护语义）。
func (e *Engine) MarkUserQuit() error {
	st := e.store.State()
	st.UserQuit = true
	if err := e.store.SaveState(st); err != nil {
		return err
	}
	flag := filepath.Join(e.layout.RuntimeDir, "user-quit.flag")
	if err := os.MkdirAll(e.layout.RuntimeDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(flag, []byte("user quit at "+fmt.Sprint(model.Now())+"\n"), 0o644)
}

// clearUserQuit 清掉「用户主动退出」标记（用户点「开始」时调用）。
func (e *Engine) clearUserQuit() {
	if st := e.store.State(); st.UserQuit {
		st.UserQuit = false
		_ = e.store.SaveState(st)
	}
	_ = os.Remove(filepath.Join(e.layout.RuntimeDir, "user-quit.flag"))
}

// Stop 优雅停止：分两段 drain（对标 riverqueue/river 的 SoftStopTimeout / StopAndCancel）。
//
// 第一段（软停）：立刻停止取新任务，等在跑任务自然收尾，最多等到宽限期；
// 第二段（硬停）：宽限期到了仍在跑的任务，取消其执行上下文并等待子进程树被杀。
//
// 改造前只有一个 30s + 60s 的"等"，到期直接返回却留下还在跑的子进程，
// 这次把"到期强制取消"补上（P0-3）。
func (e *Engine) Stop() error {
	// 用户主动停机：磁盘留 stopped，且**不**希望自愈重启后接续。
	return e.stop(false)
}

// AbortForSelfHeal 是「自愈停机」：与用户主动停机语义不同。
//
// 背景（2026-10-19 现场缺陷）：自愈链路是
// supervisor 越阈值 -> OnHeal -> eng.Stop() -> SelfHeal 拉起新进程 -> 旧进程 os.Exit(0)。
// 而 Stop() 把 phase 落盘成 stopped；新进程的自动接续判据只看
// (running|paused)，于是 stopped 不在允许集合里 —— **自愈只重启了壳，不恢复干活**。
//
// 两者的真实差别是「停机前是否确实在跑」：若在跑，说明这是被动中断（崩溃/自愈/被杀），
// 应当接续；若本来就没跑（idle/已 stopped），则是用户主动停机，应当尊重。
// 因此这里额外落一个 resumeAfterRestart 标记，供下次启动判定。
func (e *Engine) AbortForSelfHeal(reason string) error {
	e.log.Warn("自愈停机（重启后应自动接续）", "reason", model.Truncate(reason, 160))
	return e.stop(true)
}

func (e *Engine) stop(selfHeal bool) error {
	e.mu.Lock()
	cancel := e.cancel
	wasRunning := e.phase == model.PhaseRunning || e.phase == model.PhasePaused
	e.phase = model.PhaseStopped
	e.mu.Unlock()
	e.stopping.Store(true)
	if cancel != nil {
		// 具名 cause：让所有在跑任务经 context.Cause 读到「引擎停机」，
		// 与「用户终止该任务」区分开（两者运维含义相反：一个要接续、一个不要）。
		cancel(causeEngineShutdown) // 让调度循环退出，不再取新任务
	}
	if wasRunning {
		done := make(chan struct{})
		go func() { e.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(30 * time.Second):
		}
	}
	grace := time.Duration(e.cstore.Get().Executor.ShutdownGraceSeconds) * time.Second
	if grace <= 0 {
		grace = 20 * time.Second
	}
	fin := make(chan struct{})
	go func() { e.inflight.Wait(); close(fin) }()
	hardStop := false
	select {
	case <-fin:
	case <-time.After(grace):
		// 宽限期到：强制取消所有仍在跑的任务，再给 15s 收尾
		hardStop = true
		e.emitEvent(model.Event{Type: "warn", At: model.Now(), Msg: "停机宽限期已到，正在强制取消在跑任务"})
		e.cancelAllRunning()
		select {
		case <-fin:
		case <-time.After(15 * time.Second):
		}
	}
	st := e.store.State()
	st.Phase = model.PhaseStopped
	st.StoppedAt = model.Now()
	// 只对「自愈/被动中断」落接续标记，且仅在**停机前确实在跑**时落：
	// 用户主动停机（wasRunning=false，或走了 Stop()）不落，避免把用户意愿覆盖掉。
	if selfHeal && wasRunning {
		st.ResumeAfterRestart = true
		// 重启次数必须真实累计（2026-10-19 现场缺陷）。
		//
		// 现场：EngineState.RestartCount 在整个代码库里**只出现在结构体定义处**，
		// 从未被任何代码写入 —— state.json 永远显示 restartCount: 0。
		//
		// 危害（7x24 视角）：重启次数是判断「系统是否在反复崩溃」的核心健康指标。
		// 恒为 0 会掩盖自愈频繁重启：运维看面板以为一直稳，实际可能每次内存/句柄
		// 超阈值都在重启（只是「壳活着」）。
		//
		// 语义边界：只在「自愈且停机前确实在跑」时 +1 —— 用户主动停机不是重启。
		st.RestartCount++
	} else {
		// 任何一次正常（用户）停机都清掉标记，防止旧标记把「用户停过」又拉起来。
		st.ResumeAfterRestart = false
	}
	// state 落盘失败必须上报，但**不得阻断** Savepoint：队列快照写的是另一个文件（QueueFile），
	// 是停机的另一半 —— 用 errors.Join 合并两半的错误，而不是提前 return 丢掉另一半。
	// 相位没落上时也不发「引擎已停止」事件：那会让面板宣称一个磁盘上没有的状态。
	var stateErr error
	if err := e.store.SaveState(st); err != nil {
		stateErr = fmt.Errorf("落盘停止相位失败（重启后接续判据读到旧相位）: %w", err)
		e.log.Warn("引擎相位落盘失败", "err", err.Error(), "hardStop", hardStop)
	} else {
		msg := "引擎已停止"
		if hardStop {
			msg = "引擎已停止（宽限期到期，已强制取消在跑任务）"
		}
		e.log.Info("引擎已停止", "hardStop", hardStop, "graceSeconds", int(grace.Seconds()))
		e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: msg})
	}
	return errors.Join(stateErr, e.store.Savepoint())
}

// cancelAllRunning 取消所有在跑任务的执行上下文（停机兜底）。
func (e *Engine) cancelAllRunning() {
	e.taskCancels.Range(func(k, v any) bool {
		// 断言类型必须与 :957 的 Store 一致（taskCanceler）—— 不一致时这里 ok=false，
		// 表现为「按了终止但没取消」且零日志零报错。该一致性由
		// cancel_cause_contract_test.go 的源文本不变量钉住（issue #11）。
		if cancel, ok := v.(taskCanceler); ok {
			if id, ok := k.(string); ok {
				e.cancelling.Store(id, struct{}{})
			}
			cancel.Cancel(causeEngineShutdown)
		}
		return true
	})
}

// Pause 暂停取新任务（在跑任务继续完成）。
func (e *Engine) Pause() error {
	e.mu.Lock()
	if e.phase != model.PhaseRunning {
		e.mu.Unlock()
		return fmt.Errorf("引擎当前不是运行中（%s）", e.phase)
	}
	e.phase = model.PhasePaused
	e.mu.Unlock()
	st := e.store.State()
	st.Phase = model.PhasePaused
	st.PausedAt = model.Now()
	// 相位没落盘就不对用户宣布成功：重启后的接续判据 shouldAutoStart 读的是磁盘 state.json
	//（见 Start() 的注释）。磁盘仍是 running ⇒ 用户以为暂停了、7×24 重启后实例自己跑起来。
	// 内存相位不回滚，与 L435 的严格点同法：先改后落盘，落盘失败即返回错误。
	if err := e.store.SaveState(st); err != nil {
		return fmt.Errorf("落盘暂停相位失败: %w", err)
	}
	e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: "引擎已暂停"})
	return nil
}

// Resume 从暂停恢复。
func (e *Engine) Resume() error {
	e.mu.Lock()
	if e.phase != model.PhasePaused {
		e.mu.Unlock()
		return fmt.Errorf("引擎当前不是暂停态（%s）", e.phase)
	}
	e.phase = model.PhaseRunning
	e.mu.Unlock()
	st := e.store.State()
	st.Phase = model.PhaseRunning
	// 同 Pause：相位没落盘就不能对用户宣布成功（磁盘仍是 paused ⇒ 重启后不会自动接续）。
	if err := e.store.SaveState(st); err != nil {
		return fmt.Errorf("落盘恢复相位失败: %w", err)
	}
	e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: "引擎已恢复"})
	return nil
}

// Phase 返回当前阶段。
func (e *Engine) Phase() model.EnginePhase {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.phase
}

// LastError 返回最近一次错误描述。
func (e *Engine) LastError() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastErr
}

// Recover 启动时重建内存状态并收敛悬挂任务（断点续跑）。
func (e *Engine) Recover() error {
	n, err := e.store.Reconcile()
	if err != nil {
		return err
	}
	// 终态任务记录裁剪（2026-10-23 新增，治「queue.json 只增不减」）。
	//
	// 位置讲究：必须在 Reconcile **之后**（先把悬挂 running 收敛成 interrupted，
	// 否则一条残留在 running 的任务会挡住整轮裁剪 —— 虽然它本来也不该被删，
	// 但收敛后它变成终态，语义更清楚），且在 List/队列重建**之前**
	// （后续建队列用的是裁剪后的集合，不会把待删记录又塞回队列）。
	//
	// 为什么放在启动期而不是每 tick：裁剪要全量序列化一次快照（59MB -> 数 MB），
	// 放 tick 里会周期性抖内存；启动期一次足够（一天最多重启几次，而记录增长是渐进的）。
	// 失败不阻塞启动：裁剪是资源治理，不是正确性必需 —— 但必须留痕，不能静默。
	if pr, perr := e.store.PruneTasks(e.cstore.Get().Retention, false); perr != nil {
		e.log.Warn("终态任务记录裁剪失败（不影响启动，记录会继续增长）", "err", perr.Error())
	} else if pr.RemovedByAge+pr.RemovedByCap > 0 {
		// 只有真删了才记录一条事件，避免每次启动都刷一条「删了 0 条」的噪音。
		e.emitEvent(model.Event{Type: "engine", At: model.Now(),
			Msg: fmt.Sprintf("启动期裁剪终态任务记录：按龄 %d 条 + 按上限 %d 条，现存 %d 条，清孤立链 %d 条",
				pr.RemovedByAge, pr.RemovedByCap, pr.Kept, pr.RemovedChains)})
	}
	all := e.store.List()
	cfg := e.cstore.Get()
	// 评估器窗口
	e.eval.Seed(all, 400)
	// 队列重建：queued + approved 入队；interrupted 重新入队
	var requeue []*model.Task
	for _, t := range all {
		switch t.State {
		case model.StateQueued:
			requeue = append(requeue, t)
		case model.StateApproved:
			if _, err := e.store.Transition(t.ID, model.StateQueued, "system", "启动恢复入队"); err == nil {
				if nt, ok := e.store.Get(t.ID); ok {
					requeue = append(requeue, nt)
				}
			}
		case model.StateInterrupted:
			// L4 卡死救援：不再无条件重新入队。崩溃恢复必须消耗"救援额度"，
			// 否则"一跑就弄死进程"的任务会配合 KeepAlive 每分钟拉起形成无界崩溃循环。
			// 额度与分流见 stuck_rescue.go。
			if nt := e.rescueInterrupted(t); nt != nil {
				requeue = append(requeue, nt)
			}
		}
	}
	e.q.Reset(requeue)
	// 链元数据补齐（P1 修复）：改造前 UpsertChain 只在 closeChain 里调用，而链
	// 从未被创建 → queue.json 里 chains 恒为 0，链深度/扇出只残留在 task 字段，
	// 面板与治理都读不到"这条线干了什么"。这里按 chainId 反推补齐历史链。
	e.backfillChains(all)
	// 自主目标恢复（闭环续跑）：已有活跃目标直接复用；已完成则推导下一目标。
	e.bootstrapAutonomy()
	// 刷新路由/评估/链配置
	//
	// 启动期核对（round 124 · T79）：这里把 router 换成「手写的 config.Models」，正是事故路径 ——
	// 我在 round 72 给这些 id 加了渠道前缀，而池账本用裸 id，结果 6 条路线在池里没有 spec，
	// 失败记不到车道、冷却失效，一小时内空转 629 次且面板看着一切正常。只告警不拒启动。
	e.checkConfiguredLaneIDs(cfg)
	e.router.UpdateRoutes(cfg.Models)
	e.eval.UpdateConfig(cfg.Eval)
	e.gen.UpdateConfig(cfg.Chain)
	e.sem.UpdateConfig(cfg.Executor)
	e.runner.UpdateConfig(cfg.Executor)
	e.applyRateLimit(cfg.Executor)
	e.retryPolMu.Lock()
	e.retryPol = buildRetryPolicy(cfg.Executor)
	e.retryPolMu.Unlock()
	logRetryPolicyFixes(e.log, e.retryPol)
	if n > 0 {
		e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: fmt.Sprintf("恢复了 %d 个中断任务", n)})
	}
	return nil
}

// ── 主循环 ───────────────────────────────────────────────────────

func (e *Engine) loop(ctx context.Context) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			e.mu.Lock()
			e.lastErr = fmt.Sprintf("调度循环 panic: %v", r)
			e.mu.Unlock()
		}
	}()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if e.Phase() != model.PhaseRunning {
				continue
			}
			e.tick(ctx)
		}
	}
}

// tick 是一次调度推进：补候选 -> 取任务 -> 并发执行。
func (e *Engine) tick(ctx context.Context) {
	if !e.tickBusy.CompareAndSwap(false, true) {
		return // 上一轮还没跑完，跳过本次，避免调度循环堆叠
	}
	defer e.tickBusy.Store(false)
	cfg := e.cstore.Get()
	// 0) 模型池冷却闸门：整池不可用时既不补候选也不取任务，直接等到最早可试时刻。
	// 没有这道闸门时，池冷却期间任务被反复捞起又放回，空转刷满审计（见 struct 注释）。
	now := model.Now()
	// 0.2) 持续卡死救援（2026-10-22 对标 river JobRescuer 的周期轮询）。
	//
	// 位置讲究：必须放在**池冷却早退之前**。否则整池冷却期间（现场可达 30 分钟）
	// 调度循环每 3 秒到这里就 return，扫不到台账 —— 而"假 running"正是最需要
	// 在这种停摆窗口里被发现的问题。
	//
	// 节流 30 秒：tick 本身 3 秒一次，扫描是 O(任务数) 内存遍历，不必每次都扫。
	// 判定与安全边界见 runtime_rescue.go。
	if last := e.rescueSweepAt.Load(); now-last >= rescueSweepIntervalMs {
		e.rescueSweepAt.Store(now)
		if ids := e.sweepOrphanedRunning(now); len(ids) > 0 {
			e.emitEvent(model.Event{Type: "warn", At: now,
				Msg: "台账发现 " + strconv.Itoa(len(ids)) + " 条假 running 孤儿任务，已回收（详见审计台账）"})
		}
	}
	// 0) 模型池冷却闸门：整池不可用时既不补候选也不取任务，直接等到最早可试时刻。
	// 没有这道闸门时，池冷却期间任务被反复捞起又放回，空转刷满审计。
	// （闸门放在救援扫描之后：见上方 0.2 的位置说明。）
	if until := e.poolWaitUntil.Load(); until > now {
		return
	}
	// 0.5) 闸门到点即视为上次冷却窗口已经走完，退避层级归零（2026-10-21 现场修复）。
	//
	// 根因（用 20:0x~22:3x 的冷却日志复算出来的）：连续 429 会把闸门按
	// 5min→10min→20min→30min 递增，而层级只在"成功一次"时清零 —— 可闸门按住的
	// 正是唯一能产生成功的那条路。于是每次开闸后的第一批请求必然又吃到 429，
	// 层级只会往上爬，**永久钉在 30 分钟档**：现场 22:19 设的闸门落在 22:39，
	// 期间 14 条任务一条没跑。这不是"上游还没恢复"，是本机把恢复探测的间隔
	// 自己放大了 6 倍，把"限流窗口"人为拖成了"半天没产出"。
	//
	// 到点归零 = 每个冷却窗口都是一次独立、代价极低的恢复探测（一次 Pick + 一轮
	// 请求，见 requeueForPoolWait 注释）；上游若仍在限流，下一次 429 会重新建立
	// 退避层级，一个来回只多付一次请求。真退避（同一窗口内的连续 429 递增）不变，
	// 变的是"窗口结束后不要继续按 30 分钟按住自己"。
	if e.rateLimit.consecutive > 0 {
		e.endCooldownEpisode()
	}
	// 1) 只在队列**空**时补充候选任务。
	//
	// 为什么不按 concurrency 水位补：tick 由调度循环高频调用，队列一旦被取空会立刻
	// 在下一轮补上，天然维持并发；而"队列还有货时又补一批"会让"闸门过期后任务是否
	// 真的被取走"无法观测（实测：期望 1 条在跑，实际堆成 2 条），也违背"队列是唯一
	// 真值来源"的设计——补货不该改变已被观测到的队列长度。
	if e.q.Len() == 0 {
		e.supplyCandidates(ctx, cfg)
	}
	// 2) 尽可能填满并发槽
	for {
		select {
		case e.slots <- struct{}{}:
		default:
			return
		}
		t := e.q.Pop()
		if t == nil {
			<-e.slots
			return
		}
		e.inflight.Add(1)
		go func(task *model.Task) {
			defer e.inflight.Done()
			defer func() { <-e.slots }()
			defer func() {
				if r := recover(); r != nil {
					e.mu.Lock()
					e.lastErr = fmt.Sprintf("任务执行 panic: %v", r)
					e.mu.Unlock()
					// 具名类别 retry.TaskPanic（对标 asynq PanicError：panic 单独成类型，绝不落进
					// 「原因不明」桶）—— 否则面板的 unknown 桶会混进「代码 bug」与「环境问题」两种事实。
					e.failTask(task.ID, fmt.Errorf("执行 panic: %v", r), retry.TaskPanic)
				}
			}()
			e.runOne(ctx, task)
		}(t)
	}
}

// recentTopicWindow 是 RootTask 排除近期主题的回溯条数：24 < 单域池 42（取整窗口 400 会抽干池 ⇒ T32-c 饥饿），既保底每域 18 条可选又压住同域近期连续重复。
const recentTopicWindow = 24

// topicFromTitle 取「<模板名> · <主题>」的主题段（无分隔符则整条）：pickTopic 比的是池内主题字面量，窗口存的却是标题，不转换则排除恒不匹配（chain 签名不在 T36 白名单）。
func topicFromTitle(title string) string {
	if i := strings.Index(title, "·"); i >= 0 {
		return strings.TrimSpace(title[i+len("·"):])
	}
	return strings.TrimSpace(title)
}

// supplyCandidates 生成并评估新任务。策略：优先从模板池轮转，其次延续链。
func (e *Engine) supplyCandidates(ctx context.Context, cfg config.Config) {
	// 0) 自主链路优先（目标要求 1）：先由项目目标推导阶段任务；
	//    规划不出（目标完成/阻塞/预算耗尽/边界拒绝）时自动回落到下面的模板池，
	//    保证「自主可关、模板永在」的兼容性。
	if e.supplyAutonomous(ctx, cfg) {
		return
	}
	enabled := e.reg.Enabled()
	if len(enabled) == 0 {
		return
	}
	all := e.store.List()
	// 统计今天已生成数量。
	//
	// 口径必须与 tasksToday() 完全一致（2026-10-21 现场缺陷）：两处若一处含 rejected
	// 一处不含，会出现"自主链路以为还有额度、模板链路以为已满"的错位，
	// 或者反过来"两条链同时以为满了"而整机静默 —— 后者正是本轮 12 小时零产出的直接成因。
	// 因此这里不再就地重算，统一走 tasksToday()。
	daily := e.tasksToday()
	if daily >= cfg.Eval.DailyCap {
		return
	}
	// 选模板：优先从未使用/冷却结束的模板中选，避免同域扎堆
	candidates := make([]*model.Task, 0, 3)
	now := model.Now()
	// 近期主题整批映射（口径见 recentTopicWindow 注释），供 RootTask 排除。
	recent := make([]string, 0, recentTopicWindow)
	for _, title := range e.eval.RecentTitles(recentTopicWindow) {
		if topic := topicFromTitle(title); topic != "" {
			recent = append(recent, topic)
		}
	}
	for _, tpl := range enabled {
		if len(candidates) >= cfg.Executor.Concurrency {
			break
		}
		task := e.gen.RootTask(tpl, now, recent...)
		// 池尽（该域近期主题全覆盖）时跳过：不评估不落盘，窗口滑出后自然恢复。
		if task == nil {
			continue
		}
		// 若当日该模板已用过且密集，降低优先级而非硬拒
		in := evaluate.Input{Task: task, Template: &tpl, History: all, Now: now, DailyCount: daily}
		scores := e.eval.Evaluate(in)
		// 语义判分在后台异步融合（单次 LLM 调用 30s+，绝不能在调度循环里同步等待）
		if cfg.Eval.SemanticOn {
			scores.Semantic = true
			scores.Degraded = true // 先标降级，语义结果回来后再升级
		}
		task.Scores = scores
		if !evaluate.Approve(scores) {
			task.State = model.StateRejected
			if err := e.store.Create(task); err != nil {
				e.log.Warn("落盘被拒任务失败（面板与持久态会不一致）", "taskId", task.ID, "err", err.Error())
			}
			e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: task, Msg: "任务被评估拒绝"})
			continue
		}
		candidates = append(candidates, task)
	}
	// 落库并直接入队（自动批准）
	for _, task := range candidates {
		if err := e.store.Create(task); err != nil {
			continue
		}
		if _, err := e.store.Transition(task.ID, model.StateApproved, "engine", "自动批准"); err != nil {
			continue
		}
		if q, err := e.store.Transition(task.ID, model.StateQueued, "engine", "入队"); err == nil {
			e.q.Push(q)
			e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: q.ID, Data: q, Msg: "任务已入队"})
		}
	}
}

// runOne 执行单个任务的完整生命周期。
func (e *Engine) runOne(parent context.Context, task *model.Task) {
	cfg := e.cstore.Get()
	// 并发取消：引擎停止时不再开新任务。
	// WithCancelCause（issue #11）：取消必须带具名原因 —— 停机/用户终止/超时三者在
	// context.Canceled 这一个错误上是**混在一起**的，只靠 err 文本分不开（river 同判，
	// 见 job_executor.go:590-597 用 cause 做第二维）。父 ctx 已带 cause 时，子 ctx 的
	// context.Cause 会继承父的 cause ⇒ 停机路径无需逐任务传参就能归因。
	ctx, cancel := context.WithCancelCause(parent)
	// defer 释放：本 ctx 派生自引擎根 ctx，不调 cancel 会把它一直挂在父 ctx 的子表里
	// （泄漏 + go vet lostcancel）。cancel(nil) 按 Go 文档等价于 cancel(context.Canceled)，
	// 但它**只在 runOne 返回时**执行 —— 函数体内所有分类判定都已跑完，因此不会把正常收尾
	// 误分类成取消（对照：TestCancellationDoesNotPenalizeLane 的「真失败」正控分支要求终态必须是
	// failed/bad_exit，若这里语义被污染，那条立刻判红）。
	// 具名 cause（停机 / 用户终止）都在返回**之前**由显式路径写入，而 cause 只记第一次，
	// 所以这里的 nil 不会覆盖已写好的归因。
	defer cancel(nil)

	running, err := e.store.Transition(task.ID, model.StateRunning, "engine", "开始执行")
	if err != nil {
		e.q.ReleaseChain(task.ChainID)
		// 状态迁移失败是**本地状态机/存储**的事实（非法迁移或写盘失败），既不是模型问题也不是任务内容
		// 问题；落 unknown 会把排查带去查车道。判不可重试：同因必再失败。
		e.failTask(task.ID, fmt.Errorf("状态迁移失败: %w", err), retry.TransitionFailed)
		return
	}
	e.runningIDs.Store(task.ID, true)
	// 注册取消入口：面板"终止"能真正杀掉该任务的子进程树（P0-3）。
	// 存具名类型 taskCanceler 而非裸 func：取消时强制给 cause（不许静默无归因），
	// 且存取两侧形状唯一 —— 裸 func 的类型断言失败会**静默不取消**（最坏失效形态）。
	e.taskCancels.Store(task.ID, taskCanceler{cause: cancel})
	defer func() {
		e.taskCancels.Delete(task.ID)
		e.cancelling.Delete(task.ID)
		e.runningIDs.Delete(task.ID)
		e.q.ReleaseChain(running.ChainID)
	}()
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: running, Msg: "任务开始执行"})
	e.store.AppendTaskEvent(task.ID, model.Event{Type: "start", At: model.Now(), TaskID: task.ID})

	modelID := ""
	var lastErr error
	lastKind := retry.Unknown
	// 本任务内已经失败过的*模型*，重试时排除它们（注意：这里存的是模型 ID，不是错误类别）
	failedModels := map[string]bool{}
	pol := e.policy()
	maxAttempts := pol.MaxRetries + 1
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// 退避等待由策略给出（P0-2）：旧配置走列表、新配置走指数退避 + 抖动
			delay := pol.Decide(lastKind, attempt-1, "").Delay
			if delay <= 0 {
				delay = retry.DefaultBaseDelay
			}
			select {
			case <-ctx.Done():
				// 退避期被取消：同样走具名归因（这里没有 res/runErr，cause 是唯一信息源）。
				_, cText, _ := classifyCancellation(ctx, e.isCancelling(task.ID), lastKind)
				e.finishCancelled(task.ID, lastErr, cText)
				return
			case <-time.After(delay):
			}
			if _, err := e.store.Update(task.ID, func(t *model.Task) error { t.Attempts = attempt; return nil }); err != nil {
				_ = err
			}
		}
		// 选模型：按模板 tier 需求，排除本任务内已失败过的模型
		tier := "balanced"
		if tpl, ok := e.reg.Get(task.TemplateID); ok {
			tier = tpl.Params.ModelTier
		}
		picked, pickErr := e.router.Pick(tier, failedModels)
		if pickErr != nil {
			// 模型池暂时不可用不是任务的错：退回队列等待冷却结束，避免连锁判死。
			// 关键（P1 热循环修复）：必须走合法的 running→queued 迁移而不是直接改字段，
			// 并给整池设置"下次可试时刻"，否则调度循环 3 秒后又会把同一批任务捞起来，
			// 形成 queued→running→queued 空转（实测 394/400 行审计是这种迁移、0 产出）。
			e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID, Msg: "模型池不可用，任务重新排队"})
			// 判死原因已由 requeueForPoolWait 自己写进台账与日志，这里不再补话（否则又是「已死还报排队」）。
			_ = e.requeueForPoolWait(task.ID, running.ChainID)
			return
		}
		modelID = picked
		startedAt := time.Now()
		// 工作目录：external 模式下写进任务（持久化留痕），执行器据此把 cwd 指向用户目录。
		workRoot, sandboxed := e.ws.TaskWorkDir(task.ID)
		if !sandboxed {
			task.WorkRoot = workRoot
			if _, uerr := e.store.Update(task.ID, func(t *model.Task) error { t.WorkRoot = workRoot; return nil }); uerr != nil {
				e.log.Warn("写入任务工作目录失败", "taskId", task.ID, "err", uerr.Error())
			}
		}
		// ── 方案阶段（双角色闭环的第一跳）──────────────────────────
		//
		// 立规（老大 2026-10-21）：思考模型只出方案，agent 模型负责执行。
		// 改造前这里直接把 task.Prompt 喂给选中的模型 —— 方案与执行挤在一次调用里，
		// 规划没有约束力（小模型会把规划写成开头一段废话然后直接开写），
		// 且验收失败后只能"同一模型再抄一遍"，方案层面的错永远修不掉。
		//
		// 现在的顺序：方案模型出结构化方案 → 渲染成执行指令 → 执行模型落地。
		// 方案失败不阻塞执行（退化直执行 + 审计），因为通道层的 429 不是任务的错。
		execTask := task
		// 方案阶段封顶（round 63 · T64）：逐轮重付方案调用是配额黑洞（现场实测占 ~50%），
		// 到顶后直接执行 —— 方案不是交付的必要条件（退化直执行本就是既有语义），但**反复重付**是。
		// 留痕不靠静默跳过：日志 + 事件都写清是「到顶」而不是「没跑」。
		if cfg.Plan.Enabled && task.PlanAttempts >= planAttemptCap {
			e.log.Info("方案尝试已达上限，直接执行", "taskId", task.ID, "planAttempts", task.PlanAttempts, "cap", planAttemptCap)
			e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID,
				Msg: fmt.Sprintf("方案已尝试 %d 次（上限 %d），本轮直接执行", task.PlanAttempts, planAttemptCap)})
		}
		if cfg.Plan.Enabled && task.PlanAttempts < planAttemptCap {
			// 先记账再调用：即使 buildPlan 中途失败，这次"花掉的配额"也要留下痕迹。
			if _, uerr := e.store.Update(task.ID, func(t *model.Task) error { t.PlanAttempts++; return nil }); uerr != nil {
				e.log.Warn("方案尝试计数写入失败", "taskId", task.ID, "err", uerr.Error())
			}
			lastFailure := planFailureContext(task, cfg)
			planRound := task.RepairRounds
			if planRound < 0 {
				planRound = 0
			}
			pst := e.buildPlan(ctx, task, cfg, lastFailure, planRound)
			if pst.usable() {
				rendered := blueprint.RenderTask(pst.Blueprint, task.Title, task.Prompt)
				if cfg.Plan.AppendPlanToPrompt {
					execTask = cloneTaskWithPrompt(task, rendered)
				}
				e.savePlan(task, pst, blueprint.PlanPrompt(e.autonomyGoalIntentFor(task.GoalID), task.Title, task.Prompt, effectiveMinWords(minWordsOf(e, task), attempt), lastFailure), planRound)
				e.store.AppendAttempt(store.AttemptRecord{
					TaskID: task.ID, Attempt: attempt, Model: pst.Model, Kind: "plan",
					Action: "done", ElapsedMs: pst.ElapsedMs,
					Error:      "方案：" + pst.Blueprint.SummaryLine(),
					StderrPath: pst.StderrPath, ProducedRunes: pst.ProducedRunes, DSHCommand: pst.DSHCommand,
				})
				// 方案结果写回任务：面板要能看出"方案层真的跑过、用的是哪个思考模型"，
				// 否则"思考模型只出方案"就是一句无法验证的口号。
				//
				// 注意（自己踩过的死锁，2026-10-21）：mutator 里**不能**再调 store.Get ——
				// Update 已持有写锁，Get 又要读锁，Go 的 RWMutex 不可重入，直接自锁死。
				// 现场表现：测试挂在 120s 超时，栈里停在 sync.RWMutex.RLock。
				if _, uerr := e.store.Update(task.ID, func(t *model.Task) error {
					t.PlanSummary = pst.Blueprint.SummaryLine()
					t.PlanModel = pst.Model
					t.PlanDegraded = pst.Blueprint.Degraded
					return nil
				}); uerr != nil {
					e.log.Warn("写入方案结果失败", "taskId", task.ID, "err", uerr.Error())
				}
				e.log.Info("方案已产出", "taskId", task.ID, "plannerModel", pst.Model,
					"round", planRound, "plan", pst.Blueprint.SummaryLine())
			} else {
				// 退化直执行：把"方案层为什么没跑起来"落盘 + 记审计。
				// 不 return、不消耗重试次数 —— 通道故障不该判任务死刑。
				reason := "未知原因"
				if pst.Err != nil {
					reason = pst.Err.Error()
				}
				e.savePlan(task, pst, blueprint.PlanPrompt(e.autonomyGoalIntentFor(task.GoalID), task.Title, task.Prompt, effectiveMinWords(minWordsOf(e, task), attempt), lastFailure), planRound)
				e.store.AppendAttempt(store.AttemptRecord{
					TaskID: task.ID, Attempt: attempt, Model: pst.Model, Kind: "plan",
					Action: "degraded", ElapsedMs: pst.ElapsedMs, Error: reason,
					// 方案阶段的取证两件套：`producedRunes=0` = 模型没吐字；>0 = 吐了但方案 JSON 解析失败。
					StderrPath: pst.StderrPath, ProducedRunes: pst.ProducedRunes, DSHCommand: pst.DSHCommand,
				})
				_, _ = e.store.Update(task.ID, func(t *model.Task) error {
					t.PlanDegraded = true
					t.PlanSummary = "退化直执行：" + truncateRunes(reason, 120)
					return nil
				})
				e.log.Warn("方案层不可用，退化直执行", "taskId", task.ID, "err", reason)
				e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID,
					Msg: "方案层不可用（" + truncateRunes(reason, 120) + "），已退化直执行"})
			}
		}
		res, runErr := e.runner.Run(ctx, execTask, modelID)
		// 台账/健康度/日志都记**实际执行**的模型（round 44 · T27）：failover 会在候选链上换车道，
		// selected ≠ actual；记错会把失败算到没被执行的模型上、把好模型一起冷却（审计实测 3/3 不符）。
		actualModel := attemptedModel(res, modelID)
		elapsed := time.Since(startedAt)

		// ── 成功 ───────────────────────────────────────────────
		if runErr == nil {
			if res == nil {
				lastErr = fmt.Errorf("执行器返回空结果")
				lastKind = retry.Unknown
			} else if res.TimedOut {
				lastErr = fmt.Errorf("任务超时")
				lastKind = retry.Timeout
				e.router.RecordFailure(actualModel, string(executor.KindTimeout), "timeout")
			} else if res.FinalText == "" {
				lastErr = fmt.Errorf("任务无最终答案")
				lastKind = retry.Exit
				e.router.RecordFailure(actualModel, string(executor.KindExit), "empty final")
			} else {
				e.router.RecordSuccess(actualModel, res.Elapsed.Milliseconds())
				// 成功一次即清零限流退避：说明车道已恢复，下次限流重新从 5 分钟起。
				e.clearRateLimit()
				// 成功台账同样带产出体量与 stderr 位置：既是“这次真产出了多少”的读数，也让 done 与失败行**列同构**，便于对照。
				stderrPath, producedRunes, dshCommand := attemptEvidence(res)
				e.store.AppendAttempt(store.AttemptRecord{
					TaskID: task.ID, Attempt: attempt, Model: actualModel, Kind: "done",
					Action: "done", ElapsedMs: elapsed.Milliseconds(),
					StderrPath: stderrPath, ProducedRunes: producedRunes, DSHCommand: dshCommand,
				})
				e.completeTask(task, running, res, modelID, cfg)
				return
			}
		} else {
			lastErr = runErr
			failedModels[modelID] = true
		}

		// ── 限流：整池冷却，不换模型 ─────────────────────────────
		//
		// 现场实测：429 会被误判 boot_failed 后，引擎在 14 秒内把 5 个模型
		// 各试一遍（每个 2.8 秒），结果全池都记录失败，反而加剧限流。
		// 限流是**通道级**的，换模型毫无意义；正确做法是把任务退回队列 +
		// 给整池设置冷却闸门，等 OFM 车道自身恢复。
		// 判据要同时覆盖两种情况：执行器给出 res（含 ErrorKind），或只返回 error
		// （FailoverRunner 上抛时可能 res 为 nil）。文本兜底用于兜住漏分类的实现。
		// ── 池内无可用车道（T19）：与限流同路，但**不判任务失败** ─────────
		//
		// round 35 实测：全池只剩 3 条 `not_served` 车道时，引擎仍开跑，把两个任务烧成
		// `boot_failed` 任务失败。正确处置是：设池闸门 + 把任务退回队列等冷却。
		if isNoLane(res, runErr) {
			e.setPoolCooldown(rateLimitCooldownSeconds)
			capped := e.requeueForPoolWait(task.ID, running.ChainID)
			// 判死与「退回队列等待冷却」是两种事实，必须分别说话（2026-10-29 实测同一刻既报失败又报排队）。
			if capped {
				e.log.Warn("池内无可用车道且环境等待到顶，任务已判死（见台账 kind=pool_wait_capped）",
					"taskId", task.ID)
			} else {
				e.log.Warn("池内无可用车道，任务退回队列等待冷却",
					"taskId", task.ID, "waitSeconds", rateLimitCooldownSeconds)
			}
			return
		}
		if isRateLimited(res, runErr) {
			// 车道级冷却（2026-10-19 09:05 现场实测修正）：
			// our-free-model 插件把 OFM 免费 / Kilo 免费池 / EAC 协付三条**独立上游
			// 线路**统一注册成同一个 provider。实测 OFM 429 期间 Kilo 车道
			// （kilo-auto/free、reflection/beam-free）直连探测完全可用。
			// 改造前这里一律 setPoolCooldown（整池闸门），把健康车道也停掉 ——
			// 直接违反目标要求 4「自动切换到队列中其他正常可用的模型兜底」。
			// 现按 lane（真正的故障域）冷却，只有全部车道都不可用时才整池闸门。
			lane := e.router.LaneOf(modelID)
			if lane != "" {
				e.router.CooldownByLane(lane,
					time.Duration(rateLimitCooldownSeconds)*time.Second,
					string(executor.KindRateLimited), "429 rate limited")
			}
			// 限流退回队列这条也要带取证三件套：它是**最常见**的台账行之一，缺了它「哪次经 DSH 无头跑过」就查不全。
			stderrPath, producedRunes, dshCommand := attemptEvidence(res)
			e.store.AppendAttempt(store.AttemptRecord{
				TaskID: task.ID, Attempt: attempt, Model: actualModel, Kind: string(executor.KindRateLimited),
				Action: "requeue", ElapsedMs: elapsed.Milliseconds(),
				// T51：runErr 在这条路径上常为 nil（执行器退出码 0 但被判限流），
				// 旧写法只拼 runErr ⇒ 台账留下 `error=" | lane=…"` 这种「有 lane、没原因」的空壳。
				// attemptErrorText 在 runErr 为空时回退到事件流抓到的上游错误（T50 的 StreamError/LastError）。
				Error:      attemptErrorText(res, runErr) + " | lane=" + lane,
				StderrPath: stderrPath, ProducedRunes: producedRunes, DSHCommand: dshCommand,
			})
			remaining := e.router.AvailableLanes()
			// 判死时下面两条「冷却/切换车道」的措辞都不成立，直接按 capped 分流。
			//
			// round 101-112 · T78：这里把**本轮分类**传进去 —— 额度耗尽走更长的等待预算（quotaWaitBudget），
			// 否则「要账号侧动作、小时级恢复」的额度问题会在 20 分钟内把每条任务都判死（7×24 下等于一条不留）。
			// kind 取自 res.ErrorKind（executor 具名分类写回），不在这里猜上游。
			waitKind := string(res.ErrorKind)
			if waitKind == string(executor.KindQuotaExhausted) {
				e.log.Warn("免费额度耗尽：任务改走长等待预算", "taskId", task.ID, "budget", quotaWaitBudget)
			}
			capped := e.requeueForPoolWaitKind(task.ID, running.ChainID, waitKind)
			if capped {
				e.log.Warn("上游限流且环境等待到顶，任务已判死（见台账 kind=pool_wait_capped）",
					"taskId", task.ID, "model", modelID, "lane", lane)
				return
			}
			if len(remaining) > 0 {
				e.log.Warn("上游限流，仅冷却出问题的车道",
					"taskId", task.ID, "model", modelID, "lane", lane,
					"remainingLanes", strings.Join(remaining, ","))
				e.emitEvent(model.Event{Type: "warn", At: model.Now(),
					Msg: "上游限流：车道 " + lane + " 冷却，自动切换到其它车道（" +
						strings.Join(remaining, "/") + "）"})
				return
			}
			e.log.Warn("上游限流，全部车道不可用，整池冷却",
				"taskId", task.ID, "model", modelID, "lane", lane)
			e.setPoolCooldown(rateLimitCooldownSeconds)
			return
		}

		// ── 失败：先定类别 → 再判取消（取消优先于记账）→ 最后交给重试策略 ───────
		// 只做分类，**先不记车道失败**：记账必须发生在「这是不是一次真失败」判定之后。
		kind := retry.Unknown
		if res != nil && res.ErrorKind != "" {
			kind = retry.ParseKind(res.ErrorKind)
		} else if runErr != nil {
			kind = retry.Boot
		}

		// 取消优先于一切分类（issue #11 一次修掉两个缺陷）：
		//   ① 归因：以 context.Cause 为准。改造前 kind 直接取 res.ErrorKind，
		//      而 executor.go:427 用 "启动 DSH 失败: %w" 包装 context.Canceled 时，
		//      KindCancelled 根本没机会被赋上 ⇒ 台账落成 kind=unknown（生产实测 2 条）。
		//   ② 记账次序：改造前 RecordFailure 在取消判定**之前** ⇒ 用户点一次「终止」
		//      就把那条无辜车道打成一次失败冷却（限流风暴期尤其坏）。asynq 把「重试」与
		//      「算不算一次失败」分成两件事（IsFailure 谓词 + rdb.go:920 在写库处单点决定），
		//      river 停机走 interrupted 不写 attempt error —— 同一不变量：取消不污染统计。
		cancelKind, cancelText, cancelled := classifyCancellation(ctx, e.isCancelling(task.ID), kind)
		if cancelled {
			lastKind = cancelKind
			// 取消行同样带取证三件套：台账六处写入点**列同构**，读的时候不必再记「哪条有、哪条没有」。
			stderrPath, producedRunes, dshCommand := attemptEvidence(res)
			e.store.AppendAttempt(store.AttemptRecord{
				TaskID: task.ID, Attempt: attempt, Model: actualModel, Kind: string(cancelKind),
				Action: "cancel", ElapsedMs: elapsed.Milliseconds(),
				// 台账文案用具名归因而非 lastErr 原文：lastErr 常是包装泛错
				// （"启动 DSH 失败: context canceled"），与 kind=cancelled 自相矛盾 ⇒ 读台账的人仍然查不动。
				Error:      cancelText + causeSuffix(lastErr),
				StderrPath: stderrPath, ProducedRunes: producedRunes, DSHCommand: dshCommand,
			})
			e.finishCancelled(task.ID, lastErr, cancelText)
			return
		}
		// 真失败才归因到车道（取消已在上面 return，不会走到这里）。
		if res != nil && res.ErrorKind != "" {
			e.router.RecordFailure(actualModel, res.ErrorKind, errText(runErr))
		} else if runErr != nil {
			e.router.RecordFailure(actualModel, string(executor.KindBoot), errText(runErr))
		}
		lastKind = kind
		dec := pol.Decide(kind, attempt, errText(lastErr))
		// 失败台账带上取证两件套（stderr 落盘位置 + 产出体量，T15-④）。
		stderrPath, producedRunes, dshCommand := attemptEvidence(res)
		e.store.AppendAttempt(store.AttemptRecord{
			TaskID: task.ID, Attempt: attempt, Model: actualModel, Kind: string(kind),
			Action: string(dec.Action), DelayMs: dec.Delay.Milliseconds(),
			ElapsedMs: elapsed.Milliseconds(), Error: errText(lastErr),
			StderrPath: stderrPath, ProducedRunes: producedRunes, DSHCommand: dshCommand,
		})
		if dec.Action != retry.ActionRetry {
			break
		}
	}
	e.failTask(task.ID, lastErr, lastKind)
}

// completeTask 归档产出、自评、并以非阻塞方式继续链式生成。
func (e *Engine) completeTask(task *model.Task, running *model.Task, res *executor.Result, modelID string, cfg config.Config) {
	taskDir := e.layout.TaskDir(task.ID)
	// 产物协议流水线（P3）：解析 → 落盘真实文件 → 真实验收。
	// 它必须在归档之前跑，因为验收结果决定这条产出算 done 还是 degraded。
	verifyRep := e.materializeArtifacts(task, res.FinalText, taskDir, cfg)
	workDir := filepath.Join(taskDir, "workspace")
	if task.WorkRoot != "" {
		workDir = task.WorkRoot
	}

	// 采集产出：优先读 workspace 中由模型写下的文件，其次用最终文本。
	// external 模式下工作目录是用户的真实项目（可能已有成百上千文件），必须只收
	// 本次任务新写/改写的文件（mtime >= 任务开始时间），否则会把整个项目当成本次产出。
	since := int64(0)
	if task.WorkRoot != "" {
		since = task.StartedAt
		if since == 0 {
			since = task.CreatedAt
		}
	}
	produced, fileNames, err := collectOutputsSince(workDir, since)
	if err != nil {
		_ = err
	}
	if len(produced) == 0 {
		produced = map[string][]byte{"final.md": []byte(res.FinalText)}
		fileNames = []string{"final.md"}
	}
	primary := choosePrimary(fileNames, produced, task.OutputFormat)
	content := produced[primary]
	outPath := e.layout.TaskFile(task.ID, primary)
	_ = config.WriteFileAtomic(outPath, content)
	lenBytes := int64(len(content))

	// 提取摘要（用于后继任务上下文）
	summary := model.Truncate(string(content), 400)
	// 注意：这里以前调用 SaveTaskArtifact("events.jsonl", nil)，会用空内容
	// 原子覆盖掉 executor 刚写满的事件流（现场 29 个任务 events.jsonl 长度恰为 0）。
	// 正确做法是追加一条收尾事件（P0-1 修复）。
	e.store.AppendTaskEvent(task.ID, model.Event{
		Type: "finish", At: model.Now(), TaskID: task.ID,
		Msg:  "执行成功，产出 " + primary,
		Data: map[string]any{"bytes": lenBytes, "model": modelID, "elapsedMs": res.Elapsed.Milliseconds()},
	})

	// 注意：这里**不写 State**。终态由下面的验收判定决定（done / degraded），
	// 状态迁移必须走 store.Transition 才能留下审计并受状态机约束。
	// 第一版在这里直接写了 StateDone，导致随后 done→degraded 迁移被状态机拒绝、
	// 错误被忽略，degraded 永远不生效（隔离实例实测 verified=false 却 state=done）。
	_, _ = e.store.Update(task.ID, func(t *model.Task) error {
		t.OutputFile = outPath
		t.OutputDir = taskDir
		t.OutputBytes = lenBytes
		t.FinishedAt = model.Now()
		t.ElapsedMs = res.Elapsed.Milliseconds()
		t.Model = modelID
		t.TokenIn = res.Usage.InputTokens
		t.TokenOut = res.Usage.OutputTokens
		t.ToolCalls = res.ToolCalls
		t.SessionID = res.SessionID
		t.Summary = summary
		t.Error = ""
		t.ErrorKind = ""
		return nil
	})
	// 验收判定（P3）：没有通过验收的产出不算 done。
	// 这修复了改造前"截断/不可运行也算成功"的虚报 —— 现场 26 篇里 3 篇围栏未闭合，
	// 状态全是 done 并计入今日产出。
	// 完成判定抽成纯函数（round 46 · T28）：只有**交付物=文件的域**才把「无产物块」判成未完成；
	// 内容/报告类域的正文即交付（D7 契约：prose 型成功仍要跑语义后处理）。
	// T37 再叠一层模板豁免（先模板、后域）：report 型模板不被域判据拖去返工。
	finalState, finalNote := completionVerdict(verifyRep, requiresArtifactBlock(task.Domain, task.TemplateID, cfg.Executor.ArtifactDomains, cfg.Executor.ArtifactExemptTemplates))
	verified := verifyRep != nil && verifyRep.Pass
	if _, err := e.store.Update(task.ID, func(t *model.Task) error {
		t.Verify = verifyRep
		t.Verified = verified
		return nil
	}); err != nil {
		e.log.Warn("写入验收结果失败", "taskId", task.ID, "err", err.Error())
	}
	if finalState == model.StateDegraded {
		// 验收失败的两级处置（双角色闭环的关键分支）：
		//
		//  一级：返工（RepairRounds）—— 方案不变，让执行模型带着真实验收报错重做。
		//        便宜、见效快，覆盖"实现层面的错"（写错了、漏了边界）。
		//  二级：重新规划（ReplanRounds）—— 返工用完还不过，说明错在方案层面
		//        （交付物选错、验收命令不可执行、步骤缺关键一环），此时重做一百遍
		//        也没用，必须回到方案模型重新出方案。
		//
		// 改造前只有一级：同一个方案无限重做，方案层面的错永远修不掉 ——
		// 这正是"只出方案没有执行落地"与"只执行没有方案"两头都闭不上的原因。
		if shouldRepair(task, verifyRep, cfg) {
			e.scheduleRepair(task, verifyRep, workDir, cfg)
			return
		}
		if shouldReplan(task, currentPlan(task.ID, e), cfg) {
			e.scheduleReplan(task, verifyRep, cfg)
			return
		}
		_, _ = e.store.Transition(task.ID, model.StateDegraded, "engine", finalNote)
		if d, ok := e.store.Get(task.ID); ok {
			task = d
		}
		e.log.Warn("任务降级", "taskId", task.ID, "reason", failReasonOf(verifyRep), "rounds", task.RepairRounds)
		e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: task, Msg: "任务降级：" + failReasonOf(verifyRep)})
		e.runningIDs.Delete(task.ID)
		e.q.ReleaseChain(running.ChainID)
		// 降级任务不触发后继链（避免把不可用产出当上下文扩散），但保留产出供人工查。
		e.eval.Observe(task, 0, task.Domain, 400)
		return
	}
	_, _ = e.store.Transition(task.ID, model.StateDone, "engine", finalNote)
	// 成功也留结构化日志：与失败日志同格式，便于按 taskId 串联整条生命周期。
	e.log.Info("任务完成", logging.Redact([]any{
		"taskId", task.ID, "model", modelID, "bytes", lenBytes,
		"elapsedMs", res.Elapsed.Milliseconds(), "sessionId", res.SessionID,
	})...)
	done, _ := e.store.Get(task.ID)
	e.runningIDs.Delete(task.ID)
	e.q.ReleaseChain(running.ChainID)
	// 成功也必须进评估器窗口（P1 修复）：改造前只有 failTask 调 Observe，
	// 成功路径从不调用，"最近窗口"永远只有启动 Seed 的那批历史，导致运行期
	// 重复门禁形同虚设 —— 现场同一标题 1 小时内出现 2 次。
	// 质量分在 postProcess 才拿到，这里先用结构启发式估一个，postProcess 再刷新。
	e.eval.Observe(done, estimateQuality(content, minWordsOf(e, task)), task.Domain, 400)

	// 自评 + 摘要 + 链式后继（后台异步，不阻塞调度）
	e.inflight.Add(1)
	go func() {
		defer e.inflight.Done()
		e.postProcess(done, content, cfg)
	}()
}

// postProcess 做自评、摘要与后继任务生成。
func (e *Engine) postProcess(task *model.Task, content []byte, cfg config.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	summary, serr := e.sem.Summarize(ctx, task, string(content))
	if serr == nil && summary != "" {
		if _, err := e.store.Update(task.ID, func(t *model.Task) error { t.Summary = summary; return nil }); err == nil {
			if nt, ok := e.store.Get(task.ID); ok {
				task = nt
			}
		}
	}
	minWords := 300
	if tpl, ok := e.reg.Get(task.TemplateID); ok && tpl.OutputSpec.MinWords > 0 {
		minWords = tpl.OutputSpec.MinWords
	}
	critique, cerr := e.sem.Critique(ctx, task, string(content), minWords)
	if cerr != nil {
		critique = &model.Critique{Quality: estimateQuality(content, minWords), Degraded: true, At: model.Now()}
	}
	_, _ = e.store.Update(task.ID, func(t *model.Task) error { t.Critique = critique; return nil })
	if nt, ok := e.store.Get(task.ID); ok {
		task = nt
	}
	e.eval.SetChainQuality(task.ChainID, critique.Quality)
	// 自评拿到真实质量分后刷新窗口里的质量值（质量分影响链收益与后继价值）。
	e.eval.Observe(task, critique.Quality, task.Domain, 400)
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: task, Msg: "任务已完成"})

	// 自主任务：走闭环自主验收（质量判定 → 风险 → 回退/计入 → 目标验收 → 下一目标推导），
	// 不走模板链延伸 —— 自主链的"下一步"由项目目标推导，而不是父模板的固定后继表。
	if task.Source == "autonomy" && task.GoalID != "" {
		e.finishAutonomous(task, content, critique, cfg)
		return
	}
	e.extendChain(ctx, task, critique, cfg)
}

// extendChain 生成后继任务并送入评估-批准-入队链路。
func (e *Engine) extendChain(ctx context.Context, parent *model.Task, critique *model.Critique, cfg config.Config) {
	if !e.gen.ShouldExtend(parent, critique) {
		e.closeChain(parent)
		return
	}
	depth := parent.ChainDepth + 1
	now := model.Now()
	var drafts []*model.Task
	fanout := cfg.Chain.Fanout
	if fanout < 1 {
		fanout = 1
	}
	// 1) 模板链优先
	succ := e.gen.SuccessorTemplates(parent)
	for i, tpl := range succ {
		if i >= fanout {
			break
		}
		drafts = append(drafts, e.gen.Successor(tpl, parent, critique, depth, now))
	}
	// 2) 自评给出的新想法（补足 fanout）
	if len(drafts) < fanout && critique != nil {
		for _, idea := range critique.NextIdeas {
			if len(drafts) >= fanout {
				break
			}
			drafts = append(drafts, e.gen.SynthesisTask(idea, parent, depth, now))
		}
	}
	if len(drafts) == 0 {
		e.closeChain(parent)
		return
	}
	// 3) 评估 -> 自动批准 -> 入队
	all := e.store.List()
	// 每日计数统一走 tasksToday()（round 60 · T57）：这里原先就地重算「今天创建的全部任务」，
	// **含被拒草案**，而 tasksToday() 明确剔除 rejected（engine_autonomy.go:452-453）。
	// 同一台机器两套口径 ⇒ 高拒绝日里后继链会被误判「已达每日上限」而集体被拒 ——
	// 这正是 engine.go:876-879 在册要求「两处口径必须完全一致」要防的错位（历史成因：12 小时零产出）。
	daily := e.tasksToday()
	var approved []*model.Task
	for _, draft := range drafts {
		var tplPtr *model.Template
		if tpl, ok := e.reg.Get(draft.TemplateID); ok {
			tplPtr = &tpl
		}
		scores := e.eval.Evaluate(evaluate.Input{Task: draft, Template: tplPtr, History: all, Now: now, DailyCount: daily})
		draft.Scores = scores
		if !evaluate.Approve(scores) {
			draft.State = model.StateRejected
			if err := e.store.Create(draft); err != nil {
				e.log.Warn("落盘被拒后继任务失败（面板与持久态会不一致）", "taskId", draft.ID, "err", err.Error())
			}
			e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: draft.ID, Data: draft, Msg: "后继任务被评估拒绝"})
			continue
		}
		approved = append(approved, draft)
	}
	for _, d := range approved {
		if err := e.store.Create(d); err != nil {
			continue
		}
		if _, err := e.store.Transition(d.ID, model.StateApproved, "engine", "链式自动批准"); err != nil {
			continue
		}
		if q, err := e.store.Transition(d.ID, model.StateQueued, "engine", "链式入队"); err == nil {
			e.q.Push(q)
			e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: q.ID, Data: q, Msg: "后继任务入队"})
		}
	}
	if len(approved) == 0 {
		e.closeChain(parent)
	}
}

func (e *Engine) closeChain(parent *model.Task) {
	if parent.ChainID == "" {
		return
	}
	if c, ok := e.store.GetChain(parent.ChainID); ok {
		c.Status = "closed"
		_ = e.store.UpsertChain(c)
	}
}

// backfillChains 按任务上的 chainId 反推链元数据并落盘。
//
// 幂等：已存在的链只更新任务列表与深度，不覆盖 status/topic。
// 只处理非空 chainId；单任务链（无 parent 且无后继）也建，便于面板按链聚合。
func (e *Engine) backfillChains(tasks []*model.Task) {
	type agg struct {
		ids       []string
		rootID    string
		rootTitle string
		maxDepth  int
		createdAt int64
		updatedAt int64
		sessionID string
		hasOpen   bool
	}
	byChain := map[string]*agg{}
	for _, t := range tasks {
		if t == nil || t.ChainID == "" {
			continue
		}
		a := byChain[t.ChainID]
		if a == nil {
			a = &agg{rootID: t.ID, rootTitle: t.Title, createdAt: t.CreatedAt, sessionID: t.SessionID}
			byChain[t.ChainID] = a
		}
		a.ids = append(a.ids, t.ID)
		if t.ChainDepth >= a.maxDepth {
			a.maxDepth = t.ChainDepth
			a.rootID = rootOf(t, tasks)
			a.rootTitle = titleOf(rootOf(t, tasks), tasks)
		}
		if t.UpdatedAt > a.updatedAt {
			a.updatedAt = t.UpdatedAt
		}
		if t.SessionID != "" && a.sessionID == "" {
			a.sessionID = t.SessionID
		}
		switch t.State {
		case model.StateQueued, model.StateRunning, model.StateApproved, model.StateCandidate, model.StateEvaluating:
			a.hasOpen = true
		}
	}
	for chainID, a := range byChain {
		existing, ok := e.store.GetChain(chainID)
		if ok && existing != nil {
			// 已有链：只补任务列表与深度，保留原有 status/topic。
			existing.TaskIDs = a.ids
			if a.maxDepth > existing.Depth {
				existing.Depth = a.maxDepth
			}
			if len(a.ids) > 0 && !a.hasOpen {
				// 全部终态则关链（幂等）。
				existing.Status = "closed"
			}
			_ = e.store.UpsertChain(existing)
			continue
		}
		c := &model.Chain{
			ID:         chainID,
			RootTaskID: a.rootID,
			SessionID:  a.sessionID,
			Topic:      model.Truncate(a.rootTitle, 80),
			Depth:      a.maxDepth,
			MaxDepth:   e.cstore.Get().Chain.MaxDepth,
			Status:     "active",
			TaskIDs:    a.ids,
			CreatedAt:  a.createdAt,
			UpdatedAt:  a.updatedAt,
		}
		if !a.hasOpen {
			c.Status = "closed"
		}
		_ = e.store.UpsertChain(c)
	}
}

// rootOf 沿 parentId 上溯到链根任务。
func rootOf(t *model.Task, all []*model.Task) string {
	if t.ParentID == "" {
		return t.ID
	}
	byID := make(map[string]*model.Task, len(all))
	for _, x := range all {
		if x != nil {
			byID[x.ID] = x
		}
	}
	cur := t
	for i := 0; i < 64; i++ {
		p, ok := byID[cur.ParentID]
		if !ok || p == nil || p.ParentID == "" {
			if ok && p != nil {
				return p.ID
			}
			return cur.ID
		}
		cur = p
	}
	return cur.ID
}

// titleOf 返回任务标题（找不到时返回空串）。
func titleOf(id string, all []*model.Task) string {
	for _, x := range all {
		if x != nil && x.ID == id {
			return x.Title
		}
	}
	return ""
}

// minWordsOf 返回任务所属模板的产出最小字数（无模板时退化为 300）。
func minWordsOf(e *Engine, task *model.Task) int {
	if tpl, ok := e.reg.Get(task.TemplateID); ok && tpl.OutputSpec.MinWords > 0 {
		return tpl.OutputSpec.MinWords
	}
	return 300
}

// isRateLimited 判断这次失败是否属于**上游限流族**（HTTP 429）—— 含额度耗尽 quota_exhausted。
//
// 三个来源都要覆盖（现场踩过两个）：
//  1. executor 分类后的 ErrorKind = rate_limited / quota_exhausted（正常路径）；
//  2. 包装层（FailoverRunner / 未来的远程执行器）只上抛 error，res 为 nil；
//  3. 兜底文本匹配，防止某个实现漏了分类。
//
// quota_exhausted 必须进这一族（round 124 实测的漏项）：本函数决定是否走「冷却车道 + 退回队列等预算」
// 这条路径，而 T78 的长等待预算就挂在那条路径上。漏掉它的后果（线上实测）：近 12 分钟 54 次
// quota_exhausted 尝试、而「任务改走长等待预算」日志**一条都没有** —— 分级预算形同虚设，
// 任务改从普通失败路径消耗 Attempts。两者同为 429 家族，只是恢复尺度不同（预算已由 kind 分级）。
func isRateLimited(res *executor.Result, err error) bool {
	if res != nil && (res.ErrorKind == string(executor.KindRateLimited) || res.ErrorKind == string(executor.KindQuotaExhausted)) {
		return true
	}
	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "rate_limit") || strings.Contains(msg, "rate limited") ||
			strings.Contains(msg, "too many requests") || strings.Contains(msg, "429") {
			return true
		}
	}
	if res != nil {
		msg := strings.ToLower(res.LastError)
		if strings.Contains(msg, "rate_limit") || strings.Contains(msg, "too many requests") {
			return true
		}
	}
	return false
}

// isNoLane 判定「这次失败是整池无可用车道」，而不是某条车道的问题（T19）。
//
// 两条判据都认：模型池侧返回的哨兵错误（`executor.ErrNoLane`，由调度器在候选链为空时抛出），
// 或执行器结果把类别写成 `no_lane`。命中后由调用方设池闸门 + 任务退回队列，**不计任务失败**。
func isNoLane(res *executor.Result, err error) bool {
	if errors.Is(err, executor.ErrNoLane) {
		return true
	}
	return res != nil && res.ErrorKind == string(executor.KindNoLane)
}

// poolWaitBudget 是单个任务允许「因环境退回队列」的最大次数（round 60 · T54）。
//
// 取 4 的依据：现场 5134 的重投节奏约每 3~6 分钟一次（15:13/15:14/15:19/15:23/15:53…），
// 4 次 ≈ 20 分钟 —— 足够跨过既有的 5 分钟闸门重探与上游短时限流窗口，又不至于让一条任务
// 独占自主供料门数小时。判死后若上游已恢复，任务会被下一条自主草案取代；人工重试同样可重开。
const poolWaitBudget = 4

// quotaWaitBudget 是「等待原因**明确是额度耗尽**」时的放宽预算（round 101-107 · T78）。
//
// 为什么需要分级（现场 2026-10-29/30）：36-38 条车道同时冷却、十余名任务 park（poolWaits 2-3）；
// 而额度耗尽的原文写着要**账号侧动作**（Sign up for free… / Free usage limit reached…），恢复尺度是**小时级**。
// 沿用 4 次（约 20 分钟）的预算 ⇒ 额度未恢复前**每条任务都会被判死**，7×24 下就是「一条也不留」。
// 判死的初衷是「防僵尸独占供料」，而额度耗尽恰恰需要一个常驻任务占着位置等额度 —— 两者目标不同，预算就该不同。
//
// 取 48 的依据：按现场重投节奏（闸门 5-30 分钟一轮，含退避）约 6-12 小时，足够跨过一个额度周期；
// 仍**不是无限**：到顶依旧判死（保留「换一条自主草案取代」的兜底），只是把「20 分钟就枪毙」改成「等半天再判」。
const quotaWaitBudget = 48

// planAttemptCap 是单个任务允许「调用思考模型出方案」的最大次数（round 63 · T64）。
//
// 取 2 的依据（现场实测，2026-10-29 近 90 分钟）：每任务实际 plan 调用 3~5 次、占全部台账行 ≈50%，
// 而失败形态是弱车道能力问题（「方案模型无输出」「方案 JSON 解析失败」）——**同池重试的边际收益极低**。
// 2 次足以跨过一次车道抖动（换一条车道再试一次），又不会把配额对半烧在规划上；
// 到顶后走既有的「退化直执行」路径（方案并非交付的必要条件），需要真重规划时由 shouldReplan 单独放行。
const planAttemptCap = 2

// requeueForPoolWait 把任务从 running 退回 queued（合法迁移），并给整池设置等待闸门。
//
// 返回值 capped=true 表示「环境等待次数到顶、任务已判死」——调用方据此**必须**改口：
// 判死后若继续打印「任务退回队列等待冷却」，日志就在说假话（2026-10-29 生产实测：同一条任务
// 16:24:20.530 先 ERROR 任务失败、同一刻又 WARN「池内无可用车道，任务退回队列等待冷却」）。
//
// 设计要点：
//   - 只设置闸门，不 sleep、不占并发槽；
//   - 闸门取"最早结束冷却的模型时刻 + 5 秒缓冲"，无可用信息时退化为 15 秒；
//   - 任务本身不计失败（Attempts 不变），避免把环境问题算成任务问题。
func (e *Engine) requeueForPoolWait(taskID, chainID string) (capped bool) {
	return e.requeueForPoolWaitKind(taskID, chainID, "")
}

// requeueForPoolWaitKind 按**等待原因**分级预算（round 101-112 · T78）：
// 额度耗尽（kind=quota_exhausted）走 quotaWaitBudget，其余环境抖动走 poolWaitBudget。
// 判据只认调用方传进来的具名 kind（来自 executor 的分类），不在这里猜上游。
func (e *Engine) requeueForPoolWaitKind(taskID, chainID, kind string) (capped bool) {
	budget := poolWaitBudget
	if kind == string(executor.KindQuotaExhausted) {
		budget = quotaWaitBudget
	}
	// 无效重投封顶（round 60 · T54）：环境类退回**不计失败**是对的，但完全无上限会造出僵尸任务。
	//
	// 现场（2026-10-28，外置根实测）：唯一活任务 5134 在 47.7 分钟里被重投 47 次（attempts 只涨到 2/3），
	// 于是它永远不落终态；而自主供料门 inFlightTasksOfGoal 把 **queued 也算在途**（只排终态与被拒），
	// 一条卡住的自主任务 = 永久不再产生新任务 = 7×24 下产出恒为 0。实测生成时间线印证：
	// 10-28 十次引擎启动只有三次生成，且每次都精确发生在**前一个任务被判死的 3~22 秒之后**
	// （01:29:22←cd1c 01:29:04；14:28:33←d5d8 14:28:08；15:11:08←df68 15:11:04）。
	// 口径：到顶即判死（running→failed 是合法迁移，且 failTask 会释放链、留台账），
	// 面板「重试」仍可人工重开（RetryTask 允许 failed→queued 并把 Attempts 归零）——
	// 判死不是放弃任务，是把「永久独占供料」换成「可重开的一次失败」。
	waits := 0
	if _, err := e.store.Update(taskID, func(x *model.Task) error { x.PoolWaits++; return nil }); err == nil {
		if cur, ok := e.store.Get(taskID); ok {
			waits = cur.PoolWaits
		}
	}
	if waits >= budget {
		e.log.Warn("模型池重投次数到顶，判死以解锁自主供料", "taskId", taskID, "poolWaits", waits, "budget", budget, "kind", kind)
		e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: taskID,
			Msg: fmt.Sprintf("模型池连续 %d 次不可用，任务判死（可在面板重试）", waits)})
		// 具名类别 PoolWaitCapped（改造一 · 2026-10-29）：这条路径此前写死 retry.Unknown，
		// 生产 39 条 kind=unknown 的「任务失败」**全部**来自这里（逐条核对 workbench.jsonl），
		// 面板「未知错误」桶等于把「环境等待封顶」伪装成了「不知道为什么」。
		e.failTask(taskID, fmt.Errorf("模型池连续 %d 次不可用/限流，环境等待封顶判死", waits), retry.PoolWaitCapped)
		return true
	}
	if q, err := e.store.Transition(taskID, model.StateQueued, "engine", "模型池冷却，退回队列"); err == nil {
		e.q.Push(q)
	} else {
		e.log.Warn("池不可用退回队列失败", "taskId", taskID, "err", err.Error())
	}
	e.q.ReleaseChain(chainID)

	now := model.Now()
	// 闸门时长（2026-10-19 修正）：只有"当前没有任何可用车道"时才需要停调度。
	// 改造前这里无条件把闸门推到 EarliestAvailable，哪怕只是某一条车道冷却、
	// 其它车道仍然健康 —— 实测表现为 OFM 429 期间 Kilo 车道可用却调度停摆。
	if avail := e.router.AvailableLanes(); len(avail) > 0 {
		e.log.Info("仍有可用车道，任务退回队列后立即重调度",
			"taskId", taskID, "lanes", strings.Join(avail, ","))
		e.emitEvent(model.Event{Type: "warn", At: now, Msg: "模型车道切换中，任务已重新排队"})
		return
	}
	waitUntil := e.router.EarliestAvailable()
	if waitUntil <= now {
		waitUntil = now + 15_000
	} else {
		waitUntil += 5_000
	}
	// 闸门封顶 5 分钟：结构性失败的冷却是 12 小时，若整池都进了长冷却，
	// 直接把调度睡 12 小时等于 24/7 系统静默停摆（比空转更糟：空转至少还在产
	// 审计，停摆连信号都没有）。宁可每 5 分钟醒来重探一次 —— 上游恢复往往早于
	// 本地冷却到期，重探能立刻接上；重探本身只是 Pick 一次，代价可忽略。
	if maxGate := now + 5*60_000; waitUntil > maxGate {
		waitUntil = maxGate
	}
	// 只在更晚时才推进闸门，避免多个任务同时退回时把等待时间越拉越短。
	for {
		old := e.poolWaitUntil.Load()
		if old >= waitUntil || e.poolWaitUntil.CompareAndSwap(old, waitUntil) {
			break
		}
	}
	e.log.Warn("模型池冷却，调度暂停", "untilMs", waitUntil, "waitSeconds", (waitUntil-now)/1000)
	e.emitEvent(model.Event{Type: "warn", At: now, Msg: fmt.Sprintf("模型池冷却中，%d 秒后重试", (waitUntil-now)/1000)})
	return false
}

// failTask 记录失败并落真实错误类别。
//
// 改造前这里把 ErrorKind 硬编码成 "execution"，executor 当时算出的全部
// 失败类别（超时/上游失败/缺凭据/模型下线…，2026-10-22 时点为 11 类）在任务台账里全部丢失，
// 面板与统计无法区分"模型挂了"和"任务本身有问题"（P0-1 修复）。
func (e *Engine) failTask(taskID string, err error, kind retry.Kind) {
	e.runningIDs.Delete(taskID)
	msg := "未知错误"
	if err != nil {
		msg = err.Error()
	}
	t, ok := e.store.Get(taskID)
	if !ok {
		return
	}
	e.q.ReleaseChain(t.ChainID)
	e.mu.Lock()
	e.lastErr = msg
	e.mu.Unlock()
	kindText := string(kind)
	if kindText == "" {
		kindText = string(retry.Unknown)
	}
	// 留痕先于状态生效（2026-10-22 对标 asynq processor.go:336-338）。
	//
	// 顺序由 writeLedgerThenTransition seam 强制，单测可用注入闭包直接断言次序
	//（见 ledger_order.go 的注释与 ledger_order_test.go）。
	// 改造前的顺序是 Update -> Audit：状态已是 failed 而台账可能缺行；
	// 且 Update 失败时只 emit 一条瞬时事件（不落盘），任务停在 running 无结构化痕迹。
	ledgerErr, uerr := writeLedgerThenTransition(
		func() error {
			return e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: taskID, From: string(t.State),
				To: string(model.StateFailed), Actor: "engine", Kind: "error",
				Message: model.Truncate("["+kindText+"] "+msg, 300)})
		},
		func() error {
			_, err := e.store.Update(taskID, func(x *model.Task) error {
				x.Error = model.Truncate(msg, 800)
				x.ErrorKind = kindText
				x.FinishedAt = model.Now()
				if x.State == model.StateRunning {
					x.State = model.StateFailed
				}
				return nil
			})
			return err
		},
	)
	// 留痕失败不阻断状态收敛，但必须补一条兜底痕迹（否则这条失败路径整体无痕）。
	if ledgerErr != nil {
		e.appendAuditOrEvent(
			store.AuditRecord{At: model.Now(), TaskID: taskID, To: string(model.StateFailed), Actor: "engine",
				Kind: "error", Message: model.Truncate("[ledger-fallback]["+kindText+"] "+msg, 300)},
			taskID, "失败台账写入失败（已退到事件流留痕）: "+ledgerErr.Error()+" | kind="+kindText)
		e.log.Warn("失败台账写入失败，已退到事件流留痕", "taskId", taskID, "kind", kindText, "err", ledgerErr.Error())
	}
	// 状态收敛失败：任务会停在 running（既不重试也不统计），必须留痕说明，
	// 否则它只会静静躺在面板上，运维看不出"这条任务状态没写进去"。
	if uerr != nil {
		e.appendAuditOrEvent(
			store.AuditRecord{At: model.Now(), TaskID: taskID, Actor: "engine", Kind: "error",
				Message: model.Truncate("[state-update-failed]["+kindText+"] "+uerr.Error(), 300)},
			taskID, "失败状态写入出错（任务可能停在 running）: "+uerr.Error())
		e.emitEvent(model.Event{Type: "error", At: model.Now(), TaskID: taskID, Msg: "更新失败状态出错: " + uerr.Error()})
	}
	e.log.Error("任务失败", logging.Redact([]any{
		"taskId", taskID, "kind", kindText, "title", model.Truncate(t.Title, 60), "error", model.Truncate(msg, 300),
	})...)
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: taskID, Msg: "任务失败: " + model.Truncate(msg, 200)})
	// 失败也观察一次，避免同题反复生成
	if t != nil {
		e.eval.Observe(t, 0, t.Domain, 400)
	}
}

// isCancelling 判断任务是否正被用户取消（用于把"取消"与"失败"分开处理）。
func (e *Engine) isCancelling(taskID string) bool {
	_, ok := e.cancelling.Load(taskID)
	return ok
}

// finishCancelled 把任务收敛为 cancelled 终态（取消不算失败，不计入失败率）。
//
// reason 由调用方从 classifyCancellation 取得（issue #11）：具名原因必须在**判定处**决定，
// 这里不再从 err 文本反推 —— 反推正是本缺陷的旧形态（err 是包装泛错时推不出取消，
// 只能落默认文案 + unknown 类别）。reason 为空时退回旧的文本推断，保证无 cause 的
// 调用点（如测试直调）不会出现空文案台账。
func (e *Engine) finishCancelled(taskID string, err error, reason string) {
	msg := reason
	if msg == "" {
		msg = "用户取消"
		if t := ctxCancelText(err); t != "" {
			msg = t
		}
	}
	// 留痕先于状态生效（2026-10-22 对标 asynq processor.go:336-338）。
	// 取消是**调用方意图**，其痕迹优先级与失败等同：
	// 先落一条 cancel 审计（含结果），再迁状态 —— 改造前只在迁移**失败**时才写审计，
	// 正常路径的取消只有一条瞬时事件，事后无法区分「用户取消」与「任务被判失败」。
	ledgerErr, terr := writeLedgerThenTransition(
		func() error {
			return e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: taskID, Actor: "user", Kind: "cancel",
				Message: model.Truncate("取消收尾: "+msg, 200)})
		},
		func() error {
			_, err := e.store.Transition(taskID, model.StateCancelled, "user", msg)
			return err
		},
	)
	if ledgerErr != nil {
		e.log.Warn("取消台账写入失败，已退到事件流留痕", "taskId", taskID, "err", ledgerErr.Error())
		e.appendAuditOrEvent(
			store.AuditRecord{At: model.Now(), TaskID: taskID, Actor: "user", Kind: "cancel",
				Message: model.Truncate("[ledger-fallback] 取消收尾: "+msg, 200)},
			taskID, "取消台账写入失败（已退到事件流留痕）: "+ledgerErr.Error())
	}
	if terr != nil {
		// 迁移失败（已是终态等）时补一条审计说明，不覆盖既有状态
		e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: taskID, Actor: "user", Kind: "cancel", Message: "取消收尾迁移失败: " + terr.Error()})
	}
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: taskID, Msg: "任务已取消"})
}

// ── 对外操作 ─────────────────────────────────────────────────────

// EnqueueManual 手动添加任务（面板用）。
func (e *Engine) EnqueueManual(title, prompt, domain string, priority int) (*model.Task, error) {
	now := model.Now()
	if priority < 1 {
		priority = 3
	}
	task := &model.Task{
		ID: "task-" + fmt.Sprint(now) + "-" + randSuffix(), Title: title, Prompt: prompt, Domain: domain,
		Priority: priority, State: model.StateCandidate, Source: "manual", MaxAttempts: 3,
		OutputFormat: "md", TimeoutMin: e.cstore.Get().Executor.DefaultTimeout, CreatedAt: now, UpdatedAt: now,
	}
	scores := e.eval.Evaluate(evaluate.Input{Task: task, Now: now})
	task.Scores = scores
	if err := e.store.Create(task); err != nil {
		return nil, err
	}
	if !evaluate.Approve(scores) {
		_, _ = e.store.Transition(task.ID, model.StateRejected, "user", "手动任务未过阈值")
		return task, fmt.Errorf("任务未通过评估（综合分 %.1f < 阈值 %.0f）", scores.Total, scores.Threshold)
	}
	if _, err := e.store.Transition(task.ID, model.StateApproved, "user", "手动批准"); err != nil {
		return nil, err
	}
	q, err := e.store.Transition(task.ID, model.StateQueued, "user", "手动入队")
	if err != nil {
		return nil, err
	}
	e.q.Push(q)
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: q, Msg: "手动任务入队"})
	return q, nil
}

// CancelTask 取消任务（队列中直接取消；运行中中断其子进程）。
func (e *Engine) CancelTask(id string) error {
	t, ok := e.store.Get(id)
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if t.State.IsTerminal() {
		return fmt.Errorf("任务已结束（%s）", t.State)
	}
	e.q.Remove(id)
	if t.State == model.StateQueued || t.State == model.StateApproved || t.State == model.StateCandidate {
		_, err := e.store.Transition(id, model.StateCancelled, "user", "用户取消")
		return err
	}
	// 运行中：真正取消该任务的执行上下文（P0-3 修复）。
	// 改造前这里只写一条审计就返回，子进程继续跑到结束——面板上的"终止"是空操作。
	e.cancelling.Store(id, struct{}{})
	if v, ok := e.taskCancels.Load(id); ok {
		if cancel, ok := v.(taskCanceler); ok {
			// 具名 cause = 用户意图，与停机区分开（两者后续处置不同）。
			cancel.Cancel(causeUserCancel)
		}
	}
	e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: id, Actor: "user", Kind: "cancel", Message: "用户请求取消运行中任务，已取消执行上下文"})
	e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: id, Msg: "正在取消任务…"})
	return nil
}

// RetryTask 把失败/超时任务重新入队。
func (e *Engine) RetryTask(id string) error {
	t, ok := e.store.Get(id)
	if !ok {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if t.State != model.StateFailed && t.State != model.StateTimeout && t.State != model.StateRejected {
		return fmt.Errorf("只有失败/超时/被拒任务可重试（当前 %s）", t.State)
	}
	if _, err := e.store.Update(id, func(x *model.Task) error {
		x.Attempts = 0
		x.Error = ""
		x.ErrorKind = ""
		return nil
	}); err != nil {
		return err
	}
	if _, err := e.store.Transition(id, model.StateQueued, "user", "用户重试"); err != nil {
		return err
	}
	if q, ok := e.store.Get(id); ok {
		e.q.Push(q)
	}
	return nil
}

// SetPriority 调整优先级/置顶（面板操作）。
func (e *Engine) SetPriority(id string, priority int, pinned bool) error {
	if priority < 1 {
		priority = 1
	}
	if priority > 5 {
		priority = 5
	}
	if _, err := e.store.Update(id, func(t *model.Task) error {
		t.Priority = priority
		t.Pinned = pinned
		return nil
	}); err != nil {
		return err
	}
	if q, ok := e.store.Get(id); ok && q.State == model.StateQueued {
		_ = e.q.Reorder(id, priority, pinned)
	}
	return nil
}

// Rate 给产出打分（用户偏好回灌）。
func (e *Engine) Rate(id string, rating int) error {
	if rating < 0 || rating > 5 {
		return fmt.Errorf("评分必须在 0-5")
	}
	_, err := e.store.Update(id, func(t *model.Task) error { t.Rating = rating; return nil })
	if err == nil {
		e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: id, Actor: "user", Kind: "rate", Message: fmt.Sprintf("用户评分 %d", rating)})
	}
	return err
}

// ── 工作目录（可选外部目录模式） ─────────────────────────────────

// Workspace 返回工作目录管理器（面板与测试用）。
func (e *Engine) Workspace() *workspace.Manager { return e.ws }

// applyWorkspaceMode 把当前工作目录模式同步到执行器的运行层沙箱开关。
//
// 为什么必须动执行器：DSH 的 fs 沙箱边界是 session workspace（由 session cwd 推导），
// 工作台即使把子进程 cwd 指向用户目录，模型的文件工具仍会被 read-only 默认策略拒绝。
// external 模式下追加 sandboxPolicy=danger-full-access 覆盖层，由本进程的
// workspace 护栏（ResolveWrite 越界拒绝 + 审计）承担真正的作用域限制。
func (e *Engine) applyWorkspaceMode() {
	external := e.ws.IsExternal()
	e.setRunnerAllowAll(external)
}

func (e *Engine) setRunnerAllowAll(v bool) {
	r := e.runner
	for i := 0; i < 8; i++ {
		switch x := r.(type) {
		case interface{ SetAllowAll(bool) }:
			x.SetAllowAll(v)
			return
		case interface {
			Inner() executor.RunnerContract
		}:
			r = x.Inner()
			continue
		}
		return
	}
}

// wireWorkspaceRunner 给底层执行器挂"工作目录上下文采集"钩子。
//
// 装饰器（模型池故障转移）可能包在真执行器外层，所以这里沿 Inner() 链下探，
// 命中第一层支持 SetWorkspaceContext 的实现即注入。
func (e *Engine) wireWorkspaceRunner() {
	r := e.runner
	for i := 0; i < 8; i++ {
		switch x := r.(type) {
		case interface{ SetWorkspaceContext(func() string) }:
			x.SetWorkspaceContext(e.workspaceContextText)
			return
		case interface {
			Inner() executor.RunnerContract
		}:
			r = x.Inner()
			continue
		}
		return
	}
}

// workspaceContextText 采集并渲染要注入 prompt 的工作目录上下文。
// project 模式返回空串（行为与改造前逐字节一致）。
func (e *Engine) workspaceContextText() string {
	if !e.ws.IsExternal() {
		return ""
	}
	return workspace.BuildContext(e.ws.Root(), e.ws.Config()).PromptBlock()
}

// WorkspaceView 返回面板所需的完整工作目录快照（配置 + 校验 + 文件树 + 审计）。
func (e *Engine) WorkspaceView() map[string]any {
	cfg := e.cstore.Get().Workspace
	return map[string]any{
		"config":      cfg,
		"snapshot":    e.ws.Snapshot(),
		"mode":        e.ws.Mode(),
		"root":        e.ws.Root(),
		"workspaceOn": e.ws.IsExternal(),
	}
}

// ValidateWorkspaceDir 只校验不改配置（选择器的"先验"按钮用）。
func (e *Engine) ValidateWorkspaceDir(path string) (*workspace.Validation, error) {
	return e.ws.Validate(path)
}

// SelectWorkspaceDir 校验 → 落配置 → 立即生效（可选模式切换的唯一写入口）。
func (e *Engine) SelectWorkspaceDir(path string) (*workspace.Validation, error) {
	v, err := e.ws.Validate(path)
	if err != nil {
		return v, err
	}
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Workspace.Mode = workspace.ModeExternal
		c.Workspace.Root = v.Canonical
		c.Workspace.Since = model.Now()
	}); err != nil {
		return v, err
	}
	if _, serr := e.ws.SelectExternal(v.Canonical); serr != nil {
		return v, serr
	}
	e.applyWorkspaceMode()
	e.emitEvent(model.Event{Type: "workspace", At: model.Now(), Msg: "工作目录已切换到外部目录: " + v.Canonical})
	return v, nil
}

// ResetWorkspaceProject 切回项目模式（保留上次选定的目录，便于一键再切回）。
func (e *Engine) ResetWorkspaceProject() workspace.Config {
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Workspace.Mode = workspace.ModeProject
		c.Workspace.Since = model.Now()
	}); err != nil {
		e.log.Warn("切回项目模式写配置失败", "err", err.Error())
	}
	cfg := e.ws.ResetProject()
	e.applyWorkspaceMode()
	e.emitEvent(model.Event{Type: "workspace", At: model.Now(), Msg: "工作目录已切回项目模式"})
	return cfg
}

// WorkDirFor 返回任务真实工作目录（面板与产物路径展示用）。
func (e *Engine) WorkDirFor(taskID string) string {
	d, _ := e.ws.TaskWorkDir(taskID)
	return d
}

// ReloadConfig 热更新配置到各子系统。
func (e *Engine) ReloadConfig() {
	cfg := e.cstore.Get()
	// 路由表优先级：模型池（实时探测/同步后的清单）> 配置里的静态 models。
	// 模型池一旦接入，配置热更新不能把它冲回 3 条硬编码模型。
	if routes := e.poolRoutes(); len(routes) > 0 {
		e.router.UpdateRoutes(routes)
	} else {
		// 回落到手写清单时同样要核对（round 124 · T79）：池还没同步时这分支才生效，
		// 也正是「配置里写着池里不存在的 id」会真正上路跑的时机。
		e.checkConfiguredLaneIDs(cfg)
		e.router.UpdateRoutes(cfg.Models)
	}
	e.eval.UpdateConfig(cfg.Eval)
	e.gen.UpdateConfig(cfg.Chain)
	e.sem.UpdateConfig(cfg.Executor)
	e.runner.UpdateConfig(cfg.Executor)
	// 限速闸门必须在每次热更新时重算（D9 修复）：改配置里的 requestsPerMinute
	// 要真的改变发出的请求速率，而不是只改面板上那行读数。
	e.applyRateLimit(cfg.Executor)
	e.retryPolMu.Lock()
	e.retryPol = buildRetryPolicy(cfg.Executor)
	e.retryPolMu.Unlock()
	logRetryPolicyFixes(e.log, e.retryPol)
	newSlots := make(chan struct{}, cfg.Executor.Concurrency)
	e.mu.Lock()
	e.slots = newSlots
	e.mu.Unlock()
	// 工作目录：配置热更新后重新对齐模式（失败只告警，不影响其余子系统）。
	if err := e.ws.SetConfig(cfg.Workspace); err != nil {
		e.log.Warn("工作目录配置未生效", "err", err.Error())
	}
	e.applyWorkspaceMode()
}

// Stats 汇总统计。
func (e *Engine) Stats() model.Stats {
	all := e.store.List()
	var s model.Stats
	st := startOfToday()
	for _, t := range all {
		s.TotalTasks++
		switch t.State {
		case model.StateDone:
			// 只有通过真实验收的产出才计入"今日产出"。
			// 兼容历史数据：改造前的任务没有 Verify 字段，视为未验收但历史已归档，
			// 仍计入 Done 而不重复惩罚（只影响新任务）。
			s.DoneTasks++
			if t.CreatedAt >= st {
				s.TodayDone++
			}
			s.TotalBytes += t.OutputBytes
			s.TotalTokenIn += int64(t.TokenIn)
			s.TotalTokenOut += int64(t.TokenOut)
			if t.Verified {
				s.VerifiedTasks++
			}
		case model.StateFailed, model.StateTimeout:
			s.FailedTasks++
		case model.StateQueued:
			s.QueuedTasks++
		case model.StateRunning:
			s.RunningTasks++
		case model.StateRejected:
			s.RejectedTasks++
		case model.StateDegraded:
			// 有产出但验收不过：单列计数，不计入 DoneTasks / TodayDone /
			// SuccessRate 的分子，也不计 TotalBytes 的"有效产出"。
			s.DegradedTasks++
		}
	}
	finished := s.DoneTasks + s.FailedTasks
	if finished > 0 {
		s.SuccessRate = float64(s.DoneTasks) / float64(finished) * 100
	}
	if s.DoneTasks > 0 {
		var sum int64
		for _, t := range all {
			if t.State == model.StateDone {
				sum += t.ElapsedMs
			}
		}
		s.AvgSeconds = float64(sum) / float64(s.DoneTasks) / 1000
	}
	return s
}

// ── 治理指标源（实现 gov.MetricsSource，供体量治理采样） ──────────

// TasksToday 返回当日已完成任务数（业务体量指标）。
func (e *Engine) TasksToday() int { return e.Stats().TodayDone }

// ThroughputDaily 返回当日吞吐（完成数）。
func (e *Engine) ThroughputDaily() int { return e.Stats().TodayDone }

// Backlog 返回待处理积压数（排队 + 评估中）。
func (e *Engine) Backlog() int {
	s := e.Stats()
	return s.QueuedTasks + s.RunningTasks
}

// AvgCycleSeconds 返回平均单任务耗时（秒）。
func (e *Engine) AvgCycleSeconds() float64 { return e.Stats().AvgSeconds }

// FailureBucket 是按错误类别聚合的失败分布（P1-4）。
type FailureBucket struct {
	Kind    string   `json:"kind"`    // 错误类别（executor.Kind 枚举或 unknown）
	Count   int      `json:"count"`   // 该类别任务数
	LastAt  int64    `json:"lastAt"`  // 最近一次出现时间
	Samples []string `json:"samples"` // 最多 3 个任务 ID，便于面板跳转排查
}

// FailureBreakdown 返回失败原因分布（按 count 降序）。
//
// 数据来源是任务台账里的 ErrorKind（P0-1 已把真实分类写回任务），
// 不是从日志里正则捞——保证面板读数与持久化事实同源。
func (e *Engine) FailureBreakdown() []FailureBucket {
	all := e.store.List()
	byKind := map[string]*FailureBucket{}
	for _, t := range all {
		if t.State != model.StateFailed && t.State != model.StateTimeout {
			continue
		}
		kind := strings.TrimSpace(t.ErrorKind)
		if kind == "" {
			kind = "unknown"
		}
		b, ok := byKind[kind]
		if !ok {
			b = &FailureBucket{Kind: kind}
			byKind[kind] = b
		}
		b.Count++
		if t.FinishedAt > b.LastAt {
			b.LastAt = t.FinishedAt
		}
		if len(b.Samples) < 3 {
			b.Samples = append(b.Samples, t.ID)
		}
	}
	out := make([]FailureBucket, 0, len(byKind))
	for _, b := range byKind {
		out = append(out, *b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Count > out[j].Count
	})
	return out
}

// AttemptSummary 返回最近的尝试台账摘要（面板"最近尝试"与 metrics 用）。
func (e *Engine) AttemptSummary(limit int) []store.AttemptRecord {
	if limit <= 0 {
		limit = 20
	}
	all := e.store.List()
	var recs []store.AttemptRecord
	for i := len(all) - 1; i >= 0 && len(recs) < limit; i-- {
		if got, err := e.store.ListAttempts(all[i].ID); err == nil {
			recs = append(recs, got...)
		}
	}
	if len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	return recs
}

// FailureRate 返回任务失败率 0-1（无完成任务时记 0，由零活动特例兜底）。
func (e *Engine) FailureRate() float64 {
	s := e.Stats()
	finished := s.DoneTasks + s.FailedTasks
	if finished <= 0 {
		return 0
	}
	return float64(s.FailedTasks) / float64(finished)
}

// Concurrency 返回当前配置的并发执行数。
func (e *Engine) Concurrency() int { return e.cstore.Get().Executor.Concurrency }

// QueueSnapshot 返回队列任务。
func (e *Engine) QueueSnapshot() []*model.Task { return e.q.Snapshot() }

// RouterHealth 返回模型健康度。
func (e *Engine) RouterHealth() any { return e.router.Health() }

// Evaluator 暴露评估器（面板试评估用）。
func (e *Engine) Evaluator() *evaluate.Evaluator { return e.eval }

// Generator 暴露生成器。
func (e *Engine) Generator() *chain.Generator { return e.gen }

// Store 暴露存储（面板读写）。
func (e *Engine) Store() *store.Store { return e.store }

// Config 返回当前配置。
func (e *Engine) Config() config.Config { return e.cstore.Get() }

// Templates 暴露模板库。
func (e *Engine) Templates() *templates.Registry { return e.reg }

// Emit 让引擎可被外部推事件（如面板操作）。
func (e *Engine) Emit(ev model.Event) { e.emitEvent(ev) }

// ── 内部小工具 ───────────────────────────────────────────────────

func (e *Engine) emitEvent(ev model.Event) {
	if ev.At == 0 {
		ev.At = model.Now()
	}
	if e.emit != nil {
		e.emit.Emit(ev)
	}
}

func startOfToday() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
}

func randSuffix() string {
	return fmt.Sprintf("%04x", time.Now().UnixNano()&0xFFFF)
}

// collectOutputs 读取任务 workspace 下模型写出的所有文本文件。
func collectOutputs(workDir string) (map[string][]byte, []string, error) {
	return collectOutputsSince(workDir, 0)
}

// workspaceScratchDirs 是**代理/工具草稿**目录（round 61 · T60）。
//
// 为什么要跳过：工作空间是**多写者共用**的（人类、并行的 DSH 会话、守护脚本都往里写），
// 而 collectOutputsSince 只按 mtime 筛"窗口内变更"——于是别人的草稿会被当成"本任务的产物"。
// live 实锤（2026-10-28 23:15）：任务 task-dafac806656cc52b 的产物目录里出现
// `runtime/tmp/commitmsg-t59.md`，与 Lead 在工作空间的同名文件 **sha256 逐字节一致**（7917744BA17873D9）。
//
// 后果不止是证据被污染：产物集合决定"有没有产出"（len(produced)==0 时才会退回 final 文本兜底）
// 与返工提示词的内容 —— 别人写下的文件可以让一次**零产出**任务凭空"有产出"。
// 这些目录是工具草稿而非项目交付，跳过它们不损本意（本意是捞「模型没声明产物块、但真把文件写进了项目」的那种产出）。
var workspaceScratchDirs = []string{"runtime/tmp", "xtmp"}

// isWorkspaceScratch 判断工作空间相对路径是否落在草稿目录里（传入须为 slash 形式）。
func isWorkspaceScratch(rel string) bool {
	for _, p := range workspaceScratchDirs {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
	}
	return false
}

// collectOutputsSince 读取 workspace 下的文本文件；since>0 时只收 mtime >= since 的文件。
//
// external 模式必须带 since：工作目录是用户真实项目，全量采集会把历史文件当产出。
func collectOutputsSince(workDir string, since int64) (map[string][]byte, []string, error) {
	out := map[string][]byte{}
	var names []string
	// WalkDir 取代 Walk（2026-10-27 同一方向的第三处）：本函数每次尝试收尾都要走一遍工作目录，
	// external 模式下那就是用户真实项目（现场 Go 侧可见 295,305 个文件 / 431.5k 个目录项）；
	// Walk 对每个条目多做一次 Lstat，实测全树 16–21 s，WalkDir 直接复用 ReadDir 的目录项信息，
	// 实测 4.9–6.6 s（2.8–3.7 倍，区间为本轮 Lead 与独立复核者各自复算）。
	// 这里必须读 size/mtime，故保留 d.Info()（Windows 上由 ReadDir 的目录项缓存供给，不额外落盘）。
	err := filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			// 目录级跳过代理/工具草稿（T60）：整棵子树不进 Walk，既去污染也省扫描。
			if rel, rerr := filepath.Rel(workDir, path); rerr == nil && isWorkspaceScratch(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Size() > 8*1024*1024 {
			return nil
		}
		if since > 0 && info.ModTime().UnixMilli() < since {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".md", ".txt", ".json", ".csv", ".yaml", ".yml", ".html", ".py", ".js", ".ts", ".go", ".sql", ".sh", ".ps1", ".xml":
		default:
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(workDir, path)
		if relErr != nil {
			rel = filepath.Base(path)
		}
		relSlash := filepath.ToSlash(rel)
		if isWorkspaceScratch(relSlash) {
			return nil
		}
		out[relSlash] = data
		names = append(names, relSlash)
		return nil
	})
	return out, names, err
}

// choosePrimary 选出主产物文件：优先 md，其次最长的文本产物。
func choosePrimary(names []string, files map[string][]byte, format string) string {
	if format == "" {
		format = "md"
	}
	best := ""
	bestScore := -1
	for _, n := range names {
		score := len(files[n])
		ext := strings.ToLower(filepath.Ext(n))
		if ext == "."+format {
			score += 1_000_000
		}
		if ext == ".md" {
			score += 500_000
		}
		if strings.EqualFold(filepath.Base(n), "README.md") {
			score -= 100
		}
		if score > bestScore {
			bestScore = score
			best = n
		}
	}
	if best == "" {
		return "final.md"
	}
	return best
}

// estimateQuality 在语义层不可用时用结构启发式估一个质量分。
func estimateQuality(content []byte, minWords int) int {
	s := string(content)
	if len(s) == 0 {
		return 1
	}
	words := len([]rune(s))
	q := 4
	if words >= minWords {
		q += 2
	}
	if words >= minWords*2 {
		q += 1
	}
	if strings.Count(s, "\n## ") >= 3 {
		q += 2
	}
	if strings.Contains(s, "- ") || strings.Contains(s, "1. ") {
		q += 1
	}
	if q > 10 {
		q = 10
	}
	return q
}

var _ = errors.Is

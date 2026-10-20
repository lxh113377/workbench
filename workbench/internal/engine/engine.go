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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workbench/internal/autonomy"
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

	mu      sync.Mutex
	phase   model.EnginePhase
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	slots   chan struct{}
	started int64
	lastErr string

	inflight   sync.WaitGroup
	runningIDs sync.Map
	stopping   atomic.Bool
	tickBusy   atomic.Bool

	// taskCancels 保存每个运行中任务的取消函数（P0-3）。
	// 改造前 CancelTask 对 running 任务只写审计、不取消子进程，等于空操作。
	taskCancels sync.Map // taskID -> context.CancelFunc
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
	// pendingWarn 缓存**构造期**的降级原因（2026-10-20 实测缺陷 D12）。
	//
	// 问题形态：New() 里 e.log 先是 Nop，而 main.go 是在 New() **返回之后**才
	// SetLogger(logger)。于是构造期所有"失败只告警不阻断"的告警（审计文件打不开、
	// 自主子系统初始化失败）写进 io.Discard —— 永久消失。现场表现为自主链路没起来、
	// 越权审计文件一直是空的，但日志与面板里一条原因都查不到，只能靠猜。
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
	}
	// 工作目录审计：允许/拒绝都落盘，越权尝试可事后复核（打开失败只告警不阻断）。
	// L6 接线收口（D12）：改造前 aerr 被直接丢掉，"越权审计没开"完全不可见 ——
	// 面板显示 external 模式正常，但 workspace-audit.jsonl 始终为空，事后复核无从谈起。
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
	// 改造前 judge.run() 每次自己 executor.New(...)，语义子任务于是绕过引擎的
	// FailoverRunner（模型故障转移+车道健康度）与全局限速闸门，单测里还绕过
	// SetRunner 注入的假执行器 —— 直接后果是 go test 会真的拉起 DSH 子进程。
	e.sem.SetRunner(e.runner)
	e.retryPol = buildRetryPolicy(cfg.Executor)
	// 自主子系统：初始化失败只降级，不阻断引擎构造。
	// 这里**不能**重设 e.log = logging.Nop()（改造前有这行）：结构体初始化已把 log
	// 设为 Nop，再设一次等于把 SetLogger 之前的缓冲打回零输出（缺陷 D12）。
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
// 用法边界：只用于 New() 及其调用的构造期函数。运行期直接用 e.log.Warn ——
// 那时日志器已装好，缓冲反而推迟可见性。
func (e *Engine) warnDeferred(msg string, kv ...any) {
	e.pendingWarnMu.Lock()
	e.pendingWarn = append(e.pendingWarn, pendingWarnRecord{msg: msg, kv: kv})
	e.pendingWarnMu.Unlock()
}

// flushPendingWarn 把构造期缓冲的降级原因写进真实日志器（SetLogger 时调用一次）。
// 冲刷后清空：重复 SetLogger 不得把同一条原因写两遍。
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
// 改造前的形态 e.runner.(*executor.Runner) 在接口里带具体类型，等于没脱离结构体：
// main.go 用 FailoverRunner 包住真实执行器后再 SetRunner，这条断言就不匹配，
// 尝试日志对模型池路径静默失效（功能正常、只是没日志，运行期发现不了）。
// 现在沿 Inner() 链找 AttachHooks 能力；装不上也不报错（钩子是观测面，不影响执行）。
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
				e.log.Info("尝试结束", logging.Redact([]any{
					"taskId", task.ID, "model", modelID, "kind", kind,
					"elapsedMs", elapsed, "stalled", stalled,
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
		// 语义层跟着换（D7）：main.go 在这里才把 FailoverRunner 装上来，不同步的话
		// 构造期注入的执行器会继续留在语义层上，限速与故障转移照样被绕开。
		e.sem.SetRunner(r)
		// 换执行器后重装限速闸门（D9）：闸门挂在装饰器链最内层，只换外层不重装时，
		// 新链若没有已装闸门的内层就退化成「面板显示限速、实际不限速」。
		e.applyRateLimit(e.cstore.Get().Executor)
	}
}

// SetSemanticRunner 单独替换语义层（自评/摘要/判分）使用的执行器。
//
// 与 SetRunner 的区别：SetRunner 换任务主链路执行器并同步钩子/沙箱开关；这里只影响
// 语义子任务。用途：① 生产上语义层可用不同车道；② 测试里注入桩执行器，防止单测把
// 真实 DSH 子进程拉起来（实测一次 go test 遗留 20+ 个子进程，并把 t.TempDir 的文件
// 占用成 engine 包假红）。
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
		return nil
	}
	e.ctx, e.cancel = context.WithCancel(context.Background())
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

// Stop 优雅停止：分两段 drain（对标 riverqueue/river 的 SoftStopTimeout / StopAndCancel）。
//
// 第一段（软停）：立刻停止取新任务，等在跑任务自然收尾，最多等到宽限期；
// 第二段（硬停）：宽限期到了仍在跑的任务，取消其执行上下文并等待子进程树被杀。
//
// 改造前只有一个 30s + 60s 的"等"，到期直接返回却留下还在跑的子进程，
// 这次把"到期强制取消"补上（P0-3）。
func (e *Engine) Stop() error {
	e.mu.Lock()
	cancel := e.cancel
	wasRunning := e.phase == model.PhaseRunning || e.phase == model.PhasePaused
	e.phase = model.PhaseStopped
	e.mu.Unlock()
	e.stopping.Store(true)
	if cancel != nil {
		cancel() // 让调度循环退出，不再取新任务
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
	_ = e.store.SaveState(st)
	msg := "引擎已停止"
	if hardStop {
		msg = "引擎已停止（宽限期到期，已强制取消在跑任务）"
	}
	e.log.Info("引擎已停止", "hardStop", hardStop, "graceSeconds", int(grace.Seconds()))
	e.emitEvent(model.Event{Type: "engine", At: model.Now(), Msg: msg})
	return e.store.Savepoint()
}

// cancelAllRunning 取消所有在跑任务的执行上下文（停机兜底）。
func (e *Engine) cancelAllRunning() {
	e.taskCancels.Range(func(k, v any) bool {
		if cancel, ok := v.(context.CancelFunc); ok {
			if id, ok := k.(string); ok {
				e.cancelling.Store(id, struct{}{})
			}
			cancel()
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
	_ = e.store.SaveState(st)
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
	_ = e.store.SaveState(st)
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
	e.router.UpdateRoutes(cfg.Models)
	e.eval.UpdateConfig(cfg.Eval)
	e.gen.UpdateConfig(cfg.Chain)
	e.sem.UpdateConfig(cfg.Executor)
	e.runner.UpdateConfig(cfg.Executor)
	e.applyRateLimit(cfg.Executor)
	e.retryPolMu.Lock()
	e.retryPol = buildRetryPolicy(cfg.Executor)
	e.retryPolMu.Unlock()
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
	if until := e.poolWaitUntil.Load(); until > model.Now() {
		return
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
					e.failTask(task.ID, fmt.Errorf("执行 panic: %v", r), retry.Unknown)
				}
			}()
			e.runOne(ctx, task)
		}(t)
	}
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
	// 统计今天已生成数量
	todayStart := startOfToday()
	daily := 0
	for _, t := range all {
		if t.CreatedAt >= todayStart {
			daily++
		}
	}
	if daily >= cfg.Eval.DailyCap {
		return
	}
	// 选模板：优先从未使用/冷却结束的模板中选，避免同域扎堆
	candidates := make([]*model.Task, 0, 3)
	now := model.Now()
	for _, tpl := range enabled {
		if len(candidates) >= cfg.Executor.Concurrency {
			break
		}
		task := e.gen.RootTask(tpl, now)
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
			_ = e.store.Create(task)
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
	// 并发取消：引擎停止时不再开新任务
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	running, err := e.store.Transition(task.ID, model.StateRunning, "engine", "开始执行")
	if err != nil {
		e.q.ReleaseChain(task.ChainID)
		e.failTask(task.ID, fmt.Errorf("状态迁移失败: %w", err), retry.Unknown)
		return
	}
	e.runningIDs.Store(task.ID, true)
	// 注册取消函数：面板"终止"能真正杀掉该任务的子进程树（P0-3）
	e.taskCancels.Store(task.ID, cancel)
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
				e.finishCancelled(task.ID, lastErr)
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
			e.requeueForPoolWait(task.ID, running.ChainID)
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
		res, runErr := e.runner.Run(ctx, task, modelID)
		elapsed := time.Since(startedAt)

		// ── 成功 ───────────────────────────────────────────────
		if runErr == nil {
			if res == nil {
				lastErr = fmt.Errorf("执行器返回空结果")
				lastKind = retry.Unknown
			} else if res.TimedOut {
				lastErr = fmt.Errorf("任务超时")
				lastKind = retry.Timeout
				e.router.RecordFailure(modelID, string(executor.KindTimeout), "timeout")
			} else if res.FinalText == "" {
				lastErr = fmt.Errorf("任务无最终答案")
				lastKind = retry.Exit
				e.router.RecordFailure(modelID, string(executor.KindExit), "empty final")
			} else {
				e.router.RecordSuccess(modelID, res.Elapsed.Milliseconds())
				// 成功一次即清零限流退避：说明车道已恢复，下次限流重新从 5 分钟起。
				e.clearRateLimit()
				e.store.AppendAttempt(store.AttemptRecord{
					TaskID: task.ID, Attempt: attempt, Model: modelID, Kind: "done",
					Action: "done", ElapsedMs: elapsed.Milliseconds(),
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
			e.store.AppendAttempt(store.AttemptRecord{
				TaskID: task.ID, Attempt: attempt, Model: modelID, Kind: string(executor.KindRateLimited),
				Action: "requeue", ElapsedMs: elapsed.Milliseconds(),
				Error: errText(runErr) + " | lane=" + lane,
			})
			remaining := e.router.AvailableLanes()
			e.requeueForPoolWait(task.ID, running.ChainID)
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

		// ── 失败：判定类别 → 策略决定重试/失败/取消 ───────────────
		kind := retry.Unknown
		if res != nil && res.ErrorKind != "" {
			kind = retry.ParseKind(res.ErrorKind)
			e.router.RecordFailure(modelID, res.ErrorKind, errText(runErr))
		} else if runErr != nil {
			kind = retry.Boot
			e.router.RecordFailure(modelID, string(executor.KindBoot), errText(runErr))
		}
		lastKind = kind
		// 调用方取消优先于一切分类：不重试、不计失败
		if e.isCancelling(task.ID) || ctx.Err() != nil || kind == retry.Cancelled {
			e.store.AppendAttempt(store.AttemptRecord{
				TaskID: task.ID, Attempt: attempt, Model: modelID, Kind: string(kind),
				Action: "cancel", ElapsedMs: elapsed.Milliseconds(), Error: errText(lastErr),
			})
			e.finishCancelled(task.ID, lastErr)
			return
		}
		dec := pol.Decide(kind, attempt, errText(lastErr))
		e.store.AppendAttempt(store.AttemptRecord{
			TaskID: task.ID, Attempt: attempt, Model: modelID, Kind: string(kind),
			Action: string(dec.Action), DelayMs: dec.Delay.Milliseconds(),
			ElapsedMs: elapsed.Milliseconds(), Error: errText(lastErr),
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
	finalState := model.StateDone
	finalNote := "执行成功"
	verified := verifyRep != nil && verifyRep.Pass
	if verifyRep != nil && !verifyRep.Pass {
		finalState = model.StateDegraded
		finalNote = "产出未通过验收，已降级"
	}
	if _, err := e.store.Update(task.ID, func(t *model.Task) error {
		t.Verify = verifyRep
		t.Verified = verified
		return nil
	}); err != nil {
		e.log.Warn("写入验收结果失败", "taskId", task.ID, "err", err.Error())
	}
	if finalState == model.StateDegraded {
		// 返工环（P3）：验收失败先带着报错重做，而不是直接放弃。
		// 额度无限、时间可忽略 —— 这正是该多花几轮的地方。
		if shouldRepair(task, verifyRep, cfg) {
			e.scheduleRepair(task, verifyRep, workDir, cfg)
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
	daily := 0
	todayStart := startOfToday()
	for _, t := range all {
		if t.CreatedAt >= todayStart {
			daily++
		}
	}
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
			_ = e.store.Create(draft)
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

// isRateLimited 判断这次失败是否为上游限流（HTTP 429）。
//
// 三个来源都要覆盖（现场踩过两个）：
//  1. executor 分类后的 ErrorKind = rate_limited（正常路径）；
//  2. 包装层（FailoverRunner / 未来的远程执行器）只上抛 error，res 为 nil；
//  3. 兜底文本匹配，防止某个实现漏了分类。
func isRateLimited(res *executor.Result, err error) bool {
	if res != nil && res.ErrorKind == string(executor.KindRateLimited) {
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

// requeueForPoolWait 把任务从 running 退回 queued（合法迁移），并给整池设置等待闸门。
//
// 设计要点：
//   - 只设置闸门，不 sleep、不占并发槽；
//   - 闸门取"最早结束冷却的模型时刻 + 5 秒缓冲"，无可用信息时退化为 15 秒；
//   - 任务本身不计失败（Attempts 不变），避免把环境问题算成任务问题。
func (e *Engine) requeueForPoolWait(taskID, chainID string) {
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
}

// failTask 记录失败并落真实错误类别。
//
// 改造前这里把 ErrorKind 硬编码成 "execution"，executor 算出的 11 类
// 失败原因（超时/上游失败/缺凭据/模型下线…）在任务台账里全部丢失，
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
	_, uerr := e.store.Update(taskID, func(x *model.Task) error {
		x.Error = model.Truncate(msg, 800)
		x.ErrorKind = kindText
		x.FinishedAt = model.Now()
		if x.State == model.StateRunning {
			x.State = model.StateFailed
		}
		return nil
	})
	if uerr != nil {
		e.emitEvent(model.Event{Type: "error", At: model.Now(), TaskID: taskID, Msg: "更新失败状态出错: " + uerr.Error()})
	}
	e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: taskID, To: string(model.StateFailed), Actor: "engine", Kind: "error", Message: model.Truncate("["+kindText+"] "+msg, 300)})
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
func (e *Engine) finishCancelled(taskID string, err error) {
	msg := "用户取消"
	if t := ctxCancelText(err); t != "" {
		msg = t
	}
	if _, terr := e.store.Transition(taskID, model.StateCancelled, "user", msg); terr != nil {
		// 迁移失败（已是终态等）时只记审计，不覆盖既有状态
		e.store.Audit(store.AuditRecord{At: model.Now(), TaskID: taskID, Actor: "user", Kind: "cancel", Message: "取消收尾: " + terr.Error()})
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
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
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
		e.router.UpdateRoutes(cfg.Models)
	}
	e.eval.UpdateConfig(cfg.Eval)
	e.gen.UpdateConfig(cfg.Chain)
	e.sem.UpdateConfig(cfg.Executor)
	e.runner.UpdateConfig(cfg.Executor)
	// 限速闸门必须在每次热更新时重算（D9）：改配置里的 requestsPerMinute 要真的
	// 改变发出的请求速率，而不是只改面板上那行读数。
	e.applyRateLimit(cfg.Executor)
	e.retryPolMu.Lock()
	e.retryPol = buildRetryPolicy(cfg.Executor)
	e.retryPolMu.Unlock()
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

// collectOutputsSince 读取 workspace 下的文本文件；since>0 时只收 mtime >= since 的文件。
//
// external 模式必须带 since：工作目录是用户真实项目，全量采集会把历史文件当产出。
func collectOutputsSince(workDir string, since int64) (map[string][]byte, []string, error) {
	out := map[string][]byte{}
	var names []string
	err := filepath.Walk(workDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
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
		out[filepath.ToSlash(rel)] = data
		names = append(names, filepath.ToSlash(rel))
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

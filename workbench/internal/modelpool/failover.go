package modelpool

import (
	"context"
	"errors"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// ── 故障转移执行器（无感切换的核心） ──────────────────────────────
//
// 引擎一次尝试只选一个模型；如果那个模型超时/报错，传统做法是「失败 → 退避 5s
// → 换模型重试」，用户看到的是 5 秒空窗 + 失败计数 + 最终仍可能判死。
//
// FailoverRunner 包在真实执行器外面，在**一次尝试内部**完成切换：
//  1. 按池子的调度顺序（优先级/轮询）拿到候选链；
//  2. 依次调用真实执行器，失败立刻换下一个（不睡退避）；
//  3. 第一个成功的结果原样返回给引擎 —— 引擎只看到「成功」，用户只看到产出。
//
// 只有整条链都失败才把最后一个错误抛回引擎，交给既有的重试策略处理。
// 每次切换都写 model-status.jsonl（kind=switch），保证过程可追溯。

// FailoverRunner 是带模型故障转移的执行器装饰器。
type FailoverRunner struct {
	inner executor.RunnerContract
	mgr   *Manager
	sch   *Scheduler
	// MaxSwitch 是单次尝试内最多切换几个模型（0 = 用池内全部可用模型，上限 8）。
	MaxSwitch int
	// MinRemain 是切换前要求剩余的任务时间（低于它就不再换，交给引擎重试）。
	MinRemain time.Duration
}

// NewFailoverRunner 构造装饰器。
func NewFailoverRunner(inner executor.RunnerContract, mgr *Manager, sch *Scheduler) *FailoverRunner {
	return &FailoverRunner{inner: inner, mgr: mgr, sch: sch, MaxSwitch: 8, MinRemain: 45 * time.Second}
}

// UpdateConfig 透传执行器配置（满足 executor.RunnerContract）。
func (f *FailoverRunner) UpdateConfig(cfg config.ExecutorConfig) {
	if f.inner != nil {
		f.inner.UpdateConfig(cfg)
	}
}

// Inner 暴露被装饰的真实执行器，供引擎沿装饰器链下探。
//
// 关键：external 模式的 allowAll 开关与工作目录上下文钩子都挂在具体执行器上，
// 引擎用「接口断言 + Inner() 链」找它。装饰器不实现 Inner() 会让这些能力
// 静默失效（切到 external 后仍按项目模式跑），属于运行期难发现的缺陷。
func (f *FailoverRunner) Inner() executor.RunnerContract { return f.inner }

// AttachHooks 把钩子透传给内层执行器（L5 收敛，2026-10-20）。
//
// 没有它时的形态：引擎沿 Inner() 链找 AttachHooks 会在装饰器这层停下（装饰器自己
// 不实现），于是「尝试开始/尝试结束」的结构化日志对模型池路径静默失效 ——
// 属于运行期发现不了的缺陷（功能正常，只是没日志）。
func (f *FailoverRunner) AttachHooks(h *executor.Hooks) {
	if f.inner != nil {
		if x, ok := f.inner.(interface{ AttachHooks(*executor.Hooks) }); ok {
			x.AttachHooks(h)
		}
	}
}

// Run 执行一次任务：主模型失败即在内部换模型，成功结果对上层不可见地返回。
func (f *FailoverRunner) Run(ctx context.Context, task *model.Task, modelID string) (res *executor.Result, runErr error) {
	if f.inner == nil {
		return nil, errNoInner
	}
	if f.mgr == nil || f.sch == nil {
		return f.inner.Run(ctx, task, modelID)
	}
	now := nowMs()
	specs := f.mgr.Specs()
	chain := f.sch.PickChain(specs, now)
	order := make([]string, 0, len(chain)+1)
	seen := map[string]bool{}
	if strings.TrimSpace(modelID) != "" {
		order = append(order, modelID)
		seen[modelID] = true
	}
	for _, id := range chain {
		if seen[id] || len(order) >= f.MaxSwitch+1 {
			continue
		}
		order = append(order, id)
		seen[id] = true
	}
	if len(order) == 0 {
		// 池内**没有任何可用车道**（全部冷却/下线/停用）：不要拿引擎给的 modelID 硬跑 ——
		// round 35 实测：全池只剩 3 条 `not_served` 车道时，硬跑把两个任务烧成 `boot_failed` 任务失败。
		// 返回哨兵，让引擎设池闸门 + 把任务退回队列（T19）。
		return nil, executor.ErrNoLane
	}
	// 链首优先复用「刚被证明能产出」的模型（T17①）：只在候选链**内部**重排，绝不引入链外模型，
	// 因此不会把冷却/下线的模型拉回来。
	if okID, _, ok := f.mgr.LastOKModel(); ok {
		order = preferFirst(order, okID)
	}

	var lastRes *executor.Result
	var lastErr error
	// from 必须是「上一个失败让位的模型」，不是本次尝试的首发模型。
	// 历史缺陷（2026-10-19）：这里写成 from := order[0] 且循环内从不更新，
	// 于是 a 失败、b 也失败、c 成功时，两条切换记录都记 from=a（a→b、a→c）。
	// 功能没坏（切换确实发生了），只有留痕是错的 —— 拿这份日志排查会以为
	// b 从没被调用过，恰好违背需求4「保留模型状态日志便于排查问题」。
	// 每次失败后推进到当前模型（见循环末尾）。
	from := order[0]
	for i, id := range order {
		if i > 0 {
			// 切换前确认还有足够时间，避免把「任务超时」误当「模型故障」反复换。
			if !hasTimeLeft(ctx, f.MinRemain) {
				break
			}
			f.mgr.AppendSwitch(transitionRecord{
				TaskID: task.ID, From: from, To: id,
				Reason: "模型失败自动切换（" + firstNonEmpty(errTextBounded(lastErr), kindOf(lastRes), "unknown") + "）",
			})
		}
		f.sch.Acquire(id)
		res, err := f.inner.Run(ctx, task, id)
		f.sch.Release(id)
		// 本次尝试是否要求产物：**执行器上报**为准（协议是它拼进 prompt 的），
		// task.Prompt 里带协议块只作兜底（将来若改由上层拼，判据仍成立）——round 21 · T18②③。
		ok, kind, detail := judge(res, err, requireArtifactsFor(res, task))
		if ok {
			f.mgr.MarkSuccess(id, elapsedMs(res))
			return res, err
		}
		// 只有「可归因到模型」的失败才计入模型健康度：
		//   - 结构性：model_unavailable / missing_credential / unreachable；
		//   - 环境性：timeout / upstream_failed / stalled。
		// bad_exit（尤其「无最终答案」）往往是任务/提示词层面的问题，
		// 把它算到模型头上会让整池在几分钟内被打入长冷却（现场 6 个模型同时 down）。
		if modelAttributable(kind) {
			f.mgr.MarkFailure(id, kind, detail)
		} else if atKind, hit := bootAttributionKind(kind, detail); hit {
			// 上游原文写明「这条出口不服务该模型」⇒ 按 not_served 记：MarkFailure 对该类别一击即冷却 + 长冷却，
			// 且探测**不得**把它治回（它是出口级事实，不是端点连通性问题）——round 33 T17② 归因被探测抵消的教训（T17②-b）。
			f.mgr.MarkFailure(id, atKind, detail)
		} else {
			f.mgr.log(logRecord{Kind: "skip", Model: id, State: kind,
				Reason: "任务级失败，不计入模型健康度: " + trim(detail, 160)})
		}
		// 把本轮判定的 kind **写回** res.ErrorKind（2026-10-19 现场缺陷修复）。
		//
		// 现场：judge() 已经正确判出 kind（含 bad_exit「无最终答案」），
		// 但这里只把 lastRes/lastErr 原样上抛；engine 拿不到 ErrorKind，
		// 落进它的兜底分支后一律记成 boot_failed（启动失败）。
		//
		// 危害（实测 34 条 boot_failed，其中 elapsedMs 有 378478ms 即 6 分钟的）：
		//   ① UI 显示「启动失败」，实际是「模型产不出终稿」—— 排查方向完全错误；
		//   ② 6 分钟的「启动失败」自相矛盾，日志可信度受损；
		//   ③ 与「429 曾被误判 boot_failed」是同一个兜底分支的两次事故。
		//
		// 修法：失败时保证 res.ErrorKind 带上本轮 kind。
		// bad_exit 不属于 modelAttributable（上面已判定），
		// 所以写回 kind 只修「留痕语义」，不会让本可用的模型被误记故障。
		if res == nil {
			res = &executor.Result{}
		}
		if strings.TrimSpace(res.ErrorKind) == "" {
			res.ErrorKind = kind
		}
		lastRes, lastErr = res, err
		// 推进 from：下一次切换是「从当前这个失败模型」换走，
		// 而不是从首发模型换走（多跳时这一点决定日志是否可信）。
		from = id
		// 用户取消 / 上下文结束：不再换模型，原样返回。
		if ctx.Err() != nil || kind == string(executor.KindCancelled) {
			return lastRes, lastErr
		}
		// 上游限流（429）：立即停止换模型，原样上抛交给 engine 做车道冷却。
		//
		// 现场实测（2026-10-19 08:08，隔离实例）：限流时这里连试 8 个模型（每个约 3 秒），
		// 24 秒内打空整池并全部记失败。限流是通道级/账号级的，换模型毫无意义，
		// 只会延长限流窗口（现场限流因此持续 50+ 分钟）。
		//
		// 注意：这里**不** MarkFailure —— 健康模型不该因为车道限流被冷却 30 分钟。
		if kind == string(executor.KindRateLimited) {
			f.mgr.log(logRecord{Kind: "skip", Model: id, State: kind,
				Reason: "上游限流，停止换模型（车道级，非模型故障）"})
			return lastRes, lastErr
		}
	}
	// 整链失败后返回：必须保证「失败信号」与「失败类别」同时可见（2026-10-19 现场）。
	//
	// 现场：judge() 对「空产出且 inner 未返回 error」判为 bad_exit，
	// 但循环只在 res.ErrorKind 上留痕，**返回值仍是 (res, nil)** ——
	// 缺少 error 时上游若只看 err 会误判成功。
	// 这里在整链失败时补一个 error，让「有失败」这件事不依赖调用方的额外判断。
	if lastErr == nil && lastRes != nil && strings.TrimSpace(lastRes.ErrorKind) != "" {
		lastErr = errors.New(firstNonEmpty(lastRes.LastError, "执行失败: "+lastRes.ErrorKind))
	}
	return lastRes, lastErr
}

// judge 判断一次执行是否算成功，并给出失败类别与详情。
//
// 与引擎 runOne 的判定保持一致：超时、空产出、错误都算失败；错误类别优先取
// 执行器给出的 ErrorKind，其次按错误对象兜底。
func judge(res *executor.Result, err error, requireArtifacts bool) (bool, string, string) {
	if err != nil {
		kind := string(executor.KindUnknown)
		if res != nil && res.ErrorKind != "" {
			kind = res.ErrorKind
		} else if isMissingCredential(err) {
			kind = string(executor.KindMissingKey)
		}
		return false, kind, err.Error()
	}
	if res == nil {
		return false, string(executor.KindUnknown), "执行器返回空结果"
	}
	if res.TimedOut {
		return false, string(executor.KindTimeout), "任务超时"
	}
	// 结构化错误类别优先于"空产出"规则：429 时 final 本来就是空的，
	// 若先走空产出分支会被覆盖成 bad_exit（现场：限流被误判为任务问题，
	// 于是引擎换 8 个模型打空整池）。
	if res.ErrorKind != "" && res.ErrorKind != string(executor.KindNone) {
		return false, res.ErrorKind, firstNonEmpty(errTextBounded(nil), res.LastError)
	}
	if strings.TrimSpace(res.FinalText) == "" {
		return false, string(executor.KindExit), "任务无最终答案"
	}
	// 要求产物的任务：只有「吐了字」还不够，必须真带产物块（round 21 · T18）。
	// 实测形态：某车道 kind=ok 但最终文本是过程独白（含它自己的 <dots_function_call> 协议），
	// 引擎闸门随后判未完成 —— 修前 failover 已把它当成功返回，后面的产物门就成了事后诸葛。
	if requireArtifacts && !res.ArtifactBlock {
		return false, noArtifactKind, "回答不含产物块（不符合 ARTIFACTS 协议）"
	}
	return true, "", ""
}

// modelAttributable 判断一次失败是否可以归因到「模型不可用」。
func modelAttributable(kind string) bool {
	switch kind {
	case string(executor.KindModelDead), string(executor.KindMissingKey),
		string(executor.KindUpstream), string(executor.KindTimeout),
		string(executor.KindStalled):
		return true
	// 网络/传输与流中断**可以归因到模型端点**（2026-10-19 现场补齐）。
	//
	// 理由：这两个类别正是「这个端点此刻连不上/流断了」——换一个模型
	// 恰恰是正确处置（同池其他车道的网络路径不同）。
	// 与 bad_exit 相反：bad_exit 是任务/提示词层面的问题，不该记模型头上。
	case string(executor.KindTransport), string(executor.KindStreamCut):
		return true
	// 协议不合规也归因到**车道能力**（round 21 · T18）：实测某车道只会吐自有 <dots_function_call> 协议，
	// 不认本仓 <<<ARTIFACTS>>>；把它当"任务问题"会让整池继续把它排在候选前列，任务永远交不出成品。
	case noArtifactKind:
		return true
	}
	// unreachable / bad_endpoint 是探测层的分类，同样归因到模型端点。
	return kind == "unreachable" || kind == "bad_endpoint" || kind == "model_unavailable"
}

// modelAttributable 判断一次失败是否可以归因到「模型不可用」。

func kindOf(res *executor.Result) string {
	if res != nil && res.ErrorKind != "" {
		return res.ErrorKind
	}
	return ""
}

func elapsedMs(res *executor.Result) int64 {
	if res == nil {
		return 0
	}
	return res.Elapsed.Milliseconds()
}

func hasTimeLeft(ctx context.Context, need time.Duration) bool {
	dl, ok := ctx.Deadline()
	if !ok {
		return true
	}
	return time.Until(dl) > need
}

func isMissingCredential(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "missing_credential") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401")
}

// errTextBounded 与 engine/main 里的 errText **不同契约**：这里刻意**截断到 200 字符**（状态/原因字串有长度界）。
// 2026-10-25 round 177：本函数原名 errText —— 与另两个包里「返回完整 err.Error()」的同名函数契约不同，
// 属「同名不同契约」漂移（本仓在册族）。改名让契约写在名字上，行为一字未改。
func errTextBounded(err error) string {
	if err == nil {
		return ""
	}
	return trim(err.Error(), 200)
}

var errNoInner = &innerError{}

type innerError struct{}

func (e *innerError) Error() string { return "模型池：内部执行器未注入" }

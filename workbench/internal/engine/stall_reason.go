package engine

// stall_reason.go 实现「运行中但产出为 0」的自证（2026-10-21 现场需求）。
//
// 现场问题形态（用户原话）：面板显示"引擎 运行中"，但「今日产出 0」，
// 排队 0、正在执行 0 —— 三个读数同时是 0，而引擎又确实是 running。
// 用户无法从界面上判断：是在等模型？是配额满了？是规划碎了？还是卡死了？
//
// 这类"静默停摆"是 24/7 无人值守系统最坏的失效形态：所有指标都"正常"
// （进程活着、阶段是 running、没有错误），只有产出是 0。
// 系统必须能自证"我为什么没在干活"，否则运维只能靠猜。
//
// 本文件把"为什么此刻没有产出"归纳为有限的几种可解释原因，并给出人话文案。
// 判据优先级：结构性阻塞 > 配额耗尽 > 规划熔断 > 模型池冷却 > 无任务可做 > 正常。

import (
	"fmt"
	"strings"

	"workbench/internal/model"
)

// StallReason 是"为什么此刻没有产出"的结构化解释。
type StallReason struct {
	// Code 是机器可读原因码（空串表示没有停摆，正在正常产出）。
	Code string `json:"code,omitempty"`
	// Text 是给用户看的一句话（面板直接渲染）。
	Text string `json:"text,omitempty"`
	// Since 是该原因最早可观测的时刻（毫秒，0 表示未知）。
	Since int64 `json:"since,omitempty"`
	// Detail 是补充证据（计数/阈值等，便于排查）。
	Detail string `json:"detail,omitempty"`
}

// 停摆原因码（稳定标识，面板与外部集成可据此分流处理）。
const (
	// StallNone：没有停摆（有产出，或引擎不在运行阶段）。
	StallNone = ""
	// StallLaneBlocked：上游结构性阻塞（IP 封禁/地区限制），等多久都不会恢复。
	StallLaneBlocked = "lane_blocked"
	// StallRateLimited：上游限流冷却中（可自愈，等冷却结束）。
	StallRateLimited = "rate_limited"
	// StallDailyCap：当日配额耗尽（被拒草案曾把它提前吃满，见 tasksToday 注释）。
	StallDailyCap = "daily_cap"
	// StallRejectBreaker：同一目标连续被评估拒绝，规划已熔断。
	StallRejectBreaker = "reject_breaker"
	// StallPoolCooling：模型池整体冷却，任务被反复退回队列。
	StallPoolCooling = "pool_cooling"
	// StallAutonomyStalled：自主目标已阻塞或预算耗尽，且模板链也未补货。
	StallAutonomyStalled = "autonomy_stalled"
	// StallNoSupply：既没有自主草案也没有模板候选（模板池为空/全被拒）。
	StallNoSupply = "no_supply"
	// StallQueuedNoLane：有任务排队，但模型池整池冷却调度闸门挡着，一个都没在跑。
	//
	// 现场形态（2026-10-21 22:3x，用户报「排队 14 但是 0 正在执行」）：
	// 上游 429 把整池闸门推到 20~30 分钟后，tick 直接早退；队列里 14 条任务
	// 一条都不在跑，runningTasks=0、todayDone 冻结。旧口径把"有排队任务"
	// 一律当成"只是还没轮到"（见上方早退条件），于是这个形态既不报停摆、
	// 也没有任何读数解释 —— 用户看到的正是"排队 14 / 正在执行 0"这句矛盾的话。
	//
	// 与 StallPoolCooling 的区别（别混）：
	//   - StallPoolCooling 只在"没有任何任务排队/在跑"时才走到（下方第 4 条）；
	//   - 本条是它的现实常态：队列里明明有活，闸门却把它们全按住了。
	// 两条都必须存在，否则用户能看到的两种形态总有一种没有解释。
	StallQueuedNoLane = "queued_no_lane"
)

// StallUpstreamRejecting 表示"任务在跑，但上游在系统性拒答"。
//
// 与 StallRateLimited 的区别：限流是"还没发请求就被闸门挡住"（整池冷却），
// 这里说的是一批任务**已经派发出去、也确实在跑**，却全部因为上游同一类拒绝
// （EAC_UPSTREAM_FAILED / SERVER / 无资格出口）而拿不到任何产出。
//
// 现场形态（2026-10-21）：面板 running=6、queued=3，todayDone=0；
// events.jsonl 里每一轮 turn 都以
//
//	{"kind":"error","error":{"message":"no eligible anonymous exit; retry after cooldown",
//	  "code":"EAC_UPSTREAM_FAILED"}}
//
// 结束，然后换模型重来 —— 换模型没用，因为拒绝发生在账号/出口池层面，不是模型层面。
// 这是**外部约束**（上游免费车道配额），代码改不动；但必须让面板说清楚，
// 否则用户看到的就是"运行中但产出 0"这句无法解释的话。
const StallUpstreamRejecting = "upstream_rejecting"

// upstreamFailingStall 判断"跑着但上游系统性拒答"这一形态。
//
// 判据（三条同时成立，避免噪音）：
//  1. 今天确实一条都没完成（TodayDone == 0）—— 有产出就不是停摆；
//  2. 有任务正在跑（RunningTasks > 0）—— 没在跑属于别的停摆形态；
//  3. 最近的执行事件里，出现了上游拒绝类错误码。
//
// 实现只读 e.lastErr / 限流状态等**已有**信号，不新增热路径开销：
// 任务失败时引擎已把错误写进 lastErr（见 failTask），这里直接取用。
//
// stats 由调用方传入（2026-10-28 性能修复）：原先这里自带一次 e.Stats()，而调用方
// （面板 /api/status）本来就已经算过一份 ⇒ 同一次请求把全量 store.List() 重复付 5~6 次
// （生产 5001 条，实测单价 2.0–2.75ms + 2.9MB 垃圾，见 store/bench_perf_probe_test.go）。
// 传入的第二层收益：三个判据看的是**同一时刻**的快照，不再出现「前一个判据读到 running=1、
// 后一个读到 running=0」的撕裂读数（原判据各自重算，这条时间面缺口一直存在）。
func (e *Engine) upstreamFailingStall(now int64, stats model.Stats) (StallReason, bool) {
	if stats.TodayDone > 0 || stats.RunningTasks == 0 {
		return StallReason{}, false
	}
	last := e.LastError()
	if last == "" {
		return StallReason{}, false
	}
	code := upstreamRejectKind(last)
	if code == "" {
		return StallReason{}, false
	}
	return StallReason{
		Code:  StallUpstreamRejecting,
		Text:  "任务在跑但上游持续拒答（" + code + "）：换模型无效，需等上游账号/出口池冷却或换通道",
		Since: now,
		Detail: "这是外部约束（上游免费车道的账号/出口池配额），不是本机缺陷；" +
			"面板会继续重试，冷却结束后自动恢复。上游原文：" + model.Truncate(last, 160),
	}, true
}

// queuedWaitingForLane 判断"有活排队，但一个都没在跑，且整池闸门按住调度"这一形态。
//
// 现场（2026-10-21 22:31~22:39 实测，用户报的就是这个画面）：
//
//	queuedTasks=14 runningTasks=0，stallReason 为空；rateLimit.limited=true
//	remainingSeconds=442；tick 因 e.poolWaitUntil 早退，日志里一小时没有一行
//	"任务开始执行"。用户只能看到"排队 14 / 正在执行 0"，既不知道在等什么，
//	也不知道要等多久 —— 而服务端明明握着两个字段能说清（rateLimit + 队列长度）。
//
// 判据（三条同时成立，缺一不报，避免把"刚起队列的正常空窗"喊成故障）：
//  1. 队列里有任务（QueuedTasks > 0）；
//  2. 一条都没在跑（RunningTasks == 0）—— 跑着属于别的形态；
//  3. 整池冷却闸门仍在生效（poolWaitUntil > now）。
//
// 不依赖 TodayDone：这条原因解释的是"为什么此刻 0 个在跑"，不是"今天有没有产出"。
// stats 同 upstreamFailingStall：由调用方传入同一份快照，这里不重算。
func (e *Engine) queuedWaitingForLane(now int64, stats model.Stats) (StallReason, bool) {
	if stats.QueuedTasks == 0 || stats.RunningTasks > 0 {
		return StallReason{}, false
	}
	until := e.poolWaitUntil.Load()
	if until <= now {
		return StallReason{}, false
	}
	wait := (until - now) / 1000
	rl := e.RateLimitStatus()
	detail := fmt.Sprintf("整池冷却第 %d 次，剩余约 %d 秒后自动重试（闸门到点即恢复调度）。",
		rl.Consecutive, wait)
	if rl.LastReason != "" {
		detail += " " + rl.LastReason
	}
	return StallReason{
		Code:   StallQueuedNoLane,
		Text:   fmt.Sprintf("模型池冷却中：%d 个任务在排队，一个都没在跑；约 %d 秒后自动重试", stats.QueuedTasks, wait),
		Since:  rl.LastAt,
		Detail: detail,
	}, true
}

// upstreamRejectKind 从错误文本里识别上游拒绝类别（空串 = 不是这类错误）。
//
// 识别的是 OFM 插件与上游网关都会给出的稳定错误码/文案；
// 用子串匹配而不是精确相等，是因为错误前缀会随上下文变化。
func upstreamRejectKind(lastErr string) string {
	switch {
	case strings.Contains(lastErr, "no eligible anonymous exit"),
		strings.Contains(lastErr, "EAC_UPSTREAM_FAILED"):
		return "账号/出口池不可用"
	case strings.Contains(lastErr, "EAC_UPSTREAM_FAILED: Function"),
		strings.Contains(lastErr, "Not found for account"):
		return "上游账号能力缺失"
	case strings.Contains(lastErr, "UNSUPPORTED_REASONING_EFFORT"):
		return "模型不支持该参数"
	case strings.Contains(lastErr, "does not support reasoning effort"):
		return "模型不支持该参数"
	}
	return ""
}

// StallReason 计算"为什么此刻没有产出"。
//
// 只在"引擎处于 running 且当前无产出压力"时给出非空原因；
// 其余情形返回 StallNone（正常产出中，或引擎本来就没在跑）。
func (e *Engine) StallReason() StallReason {
	// 兼容入口（对外签名不变）：手上没有 stats 的调用方（CLI / 治理采样）自算一份。
	// 已经算过 Stats() 的调用方（面板 /api/status）**必须改走 StallReasonWith**，
	// 否则同一次请求里全量 store.List() 会被重复付 —— 由来见 upstreamFailingStall 注释。
	return e.StallReasonWith(e.Stats())
}

// StallReasonWith 与 StallReason 判据、文案、优先级完全相同，只多收一份调用方已算好的
// stats 快照。拆两个入口而不是给 StallReason 加参数：后者是对外 API（web/CLI 都在调），
// 改签名就违反「对外接口不变」。
func (e *Engine) StallReasonWith(stats model.Stats) StallReason {
	if e.Phase() != model.PhaseRunning {
		return StallReason{}
	}
	now := model.Now()
	// 「有任务在跑」不等于「在产出」（2026-10-21 现场，这是用户报的那句话的正解）：
	//   面板显示 running=6 / queued=3，看起来一切正常，但 todayDone=0 —— 因为 6 条任务
	//   全部卡在"上游拒答→换模型→再被拒"的重试环里，一条都没有真正完成。
	//   旧的早退条件（RunningTasks > 0 就不报停摆）恰好把这个形态判成"正常"，
	//   于是最需要解释的现象反而没有解释。必须先判"跑着但没有任何完成"。
	if stall, hit := e.upstreamFailingStall(now, stats); hit {
		return stall
	}
	// 「有排队任务」不等于「不用解释」（2026-10-21 22:3x 现场补判）：
	// 整池闸门按住调度时队列会一直满着而 running 恒为 0，旧早退条件把这个形态
	// 当成"只是还没轮到"，于是用户看到的"排队 14 / 正在执行 0"没有任何解释。
	// 这条必须在下面的早退之前判 —— 它比"还没轮到"更具体，有确定的等待时长。
	if stall, hit := e.queuedWaitingForLane(now, stats); hit {
		return stall
	}
	if stats.TodayDone > 0 || stats.RunningTasks > 0 || stats.QueuedTasks > 0 {
		// 仍有在跑或待跑的活，且没有证据表明上游在系统性拒答 => 不算停摆。
		return StallReason{}
	}

	// 1) 结构性阻塞最优先（等下去不会好，必须让人知道）。
	if rl := e.RateLimitStatus(); rl.Limited {
		if rl.Blocked {
			return StallReason{
				Code:   StallLaneBlocked,
				Text:   "上游结构性阻塞（IP 封禁/地区限制）：冷却结束后也不会自动恢复，需换网络出口或等解封",
				Since:  rl.LastAt,
				Detail: rl.LaneEvidence,
			}
		}
		return StallReason{
			Code:   StallRateLimited,
			Text:   fmt.Sprintf("上游限流冷却中（剩余 %d 秒）：冷却结束后会自动恢复", rl.RemainingSeconds),
			Since:  rl.LastAt,
			Detail: rl.LastReason,
		}
	}

	// 2) 当日配额耗尽：这是"整天不再产出"的直接成因，必须说清计数与阈值。
	cfg := e.cstore.Get()
	if cfg.Eval.DailyCap > 0 {
		if n := e.tasksToday(); n >= cfg.Eval.DailyCap {
			return StallReason{
				Code:   StallDailyCap,
				Text:   fmt.Sprintf("今日任务配额已用满（%d/%d）：引擎仍运行，但今日不再生成新任务", n, cfg.Eval.DailyCap),
				Since:  startOfToday(),
				Detail: "配额只统计真正执行过的任务（被拒草案不计入）；次日 0 点自动恢复",
			}
		}
	}

	// 3) 规划熔断：连续被拒后主动停手（本轮现场的根因表现）。
	if e.autonomy != nil && cfg.Autonomy.Enabled {
		if g, ok := e.autonomy.Active(); ok && g != nil {
			// 诊断口径与供料口径一致（round 47 · T35）：都走同一个时间窗，
			// 否则「停机原因说熔断」而「供料其实已经放过」—— 诊断与行为对不上。
			if streak, tripped := e.rejectionPressure(g.ID, cfg.Autonomy.RejectStreakWindowMinutes); tripped {
				return StallReason{
					Code: StallRejectBreaker,
					Text: fmt.Sprintf("自主规划已熔断：目标「%s」连续 %d 条草案被评估拒绝，暂停规划以免空转刷台账",
						model.Truncate(g.Title, 40), streak),
					Since:  now,
					Detail: "修复评估口径后熔断会自动解除；也可在面板切换/恢复目标强制重来",
				}
			}
			if g.Status != "active" {
				return StallReason{
					Code:   StallAutonomyStalled,
					Text:   fmt.Sprintf("自主目标未在推进（状态 %s）：%s", g.Status, model.Truncate(g.BlockReason, 80)),
					Since:  g.UpdatedAt,
					Detail: "可在面板「自主」里恢复该目标，或设置一个新目标",
				}
			}
		}
	}

	// 4) 模型池整体冷却：任务会被反复退回队列，表现为"一直没开始跑"。
	if until := e.poolWaitUntil.Load(); until > now {
		return StallReason{
			Code:   StallPoolCooling,
			Text:   fmt.Sprintf("模型池整体冷却中（剩余 %d 秒）：冷却结束后自动继续", (until-now)/1000),
			Since:  now - 1,
			Detail: "全部模型都处于冷却/不可用状态时，任务会被退回队列等待",
		}
	}

	// 5) 没有任何供货：既没自主草案也没模板候选。
	if e.store != nil {
		if len(e.Templates().Enabled()) == 0 && !e.autonomyEnabled() {
			return StallReason{
				Code:   StallNoSupply,
				Text:   "没有任何可用任务来源：模板池为空且自主规划未启用",
				Detail: "到「模板」页添加模板，或在设置里启用自主规划",
			}
		}
	}
	// 兜底：正在正常供货但有短暂空窗（tick 间隔内），不报停摆以免噪音。
	return StallReason{}
}

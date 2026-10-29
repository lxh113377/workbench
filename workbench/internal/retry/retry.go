// Package retry 把"失败之后怎么办"从引擎里的 for 循环提炼成可配置、可测试的策略对象。
//
// 设计来源（对标 riverqueue/river 的 ClientRetryPolicy 与 hibiken/asynq 的
// retryDelayFunc / SkipRetry 哨兵语义）：
//   - river：策略对象化，输入是作业行（含 Attempt/MaxAttempts/Kind），输出"下次重试时间"；
//   - asynq：把"不值得重试"做成显式返回值（SkipRetry / RevokeTask），而不是靠调用方猜。
//
// 本包只做纯计算：不 sleep、不碰磁盘、不读配置，便于单测与复用。
package retry

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Kind 是失败类别。取值与 executor.Kind 的字符串一一对应，
// 但本包不 import executor，避免把重试策略和"怎么执行"绑死。
type Kind string

const (
	None       Kind = ""
	Timeout    Kind = "timeout"
	MissingKey Kind = "missing_credential"
	ModelDead  Kind = "model_unavailable"
	Upstream   Kind = "upstream_failed"
	Boot       Kind = "boot_failed"
	Exit       Kind = "bad_exit"
	// Transport / StreamCut 与 executor.Kind 的字符串一一对应（2026-10-19 现场补齐）。
	//
	// 为什么要同步新增：executor.classify() 现在会把网络类与流中断判成这两个类别，
	// 若 retry 侧不认识它们，ParseKind 会把它们收敛成 Unknown ——
	// 类别信息在重试层被丢弃，且**可重试表**（配置 RetryableKinds）也管不到它们，
	// 网络抖动这种「重试就能好」的故障会变成一次判死。
	Transport Kind = "transport_failed"
	StreamCut Kind = "stream_cut"
	Cancelled Kind = "cancelled"
	Stalled   Kind = "stalled"
	// NoArtifact 对应「回答有内容、但没有产物块」（executor.KindNoArtifact）。
	// 它**可重试**：换一条车道往往就能拿到合规产物（round 22 实测：同一次尝试内 dots/kilo/liquid 连续因此被拒）。
	// 为什么要在这里新增（2026-10-26 round 22 实测缺口）：不进 ParseKind 的话，类别落到 Unknown，
	// 引擎台账只写 `kind=unknown`，「判据生效了但日志里查不到原因」。
	NoArtifact Kind = "no_artifact"
	// NotServed 对应「上游在这条出口上不服务该模型」（executor.KindNotServed）。
	// 必须在这里登记：否则 ParseKind 收敛成 Unknown，引擎台账只写 kind=unknown（round 22 同款教训）。
	NotServed Kind = "not_served"
	// NoLane 对应「池内没有任何可用车道」（executor.KindNoLane）：它不是任务失败，而是整池此刻不可用，
	// 引擎应退回队列等冷却（T19）。登记它是为了让台账不落成 unknown。
	NoLane Kind = "no_lane"
	// RateLimited 对应 executor.KindRateLimited（上游 429，车道级限流）。
	//
	// 为什么现在才补：executor 侧早有 KindRateLimited，retry 侧却没有对应常量 ⇒ ParseKind("rate_limited")
	// 一律收敛成 Unknown，连「可重试表」（RetryableKinds）都管不到它。这是同一形态的**第四次**复发 ——
	// 上面 transport/stream_cut、no_artifact、not_served、no_lane 四处注释写的都是同一句教训
	// 「不进 ParseKind ⇒ 台账只写 kind=unknown」。前几次靠「谁撞上谁补」，本轮起由 kind_drift_test.go
	// 逐条比对 executor 的 Kind 枚举，把这类漏登记变成必红（对标 river 的「枚举漏项即 panic」）。
	// 语义：可重试 —— 429 是「等冷却再试」，不是「任务坏了」，与 executor 侧 Retryable 同判。
	RateLimited Kind = "rate_limited"
	// PoolWaitCapped 是「因环境（池不可用/限流）退回队列的次数到顶，任务判死」
	// （engine.requeueForPoolWait，round 60 · T54 的封顶路径）。
	//
	// 现场（生产 logs/workbench.jsonl 全量复核，2026-10-29）：ERROR「任务失败」里 kind=unknown 共 39 条，
	// **39 条全部**是这句「模型池连续 4 次不可用/限流，环境等待封顶判死」，0 条来自别处
	// ⇒ 面板「未知错误」那一桶整个都是这条路径，据此排查方向被完全带偏。
	// 对标：asynq 把租约过期这类基础设施事实也建模成具名 ErrLeaseExpired，river 的 unknown 是带字段的
	// 具名类型（UnknownJobKindError），两家都不留 residual 桶。
	//
	// 判「不可重试」：本路径就是重试预算耗尽的产物（引擎靠它解锁自主供料），再重试等于放回僵尸任务；
	// 也不归因到某条车道（不在 modelpool.modelAttributable 名单里，不该冷却无辜模型）。
	PoolWaitCapped Kind = "pool_wait_capped"
	// TaskPanic 是「执行任务时引擎内 panic，被 defer recover 兜住」。
	//
	// 对标 asynq 的 PanicError（recover 时带 file:line、单独成类型）：「代码炸了」与「原因不明」是两种事实，
	// 混在一桶里就没法只对 panic 报警。判不可重试：panic 是确定性缺陷，重投只会再炸一次。
	TaskPanic Kind = "panic"
	// TransitionFailed 是「状态迁移失败」（store.Transition 返回非法迁移或写盘失败）。
	//
	// 为什么单列：它既不是模型问题也不是任务内容问题，而是**本地状态机/存储**的事实。
	// 落 unknown 会让人去查模型车道，正确动作是查台账与磁盘。判不可重试（同因必再失败）。
	TransitionFailed Kind = "transition_failed"
	// ── WorkBench 的「第三份隐性 kind 名单」（issue #9）收编 ──────────────────
	// 这三类此前是**裸字符串直写 task.ErrorKind**，不走 ParseKind、不在任何枚举里，三处危害：
	//   ① 收编前 IsRetryable("crash_loop") 恒为 **true**（实测，见下）：ParseKind 把它收敛成 Unknown，
	//      而 Unknown.Retryable()==true ⇒ 「不在枚举里」不等于「判死」，它反而落进**最宽**的重试桶 ——
	//      判死类任务被重试层当「不明原因、值得再试」。这是比 issue #9 正文所说的「判 false」更危险的形态。
	//      收编后显式登记并判不可重试 ⇒ IsRetryable 由 true 变 false（口径修正）；三个写入点都在重试循环
	//      之外（判死/退回队列各走自己的分支），所以这次修正不改变既有生产行为，只关闭「未来流入」的口子。
	//   ② Prometheus 侧靠「实测值并集」才没丢读数，若改成只输出 baseline 就静默丢；
	//   ③ 面板中文标签只对账 AllKinds ⇒ 这几类在中文面板里裸显英文桶名。
	// 收编时**字符串值逐字保持**（crash_loop / verify_failed / replan）：生产数据里已是这个值，改名等于篡改历史读数。
	// 三者共同语义：「非模型原因的终态处置」——处置由引擎自己的救援/返工/重规划环负责，
	// 所以都判**不可重试**（与 PoolWaitCapped 同判），也都**不进** modelpool.modelAttributable（不该冷却无辜车道）。
	//
	// CrashLoop = 崩溃救援额度耗尽后判死（engine.stuck_rescue.go 的 crashRescueKind、engine.runtime_rescue.go 孤儿判死）。
	// 判不可重试：它本身就是「重投会把进程再弄死一次」的证据，再重试等于放回僵尸任务。
	CrashLoop Kind = "crash_loop"
	// VerifyFailed = 产物验收未通过、已排入下一轮返工（engine.repair_schedule.go）。
	// 判不可重试：再跑一轮由返工环安排（RepairRounds 记账），重试策略若再叠一层会把两套额度混在一起。
	VerifyFailed Kind = "verify_failed"
	// Replan = 返工额度用尽、退回方案层重出方案（engine.replan.go；cmd/workbench/plan_selftest.go 第 12 项断言它）。
	// 判不可重试：同 verify_failed —— 后续动作是重新规划，不是换一次执行。
	Replan  Kind = "replan"
	Unknown Kind = "unknown"
)

// AllKinds 是「非空类别」的单一权威清单（不含 None）。
//
// 为什么要函数而不是各处各抄一份列表：本仓同一个枚举被抄过四处 —— retry 常量表、
// executor 常量表、Prometheus 的 promFailureKindsBaseline、面板的中文标签表。抄写的地方
// 每加一个类别都会漏（2026-10-29 实测：baseline 缺 not_served/no_artifact/no_lane，
// 面板标签缺全部四个新类）。现在下游一律**从这个清单生成**，漏配在编译期之后必红。
func AllKinds() []Kind {
	return []Kind{
		Timeout, MissingKey, ModelDead, Upstream, Boot, Exit,
		Transport, StreamCut, Cancelled, Stalled, NoArtifact, NotServed,
		NoLane, RateLimited, PoolWaitCapped, TaskPanic, TransitionFailed,
		CrashLoop, VerifyFailed, Replan, Unknown,
	}
}

// NonFailureLedgerKinds 是「台账里出现、但**不是失败类别**」的 kind 名单（issue #9 第 3 条的显式旁路）。
//
// 为什么需要旁路而不是塞进 AllKinds：引擎在 store.AttemptRecord.Kind 上直写的字面量里，
//
//	· plan —— 是**尝试类别**（这一轮在跑方案层），不是失败原因。塞进失败枚举会带来三个实际损害：
//	  Prometheus 会为它预置一条「失败」序列、面板会为它补一条中文失败标签、
//	  Retryable()/白名单会被拉进一个永远用不到的分支。台账实测（数据根 dist/workbench-output，
//	  savedAt 2026-10-24T16:09Z）：attempts.jsonl 共 2467 行，kind=plan 1035 行 = 量最大的一类（42%）
//	  —— 把它当失败类别，面板「失败原因分布」与 Prometheus 失败序列会一起被带歪。
//	· done —— 是成功行的占位（AttemptRecord 六处写入点「列同构」，成功也记一行）。
//
// 旁路名单的作用：源文本对账判据说「所有直写 ErrorKind / 台账 Kind 的字面量必须落在
// 枚举 ∪ 本名单 里」，找不到判红。这样既不会把非失败类别混进失败枚举，
// 也不放行「凭空新增一个裸字符串类别」（那正是本轮要消除的形态）。
// 名单被 internal/retry/kind_writeback_contract_test.go 引用并强制对账。
func NonFailureLedgerKinds() []string {
	return []string{"plan", "done"}
}

// ParseKind 把任意字符串收敛成已知 Kind（未知值一律 Unknown，避免策略表漏配）。
func ParseKind(s string) Kind {
	k := Kind(strings.TrimSpace(s))
	switch k {
	case Timeout, MissingKey, ModelDead, Upstream, Boot, Exit, Cancelled, Stalled, NoArtifact, NotServed, NoLane, Unknown,
		Transport, StreamCut, RateLimited, PoolWaitCapped, TaskPanic, TransitionFailed,
		CrashLoop, VerifyFailed, Replan:
		return k
	}
	return Unknown
}

// Retryable 判断该类别是否值得再试一次。
// 缺凭据与模型下线属于"环境性不可恢复"，重试只会白烧额度；
// 取消是调用方意图，必须立刻停。
func (k Kind) Retryable() bool {
	switch k {
	// 网络/传输与流中断**应当重试**（2026-10-19）：
	// ETIMEDOUT/ENOTFOUND 多为瞬时；stream_cut 常是上游抖动。
	// 这两类恰恰是「换模型/重试就能好」的典型场景，判死不合理。
	// RateLimited 在此列（429 等冷却/换车道就能过），与 executor 侧同判。
	// PoolWaitCapped / TaskPanic / TransitionFailed **不在**此列：三者都是「同因必再失败」的确定性终态，
	// 重试只会重演僵尸任务或反复 panic（对标 asynq SkipRetry 直接归档、不烧剩余重试）。
	// CrashLoop / VerifyFailed / Replan 同样**不在**此列（issue #9 收编时定调，与 PoolWaitCapped 同判）：
	// 三者的后续处置各有自己的环（崩溃救援已判死、返工环、重规划环），重试策略再叠一层只会
	// 重复消耗 attempt 额度并掩盖真实处置路径。
	case Timeout, Upstream, Exit, Stalled, NoArtifact, NotServed, Unknown, Transport, StreamCut, RateLimited:
		return true
	}
	return false
}

// Action 是策略给出的动作。
type Action string

const (
	ActionRetry  Action = "retry"  // 延迟后重试
	ActionFail   Action = "fail"   // 直接判失败（不可重试或次数耗尽）
	ActionCancel Action = "cancel" // 调用方取消，立即停
)

// Decision 是一次重试判定的结果。
type Decision struct {
	Action Action        `json:"action"`
	Kind   Kind          `json:"kind"`
	Delay  time.Duration `json:"delayMs"`
	Reason string        `json:"reason,omitempty"`
}

const (
	// DefaultBaseDelay 是首次重试的基准等待。
	DefaultBaseDelay = 5 * time.Second
	// DefaultMaxDelay 是单次重试等待的上限（指数退避封顶）。
	DefaultMaxDelay = 10 * time.Minute
	// DefaultJitter 是抖动比例，取 0.2 表示在 ±20% 区间内随机。
	DefaultJitter = 0.2
)

// Policy 是可配置的重试策略。
//
// 兼容旧配置：MaxRetries 与 BackoffMs 沿用 ExecutorConfig 的既有字段，
// 当 BackoffMs 非空时按列表取值（旧行为），否则按 BaseDelay 指数退避。
type Policy struct {
	MaxRetries int           // 最多重试次数（不含首次执行）
	BackoffMs  []int         // 显式退避列表（毫秒），优先于指数退避
	BaseDelay  time.Duration // 指数退避基准
	MaxDelay   time.Duration // 指数退避上限
	Jitter     float64       // 抖动比例 0..1
	// RetryableKinds 为空表示用 Kind.Retryable 的默认表；
	// 非空时只对这些类别重试（用于按环境裁剪）。
	RetryableKinds []Kind

	// Fixes 记录构造期被修正的配置项（跨字段矛盾 / 非法值）。
	//
	// 为什么需要（2026-10-22 对标 river job_executor.go:563-571）：river 在策略给出
	// 非法值（早于当前时间）时会 WarnContext 并回落到默认策略。本项目把同一语义前移到
	// 构造期：修正是静默的，但**修正本身必须留痕**，否则 7x24 无人值守系统里
	// 「配置写坏」与「配置正确但值很小」在日志里完全同形，运维无从判断。
	Fixes []PolicyFix

	// runtime 持有运行期可变状态（告警去重表 + 抖动随机源）。
	//
	// 为什么拆成子结构并**用指针持有**（2026-10-22 实测，go vet copylocks 判红）：
	// 旧实现把 warnMu sync.Mutex 直接内嵌在 Policy 里，而 Policy 在本仓是按值传递的
	// （engine/helpers.go 先构造字面量、再交给 New）。含锁结构体按值传递被 vet 判死三处：
	//   retry.go:138  New passes lock by value
	//   retry.go:179  assignment copies lock value to cp
	//   engine/helpers.go:37  call of retry.New copies lock value
	// 后果不止静态告警：每复制一份 Policy 就多出一份独立的告警表 ——
	// 同一份配置矛盾在不同副本里各记一次，Warnings() 读数随副本而异。
	// 改动后配置字段仍可安全按值传递（指针字段不复制锁），运行期状态只有一份。
	//
	// 契约：Policy 应经 New() 构造；零值策略仍可用，但其告警无处累积（见 rt()）。
	runtime *policyRuntime
}

// policyRuntime 是 Policy 的运行期可变状态（与配置字段分离，见 Policy.runtime 的说明）。
type policyRuntime struct {
	mu       sync.Mutex
	warnings []string
	rnd      *rand.Rand
}

// PolicyFix 是一条配置修正记录（字段名 + 人话原因）。
type PolicyFix struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// New 用默认值补齐零值字段，保证策略永远可用。
//
// 跨字段自洽（2026-10-22 对标 river client.go:528-534 后新增）：
// MaxDelay < BaseDelay 是"上限比基准还小"的矛盾配置。旧实现只在 nextDelay 里
// 静默把它钳到 MaxDelay，结果是退避永远等于 MaxDelay 且**看不出配置写坏了**。
// 这里显式登记到 Fixes，让调用方（引擎）能在启动时把话说清楚，同时策略仍可用。
func New(p Policy) *Policy {
	if p.MaxRetries < 0 {
		p.MaxRetries = 0
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultBaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultMaxDelay
	}
	if p.MaxDelay < p.BaseDelay {
		// 矛盾配置：抬到 BaseDelay（而不是反过来压低基准 —— 那会让首次重试
		// 立即封顶，高频失败时退避失效）。登记原因供调用方告警。
		p.Fixes = append(p.Fixes, PolicyFix{
			Field:  "MaxDelayMs",
			Reason: fmt.Sprintf("退避上限 %v 小于基准 %v，已抬到基准值", p.MaxDelay, p.BaseDelay),
		})
		p.MaxDelay = p.BaseDelay
	}
	if p.Jitter < 0 {
		p.Fixes = append(p.Fixes, PolicyFix{Field: "Jitter", Reason: fmt.Sprintf("抖动比例 %v 为负，已置 0", p.Jitter)})
		p.Jitter = 0
	}
	if p.Jitter > 1 {
		p.Fixes = append(p.Fixes, PolicyFix{Field: "Jitter", Reason: fmt.Sprintf("抖动比例 %v 大于 1，已置 1", p.Jitter)})
		p.Jitter = 1
	}
	// 显式退避列表里的非法值（<=0）会让 nextDelay 退化成 1ms，
	// 高频失败时等于热循环。这里登记但**不修改用户列表**（保持兼容），
	// 由 nextDelay 在取用时逐个跳过并计入 Warnings。
	for i, ms := range p.BackoffMs {
		if ms <= 0 {
			p.Fixes = append(p.Fixes, PolicyFix{
				Field:  fmt.Sprintf("RetryBackoffMs[%d]", i),
				Reason: fmt.Sprintf("退避列表第 %d 项为 %dms（非正数），该次将改用指数退避", i, ms),
			})
		}
	}
	// 运行期状态一律**新建**、不复用入参的：New 的语义是「产出一份新的、可用的策略」，
	// 复用入参的运行期状态会把上一份策略的告警带进新策略（判据见
	// TestNewDoesNotInheritRuntimeState）。配置字段本身仍按值复制。
	cp := p
	cp.runtime = newPolicyRuntime()
	return &cp
}

// newPolicyRuntime 造一份干净的运行期状态（独立随机源）。
func newPolicyRuntime() *policyRuntime {
	return &policyRuntime{rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}
}

// rt 取运行期状态。
//
// 零值策略（未经 New 构造，例如外部直接写 retry.Policy{...}）走兜底分支：
// 返回一份**临时**状态而不写回 p —— 写回会在共享同一个 Policy 的并发调用之间
// 产生数据竞争；宁可让这类调用方的告警不累积，也不引入竞态。
// 本仓所有构造点都走 New()，正常运行路径上永远是持久状态。
func (p *Policy) rt() *policyRuntime {
	if rt := p.runtime; rt != nil {
		return rt
	}
	return newPolicyRuntime()
}

// Default 返回开箱策略（与历史行为一致：3 次重试、5s/20s/60s）。
func Default() *Policy {
	return New(Policy{MaxRetries: 3, BackoffMs: []int{5000, 20000, 60000}})
}

// allowed 判断类别是否在允许重试的范围里。
func (p *Policy) allowed(k Kind) bool {
	if !k.Retryable() {
		return false
	}
	if len(p.RetryableKinds) == 0 {
		return true
	}
	for _, allow := range p.RetryableKinds {
		if allow == k {
			return true
		}
	}
	return false
}

// Decide 给出第 attempt 次尝试失败后的动作。
//
// attempt 从 0 开始（0 = 首次执行失败后要决定是否重试第 2 次）。
func (p *Policy) Decide(kind Kind, attempt int, errText string) Decision {
	if kind == Cancelled {
		return Decision{Action: ActionCancel, Kind: kind, Reason: "调用方取消"}
	}
	if !p.allowed(kind) {
		return Decision{Action: ActionFail, Kind: kind, Reason: "该类错误不可重试: " + string(kind)}
	}
	if attempt >= p.MaxRetries {
		return Decision{Action: ActionFail, Kind: kind, Reason: "重试次数已耗尽"}
	}
	delay := p.nextDelay(attempt)
	return Decision{Action: ActionRetry, Kind: kind, Delay: delay, Reason: strings.TrimSpace(errText)}
}

// nextDelay 计算第 attempt 次重试的等待时间。
//
// 非法值处置（2026-10-22 对标 river job_executor.go:563-571）：
// 旧实现对"算出来的 delay 非正"只静默钳到 1ms —— 那正是热循环的配方
// （高频失败时每秒重试一次，比不退避更糟：既打上游又刷台账）。
// 现在：算出的 base <= 0 时**回落到可信的指数退避**并登记告警，
// 与 river 的 "using default retry policy instead" 同一语义。
func (p *Policy) nextDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	var base time.Duration
	if attempt < len(p.BackoffMs) && p.BackoffMs[attempt] > 0 {
		base = time.Duration(p.BackoffMs[attempt]) * time.Millisecond
	} else {
		// 退避列表越界或该项非法（<=0）：改用指数退避，并登记一条告警。
		// 不登记的后果：配置写坏时行为与"列表更短"完全同形，运维无从判断。
		if attempt < len(p.BackoffMs) {
			p.warnInvalid("RetryBackoffMs["+strconv.Itoa(attempt)+"]",
				fmt.Sprintf("第 %d 项为 %dms（非正数），已改用指数退避 %v", attempt, p.BackoffMs[attempt], p.BaseDelay))
		}
		base = p.BaseDelay
		for i := 0; i < attempt && base < p.MaxDelay; i++ {
			base *= 2
		}
	}
	// 非法值兜底：算出的等待非正（配置把列表项写成 0/负数且指数退避也被压塌时）。
	// 回落到 DefaultBaseDelay 的指数退避，而不是钳到 1ms。
	if base <= 0 {
		p.warnInvalid("nextDelay", fmt.Sprintf("第 %d 次重试的等待算出 %v（非正），回落默认指数退避", attempt, base))
		base = DefaultBaseDelay
		for i := 0; i < attempt && base < DefaultMaxDelay; i++ {
			base *= 2
		}
		if base > DefaultMaxDelay {
			base = DefaultMaxDelay
		}
	}
	if base > p.MaxDelay {
		base = p.MaxDelay
	}
	if p.Jitter > 0 {
		// 抖动只向下、不全额随机：保证不会超过 MaxDelay
		spread := float64(base) * p.Jitter
		base = time.Duration(float64(base) - spread + p.rt().rnd.Float64()*spread)
	}
	// 抖动后可能塌到 0（Jitter=1 时 spread 等于 base，取值域是 [0, base)），
	// 也可能塌到"远低于基准"的小值（1.6s 这种）—— 后者同样是热循环配方：
	// 高频失败时每秒级重试，比不退避更糟（既打上游又刷台账）。
	// 判据用 BaseDelay/2：至少等半个基准周期，且对正常抖动（>base*0.8）无感。
	if base <= 0 || base < p.BaseDelay/2 {
		p.warnInvalid("jitter", fmt.Sprintf("抖动后等待为 %v（低于基准 %v 的一半），回落到基准", base, p.BaseDelay))
		base = p.BaseDelay
	}
	if base < time.Millisecond {
		base = time.Millisecond
	}
	return base
}

// warnInvalid 登记一条"策略给出了非法值"的告警（去重后累计，供调用方读取）。
//
// 为什么用累计而不是每次打印：nextDelay 在重试循环里被高频调用，
// 同一条配置错误会被重复触发；累计 + 去重让调用方一次性把话说清楚。
func (p *Policy) warnInvalid(field, reason string) {
	rt := p.rt()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	msg := field + ": " + reason
	for _, w := range rt.warnings {
		if w == msg {
			return
		}
	}
	rt.warnings = append(rt.warnings, msg)
}

// Warnings 返回策略运行期登记的非法值告警（去重、按发生顺序）。
// 引擎在启动与热更新时读取它并写日志，让"配置写坏"在日志里看得见。
func (p *Policy) Warnings() []string {
	rt := p.rt()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	out := make([]string, len(rt.warnings))
	copy(out, rt.warnings)
	return out
}

// IsRetryable 是包级便捷判定（供不能持有 Policy 的调用方使用）。
func IsRetryable(kind string) bool { return ParseKind(kind).Retryable() }

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
	Unknown    Kind = "unknown"
)

// ParseKind 把任意字符串收敛成已知 Kind（未知值一律 Unknown，避免策略表漏配）。
func ParseKind(s string) Kind {
	k := Kind(strings.TrimSpace(s))
	switch k {
	case Timeout, MissingKey, ModelDead, Upstream, Boot, Exit, Cancelled, Stalled, NoArtifact, Unknown,
		Transport, StreamCut:
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
	case Timeout, Upstream, Exit, Stalled, NoArtifact, Unknown, Transport, StreamCut:
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

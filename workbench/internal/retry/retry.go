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
	"math/rand"
	"strings"
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
	Cancelled  Kind = "cancelled"
	Stalled    Kind = "stalled"
	Unknown    Kind = "unknown"
)

// ParseKind 把任意字符串收敛成已知 Kind（未知值一律 Unknown，避免策略表漏配）。
func ParseKind(s string) Kind {
	k := Kind(strings.TrimSpace(s))
	switch k {
	case Timeout, MissingKey, ModelDead, Upstream, Boot, Exit, Cancelled, Stalled, Unknown:
		return k
	}
	return Unknown
}

// Retryable 判断该类别是否值得再试一次。
// 缺凭据与模型下线属于"环境性不可恢复"，重试只会白烧额度；
// 取消是调用方意图，必须立刻停。
func (k Kind) Retryable() bool {
	switch k {
	case Timeout, Upstream, Exit, Stalled, Unknown:
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

	rnd *rand.Rand
}

// New 用默认值补齐零值字段，保证策略永远可用。
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
	if p.Jitter < 0 {
		p.Jitter = 0
	}
	if p.Jitter > 1 {
		p.Jitter = 1
	}
	if p.rnd == nil {
		p.rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	cp := p
	return &cp
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
func (p *Policy) nextDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	var base time.Duration
	if attempt < len(p.BackoffMs) && p.BackoffMs[attempt] > 0 {
		base = time.Duration(p.BackoffMs[attempt]) * time.Millisecond
	} else {
		// 指数退避：base * 2^attempt，封顶 MaxDelay
		base = p.BaseDelay
		for i := 0; i < attempt && base < p.MaxDelay; i++ {
			base *= 2
		}
	}
	if base > p.MaxDelay {
		base = p.MaxDelay
	}
	if p.Jitter > 0 {
		// 抖动只向下、不全额随机：保证不会超过 MaxDelay
		spread := float64(base) * p.Jitter
		base = time.Duration(float64(base) - spread + p.rnd.Float64()*spread)
	}
	if base < time.Millisecond {
		base = time.Millisecond
	}
	return base
}

// IsRetryable 是包级便捷判定（供不能持有 Policy 的调用方使用）。
func IsRetryable(kind string) bool { return ParseKind(kind).Retryable() }

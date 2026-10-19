package engine

import (
	"time"

	"workbench/internal/model"
)

// 上游限流（HTTP 429）的整池冷却策略。
//
// 现场实测依据：our-free-model 的免费车道按 session 统计速率，短时间打满即返回
// 429（插件 README「已知边界」原文：该车道按 session 统计速率，短时间内打满会
// 返回 429，插件将该模型标记为"已达限额"，下一轮探测自动恢复）。因此：
//   - 换模型无效（限流是通道级/账号级，不是单模型故障）；
//   - 继续快速重试无效，只会把 429 窗口拖长；
//   - 正确做法是把整个池子冷却一段时间，等车道自己恢复。
const (
	// rateLimitCooldownSeconds 是首次 429 后的整池冷却时长。
	// 取 5 分钟：插件侧按 session 统计速率，5 分钟足以让窗口滑出；
	// 太短会反复撞限流，太长会让 24/7 系统无谓停摆。
	rateLimitCooldownSeconds = 300
	// rateLimitBackoffCapSeconds 是连续限流时的指数退避上限（30 分钟）。
	rateLimitBackoffCapSeconds = 1800
)

// rateLimitState 记录连续限流次数，用于指数退避。
// 放在 Engine 里而不是全局：多实例互不影响。
type rateLimitState struct {
	consecutive int
	lastAt      int64
	// cooldownEnd 记录本轮冷却的截止时刻：下一次 429 落在冷却期内视为同一轮
	// （继续升级退避），早于"冷却结束后一个周期"才算新一轮（从 5 分钟重新起算）。
	cooldownEnd int64
}

// poolCooldownUntil 返回当前整池冷却的截止时刻（毫秒，0 表示未冷却）。
func (e *Engine) poolCooldownUntil() int64 {
	return e.poolWaitUntil.Load()
}

// setPoolCooldown 设置整池冷却闸门（带指数退避）。
//
// 语义：把闸门推到 max(现有闸门, now+秒数)，因此多次并发限流不会互相缩短等待；
// 连续限流按 5min → 10min → 20min → 上限 30min 递增；成功一次即清零。
func (e *Engine) setPoolCooldown(seconds int) {
	now := model.Now()
	e.rateLimitMu.Lock()
	st := &e.rateLimit
	// 「连续」的判据（2026-10-19 二次实测修正）：只认"上一轮冷却是否已结束"。
	//
	// 旧实现用 now-lastAt > 2x首次冷却 判断是否新一轮，结果 08:13 设 600 秒冷却、
	// 08:14 冷却期内再次触发时，计数被错误清零回 1，退避永远涨不上去 ——
	// 现场日志：consecutive 2 -> 1 -> 1 -> 1。
	//
	// 正确语义：**冷却期内再次收到 429 = 上游仍未恢复 = 继续升级**。
	if st.lastAt > 0 && st.cooldownEnd > 0 && now > st.cooldownEnd+int64(rateLimitCooldownSeconds)*1000 {
		st.consecutive = 0
	}
	st.consecutive++
	st.lastAt = now
	consec := st.consecutive
	e.rateLimitMu.Unlock()

	wait := seconds
	for i := 1; i < consec; i++ {
		wait *= 2
		if wait >= rateLimitBackoffCapSeconds {
			wait = rateLimitBackoffCapSeconds
			break
		}
	}
	if wait <= 0 {
		wait = rateLimitCooldownSeconds
	}
	until := now + int64(wait)*1000
	// 只推进不后退：并发限流时取最晚的那个。
	for {
		old := e.poolWaitUntil.Load()
		if old >= until || e.poolWaitUntil.CompareAndSwap(old, until) {
			break
		}
	}
	// 记录本轮冷却截止，供下次判定"是否同一轮"（冷却期内再 429 = 继续升级）。
	e.rateLimitMu.Lock()
	if until > e.rateLimit.cooldownEnd {
		e.rateLimit.cooldownEnd = until
	}
	e.rateLimitMu.Unlock()
	e.log.Warn("模型池限流冷却", "consecutive", consec, "waitSeconds", wait, "untilMs", until)
	e.emitEvent(model.Event{Type: "warn", At: now,
		Msg: "上游限流：整池冷却 " + itoaSeconds(wait) + "（连续第 " + itoaSeconds(consec) + " 次）"})
}

// clearRateLimit 在成功一次后清零限流计数（说明车道已恢复）。
func (e *Engine) clearRateLimit() {
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 0
	e.rateLimit.lastAt = 0
	e.rateLimitMu.Unlock()
}

// itoaSeconds 把整数转十进制字符串（避免为此引入 strconv 到热路径的格式化开销时仍然清晰）。
func itoaSeconds(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// 让 time 包被引用（保留给未来的 Retry-After 解析）
var _ = time.Second

// SetProbeSuppressor 注入"探测抑制器"回调（由 main 装配 modelpool 时设入）。
//
// 解耦设计：engine 不 import modelpool（modelpool 反过来依赖 executor），
// 用一个回调把"限流了，别再探测"这件事传出去。为 nil 时静默跳过。
func (e *Engine) SetProbeSuppressor(fn func(seconds int, reason string)) {
	e.probeSuppressorMu.Lock()
	e.probeSuppressor = fn
	e.probeSuppressorMu.Unlock()
}

package engine

import (
	"workbench/internal/model"
)

// 限流状态自证（2026-10-19 现场需求）。
//
// 问题：上游持续 429 时，工作台处于"运行中但 0 产出"状态。此时
//   - phase = running（看起来一切正常）
//   - lastError 为空（从未被设置为限流信息）
//   - 面板只显示"运行中"，用户完全看不出发生了什么
// 24/7 无人值守系统最怕这种"静默停摆"：必须能自证"我为什么没在干活"。

// RateLimitStatus 是限流状态快照（面板与 API 共用）。
type RateLimitStatus struct {
	// Limited 表示当前是否处于限流冷却中。
	Limited bool `json:"limited"`
	// CooldownUntilMs 是本轮冷却截止时刻（毫秒，0 表示未冷却）。
	CooldownUntilMs int64 `json:"cooldownUntilMs"`
	// RemainingSeconds 是距离冷却结束还有多少秒（<=0 表示已结束）。
	RemainingSeconds int64 `json:"remainingSeconds"`
	// Consecutive 是连续限流次数（用于解释退避层级）。
	Consecutive int `json:"consecutive"`
	// LastReason 是最近一次限流的人类可读说明。
	LastReason string `json:"lastReason,omitempty"`
	// LastAt 是最近一次限流发生的时刻（毫秒）。
	LastAt int64 `json:"lastAt,omitempty"`
	// Blocked 表示车道健康探测已确认结构性阻塞（IP 封禁 / 地区限制）。
	// 与 Limited 区分：限流可等，阻塞等多久都没用。
	Blocked bool `json:"blocked"`
	// BlockKind 是阻塞类别：ip_ban / region_blocked（空表示非结构性）。
	BlockKind string `json:"blockKind,omitempty"`
	// LaneEvidence 保留上游原文证据。
	LaneEvidence string `json:"laneEvidence,omitempty"`
	// QuotaExhausted 表示「当前冷却的主因是**免费额度耗尽**」而不是瞬时抖动（round 95 · T77）。
	//
	// 为什么要单列（现场 2026-10-29）：44 条车道里 37 条同时在冷却，面板只说「上游限流冷却中，剩余 N 秒」，
	// 运维会以为「等 N 秒就好」；而其中相当一部分的原文是
	//   `Free usage limit reached. Free models work in OpenCode (…)`、`Sign up for free to continue …`
	// —— 那是**要账号侧动作**才能恢复的额度事实，等到天亮也不会恢复。把两者混在一句提示里，
	// 就是「面板自证」这件事本身失真（比不给提示更糟，因为它给出的是**错的action**）。
	QuotaExhausted bool `json:"quotaExhausted,omitempty"`
	// QuotaLanes 是本次判定中「额度耗尽」的车道数（证据，供运维判断影响面）。
	QuotaLanes int `json:"quotaLanes,omitempty"`
}

// RateLimitStatus 返回当前限流状态（并发安全）。
func (e *Engine) RateLimitStatus() RateLimitStatus {
	now := model.Now()
	until := e.poolWaitUntil.Load()
	e.rateLimitMu.Lock()
	consec := e.rateLimit.consecutive
	lastAt := e.rateLimit.lastAt
	e.rateLimitMu.Unlock()

	st := RateLimitStatus{
		CooldownUntilMs: until,
		Consecutive:     consec,
		LastAt:          lastAt,
	}
	if until > now {
		st.Limited = true
		st.RemainingSeconds = (until - now) / 1000
		st.LastReason = "上游限流（HTTP 429）：免费车道按 session 统计速率，短时间内打满即返回 429。" +
			"已整池冷却，冷却结束后自动恢复；换模型无效（限流是通道级）。"
		// 车道健康探测：若已确认 IP 封禁 / 地区限制，必须升级为真实根因。
		//
		// 现场（2026-10-19 09:20）：面板显示"冷却结束后自动恢复"，但真相是上游按 IP
		// 封禁 —— 冷却结束后**不会**恢复。用户会被误导去等一个永远不来的恢复。
		if h := ProbeLaneHealth(e.layout.DSHHome); h.Blocked {
			st.Blocked = true
			if laneContains(h.Reason, "地区") {
				st.BlockKind = "region_blocked"
			} else {
				st.BlockKind = "ip_ban"
			}
			st.LaneEvidence = h.Evidence
			st.LastReason = h.Reason + "；这是结构性阻塞，冷却结束后不会自动恢复。"
		}
	} else if consec > 0 {
		// 冷却已过但计数未清零：说明上一轮限流刚结束、还没成功产出过。
		st.LastReason = "上一轮限流冷却已结束，等待下一次执行验证车道是否恢复。"
	}
	// 额度耗尽维度（round 95 · T77）：读路由账本里「冷却中且主因为 quota_exhausted」的车道。
	//
	// 判据只认**账本里记录的 lastErr 前缀**（router 的 CooldownByLane 写的就是 kind: msg），
	// 不去猜上游：面板说的是「账本事实」，不是推断。
	if e.router != nil {
		if n := e.router.CountCoolingByKind("quota_exhausted"); n > 0 {
			st.QuotaExhausted = true
			st.QuotaLanes = n
			// 覆盖提示：额度耗尽时「等冷却结束」是**错的动作**，必须直说。
			st.LastReason = "免费额度已耗尽（" + itoaInt(n) + " 条车道因此冷却）：这是账号侧额度事实，" +
				"冷却结束**不会**自动恢复。动作：为上游账号补额度/换有额度的 provider，或等额度周期刷新——" +
				"反复重试只会继续烧尝试。"
		}
	}
	return st
}

// itoaInt 是 strconv.Itoa 的本地替身（本文件刻意只 import model，避免为一句拼串加依赖）。
func itoaInt(n int) string {
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

// laneContains 判断结论里是否包含某关键词（避免引入 strings 到已很密的 import）。
func laneContains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// LimitedSuffix 返回给面板/日志用的一句话状态（无则空串）。
func (e *Engine) LimitedSuffix() string {
	st := e.RateLimitStatus()
	if !st.Limited {
		return ""
	}
	return "上游限流冷却中，剩余 " + itoaSeconds(int(st.RemainingSeconds)) + " 秒（连续第 " +
		itoaSeconds(st.Consecutive) + " 次）"
}

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
	return st
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

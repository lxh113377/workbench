package modelroute

import (
	"testing"
	"time"
)

// TestCountCoolingByKind 钉住「面板要能区分等冷却与等额度」的账本判据（round 95 · T77）。
//
// 现场（2026-10-29）：44 条车道里 37 条同时在冷却，面板只说「上游限流冷却中，剩余 N 秒」；
// 而其中相当一部分的原文是 `Free usage limit reached…` / `Sign up for free…` —— 那是要账号侧动作的额度事实，
// 等到天亮也不会恢复。混成一句提示，给的就是**错的 action**。
//
// 判据边界（三条都要钉）：
//
//	· 只数**仍在冷却**的车道（冷却已到期的历史原因不算「现在还没恢复」）；
//	· 只数**该 kind** 的车道（别的 kind 冷却中不得混进来）；
//	· 前缀必须是 `kind:`（`quota_exhaustedX: …` 这种不得误命中）。
func TestCountCoolingByKind(t *testing.T) {
	r := New(laneRoutes())
	// ofm-a / ofm-b 属 lane:ofm —— 用它做「本车道冷却」
	if n := r.CooldownByLane("lane:ofm", 5*time.Minute, "quota_exhausted", "free usage limit"); n != 2 {
		t.Fatalf("lane:ofm 下应冷却 2 条，实际 %d", n)
	}
	if got := r.CountCoolingByKind("quota_exhausted"); got != 2 {
		t.Fatalf("应数到 2 条额度耗尽冷却，实际 %d", got)
	}
	// 别的 kind 不得混入（对同一批车道用不同 kind 冷却，计数应独立）。
	if got := r.CountCoolingByKind("rate_limited"); got != 0 {
		t.Fatalf("不同 kind 不得混入，实际 %d", got)
	}
	// 冷却已到期的不得再数（历史原因 ≠ 现在还没恢复）。
	r2 := New(laneRoutes())
	r2.CooldownByLane("lane:ofm", 5*time.Minute, "quota_exhausted", "x")
	r2.CooldownByLane("lane:ofm", -1*time.Second, "quota_exhausted", "x")
	if got := r2.CountCoolingByKind("quota_exhausted"); got != 2 {
		t.Fatalf("负时长不应改变冷却（CooldownByLane 对 d<=0 直接返回 0），实际 %d", got)
	}
	// 前缀误命中：`quota_exhaustedX:` 不算。
	r3 := New(laneRoutes())
	r3.CooldownByLane("lane:ofm", 5*time.Minute, "quota_exhaustedX", "x")
	if got := r3.CountCoolingByKind("quota_exhausted"); got != 0 {
		t.Fatalf("前缀必须带冒号，`quota_exhaustedX:` 不得命中，实际 %d", got)
	}
	// 空 kind 恒 0（防把「统计全部」误当「统计某类」）。
	if got := r.CountCoolingByKind(""); got != 0 {
		t.Fatalf("空 kind 应返回 0，实际 %d", got)
	}
}

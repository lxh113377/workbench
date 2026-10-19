package engine

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRateLimitStatusUpgradedToIPBan 是"误导性恢复承诺"的回归测试。
//
// 现场（2026-10-19 09:20）：面板显示
//
//	"上游限流（HTTP 429）...已整池冷却，冷却结束后自动恢复"
//
// 但真相是上游按 IP 封禁 —— 冷却结束后**不会**恢复。
// 用户会被误导去等待一个永远不来的恢复。
//
// 期望：限流状态下若车道健康显示 blocked，则 LastReason 必须升级为真实根因，
// 并带 blocked/blockKind 标记。
func TestRateLimitStatusUpgradedToIPBan(t *testing.T) {
	e, _, layout := newTestEngine(t)

	// 造一份"IP 被封"的车道数据
	dir := filepath.Join(layout.DSHHome, "our-free-model")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := `{"at":1,"egress":{"ip":"1.2.3.4","country":"CN"},"results":{
	  "m":{"state":"unavailable","detail":"we've detected abusive traffic from this IP address"}}}`
	if err := os.WriteFile(filepath.Join(dir, "availability.json"), []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// 进入限流冷却
	e.setPoolCooldown(rateLimitCooldownSeconds)

	st := e.RateLimitStatus()
	if !st.Limited {
		t.Fatal("冷却期间 Limited 应为 true")
	}
	if !st.Blocked {
		t.Fatal("车道已确认 IP 封禁时，限流状态必须标记 Blocked")
	}
	if st.BlockKind != "ip_ban" {
		t.Fatalf("BlockKind = %q, want ip_ban", st.BlockKind)
	}
	if containsStr(st.LastReason, "冷却结束后自动恢复") {
		t.Fatalf("不得再对结构性封禁承诺自动恢复: %q", st.LastReason)
	}
	if !containsStr(st.LastReason, "封禁") && !containsStr(st.LastReason, "IP") {
		t.Fatalf("原因应指出 IP 封禁: %q", st.LastReason)
	}
}

// TestRateLimitStatusNormalWhenLaneHealthy 车道健康时保持常规限流文案。
func TestRateLimitStatusNormalWhenLaneHealthy(t *testing.T) {
	e, _, layout := newTestEngine(t)
	dir := filepath.Join(layout.DSHHome, "our-free-model")
	_ = os.MkdirAll(dir, 0o755)
	fixture := `{"at":1,"egress":{"ip":"1.2.3.4","country":"CN"},"results":{"m":{"state":"available"}}}`
	_ = os.WriteFile(filepath.Join(dir, "availability.json"), []byte(fixture), 0o644)

	e.setPoolCooldown(rateLimitCooldownSeconds)
	st := e.RateLimitStatus()
	if st.Blocked {
		t.Fatal("车道可用时不应标记 Blocked")
	}
	if !containsStr(st.LastReason, "冷却结束后自动恢复") {
		t.Fatalf("临时限流应保留'会自动恢复'的说明: %q", st.LastReason)
	}
}

func containsStr(s, sub string) bool {
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

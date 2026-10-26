package retry

import "testing"

// TestNoArtifactKind 钉住「无产物块」类别的贯通（round 22 实测缺口 T18⑤）。
//
// 由来：failover 层已把「要求产物却只吐过程独白」判成 no_artifact 并换车道，但 retry 侧不认识这个字符串，
// ParseKind 收敛成 Unknown ⇒ 引擎台账只写 `kind=unknown`、引擎日志里 `no_artifact` 出现 0 次
// （判据生效了，但排查时查不到原因）。本测试把「可解析 + 可重试」两条口径钉死。
func TestNoArtifactKind(t *testing.T) {
	if got := ParseKind("no_artifact"); got != NoArtifact {
		t.Fatalf("ParseKind(no_artifact) = %q, want %q", got, NoArtifact)
	}
	// 口径与既有实现一致：只 trim，不做大小写折叠（ParseKind 是大小写敏感的类别契约）。
	if got := ParseKind("  no_artifact  "); got != NoArtifact {
		t.Fatalf("首尾空格应被 trim，实得 %q", got)
	}
	if !NoArtifact.Retryable() {
		t.Fatal("无产物块应可重试（换车道常能拿到合规产物）")
	}
	if !IsRetryable("no_artifact") {
		t.Fatal("IsRetryable(no_artifact) 应为 true")
	}
	// 既有口径不得被改坏。
	if ParseKind("timeout") != Timeout || !Timeout.Retryable() {
		t.Fatal("既有 timeout 口径被改坏")
	}
	if ParseKind("missing_credential") != MissingKey || MissingKey.Retryable() {
		t.Fatal("既有 missing_credential（不可重试）口径被改坏")
	}
}

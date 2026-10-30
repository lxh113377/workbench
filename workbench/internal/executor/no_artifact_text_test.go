package executor

import "testing"

// TestNoArtifactTextRule 钉住 T102（round 146）：无产物块在**文本路径**上也必须能归因。
//
// 现场（agent 根 attempts.jsonl）：同一句原文「执行失败: no_artifact」共 4 行 ——
//
//	3 行走 judge 的结构判定（requireArtifacts 且无 ARTIFACTS 块）正确记为 no_artifact，
//	1 行走文本兜底（2026-10-26 19:00:53，action=retry）落到 unknown。
//
// 这既是历史残留，也是**审计盲区**：文本可判定的类别缺规则 ⇒ 归因一致性扫法只能算作"判不了"，
// 缺陷会藏在"判不了"里。补规则的判据要同时证明"认得出来"与"不遮住更紧急的事实"。
func TestNoArtifactTextRule(t *testing.T) {
	// 正面：两条自家协议原文都要认得
	if got := classify("执行失败: no_artifact", &Result{}); got != KindNoArtifact {
		t.Fatalf("文本路径应判 no_artifact，实得 %q ⇒ 台账会落到 unknown（面板显示「未知错误」）", got)
	}
	if got := classify("回答不含产物块（不符合 ARTIFACTS 协议）", &Result{}); got != KindNoArtifact {
		t.Fatalf("中文原文同理，实得 %q", got)
	}
	// 保守面三条：新分支排在网络/出口/限流之后 ⇒ 不许遮住更紧急的事实
	if got := classify(`dsh: RATE_LIMIT: too many requests; retry later`, &Result{}); got != KindRateLimited {
		t.Fatalf("限流必须仍判 rate_limited，实得 %q ⇒ 新规则越位了", got)
	}
	if got := classify(`dsh: SERVER: our free model does not serve "x" on this egress`, &Result{}); got != KindNotServed {
		t.Fatalf("出口不服务必须仍判 not_served，实得 %q", got)
	}
	if got := classify(`dsh: 启动失败: spawn ENOENT`, &Result{}); got != KindBoot {
		t.Fatalf("纯启动失败必须仍判 boot_failed（不许因新分支放宽），实得 %q", got)
	}
}

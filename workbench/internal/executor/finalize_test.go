package executor

import (
	"strings"
	"testing"
)

// TestFinalizeResultKeepsStructuredRateLimit 是本轮最关键缺陷的回归测试。
//
// 现场形态（2026-10-19 08:19，隔离实例 events.jsonl 实录）：
//
//	{"type":"status","phase":"turn_end","reason":{"kind":"error",
//	 "error":{"message":"too many requests; retry later","code":"RATE_LIMIT","status":429}}}
//	{"type":"final","text":""}
//
// 旧实现的两个 bug：
//  1. consume 只读顶层 ev.Error，reason.error 里的 429 完全不可见；
//  2. FinalizeResult 无条件用 stderr 覆盖 LastError，即使解析到了也会被冲掉。
//
// 结果：429 被归成 bad_exit（"无最终答案"），限流时换 8 个模型打空整池。
func TestFinalizeResultKeepsStructuredRateLimit(t *testing.T) {
	res := &Result{
		LastError: "RATE_LIMIT: too many requests; retry later",
		ErrorKind: string(KindUpstream),
		ExitCode:  1,

		FinalText: "", // 429 时 final 为空
	}
	// stderr 在 429 时通常也只有一句 dsh: RATE_LIMIT...
	err := FinalizeResult(res, "dsh: RATE_LIMIT: too many requests; retry later", false)
	if err == nil {
		t.Fatal("结构化失败必须返回错误")
	}
	if res.ErrorKind != string(KindRateLimited) {
		t.Fatalf("ErrorKind = %q, want %q（429 必须被正确分类）", res.ErrorKind, KindRateLimited)
	}
}

// TestFinalizeResultStructuredErrorNotOverwrittenByStderr 保证 stderr 不覆盖结构化错误。
func TestFinalizeResultStructuredErrorNotOverwrittenByStderr(t *testing.T) {
	res := &Result{
		LastError: "EAC_UPSTREAM_FAILED: upstream exploded",
		ExitCode:  1,
	}
	_ = FinalizeResult(res, "some unrelated stderr noise", false)
	if !strings.Contains(res.LastError, "EAC_UPSTREAM_FAILED") {
		t.Fatalf("结构化错误被 stderr 覆盖了: %q", res.LastError)
	}
	if res.ErrorKind != string(KindModelDead) {
		t.Fatalf("ErrorKind = %q, want %q", res.ErrorKind, KindModelDead)
	}
}

// TestFinalizeResultNoOutputStillExit 保证真正的空产出（无结构化失败）仍归 bad_exit。
func TestFinalizeResultNoOutputStillExit(t *testing.T) {
	res := &Result{FinalText: "", LastError: ""}
	err := FinalizeResult(res, "", false)
	if err == nil {
		t.Fatal("空产出必须返回错误")
	}
	if res.ErrorKind != string(KindExit) {
		t.Fatalf("ErrorKind = %q, want %q", res.ErrorKind, KindExit)
	}
}

// TestFinalizeResultSuccess 保证正常产出不被误判。
func TestFinalizeResultSuccess(t *testing.T) {
	res := &Result{FinalText: "正文", LastError: ""}
	if err := FinalizeResult(res, "", false); err != nil {
		t.Fatalf("正常产出不应报错: %v", err)
	}
}

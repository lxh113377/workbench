package modelpool

import (
	"testing"

	"workbench/internal/executor"
)

// TestJudgePrefersStructuredErrorKind 是本轮最深层缺陷的回归测试。
//
// 现场形态（2026-10-19 08:23）：429 时 executor 返回
//
//	res.ErrorKind = "rate_limited", res.FinalText = "", err = nil
//
// 旧 judge() 先判 "FinalText 为空 -> KindExit"，把 rate_limited 覆盖成 bad_exit，
// 于是 FailoverRunner 认为只是"这个模型没出话"，继续换下一个 —— 连换 8 个打空整池。
func TestJudgePrefersStructuredErrorKind(t *testing.T) {
	res := &executor.Result{
		ErrorKind: string(executor.KindRateLimited),
		LastError: "RATE_LIMIT: too many requests",
		FinalText: "",
	}
	ok, kind, detail := judge(res, nil, false)
	if ok {
		t.Fatal("429 不应判成功")
	}
	if kind != string(executor.KindRateLimited) {
		t.Fatalf("kind = %q, want %q（不能被空产出规则覆盖成 bad_exit）", kind, executor.KindRateLimited)
	}
	if detail == "" {
		t.Fatal("应带失败详情")
	}
}

// TestJudgeEmptyOutputWithoutErrorKind 保证"真的没出话"仍归 bad_exit。
func TestJudgeEmptyOutputWithoutErrorKind(t *testing.T) {
	ok, kind, _ := judge(&executor.Result{FinalText: ""}, nil, false)
	if ok {
		t.Fatal("空产出不应判成功")
	}
	if kind != string(executor.KindExit) {
		t.Fatalf("kind = %q, want %q", kind, executor.KindExit)
	}
}

// TestJudgeSuccess 保证正常产出不被误判。
func TestJudgeSuccess(t *testing.T) {
	ok, kind, _ := judge(&executor.Result{FinalText: "正文"}, nil, false)
	if !ok || kind != "" {
		t.Fatalf("正常产出应判成功: ok=%v kind=%q", ok, kind)
	}
}

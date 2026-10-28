package engine

import (
	"errors"
	"os"
	"strings"
	"testing"

	"workbench/internal/executor"
)

// TestAttemptErrorText 钉住台账 error 列的取值口径（round 58 · T51）。
//
// live 实锤：限流分支旧写法是 errText(runErr) + " | lane=…"，而执行器在「退出码 0 但被判限流」这条路径上
// runErr 为 nil ⇒ 台账只剩 `error=" | lane=lane:kilo"`（14:33/14:37/14:44 三条实测），lane 有、上游原因丢了。
func TestAttemptErrorText(t *testing.T) {
	res := &executor.Result{LastError: "RATE_LIMIT: Provider returned error"}
	// ① runErr 优先：包装层的结论带重试语境，不能被事件流文本顶掉。
	if got := attemptErrorText(res, errors.New("上游限流，已重试 3 次")); got != "上游限流，已重试 3 次" {
		t.Fatalf("runErr 非空时应优先用它，实得 %q", got)
	}
	// ② runErr 为 nil 时回退到上游错误 —— 正是那条空壳的补救。
	if got := attemptErrorText(res, nil); !strings.Contains(got, "RATE_LIMIT") {
		t.Fatalf("runErr 为空时应回退 res.LastError，实得 %q", got)
	}
	// ③ 两头皆空：返回空串，不硬造假原因（nil 安全；台账六处写入点列同构）。
	if got := attemptErrorText(nil, nil); got != "" {
		t.Fatalf("两头皆空应返回空串，实得 %q", got)
	}
}

// TestRateLimitLedgerWired 是接线守卫：限流分支必须真的用 attemptErrorText。
//
// 为什么还要一条源文本断言：本会话已四次踩到「辅助函数有单测、生产不调用」（T36/T42/T41/T50），
// 而这条分支嵌在 runAttempt 内部、要跑真执行器才能覆盖，成本远高于收益。
func TestRateLimitLedgerWired(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读 engine.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "Error:      attemptErrorText(res, runErr)") {
		t.Fatal("限流分支未接 attemptErrorText —— 台账仍会写「有 lane 没原因」的空壳")
	}
}

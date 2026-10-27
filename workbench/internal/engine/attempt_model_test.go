package engine

import (
	"testing"

	"workbench/internal/executor"
)

// TestAttemptedModel 钉住「台账/健康度记实际执行模型」的口径（round 44 · T27）。
func TestAttemptedModel(t *testing.T) {
	if got := attemptedModel(&executor.Result{Model: "combo:auto-vision"}, "z-ai/glm-5.3"); got != "combo:auto-vision" {
		t.Fatalf("应优先用执行器上报的实际模型，实际 %q", got)
	}
	// 回落：老实现、或压根没跑到子进程（ErrNoLane 时 res 为 nil）—— 记 selected 比记空更可诊断。
	if got := attemptedModel(&executor.Result{}, "z-ai/glm-5.3"); got != "z-ai/glm-5.3" {
		t.Fatalf("执行器未上报时应回落 selected，实际 %q", got)
	}
	if got := attemptedModel(nil, "z-ai/glm-5.3"); got != "z-ai/glm-5.3" {
		t.Fatalf("nil 结果应回落 selected，实际 %q", got)
	}
}

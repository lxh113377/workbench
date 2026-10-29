package engine

import (
	"testing"

	"workbench/internal/executor"
)

// TestIsRateLimitedCoversQuota 钉住「额度耗尽属于限流族」（round 124 的漏项）。
//
// 为什么必须钉：isRateLimited 决定这次失败是否走「冷却车道 + 退回队列等预算」，
// 而 T78 的长等待预算挂在这条路径上。此前它只认 rate_limited ⇒ 线上 54 次 quota_exhausted
// 一条也没走到「任务改走长等待预算」，分级预算形同虚设。
func TestIsRateLimitedCoversQuota(t *testing.T) {
	if !isRateLimited(&executor.Result{ErrorKind: string(executor.KindQuotaExhausted)}, nil) {
		t.Fatal("quota_exhausted 必须属于限流族（否则长等待预算永远够不到）")
	}
	if !isRateLimited(&executor.Result{ErrorKind: string(executor.KindRateLimited)}, nil) {
		t.Fatal("rate_limited 仍须在族内（既有行为不得回退）")
	}
	// 反向：别把无关类别卷进来（bad_exit 是任务级，走这条会错设池闸门）。
	if isRateLimited(&executor.Result{ErrorKind: string(executor.KindExit)}, nil) {
		t.Fatal("bad_exit 不得被判成限流族")
	}
	if isRateLimited(nil, nil) {
		t.Fatal("空结果不得被判成限流族")
	}
}

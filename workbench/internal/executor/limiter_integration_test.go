package executor

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/ratelimit"
)

// TestRunnerLimiterThrottlesRequests 是 2026-10-19 事故的**运行时实证**。
//
// 事故形态：8 路并发 × 每个任务失败后立即换模型 = 每分钟 45-70 次请求
// -> 免费车道 429 持续 50 分钟。
//
// 本测试模拟"大量并发尝试"：12 个 goroutine 同时请求一个必然失败的
// DSH 调用（用不存在的可执行文件让它快速失败），时限流器设为 60/分钟
// （1/秒，桶容量 2）。若限流生效，12 次尝试至少需要 (12-2)/1 = 10 秒。
// 未装限流器时会在一秒内全部打完。
func TestRunnerLimiterThrottlesRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过耗时的限流实证测试")
	}
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	_ = layout.EnsureDirs()

	cfg := config.DefaultConfig().Executor
	cfg.DSHExe = filepath.Join(dir, "nonexistent-dsh.exe") // 必定快速失败
	r := New(cfg, layout)
	// 60/分钟 = 1 次/秒，桶容量 2
	r.SetLimiter(ratelimit.New(1.0, 2))

	const attempts = 12
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(n int) {
			defer wg.Done()
			task := &model.Task{ID: "throttle-" + itoaSmall(n), Title: "限流测试", TimeoutMin: 1}
			_, _ = r.Run(context.Background(), task, "test-model")
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 至少 (12-2)/1 = 10 秒；给一定余量，但必须远大于 3 秒
	if elapsed < 3*time.Second {
		t.Fatalf("%d 次请求只用了 %v —— 限流器未生效（事故根因未修复）", attempts, elapsed)
	}
}

// TestRunnerNilLimiterNoDelay 未装限流器时应无额外延迟（向后兼容）。
func TestRunnerNilLimiterNoDelay(t *testing.T) {
	dir := t.TempDir()
	layout, _ := config.BuildLayout(dir, "")
	_ = layout.EnsureDirs()
	cfg := config.DefaultConfig().Executor
	cfg.DSHExe = filepath.Join(dir, "nonexistent-dsh.exe")
	r := New(cfg, layout)
	if r.limiter != nil {
		t.Fatal("未注入时限流器应为 nil")
	}
	if s := r.LimiterSnapshot(); s.RatePerSecond != 0 {
		t.Fatalf("nil 限流器快照应为零值: %+v", s)
	}
	start := time.Now()
	task := &model.Task{ID: "no-limit", Title: "无限流", TimeoutMin: 1}
	_, _ = r.Run(context.Background(), task, "m")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("未装限流器时不应有等待: %v", d)
	}
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

var _ = os.TempDir

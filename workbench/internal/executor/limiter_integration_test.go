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

// TestRunnerNilLimiterNoDelay 未装限流器时不应有额外延迟（向后兼容）。
//
// 判据改法（2026-10-20 实测）：原先断言"单次 Run < 2 秒"，但全量并行跑
// （go test ./...）时实测 4.12 秒而判红 —— 那 4 秒不是限流等待，而是
// Run 里的固定环境成本：linkDir 要 spawn cmd.exe 建 junction + 建目录 +
// 写覆盖层 + 开事件文件，20 多个包并行时宿主排队。绝对时限把"宿主慢"
// 误判成"限流器在工作"，属反模式（判据测错了对象）。
//
// 现在测**相对差**：令牌桶的等待只发生在取令牌时，满桶的首次调用立即通过，
// 第二次才可能被 rate 拖住。所以"第二次不比第一次慢"正是"没有闸门"的
// 充要观测面；宿主整体变慢时两次一起变慢，判据不受影响。
func TestRunnerNilLimiterNoDelay(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过耗时的限流实证测试")
	}
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
	measure := func(id string) time.Duration {
		start := time.Now()
		_, _ = r.Run(context.Background(), &model.Task{ID: id, Title: "无限流", TimeoutMin: 1}, "m")
		return time.Since(start)
	}
	first := measure("no-limit-1")
	second := measure("no-limit-2")
	// 闸门若存在（哪怕速率很宽），第二次必然被令牌补充拖住；nil 时两次同量级。
	if slack := second - first; slack > time.Second {
		t.Fatalf("未装限流器却出现额外等待：first=%v second=%v 差=%v", first, second, slack)
	}
	if slack := first - second; slack > time.Second {
		t.Logf("首次反而更慢（冷启动/建 junction 成本）：first=%v second=%v", first, second)
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

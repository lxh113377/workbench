package engine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"workbench/internal/logging"
)

// captureLogs 是 logging.Handler 的内存实现（面板 /api/logs 同一接口）。
//
// 为什么必须带锁（round 140 · CI 实测）：`go test -race` 报
//
//	DATA RACE at log_flush_test.go:18 —— Write by captureLogs.Handle / Read by captureLogs.find
//
// 触发者是本轮新增的观测点判据：它们调 e.tick()，而 tick 会**异步**起 runOne goroutine，
// 那些 goroutine 在测试读 cap 的同时还在写日志。日志器本身是并发安全的（生产要求），
// 是**这个测试替身**没按 Handler 的契约做成并发安全 —— 契约错在测试件这边，修这里。
type captureLogs struct {
	mu   sync.Mutex
	recs []logging.Record
}

func (c *captureLogs) Handle(rec logging.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, rec)
}

func (c *captureLogs) text() string {
	c.mu.Lock()
	snap := append([]logging.Record(nil), c.recs...)
	c.mu.Unlock()
	var b strings.Builder
	for _, r := range snap {
		b.WriteString(r.Level)
		b.WriteString(" ")
		b.WriteString(r.Message)
		keys := make([]string, 0, len(r.Fields))
		for k := range r.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf(" %s=%v", k, r.Fields[k]))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (c *captureLogs) find(sub string) []logging.Record {
	c.mu.Lock()
	snap := append([]logging.Record(nil), c.recs...)
	c.mu.Unlock()
	out := []logging.Record{}
	for _, r := range snap {
		if strings.Contains(r.Message, sub) {
			out = append(out, r)
		}
	}
	return out
}

func newCaptureLogger(t *testing.T, c *captureLogs) *logging.Logger {
	t.Helper()
	lg, err := logging.Open(logging.Options{Handler: c})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lg.Close() })
	return lg
}

// TestConstructionWarningIsFlushed 守护 L6 接线收口（2026-10-20 实测缺口）：
// New() 期间 e.log 恒为 Nop，而 main.go 是在 New() **返回之后**才 SetLogger(logger)。
// 改造前构造期所有"失败只告警不阻断"的告警（工作目录越权审计打不开、自主子系统
// 初始化失败）都写进 io.Discard —— 现场表现为自主链路没起来、审计文件一直是空的，
// 但日志与面板里一条原因都查不到，只能靠猜。现在先缓冲、注入日志器时冲刷。
func TestConstructionWarningIsFlushed(t *testing.T) {
	e, _, _ := newTestEngine(t)
	e.warnDeferred("测试降级原因", "err", "boom", "file", "x.json")

	cap := &captureLogs{}
	if n := len(cap.recs); n != 0 {
		t.Fatalf("注入日志器前不该有任何输出: %d 条", n)
	}
	e.SetLogger(newCaptureLogger(t, cap))

	hits := cap.find("测试降级原因")
	if len(hits) != 1 {
		t.Fatalf("构造期降级原因冲刷条数 = %d, want 1（0 即 L6 回归：告警仍然静默）。全文:\n%s", len(hits), cap.text())
	}
	if hits[0].Level != "WARN" {
		t.Fatalf("级别 = %s, want WARN", hits[0].Level)
	}
	if hits[0].Fields["err"] != "boom" || hits[0].Fields["file"] != "x.json" {
		t.Fatalf("字段丢失: %v", hits[0].Fields)
	}
	if hits[0].Fields["component"] != "engine" {
		t.Fatalf("component 字段未打上: %v（面板无法按来源过滤）", hits[0].Fields)
	}

	// 冲刷必须清空缓冲：二次 SetLogger 不得把同一条原因再写一遍
	cap2 := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap2))
	if n := len(cap2.find("测试降级原因")); n != 0 {
		t.Fatalf("重复冲刷 %d 条（缓冲未清空）", n)
	}
}

// TestNoPhantomWarningOnCleanConstruction 反向判据（能证不误报）：
// 正常构造不得凭空造出"已降级/未启用"告警 —— 假告警比没告警更坏，它会把排查
// 引向不存在的问题。
func TestNoPhantomWarningOnCleanConstruction(t *testing.T) {
	e, _, _ := newTestEngine(t)
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))
	for _, r := range cap.recs {
		if r.Level == "WARN" || r.Level == "ERROR" {
			if strings.Contains(r.Message, "已降级") || strings.Contains(r.Message, "越权审计未启用") {
				t.Fatalf("无降级事实却输出告警（假告警）: %q", r.Message)
			}
		}
	}
}

// TestWarnDeferredConcurrencySafe 缓冲可能被多 goroutine 写（自主初始化链上有回调），
// -race 下验；并校验不丢不重。
func TestWarnDeferredConcurrencySafe(t *testing.T) {
	e, _, _ := newTestEngine(t)
	done := make(chan struct{}, 8)
	const per = 50
	for i := 0; i < 8; i++ {
		go func(n int) {
			for k := 0; k < per; k++ {
				e.warnDeferred("并发写", "n", fmt.Sprint(n))
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 8; i++ {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("并发写入超时")
		}
	}
	cap := &captureLogs{}
	e.SetLogger(newCaptureLogger(t, cap))
	if got := len(cap.find("并发写")); got != 8*per {
		t.Fatalf("冲刷条数 = %d, want %d（缓冲有丢失或重复）", got, 8*per)
	}
}

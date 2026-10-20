package logging

import (
	"testing"
)

// TestComponentReachesHandlerRecord 是缺陷 D11 的直接判据（2026-10-20 实测）：
// 改造前 At("engine") 只把 component 加到 slog 侧，handler 通道的 Record 另起一路、
// 仅从本次 kv 构造 —— 于是落盘 JSONL 有 component、面板 /api/logs 却没有，
// ARCHITECTURE.md 承诺的"component 字段可按来源过滤"在面板链路上是假的。
// lastRec 取 memHandler 最后一条（memHandler 定义在 logging_test.go，复用不重复声明）。
func lastRec(t *testing.T, h *memHandler) Record {
	t.Helper()
	if len(h.records) == 0 {
		t.Fatal("handler 没收到任何记录")
	}
	return h.records[len(h.records)-1]
}

func TestComponentReachesHandlerRecord(t *testing.T) {
	h := &memHandler{}
	lg, err := Open(Options{Handler: h})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.At("engine").Warn("降级了", "err", "boom")
	rec := lastRec(t, h)
	if rec.Fields["component"] != "engine" {
		t.Fatalf("handler 记录里 component 缺失: %v（D11 回归）", rec.Fields)
	}
	if rec.Fields["err"] != "boom" {
		t.Fatalf("本次 kv 丢失: %v", rec.Fields)
	}
}

// TestWithFieldsReachHandlerRecord 守护 With 的自定义固定字段同样要进 handler。
func TestWithFieldsReachHandlerRecord(t *testing.T) {
	h := &memHandler{}
	lg, err := Open(Options{Handler: h})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.With("taskId", "task-1").Info("开始")
	if got := lastRec(t, h).Fields["taskId"]; got != "task-1" {
		t.Fatalf("With 字段未进 handler 记录: %v", lastRec(t, h).Fields)
	}
}

// TestSiblingSubLoggersDoNotLeakFields 守护 copyAttrs 的独立副本语义（关键边界）：
// Logger 会被按值复制，若子日志器把 attrs append 到父的底层数组，
// 兄弟子日志器之间会静默串字段 —— 表现为日志里 taskId 张冠李戴。
func TestSiblingSubLoggersDoNotLeakFields(t *testing.T) {
	h := &memHandler{}
	base, err := Open(Options{Handler: h})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = base.Close() }()

	a := base.With("attempt", "A")
	b := base.With("attempt", "B")
	a.Info("来自 A")
	recA := lastRec(t, h)
	if recA.Fields["attempt"] != "A" {
		t.Fatalf("A 的字段被 B 踩掉了: %v（attrs 共享底层数组）", recA.Fields)
	}
	b.Info("来自 B")
	if got := lastRec(t, h).Fields["attempt"]; got != "B" {
		t.Fatalf("B 的字段不对: %v", got)
	}
	// 父日志器不应被子节点的字段污染
	base.Info("来自父")
	if _, ok := lastRec(t, h).Fields["attempt"]; ok {
		t.Fatalf("父日志器被子日志器字段污染: %v", lastRec(t, h).Fields)
	}
}

// TestCallKvOverridesFixedField 与 slog 语义一致：本次调用的同名键覆盖固定字段。
func TestCallKvOverridesFixedField(t *testing.T) {
	h := &memHandler{}
	lg, err := Open(Options{Handler: h})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lg.Close() }()

	lg.With("model", "fixed").Info("尝试", "model", "override")
	if got := lastRec(t, h).Fields["model"]; got != "override" {
		t.Fatalf("本次 kv 应覆盖固定字段，实际 model=%v", got)
	}
}

// TestNilLoggerSubLoggersAreSafe 负面用例：nil 接收者不得 panic（库默认零输出语义）。
func TestNilLoggerSubLoggersAreSafe(t *testing.T) {
	var nilLog *Logger
	if got := nilLog.At("x"); got == nil {
		t.Fatal("nilLogger.At 应返回可用的 Nop 等价物")
	}
	if got := nilLog.With("k", "v"); got == nil {
		t.Fatal("nilLogger.With 应返回可用的 Nop 等价物")
	}
	nilLog.Warn("不该 panic")
	_ = nilLog.Path()
}

package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	lg, err := Open(Options{Dir: dir, Level: LevelInfo})
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	lg.Info("引擎启动", "concurrency", 2)
	lg.At("executor").Warn("模型冷却", "model", "z-ai/glm-5.3")
	lg.Error("任务失败", "taskId", "task-1", "kind", "timeout")

	data, err := os.ReadFile(filepath.Join(dir, "workbench.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("log lines = %d, want 3; content=%q", len(lines), string(data))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "{") || !strings.HasSuffix(line, "}") {
			t.Fatalf("每行必须是单条 JSON: %q", line)
		}
	}
	if !strings.Contains(lines[0], "\"msg\":\"引擎启动\"") {
		t.Fatalf("首行缺少消息: %q", lines[0])
	}
	if !strings.Contains(lines[1], "\"component\":\"executor\"") {
		t.Fatalf("第二行缺少 component 字段: %q", lines[1])
	}
}

func TestLevelFiltering(t *testing.T) {
	dir := t.TempDir()
	lg, err := Open(Options{Dir: dir, Level: LevelWarn})
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	lg.Debug("不该出现-debug")
	lg.Info("不该出现-info")
	lg.Warn("应该出现-warn")
	data, _ := os.ReadFile(filepath.Join(dir, "workbench.jsonl"))
	content := string(data)
	if strings.Contains(content, "不该出现") {
		t.Fatalf("低于级别的日志被写入: %q", content)
	}
	if !strings.Contains(content, "应该出现-warn") {
		t.Fatalf("符合条件的日志未写入: %q", content)
	}
}

func TestHandlerReceivesRecords(t *testing.T) {
	h := &memHandler{}
	lg, err := Open(Options{Dir: t.TempDir(), Level: LevelDebug, Handler: h})
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	lg.Debug("hello", "k", "v")
	if len(h.records) != 1 {
		t.Fatalf("handler records = %d, want 1", len(h.records))
	}
	if h.records[0].Fields["k"] != "v" {
		t.Fatalf("字段未传递: %+v", h.records[0])
	}
	if h.records[0].Level != "DEBUG" {
		t.Fatalf("level = %s, want DEBUG", h.records[0].Level)
	}
}

func TestNopLoggerSafe(t *testing.T) {
	var lg *Logger
	lg.Info("no panic", "a", 1)
	lg.Warn("no panic")
	lg.At("x").Error("no panic")
	if lg.Path() != "" {
		t.Fatal("nil logger Path 应为空")
	}
	n := Nop()
	n.Info("ok")
	if n.Path() != "" {
		t.Fatal("Nop logger Path 应为空")
	}
}

func TestRedact(t *testing.T) {
	kv := Redact([]any{"apiKey", "sk-abcdefg", "model", "glm-5.3", "token", "abc"})
	if kv[1] != "***redacted***" {
		t.Fatalf("apiKey 未打码: %v", kv[1])
	}
	if kv[3] != "glm-5.3" {
		t.Fatalf("普通字段被误伤: %v", kv[3])
	}
	if kv[5] != "abc" {
		t.Fatalf("短值不应打码（避免误伤）: %v", kv[5])
	}
}

func TestAtDoesNotMutateBase(t *testing.T) {
	dir := t.TempDir()
	lg, err := Open(Options{Dir: dir, Level: LevelInfo})
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	child := lg.At("engine")
	child.Info("child")
	lg.Info("base")
	data, _ := os.ReadFile(filepath.Join(dir, "workbench.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}
	if strings.Contains(lines[1], "component") {
		t.Fatalf("基日志器被子日志器污染: %q", lines[1])
	}
}

type memHandler struct{ records []Record }

func (m *memHandler) Handle(r Record) { m.records = append(m.records, r) }

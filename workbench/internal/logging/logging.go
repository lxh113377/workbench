// Package logging 提供工作台统一的结构化日志出口。
//
// 设计目标（对标 riverqueue/river 与 n8n 的可观测性做法）：
//   - 一条日志 = 一行 JSON，字段化（ts/level/msg/component + 任意 kv），便于 grep 与后续接入采集；
//   - 落盘与终端双写：文件 workbench.jsonl 供事后排查，stderr 供前台调试；
//   - 不引入第三方库（项目硬约束：Go 纯标准库 / 单 EXE / 供应链干净），底层用标准库 log/slog。
//
// 与历史写法的关系：删除了 engine-debug.log / crash.log / supervisor.log 三处各自为政的
// 裸文件写入（各写各的格式、各写各的时间戳、无法统一检索），统一改走本包。
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level 日志级别（与 slog 对齐，便于调用方不必 import slog）。
type Level = slog.Level

const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Handler 是终端侧的日志处理器：CLI/前台运行时输出人读格式，GUI 模式静默。
type Handler interface {
	Handle(rec Record)
}

// Record 是一条落盘日志的只读视图（供面板 /api/logs 与测试使用）。
type Record struct {
	At      int64          `json:"at"`
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// Logger 是工作台的日志门面。
type Logger struct {
	// mu 用指针：Logger 会被 At/With 按值复制（子日志器），持锁值会触发 vet 的 copylocks。
	mu      *sync.RWMutex
	inner   *slog.Logger
	path    string
	level   slog.Level
	file    *os.File
	handler Handler
}

// Options 控制日志初始化。
type Options struct {
	// Dir 是落盘目录（通常为 <DataRoot>/logs）；为空表示只输出到终端。
	Dir string
	// FileName 落盘文件名，默认 workbench.jsonl。
	FileName string
	// Level 最低级别，默认 Info。
	Level slog.Level
	// Console 为 true 时同时写 stderr（桌面 GUI 模式应传 false）。
	Console bool
	// Handler 可选：额外的终端/内存处理器（测试与面板用）。
	Handler Handler
}

// Open 打开日志器。Dir 不存在时自动创建。
func Open(opt Options) (*Logger, error) {
	if opt.FileName == "" {
		opt.FileName = "workbench.jsonl"
	}
	l := &Logger{level: opt.Level, handler: opt.Handler, mu: &sync.RWMutex{}}
	var writers []io.Writer
	if opt.Dir != "" {
		if err := os.MkdirAll(opt.Dir, 0o755); err != nil {
			return nil, err
		}
		path := filepath.Join(opt.Dir, opt.FileName)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		l.file = f
		l.path = path
		writers = append(writers, f)
	}
	if opt.Console {
		writers = append(writers, os.Stderr)
	}
	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}
	multi := io.MultiWriter(writers...)
	handler := slog.NewJSONHandler(&syncWriter{w: multi}, &slog.HandlerOptions{Level: opt.Level})
	l.inner = slog.New(handler)
	return l, nil
}

// Nop 返回一个不落盘的日志器（测试与库默认值用）。
func Nop() *Logger {
	return &Logger{inner: slog.New(slog.NewTextHandler(io.Discard, nil)), level: LevelInfo, mu: &sync.RWMutex{}}
}

// At 用指定字段返回子日志器（component=xxx 是约定字段）。
func (l *Logger) At(component string) *Logger {
	if l == nil || l.inner == nil {
		return Nop()
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	cp := *l
	cp.inner = l.inner.With("component", component)
	return &cp
}

// With 附加固定字段。
func (l *Logger) With(kv ...any) *Logger {
	if l == nil || l.inner == nil {
		return Nop()
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	cp := *l
	cp.inner = l.inner.With(kv...)
	return &cp
}

func (l *Logger) emit(level slog.Level, msg string, kv []any) {
	if l == nil || l.inner == nil {
		return
	}
	if level < l.level {
		return
	}
	ctx := context.Background()
	l.inner.Log(ctx, level, msg, kv...)
	if l.handler != nil {
		fields := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			if k, ok := kv[i].(string); ok {
				fields[k] = kv[i+1]
			}
		}
		l.handler.Handle(Record{At: time.Now().UnixMilli(), Level: level.String(), Message: msg, Fields: fields})
	}
}

// Debug 记录调试日志。
func (l *Logger) Debug(msg string, kv ...any) { l.emit(LevelDebug, msg, kv) }

// Info 记录一般信息。
func (l *Logger) Info(msg string, kv ...any) { l.emit(LevelInfo, msg, kv) }

// Warn 记录警告。
func (l *Logger) Warn(msg string, kv ...any) { l.emit(LevelWarn, msg, kv) }

// Error 记录错误。
func (l *Logger) Error(msg string, kv ...any) { l.emit(LevelError, msg, kv) }

// Path 返回落盘文件路径（未落盘返回空串）。
func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.path
}

// Close 关闭落盘文件。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// syncWriter 串行化多写入器的写操作，避免 slog 并发写交错。
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// Redact 把已知敏感字段（token/key/secret 形式）打码，日志里不留明文凭据。
// 规则刻意保守：只处理键名命中 + 值长度 > 4 的字符串，避免误伤正常文本。
func Redact(kv []any) []any {
	out := make([]any, 0, len(kv))
	for i := 0; i < len(kv); i += 2 {
		if i+1 >= len(kv) {
			out = append(out, kv[i])
			break
		}
		key, ok := kv[i].(string)
		val := kv[i+1]
		if ok && isSensitiveKey(key) {
			if s, isStr := val.(string); isStr && len(s) > 4 {
				val = "***redacted***"
			}
		}
		out = append(out, kv[i], val)
	}
	return out
}

func isSensitiveKey(k string) bool {
	lower := strings.ToLower(k)
	for _, pat := range []string{"token", "secret", "password", "apikey", "api_key", "credential"} {
		if strings.Contains(lower, pat) {
			return true
		}
	}
	return false
}
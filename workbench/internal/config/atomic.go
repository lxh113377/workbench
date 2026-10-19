package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// WriteFileAtomic 以 tmp -> fsync -> rename 的方式原子写文件，保证断电/崩溃后
// 目标文件要么是旧内容、要么是新内容，绝不出现半截 JSON。
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建父目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			tmp.Close()
		}
		os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("fsync 失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	tmp = nil
	if err := os.Rename(tmpName, path); err != nil {
		// Windows 上目标被占用时 rename 会失败，退化为尽力替换。
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("替换目标文件失败: %w", err)
		}
		if err2 := os.Rename(tmpName, path); err2 != nil {
			return fmt.Errorf("重命名失败: %w", err2)
		}
	}
	return nil
}

// WriteJSONAtomic 原子写入缩进 JSON。
func WriteJSONAtomic(path string, v any) error {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 JSON 失败: %w", err)
	}
	buf = append(buf, '\n')
	return WriteFileAtomic(path, buf)
}

// ReadJSON 读取 JSON；文件不存在时返回 os.ErrNotExist 供调用方判断。
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// 剥掉 UTF-8 / UTF-16 BOM（Windows 记事本/PowerShell 会写 UTF-8 BOM，
	// Go 的 json 会报 invalid character 从而让工作台启动即退出）。
	data = stripBOM(data)
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", filepath.Base(path), err)
	}
	return nil
}

// AppendJSONL 以 O_APPEND 追加一条 JSON 记录（单行）。并发安全。
type JSONLWriter struct {
	mu   sync.Mutex
	path string
}

// NewJSONLWriter 构造追加写入器。
func NewJSONLWriter(path string) *JSONLWriter { return &JSONLWriter{path: path} }

// Path 返回底层文件路径。
func (w *JSONLWriter) Path() string { return w.path }

// Append 追加一条记录；序列化失败时报错且不写入。
func (w *JSONLWriter) Append(v any) error {
	buf, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("序列化记录失败: %w", err)
	}
	buf = append(buf, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开日志失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(buf); err != nil {
		return fmt.Errorf("写入日志失败: %w", err)
	}
	return nil
}

// stripBOM 剥掉常见 BOM 前缀（UTF-8 三字节 / UTF-16 LE / BE 两字节）。
func stripBOM(b []byte) []byte {
	switch {
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return b[3:]
	case len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE:
		return b[2:]
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		return b[2:]
	}
	return b
}

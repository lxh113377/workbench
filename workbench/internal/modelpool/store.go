package modelpool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"workbench/internal/config"
)

// Store 是模型池的磁盘账本：pool.json（原子写）+ model-status.jsonl（追加状态日志）。
type Store struct {
	dir    string
	logMax int64
}

// NewStore 构造账本；dir 为数据根（通常是 layout.DataRoot）。
func NewStore(dir string) *Store { return &Store{dir: dir, logMax: 16 << 20} }

// Dir 返回池子数据目录。
func (s *Store) Dir() string { return filepath.Join(s.dir, "models") }

// filePath 返回池子定义文件路径。
func (s *Store) filePath() string { return filepath.Join(s.Dir(), "pool.json") }

// LogPath 返回状态日志路径。
func (s *Store) LogPath() string { return filepath.Join(s.Dir(), "model-status.jsonl") }

// Load 读取池子定义；不存在返回全新空池（调用方负责灌 seed）。
func (s *Store) Load() (*file, error) {
	var f file
	if err := config.ReadJSON(s.filePath(), &f); err != nil {
		if os.IsNotExist(err) {
			return &file{Version: 1, Health: map[string]*Health{}}, nil
		}
		return nil, err
	}
	if f.Health == nil {
		f.Health = map[string]*Health{}
	}
	if f.Version <= 0 {
		f.Version = 1
	}
	return &f, nil
}

// Save 原子落盘。
func (s *Store) Save(f *file) error {
	if f == nil {
		return fmt.Errorf("空池")
	}
	f.Version = 1
	f.UpdatedAt = nowMs()
	return config.WriteJSONAtomic(s.filePath(), f)
}

// AppendLog 追加一条状态日志（JSONL）。
//
// 日志用于「切换过程无感但可追溯」：每次探测、故障转移、恢复、面板增删改都留一行。
// 超过 logMax 时把旧日志滚动为 .1（只保留两代，避免 7x24 无限膨胀）。
func (s *Store) AppendLog(rec logRecord) {
	if rec.At == 0 {
		rec.At = nowMs()
	}
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return
	}
	path := s.LogPath()
	if st, err := os.Stat(path); err == nil && s.logMax > 0 && st.Size() > s.logMax {
		_ = os.Remove(path + ".1")
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = f.Write(append(b, '\n'))
}

// TailLog 返回最近 n 行状态日志（面板「状态日志」分区用）。
func (s *Store) TailLog(n int) []string {
	if n <= 0 {
		n = 200
	}
	data, err := os.ReadFile(s.LogPath())
	if err != nil {
		return []string{}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record 是一次路径决策的审计记录（允许与拒绝都记，便于事后复核）。
type Record struct {
	At      int64  `json:"at"`
	Op      string `json:"op"`             // validate/select/reset/read/write
	Path    string `json:"path"`           // 被判定（或解析后）的绝对路径
	Root    string `json:"root,omitempty"` // 当时的授权根
	TaskID  string `json:"taskId,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// AuditLog 是追加写入的 JSONL 审计（workspace-audit.jsonl）。
// 审计写失败绝不阻塞业务：护栏判定已经在内存里完成，落盘只影响可追溯性。
type AuditLog struct {
	mu   sync.Mutex
	path string
}

// OpenAudit 打开（或创建）审计文件。路径为空时返回 nil（调用方按"不审计"处理）。
func OpenAudit(path string) (*AuditLog, error) {
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &AuditLog{path: path}, nil
}

// Path 返回审计文件路径（面板展示用）。
func (a *AuditLog) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// Append 追加一条记录（nil 接收者安全：未配置审计时静默跳过）。
func (a *AuditLog) Append(rec Record) {
	if a == nil {
		return
	}
	if rec.At == 0 {
		rec.At = time.Now().UnixMilli()
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
	_ = f.Close()
}

// Tail 返回最近 n 条审计记录（新→旧）。
func (a *AuditLog) Tail(n int) []Record {
	if a == nil {
		return nil
	}
	if n <= 0 {
		n = 50
	}
	recs, err := ReadAudit(a.path)
	if err != nil {
		return nil
	}
	if len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	out := make([]Record, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		out = append(out, recs[i])
	}
	return out
}

// ReadAudit 读取整个审计文件（按写入顺序，旧→新）。
// 坏行（半行/崩溃残留）跳过，保证面板不因一行脏数据整块失败。
func ReadAudit(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []Record
	start := 0
	for i := 0; i <= len(data); i++ {
		if i < len(data) && data[i] != '\n' {
			continue
		}
		line := data[start:i]
		start = i + 1
		if len(line) == 0 {
			continue
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// Summary 汇总审计：总数 / 拒绝数 / 最近拒绝原因（面板概览用）。
type Summary struct {
	Total      int      `json:"total"`
	Allowed    int      `json:"allowed"`
	Denied     int      `json:"denied"`
	LastDeny   string   `json:"lastDeny,omitempty"`
	LastDenyAt int64    `json:"lastDenyAt,omitempty"`
	Reasons    []string `json:"reasons,omitempty"`
}

// Summarize 把审计记录汇总成人读概览。
func Summarize(recs []Record) Summary {
	var s Summary
	seen := map[string]bool{}
	for _, r := range recs {
		s.Total++
		if r.Allowed {
			s.Allowed++
			continue
		}
		s.Denied++
		if r.At >= s.LastDenyAt {
			s.LastDenyAt = r.At
			s.LastDeny = fmt.Sprintf("%s %s: %s", r.Op, r.Path, r.Reason)
		}
		reason := r.Reason
		if reason == "" {
			reason = "未说明"
		}
		if !seen[reason] && len(s.Reasons) < 10 {
			seen[reason] = true
			s.Reasons = append(s.Reasons, reason)
		}
	}
	return s
}

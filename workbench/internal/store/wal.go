package store

// WAL（Write-Ahead Log）追加日志：把"每次状态迁移都全量重写 queue.json"改成
// "追加一条事件 + 定期压缩成快照"，解决任务量大时的写放大。
//
// 设计（对标 riverqueue/river 的 insert-only jobs 表 + 定期 reindex）：
//   - 每次 Create / Update / Transition / UpsertChain 追加一行 JSONL 到 queue.wal.jsonl；
//   - 启动时 = 读 queue.json 快照 + 顺序回放 WAL 里比快照新的操作；
//   - WAL 条数超过阈值（默认 2000）时压缩：先把当前内存全量写成新快照（fsync），
//     再清空 WAL（先写快照后截断，任何一步崩溃都不会丢数据）；
//   - 快照丢失/损坏时退化为"WAL 全量回放"，只要 WAL 还在就能重建。
//
// 不变量：同一时刻内存状态 == 快照 ⊕ WAL（按 at 单调递增回放）。
//
// 实现取舍：**每次追加都 open→write→close**，不长期持有文件句柄。
// 理由是 Windows 上长期句柄会让测试的临时目录清理失败（unlinkat: file in use），
// 而单次 O_APPEND 的 open/close 开销远小于全量快照序列化（这才是要治的写放大）。

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"workbench/internal/model"
)

// walOp 是一次写操作的类型。
type walOp string

const (
	OpCreate     walOp = "create"     // 新建任务
	OpUpdate     walOp = "update"     // 更新任务（含状态迁移）
	OpChain      walOp = "chain"      // 新建/更新链
	OpTransition walOp = "transition" // 状态迁移（与 update 同义，保留可读性）
)

// walRecord 是 WAL 的一行。
type walRecord struct {
	At    int64        `json:"at"`
	Op    walOp        `json:"op"`
	Task  *model.Task  `json:"task,omitempty"`
	Chain *model.Chain `json:"chain,omitempty"`
}

// defaultWALCompactThreshold 是触发压缩的 WAL 条数阈值。
const defaultWALCompactThreshold = 2000

// wal 是 WAL 写入器 + 计数（不持有长期文件句柄）。
type wal struct {
	mu        sync.Mutex
	path      string
	lines     int // 当前 WAL 里的有效行数
	threshold int
}

// openWAL 创建/打开 WAL 并统计现有行数（用于恢复阈值判断）。
func openWAL(path string) (*wal, error) {
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &wal{path: path, lines: countLines(path), threshold: defaultWALCompactThreshold}, nil
}

func filepathDir(p string) string {
	if i := strings.LastIndexAny(p, "/\\"); i > 0 {
		return p[:i]
	}
	return "."
}

// countLines 统计文件行数（文件不存在返回 0）。
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			n++
		}
	}
	return n
}

// append 追加一条 WAL 记录（open → write → close，不持有句柄）。
func (w *wal) append(rec walRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	rec.At = model.Now()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if _, werr := f.Write(line); werr != nil {
		_ = f.Close()
		return werr
	}
	if serr := f.Sync(); serr != nil {
		_ = f.Close()
		return serr
	}
	if cerr := f.Close(); cerr != nil {
		return cerr
	}
	w.lines++
	return nil
}

// count 返回当前 WAL 行数。
func (w *wal) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lines
}

// needsCompact 判断是否需要压缩。
func (w *wal) needsCompact() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.threshold > 0 && w.lines >= w.threshold
}

// reset 截断 WAL（压缩后调用；调用前必须已经写好快照）。
func (w *wal) reset() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	w.lines = 0
	return nil
}

// close 是兼容占位：本实现不持句柄，无需释放。
func (w *wal) close() error { return nil }

// replayWAL 按顺序回放 WAL，把记录应用到 apply 回调。
//
// 容错：遇到半行（进程崩溃在写一半）或单行坏 JSON 时，**丢弃该行及其后续**
// （后续行可能是同一次写入的延续），已成功回放的部分保留。
// 返回成功回放的行数。
func replayWAL(path string, apply func(rec walRecord)) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("打开 WAL 失败: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256*1024), 16*1024*1024)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec walRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			// 坏行/半行：停止回放（保守：宁缺勿错）
			break
		}
		apply(rec)
		n++
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return n, err
	}
	return n, nil
}

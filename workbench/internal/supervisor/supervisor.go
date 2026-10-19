// Package supervisor 实现资源治理与自愈：
//   - 每 N 秒采样堆内存 / 句柄数 / goroutine 数，落 metrics.csv
//   - 连续多次越过阈值 -> 记录快照 -> flush 状态 -> 拉起新进程后退出旧进程
//   - 单个 goroutine panic 通过 Recover 兜住，不让整个引擎崩掉
package supervisor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workbench/internal/config"
	"workbench/internal/logging"
)

// Sample 是一次资源采样。
type Sample struct {
	At        int64   `json:"at"`
	HeapMB    float64 `json:"heapMB"`
	AllocMB   float64 `json:"allocMB"`
	Goroutine int     `json:"goroutine"`
	Handles   int     `json:"handles"`
	NumGC     uint32  `json:"numGC"`
}

// Manager 是资源治理器。
type Manager struct {
	layout  config.Layout
	cfg     func() config.SupervisorConfig
	mu      sync.Mutex
	strikes int
	samples []Sample
	// csvLastAt 是已落盘样本的最大时间戳（增量落盘去重水位）。
	csvLastAt int64
	onHeal    func(reason string)
	stop      chan struct{}
	started   atomic.Bool
	log       *logging.Logger
}

// New 构造治理器。
func New(layout config.Layout, cfg func() config.SupervisorConfig) *Manager {
	return &Manager{layout: layout, cfg: cfg, onHeal: func(string) {}, log: logging.Nop()}
}

// SetLogger 注入结构化日志器（P1-1）。
func (m *Manager) SetLogger(lg *logging.Logger) {
	if lg == nil {
		lg = logging.Nop()
	}
	m.log = lg.At("supervisor")
}

// OnHeal 注册自愈回调（用于记录与通知）。
func (m *Manager) OnHeal(fn func(reason string)) { m.mu.Lock(); m.onHeal = fn; m.mu.Unlock() }

// Start 启动采样循环。
func (m *Manager) Start() {
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	m.stop = make(chan struct{})
	go m.loop()
}

// Stop 停止采样。
func (m *Manager) Stop() {
	if m.started.CompareAndSwap(true, false) && m.stop != nil {
		close(m.stop)
	}
}

// Samples 返回最近采样（最多保留 720 条）。
func (m *Manager) Samples() []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Sample(nil), m.samples...)
}

// WriteCSV 增量落盘：只追加新样本，表头不重复，历史永不丢。
//
// 现场缺陷（2026-10-19）：改造前每次把**内存里的全部样本**重写整个文件。
// 而内存是 720 样本的环形缓冲（20 秒采样约 4 小时），24 小时压测里一旦绕圈，
// 文件就被截断成最近 4 小时 —— A7 的 24h 证据链会凭空消失。
//
// 正确语义：文件是 append-only 账本，内存裁剪只影响展示，不影响历史。
func (m *Manager) WriteCSV(path string) error {
	m.mu.Lock()
	samples := append([]Sample(nil), m.samples...)
	lastAt := m.csvLastAt
	m.mu.Unlock()

	// 只取比上次已落盘的更新的样本（按时间戳去重）。
	var fresh []Sample
	for _, s := range samples {
		if s.At > lastAt {
			fresh = append(fresh, s)
		}
	}
	if len(fresh) == 0 {
		return nil // 无新样本：不碰文件（mtime 不变）
	}

	_, err := os.Stat(path)
	exists := err == nil
	var b strings.Builder
	if !exists {
		b.WriteString("at,heap_mb,alloc_mb,goroutine,handles,num_gc\n")
	}
	for _, s := range fresh {
		b.WriteString(fmt.Sprintf("%d,%.2f,%.2f,%d,%d,%d\n", s.At, s.HeapMB, s.AllocMB, s.Goroutine, s.Handles, s.NumGC))
	}
	if exists {
		f, oerr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if oerr != nil {
			return oerr
		}
		defer f.Close()
		if _, werr := f.WriteString(b.String()); werr != nil {
			return werr
		}
	} else {
		if werr := config.WriteFileAtomic(path, []byte(b.String())); werr != nil {
			return werr
		}
	}
	m.mu.Lock()
	if fresh[len(fresh)-1].At > m.csvLastAt {
		m.csvLastAt = fresh[len(fresh)-1].At
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) loop() {
	cfg := m.cfg()
	interval := time.Duration(cfg.SampleSeconds) * time.Second
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			s := m.sample()
			m.mu.Lock()
			m.samples = append(m.samples, s)
			if len(m.samples) > 720 {
				m.samples = m.samples[len(m.samples)-720:]
			}
			m.mu.Unlock()
			m.evaluate(s)
		}
	}
}

func (m *Manager) sample() Sample {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return Sample{
		At:        time.Now().UnixMilli(),
		HeapMB:    float64(ms.HeapInuse) / (1 << 20),
		AllocMB:   float64(ms.Alloc) / (1 << 20),
		Goroutine: runtime.NumGoroutine(),
		Handles:   processHandleCount(),
		NumGC:     ms.NumGC,
	}
}

func (m *Manager) evaluate(s Sample) {
	cfg := m.cfg()
	if !cfg.Enabled {
		return
	}
	over := false
	reason := ""
	if cfg.MemLimitMB > 0 && s.HeapMB > float64(cfg.MemLimitMB) {
		over = true
		reason = fmt.Sprintf("堆内存 %.0fMB 超过上限 %dMB", s.HeapMB, cfg.MemLimitMB)
	}
	if cfg.GoroutineLimit > 0 && s.Goroutine > cfg.GoroutineLimit {
		over = true
		reason = fmt.Sprintf("goroutine %d 超过上限 %d", s.Goroutine, cfg.GoroutineLimit)
	}
	if cfg.HandleLimit > 0 && s.Handles > cfg.HandleLimit {
		over = true
		reason = fmt.Sprintf("句柄 %d 超过上限 %d", s.Handles, cfg.HandleLimit)
	}
	m.mu.Lock()
	if over {
		m.strikes++
	} else {
		m.strikes = 0
	}
	strikes := m.strikes
	heal := m.onHeal
	m.mu.Unlock()
	if strikes < cfg.StrikesToRestart || cfg.StrikesToRestart <= 0 {
		return
	}
	m.mu.Lock()
	m.strikes = 0
	m.mu.Unlock()
	if heal != nil {
		heal(reason)
	}
}

// SelfHeal 记录自愈并拉起新进程，随后由调用方退出当前进程。
func (m *Manager) SelfHeal(reason string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// 自愈事件统一走结构化日志（原 supervisor.log 的裸写已下线）。
	m.log.Warn("资源自愈触发", "reason", reason, "exe", exe)
	cmd := exec.Command(exe, "--silent")
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起新进程失败: %w", err)
	}
	return nil
}

// Recover 把 goroutine 中的 panic 兜住并写日志，返回是否发生 panic。
//
// 兼容旧签名：panic 内容现在写进结构化日志（component=panic），
// 不再单独维护 crash.log；调用方无需改动。
func Recover(layout config.Layout, where string) (panicked bool) {
	if r := recover(); r != nil {
		panicked = true
		lg := subsystemLogger(layout)
		lg.Error("goroutine panic 已兜住", "where", where, "panic", fmt.Sprint(r))
	}
	return panicked
}

// subsystemLogger 返回一个落盘到 layout.LogsDir 的日志器（按需惰性创建，进程内复用）。
// 用途：让没有持有 Manager 的调用点（如 Recover）也能写结构化日志。
var (
	subOnce sync.Once
	subLog  *logging.Logger
)

func subsystemLogger(layout config.Layout) *logging.Logger {
	subOnce.Do(func() {
		lg, err := logging.Open(logging.Options{Dir: layout.LogsDir, Level: logging.LevelInfo})
		if err != nil {
			subLog = logging.Nop()
			return
		}
		subLog = lg.At("panic")
	})
	if subLog == nil {
		return logging.Nop()
	}
	return subLog
}

func processHandleCount() int { return handleCount() }

package gov

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"workbench/internal/config"
	"workbench/internal/supervisor"
)

// MetricsSource 是体量监视器的数据源（由调度引擎提供，避免反向依赖）。
type MetricsSource interface {
	TasksToday() int
	ThroughputDaily() int
	Backlog() int
	AvgCycleSeconds() float64
	FailureRate() float64
	Concurrency() int
}

// SilentSource 是无引擎时的兜底数据源（CLI 单独跑治理时使用）。
type SilentSource struct{}

func (SilentSource) TasksToday() int          { return 0 }
func (SilentSource) ThroughputDaily() int     { return 0 }
func (SilentSource) Backlog() int             { return 0 }
func (SilentSource) AvgCycleSeconds() float64 { return 0 }
func (SilentSource) FailureRate() float64     { return 0 }
func (SilentSource) Concurrency() int         { return 0 }

// Engine 是治理监控循环：定期采样体量 → 重算预警/调整/联动 → 落盘。
type Engine struct {
	layout config.Layout
	store  *Store
	src    MetricsSource
	sup    *supervisor.Manager
	cfg    func() config.GovConfig

	mu         sync.Mutex
	lastSample Sample
	lastAt     int64
	started    atomic.Bool
	stop       chan struct{}
	cycles     atomic.Int64
}

// NewEngine 构造治理引擎。
func NewEngine(layout config.Layout, st *Store, src MetricsSource, sup *supervisor.Manager, cfg func() config.GovConfig) *Engine {
	if src == nil {
		src = SilentSource{}
	}
	if cfg == nil {
		cfg = func() config.GovConfig { return config.DefaultConfig().Gov }
	}
	return &Engine{layout: layout, store: st, src: src, sup: sup, cfg: cfg}
}

// Store 返回底层存储（HTTP 层只读访问用）。
func (e *Engine) Store() *Store { return e.store }

// Cycles 返回已完成的采样周期数。
func (e *Engine) Cycles() int64 { return e.cycles.Load() }

// Start 启动监控循环（幂等）。
func (e *Engine) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	e.stop = make(chan struct{})
	go e.loop()
}

// Stop 停止监控循环。
func (e *Engine) Stop() {
	if e.started.CompareAndSwap(true, false) && e.stop != nil {
		close(e.stop)
	}
}

func (e *Engine) loop() {
	interval := e.sampleInterval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// 启动即采一次，保证面板首屏有数据（无引擎任务时也能看到资源基线）
	e.tick(time.Now())
	for {
		select {
		case <-e.stop:
			return
		case t := <-ticker.C:
			e.tick(t)
		}
	}
}

func (e *Engine) sampleInterval() time.Duration {
	sec := e.cfg().SampleSeconds
	if sec < 5 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// tick 执行一次完整治理周期：采样 → 分析 → 落盘 → 升级处理。
func (e *Engine) tick(now time.Time) {
	s := e.Sample(now)
	if err := e.store.AddSample(s); err != nil {
		return
	}
	alerts, adjust, lr := e.store.Recompute(now)
	_ = lr
	e.mu.Lock()
	e.lastSample = s
	e.lastAt = s.At
	e.mu.Unlock()
	e.cycles.Add(1)
	// 自动升级：出现红区且配置开启时，把建议写进审计流（不自动改配置，防越权）
	if e.cfg().AutoEscalate {
		for _, a := range alerts {
			if a.Severity == SeverityCritical {
				e.audit("escalate", a.ID+" "+a.Title+" | "+a.Suggestion)
			}
		}
	}
	_ = adjust
}

// Sample 采集一次体量样本（资源取 supervisor 最新采样，业务/运营取指标源）。
func (e *Engine) Sample(now time.Time) Sample {
	s := Sample{At: now.UnixMilli()}
	s.TasksDaily = e.src.TasksToday()
	s.ThroughputDaily = e.src.ThroughputDaily()
	s.Backlog = e.src.Backlog()
	s.AvgCycleSec = round2(e.src.AvgCycleSeconds())
	s.FailureRate = round2(e.src.FailureRate())
	s.Concurrency = e.src.Concurrency()
	// 契约侧
	contracts := e.store.Contracts()
	s.ContractsActive = 0
	for _, c := range contracts {
		if activeContract(c) {
			s.ContractsActive++
			if c.Amount > s.ContractAmount {
				s.ContractAmount = c.Amount
			}
		}
	}
	// 资源侧
	if e.sup != nil {
		if ss := e.sup.Samples(); len(ss) > 0 {
			last := ss[len(ss)-1]
			s.MemMB = round2(last.HeapMB)
			s.Handles = last.Handles
			s.Goroutines = last.Goroutine
		}
	}
	if s.Goroutines == 0 {
		s.Goroutines = runtime.NumGoroutine()
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if s.MemMB == 0 {
		s.MemMB = round2(float64(ms.HeapInuse) / (1 << 20))
	}
	// 产出区与事件流体量
	s.OutputBytes = dirSize(e.layout.DataRoot)
	s.EventsBytes = maxTaskEventsBytes(e.layout.TasksDir)
	return s
}

// RunOnce 同步跑一次治理周期（CLI / e2e 使用，不启动 goroutine）。
func (e *Engine) RunOnce(now time.Time) Summary {
	e.tick(now)
	return e.store.Summary(now)
}

// Summary 返回当前治理总览（不触发采样）。
func (e *Engine) Summary() Summary { return e.store.Summary(time.Now()) }

func (e *Engine) audit(kind, detail string) {
	path := filepath.Join(e.layout.LogsDir, "governance.jsonl")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(map[string]string{"at": time.Now().Format(time.RFC3339), "kind": kind, "detail": detail})
	_, _ = f.Write(append(b, '\n'))
}

// dirSize 统计目录字节数（跳过 .git / runtime 缓存目录，防把体量分母算胖）。
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// maxTaskEventsBytes 返回单任务事件流的最大字节数（体量治理的"单件上限"来源）。
func maxTaskEventsBytes(tasksDir string) int64 {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return 0
	}
	var max int64
	for _, en := range entries {
		if !en.IsDir() {
			continue
		}
		if info, ierr := os.Stat(filepath.Join(tasksDir, en.Name(), "events.jsonl")); ierr == nil && info.Size() > max {
			max = info.Size()
		}
	}
	return max
}

// FormatSummary 把总览格式化为 CLI 可读文本（e2e 断言与人工排障共用）。
func FormatSummary(sum Summary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "治理等级: %s | 契约 %d 份（在履约 %d，合规均分 %.1f，逾期 %d，临期 %d）\n",
		LevelName(sum.Level), sum.Contracts.Total, sum.Contracts.Active, sum.Contracts.ComplianceAvg,
		sum.Contracts.Overdue, sum.Contracts.ExpiringSoon)
	fmt.Fprintf(&b, "体量: 采样 %d 点 | 等级 %s | 任务/日 %d | 契约负载 %d | 内存 %.1fMB | 句柄 %d\n",
		sum.Volume.Samples, LevelName(sum.Volume.Level), sum.Volume.TasksDaily, sum.Volume.ContractsActive,
		sum.Volume.MemMB, sum.Volume.Handles)
	for _, d := range sum.Domains {
		fmt.Fprintf(&b, "  [%s] %s 绿=%d 黄=%d 红=%d 未验证=%d 峰值占用=%.0f%%\n",
			LevelName(d.Level), d.Name, d.OK, d.Warn, d.Red, d.Unknown, d.PeakRatio*100)
	}
	fmt.Fprintf(&b, "预警: 总 %d（红 %d / 黄 %d / 蓝 %d）\n", sum.Alerts.Total, sum.Alerts.Critical, sum.Alerts.Warn, sum.Alerts.Info)
	for _, a := range sum.Alerts.Top {
		fmt.Fprintf(&b, "  - [%s] %s：%s\n", a.Severity, a.Title, a.Detail)
	}
	fmt.Fprintf(&b, "自适应调整: %d 条\n", len(sum.Adjustments))
	for _, a := range sum.Adjustments {
		fmt.Fprintf(&b, "  - [%s] %s：%s（%s）\n", a.Kind, a.Metric, a.Action, a.Basis)
	}
	fmt.Fprintf(&b, "联动: 等级 %s | 负载率 %.0f%% | 耗时相关 %.2f | 失败相关 %.2f\n",
		LevelName(sum.Linkage.Level), sum.Linkage.LoadRatio*100, sum.Linkage.CorrCycle, sum.Linkage.CorrFailure)
	for _, s := range sum.Linkage.Signals {
		if s.Severity == SeverityInfo {
			continue
		}
		fmt.Fprintf(&b, "  - [%s] %s → %s\n", s.Severity, s.Title, s.Decision)
	}
	fmt.Fprintf(&b, "考核校验: PASS %d / FAIL %d（等级 %s）\n", sum.Compliance.Pass, sum.Compliance.Fail, LevelName(sum.Compliance.Level))
	for _, it := range sum.Compliance.Items {
		mark := "PASS"
		if !it.Pass {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "  - [%s] %s %s：目标 %s，实际 %s（%s）\n", mark, it.Code, it.Name, it.Target, it.Actual, it.Evidence)
	}
	if len(sum.DataGaps) > 0 {
		fmt.Fprintf(&b, "数据缺口: %s\n", strings.Join(sum.DataGaps, "；"))
	}
	return b.String()
}

// ParseFloat 是配置解析辅助（阈值覆盖用）。
func ParseFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}
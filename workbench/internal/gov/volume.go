package gov

import (
	"fmt"
	"math"
	"sort"
)

// Domain 是体量治理的三类业务域。
type Domain string

const (
	DomainBusiness  Domain = "business"  // 业务体量
	DomainResource  Domain = "resource"  // 资源承载体量
	DomainOperation Domain = "operation" // 运营规模体量
)

// LevelName 返回等级中文名（CLI 与文档共用，避免"unverified"裸奔进报告）。
func LevelName(l string) string {
	switch l {
	case LevelOK:
		return "绿区"
	case LevelWarn:
		return "黄区"
	case LevelRed:
		return "红区"
	}
	return "未验证"
}

// DomainName 返回中文域名（文档与面板共用）。
func DomainName(d Domain) string {
	switch d {
	case DomainBusiness:
		return "业务体量"
	case DomainResource:
		return "资源承载体量"
	case DomainOperation:
		return "运营规模体量"
	}
	return string(d)
}

// Budget 是体量治理的量化阈值（可配置覆盖，默认值来自规范文档）。
type Budget struct {
	// 业务体量
	TaskDailyMax      int     `json:"taskDailyMax"`      // 日产出任务上限
	ActiveContractMax int     `json:"activeContractMax"` // 在履约契约上限
	ContractAmountMax float64 `json:"contractAmountMax"` // 单契约金额上限（元）
	OutputBytesMax    int64   `json:"outputBytesMax"`    // 产出区总量上限（字节）
	// 资源承载
	MemLimitMB      int `json:"memLimitMB"`
	HandleLimit     int `json:"handleLimit"`
	GoroutineLimit  int `json:"goroutineLimit"`
	ConcurrencyMax  int `json:"concurrencyMax"`
	EventsBytesMax  int64 `json:"eventsBytesMax"`
	// 运营规模
	BacklogMax         int     `json:"backlogMax"`
	ThroughputDailyMin int     `json:"throughputDailyMin"`
	AvgCycleSecondsMax int     `json:"avgCycleSecondsMax"`
	FailureRateMax     float64 `json:"failureRateMax"`
}

// DefaultBudget 返回默认阈值（与 docs/治理规范 一致，改代码必须同步文档）。
func DefaultBudget() Budget {
	return Budget{
		TaskDailyMax: 200, ActiveContractMax: 120, ContractAmountMax: 5_000_000,
		OutputBytesMax: 5 << 30,
		MemLimitMB: 700, HandleLimit: 3000, GoroutineLimit: 1500, ConcurrencyMax: 6,
		EventsBytesMax: 32 << 20,
		BacklogMax: 60, ThroughputDailyMin: 8, AvgCycleSecondsMax: 900, FailureRateMax: 0.10,
	}
}

// Normalize 修正越界阈值，保证运行期不会因手改配置而失去判据。
func (b *Budget) Normalize() {
	d := DefaultBudget()
	if b.TaskDailyMax <= 0 {
		b.TaskDailyMax = d.TaskDailyMax
	}
	if b.ActiveContractMax <= 0 {
		b.ActiveContractMax = d.ActiveContractMax
	}
	if b.ContractAmountMax <= 0 {
		b.ContractAmountMax = d.ContractAmountMax
	}
	if b.OutputBytesMax <= 0 {
		b.OutputBytesMax = d.OutputBytesMax
	}
	if b.MemLimitMB <= 0 {
		b.MemLimitMB = d.MemLimitMB
	}
	if b.HandleLimit <= 0 {
		b.HandleLimit = d.HandleLimit
	}
	if b.GoroutineLimit <= 0 {
		b.GoroutineLimit = d.GoroutineLimit
	}
	if b.ConcurrencyMax <= 0 {
		b.ConcurrencyMax = d.ConcurrencyMax
	}
	if b.EventsBytesMax <= 0 {
		b.EventsBytesMax = d.EventsBytesMax
	}
	if b.BacklogMax <= 0 {
		b.BacklogMax = d.BacklogMax
	}
	if b.ThroughputDailyMin < 0 {
		b.ThroughputDailyMin = d.ThroughputDailyMin
	}
	if b.AvgCycleSecondsMax <= 0 {
		b.AvgCycleSecondsMax = d.AvgCycleSecondsMax
	}
	if b.FailureRateMax <= 0 || b.FailureRateMax > 1 {
		b.FailureRateMax = d.FailureRateMax
	}
}

// Sample 是一次体量采样（由调度循环定期落盘，JSONL 追加）。
type Sample struct {
	At             int64   `json:"at"` // Unix 毫秒
	TasksDaily     int     `json:"tasksDaily"`
	ContractsActive int    `json:"contractsActive"`
	ContractAmount float64 `json:"contractAmount"` // 单笔最大金额
	OutputBytes    int64   `json:"outputBytes"`
	MemMB          float64 `json:"memMB"`
	Handles        int     `json:"handles"`
	Goroutines     int     `json:"goroutines"`
	Concurrency    int     `json:"concurrency"`
	EventsBytes    int64   `json:"eventsBytes"`
	Backlog        int     `json:"backlog"`
	ThroughputDaily int    `json:"throughputDaily"`
	AvgCycleSec    float64 `json:"avgCycleSec"`
	FailureRate    float64 `json:"failureRate"`
}

// 指标等级。
const (
	LevelOK       = "ok"
	LevelWarn     = "warn"
	LevelRed      = "red"
	LevelUnknown  = "unverified"
)

// Direction 表示指标方向：high=越高越差（管上限），low=越低越差（管下限）。
type Direction string

const (
	DirHigh Direction = "high"
	DirLow  Direction = "low"
)

// Eval 是单个指标的评估结果。
type Eval struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Domain    Domain    `json:"domain"`
	Unit      string    `json:"unit"`
	Direction Direction `json:"direction"`
	Value     float64   `json:"value"`
	Warn      float64   `json:"warn"`
	Red       float64   `json:"red"`
	Level     string    `json:"level"`
	Ratio     float64   `json:"ratio"` // 相对红线的占用率（high 方向: value/red；low 方向: red/value）
}

// level 判定：high 方向 value>=red 判红、>=warn 判黄；low 方向取反。
func levelOf(v, warn, red float64, d Direction) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return LevelUnknown
	}
	if d == DirHigh {
		switch {
		case v >= red:
			return LevelRed
		case v >= warn:
			return LevelWarn
		default:
			return LevelOK
		}
	}
	switch {
	case v <= red:
		return LevelRed
	case v <= warn:
		return LevelWarn
	default:
		return LevelOK
	}
}

func ratioOf(v, warn, red float64, d Direction) float64 {
	if d == DirHigh {
		if red <= 0 {
			return 0
		}
		return round2(v / red)
	}
	if v <= 0 {
		return 0 // 零吞吐属红，占用率按 0 记，避免除零把面板打爆
	}
	if red <= 0 {
		return 0
	}
	return round2(red / v)
}

// Evaluate 把一次采样按预算换算成 13 项指标评估（三域全覆盖）。
//
// 一处刻意的特例：**零活动态**（当日任务 0 且积压 0）下，低方向指标（日吞吐）
// 判 unverified 而不是红——新装环境没有任何任务时，吞吐为 0 是"没数据"而不是"产能不足"。
// 一旦有任务或积压，0 吞吐就是真红（引擎在跑却不出活）。这条特例有测试锁定。
func Evaluate(s Sample, b Budget) []Eval {
	b.Normalize()
	zeroActivity := s.TasksDaily == 0 && s.Backlog == 0
	mk := func(key, name, unit string, dom Domain, dir Direction, v, warn, red float64) Eval {
		lvl := levelOf(v, warn, red, dir)
		if dir == DirLow && v == 0 && zeroActivity {
			lvl = LevelUnknown
		}
		// 资源域指标在真实进程里不可能为 0（内存/句柄/协程/并发恒 >0）；
		// 取到 0 只可能是"没测到"（如 CLI 单跑无 supervisor），记 unverified 而非绿。
		if dom == DomainResource && v <= 0 {
			lvl = LevelUnknown
		}
		ratio := ratioOf(v, warn, red, dir)
		if lvl == LevelUnknown {
			ratio = 0
		}
		return Eval{Key: key, Name: name, Unit: unit, Domain: dom, Direction: dir,
			Value: round2(v), Warn: warn, Red: red, Level: lvl, Ratio: ratio}
	}
	return []Eval{
		mk("tasksDaily", "日产出任务数", "个/日", DomainBusiness, DirHigh,
			float64(s.TasksDaily), float64(b.TaskDailyMax)*0.8, float64(b.TaskDailyMax)),
		mk("contractsActive", "在履约契约数", "份", DomainBusiness, DirHigh,
			float64(s.ContractsActive), float64(b.ActiveContractMax)*0.8, float64(b.ActiveContractMax)),
		mk("contractAmount", "单契约金额", "元", DomainBusiness, DirHigh,
			s.ContractAmount, b.ContractAmountMax*0.8, b.ContractAmountMax),
		mk("outputBytes", "产出区总量", "MiB", DomainBusiness, DirHigh,
			bytesToMiB(s.OutputBytes), bytesToMiB(b.OutputBytesMax)*0.8, bytesToMiB(b.OutputBytesMax)),
		mk("memMB", "常驻内存", "MB", DomainResource, DirHigh,
			s.MemMB, float64(b.MemLimitMB)*0.8, float64(b.MemLimitMB)),
		mk("handles", "句柄数", "个", DomainResource, DirHigh,
			float64(s.Handles), float64(b.HandleLimit)*0.8, float64(b.HandleLimit)),
		mk("goroutines", "协程数", "个", DomainResource, DirHigh,
			float64(s.Goroutines), float64(b.GoroutineLimit)*0.8, float64(b.GoroutineLimit)),
		mk("concurrency", "并发执行数", "个", DomainResource, DirHigh,
			float64(s.Concurrency), float64(b.ConcurrencyMax)*0.8, float64(b.ConcurrencyMax)),
		mk("eventsBytes", "单任务事件流", "MiB", DomainResource, DirHigh,
			bytesToMiB(s.EventsBytes), bytesToMiB(b.EventsBytesMax)*0.8, bytesToMiB(b.EventsBytesMax)),
		mk("backlog", "待处理积压", "个", DomainOperation, DirHigh,
			float64(s.Backlog), float64(b.BacklogMax)*0.8, float64(b.BacklogMax)),
		mk("throughputDaily", "日吞吐量", "个/日", DomainOperation, DirLow,
			float64(s.ThroughputDaily), float64(b.ThroughputDailyMin)*1.2, float64(b.ThroughputDailyMin)),
		mk("avgCycleSec", "平均单任务耗时", "秒", DomainOperation, DirHigh,
			s.AvgCycleSec, float64(b.AvgCycleSecondsMax)*0.8, float64(b.AvgCycleSecondsMax)),
		mk("failureRate", "任务失败率", "比例", DomainOperation, DirHigh,
			s.FailureRate, b.FailureRateMax*0.7, b.FailureRateMax),
	}
}

func bytesToMiB(n int64) float64 { return round2(float64(n) / (1 << 20)) }

// DomainSummary 是单域汇总。
type DomainSummary struct {
	Domain    Domain `json:"domain"`
	Name      string `json:"name"`
	Total     int    `json:"total"`
	OK        int    `json:"ok"`
	Warn      int    `json:"warn"`
	Red       int    `json:"red"`
	Unknown   int    `json:"unknown"`
	Level     string `json:"level"`
	PeakRatio float64 `json:"peakRatio"`
}

// Summarize 汇总三域（某域任一指标红 ⇒ 该域红；有黄无红 ⇒ 黄；全绿 ⇒ 绿）。
func Summarize(evals []Eval) []DomainSummary {
	order := []Domain{DomainBusiness, DomainResource, DomainOperation}
	out := make([]DomainSummary, 0, len(order))
	for _, d := range order {
		s := DomainSummary{Domain: d, Name: DomainName(d), Level: LevelOK}
		for _, e := range evals {
			if e.Domain != d {
				continue
			}
			s.Total++
			switch e.Level {
			case LevelRed:
				s.Red++
			case LevelWarn:
				s.Warn++
			case LevelUnknown:
				s.Unknown++
			default:
				s.OK++
			}
			if e.Ratio > s.PeakRatio {
				s.PeakRatio = e.Ratio
			}
		}
		switch {
		case s.Red > 0:
			s.Level = LevelRed
		case s.Warn > 0:
			s.Level = LevelWarn
		case s.Total == 0 || s.Unknown == s.Total:
			s.Level = LevelUnknown
		}
		out = append(out, s)
	}
	return out
}

// Adjustment 是自适应调整建议（可执行、带滞回依据）。
type Adjustment struct {
	Metric     string `json:"metric"`
	Domain     Domain `json:"domain"`
	Action     string `json:"action"`     // 调整动作（人可读、带建议值）
	Kind       string `json:"kind"`       // escalate（收紧）/ relax（放宽）/ hold（维持）
	Basis      string `json:"basis"`      // 依据：连续 N 个样本红/绿
	Delta      string `json:"delta"`      // 建议变动量（如 "+20%"）
	Confidence float64 `json:"confidence"` // 0-1，样本数越多越高
}

// 滞回参数（写死为常量，避免配置漂移；如需改必须同步规范文档与测试）。
const (
	HysteresisRedStreak   = 2 // 连续 N 次红才升级调整
	HysteresisGreenStreak = 3 // 连续 N 次绿才放宽
	MaxAdjustPct          = 20.0
)

// Adjust 依据历史样本给出自适应调整建议（滞回 + 单轮步长上限）。
// history 按时间升序；末位为最新样本。
func Adjust(history []Sample, b Budget) []Adjustment {
	if len(history) == 0 {
		return nil
	}
	b.Normalize()
	all := make([][]Eval, len(history))
	for i, s := range history {
		all[i] = Evaluate(s, b)
	}
	last := all[len(all)-1]
	var out []Adjustment
	for i, e := range last {
		streak, streakLevel := tailStreak(all, i, e.Level)
		conf := math.Min(1, float64(streak)/float64(HysteresisGreenStreak))
		switch e.Level {
		case LevelRed:
			if streak >= HysteresisRedStreak {
				out = append(out, Adjustment{
					Metric: e.Key, Domain: e.Domain, Kind: "escalate",
					Action: adjustAction(e, b, true),
					Basis:  fmt.Sprintf("连续 %d 个样本处于红区（阈值 %d）", streak, HysteresisRedStreak),
					Delta:  fmt.Sprintf("+%.0f%% 容量或 -%.0f%% 负载", MaxAdjustPct, MaxAdjustPct),
					Confidence: conf,
				})
			}
		case LevelWarn:
			// 黄区不调整，只观察；红区才动，避免抖动。
		case LevelOK:
			if streak >= HysteresisGreenStreak && e.Ratio > 0 && e.Ratio < 0.5 {
				out = append(out, Adjustment{
					Metric: e.Key, Domain: e.Domain, Kind: "relax",
					Action: adjustAction(e, b, false),
					Basis:  fmt.Sprintf("连续 %d 个样本占用率 < 50%%（当前 %.0f%%），可回收余量", streak, e.Ratio*100),
					Delta:  fmt.Sprintf("-%.0f%% 预算或 +%.0f%% 负载", MaxAdjustPct*0.5, MaxAdjustPct),
					Confidence: math.Min(1, float64(streak)/float64(HysteresisGreenStreak+2)),
				})
			}
		}
		_ = streakLevel
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "escalate"
		}
		return out[i].Metric < out[j].Metric
	})
	return out
}

// tailStreak 返回末尾连续处于 level 的样本数。
func tailStreak(all [][]Eval, metricIdx int, level string) (int, string) {
	n := 0
	for i := len(all) - 1; i >= 0; i-- {
		if metricIdx >= len(all[i]) || all[i][metricIdx].Level != level {
			break
		}
		n++
	}
	return n, level
}

// adjustAction 生成人可读的调整动作文案。
func adjustAction(e Eval, b Budget, escalate bool) string {
	dir := "上调阈值"
	verb := "扩容"
	if e.Direction == DirLow {
		dir = "下调阈值"
		verb = "提产能"
	}
	if !escalate {
		dir = "下调阈值（回收余量）"
		verb = "放宽负载"
	}
	switch e.Key {
	case "memMB":
		return fmt.Sprintf("%s：内存上限 %.0fMB → 建议 %s（当前 %.0fMB）", verb, float64(b.MemLimitMB), dir, e.Value)
	case "handles":
		return fmt.Sprintf("%s：句柄上限 %d → %s（当前 %.0f）", verb, b.HandleLimit, dir, e.Value)
	case "goroutines":
		return fmt.Sprintf("%s：协程上限 %d → %s（当前 %.0f）", verb, b.GoroutineLimit, dir, e.Value)
	case "concurrency":
		return fmt.Sprintf("%s：并发上限 %d → %s（当前 %.0f）", verb, b.ConcurrencyMax, dir, e.Value)
	case "eventsBytes":
		return fmt.Sprintf("%s：事件流上限 %.0fMiB → %s（当前 %.1fMiB）", verb, bytesToMiB(b.EventsBytesMax), dir, e.Value)
	case "tasksDaily":
		return fmt.Sprintf("%s：日产上限 %d → %s（当前 %.0f）", verb, b.TaskDailyMax, dir, e.Value)
	case "contractsActive":
		return fmt.Sprintf("%s：在履约契约上限 %d → %s（当前 %.0f）", verb, b.ActiveContractMax, dir, e.Value)
	case "contractAmount":
		return fmt.Sprintf("%s：单契约金额上限 %.0f → %s（当前 %.0f）", verb, b.ContractAmountMax, dir, e.Value)
	case "outputBytes":
		return fmt.Sprintf("%s：产出区上限 %.0fMiB → %s（当前 %.1fMiB）", verb, bytesToMiB(b.OutputBytesMax), dir, e.Value)
	case "backlog":
		return fmt.Sprintf("%s：积压上限 %d → %s（当前 %.0f）", verb, b.BacklogMax, dir, e.Value)
	case "throughputDaily":
		return fmt.Sprintf("%s：日吞吐下限 %d → %s（当前 %.0f）", verb, b.ThroughputDailyMin, dir, e.Value)
	case "avgCycleSec":
		return fmt.Sprintf("%s：单任务耗时上限 %d 秒 → %s（当前 %.0fs）", verb, b.AvgCycleSecondsMax, dir, e.Value)
	case "failureRate":
		return fmt.Sprintf("%s：失败率上限 %.0f%% → %s（当前 %.1f%%）", verb, b.FailureRateMax*100, dir, e.Value*100)
	}
	return fmt.Sprintf("%s：%s 阈值%s（当前 %.2f）", verb, e.Name, dir, e.Value)
}
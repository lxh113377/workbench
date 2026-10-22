package gov

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// LinkPoint 是联动分析的一个时间点（由体量样本 + 当点在履约契约快照聚合而成）。
type LinkPoint struct {
	At              int64   `json:"at"`
	ContractsActive int     `json:"contractsActive"`
	TasksDaily      int     `json:"tasksDaily"`
	AvgCycleSec     float64 `json:"avgCycleSec"`
	FailureRate     float64 `json:"failureRate"`
	ComplianceAvg   float64 `json:"complianceAvg"`
	DeliveryAvg     float64 `json:"deliveryAvg"`
}

// LinkSignal 是协同决策信号（两条治理线联动的结论）。
type LinkSignal struct {
	ID          string  `json:"id"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Detail      string  `json:"detail"`
	Decision    string  `json:"decision"`
	Correlation float64 `json:"correlation"`
	Confidence  float64 `json:"confidence"`
}

// Allocation 是单份契约的体量风险分摊（协同决策的直接输入）。
type Allocation struct {
	ContractID string  `json:"contractId"`
	Title      string  `json:"title"`
	Amount     float64 `json:"amount"`
	Share      float64 `json:"share"`     // 金额占比 0-1
	LoadScore  float64 `json:"loadScore"` // 综合负载分 0-100（金额占比 × 履约质量）
	Rating     string  `json:"rating"`    // 承载评级 A/B/C/D
	Suggestion string  `json:"suggestion"`
}

// LinkageReport 是两类治理数据互通链路的产出。
type LinkageReport struct {
	GeneratedAt     string       `json:"generatedAt"`
	Window          int          `json:"window"`
	Series          []LinkPoint  `json:"series"`
	Signals         []LinkSignal `json:"signals"`
	Allocations     []Allocation `json:"allocations"`
	ContractsActive int          `json:"contractsActive"`
	ComplianceAvg   float64      `json:"complianceAvg"`
	DeliveryAvg     float64      `json:"deliveryAvg"`
	LoadRatio       float64      `json:"loadRatio"`   // 契约负载率（在履约数/上限）
	CorrCycle       float64      `json:"corrCycle"`   // 契约负载 × 平均耗时 相关系数
	CorrFailure     float64      `json:"corrFailure"` // 契约负载 × 失败率 相关系数
	DataGaps        []string     `json:"dataGaps"`    // 数据缺口（缺样本时必须显式声明，不许当 0）
	Level           string       `json:"level"`
}

// activeContract 判断契约是否计入"在履约"负载。
func activeContract(c Contract) bool {
	return c.Stage == StageSigned || c.Stage == StagePerforming
}

// BuildLinkage 构建联动分析报告：把契约履约数据与体量样本按时间窗口合并。
func BuildLinkage(contracts []Contract, samples []Sample, b Budget, windowSize int, now string) LinkageReport {
	b.Normalize()
	if windowSize <= 0 {
		windowSize = 12
	}
	rep := LinkageReport{GeneratedAt: now, Window: windowSize}
	// 1) 契约侧聚合
	var active []Contract
	compSum, delSum, amtSum := 0.0, 0.0, 0.0
	for _, c := range contracts {
		if activeContract(c) {
			active = append(active, c)
			compSum += ComplianceScore(c)
			if c.DeliveryOnTime > 0 {
				delSum += c.DeliveryOnTime
			}
			amtSum += c.Amount
		}
	}
	rep.ContractsActive = len(active)
	if len(active) > 0 {
		rep.ComplianceAvg = round2(compSum / float64(len(active)))
		rep.DeliveryAvg = round2(delSum / float64(len(active)))
	}
	if b.ActiveContractMax > 0 {
		rep.LoadRatio = round2(float64(len(active)) / float64(b.ActiveContractMax))
	}
	// 2) 时间窗口
	if len(samples) > windowSize {
		samples = samples[len(samples)-windowSize:]
	}
	if len(samples) == 0 {
		rep.DataGaps = append(rep.DataGaps, "无体量采样（samples=0）⇒ 联动结论记 unverified，不得记 PASS")
	} else {
		for _, s := range samples {
			rep.Series = append(rep.Series, LinkPoint{
				At: s.At, ContractsActive: s.ContractsActive, TasksDaily: s.TasksDaily,
				AvgCycleSec: s.AvgCycleSec, FailureRate: s.FailureRate,
				ComplianceAvg: rep.ComplianceAvg, DeliveryAvg: rep.DeliveryAvg,
			})
		}
	}
	// 3) 相关系数（用契约负载序列 vs 耗时/失败率序列）
	var loadSeries, cycleSeries, failSeries []float64
	for _, s := range samples {
		loadSeries = append(loadSeries, float64(s.ContractsActive))
		cycleSeries = append(cycleSeries, s.AvgCycleSec)
		failSeries = append(failSeries, s.FailureRate)
	}
	rep.CorrCycle = round2(pearson(loadSeries, cycleSeries))
	rep.CorrFailure = round2(pearson(loadSeries, failSeries))
	if len(samples) < 3 {
		rep.DataGaps = append(rep.DataGaps, fmt.Sprintf("采样仅 %d 点（<3）⇒ 相关系数不可靠，只作观察", len(samples)))
	}
	// 4) 分摊
	if amtSum > 0 {
		byAmount := append([]Contract{}, active...)
		sort.SliceStable(byAmount, func(i, j int) bool { return byAmount[i].Amount > byAmount[j].Amount })
		for _, c := range byAmount {
			share := c.Amount / amtSum
			comp := ComplianceScore(c)
			// 负载分 = 金额占比 60 + 履约质量缺口 40（质量越差负载越高）
			load := share*60 + (100-math.Max(comp, 0))*0.4
			rep.Allocations = append(rep.Allocations, Allocation{
				ContractID: c.ID, Title: c.Title, Amount: c.Amount,
				Share: round2(share), LoadScore: round2(load), Rating: ratingOf(load),
				Suggestion: allocationSuggestion(load, share),
			})
		}
	}
	// 5) 信号
	rep.Signals = linkageSignals(rep, b)
	// 6) 总等级
	rep.Level = LevelOK
	for _, s := range rep.Signals {
		if s.Severity == SeverityCritical {
			rep.Level = LevelRed
			break
		}
		if s.Severity == SeverityWarn {
			rep.Level = LevelWarn
		}
	}
	if len(rep.DataGaps) > 0 && rep.Level == LevelOK {
		rep.Level = LevelUnknown
	}
	return rep
}

func ratingOf(load float64) string {
	switch {
	case load >= 70:
		return "D"
	case load >= 50:
		return "C"
	case load >= 30:
		return "B"
	}
	return "A"
}

func allocationSuggestion(load, share float64) string {
	switch ratingOf(load) {
	case "D":
		return "高风险承载：冻结新增同类契约，30 天内完成履约整改或降额"
	case "C":
		return "偏高承载：纳入季度重点抽查，金额占比 " + fmt.Sprintf("%.0f%%", share*100)
	case "B":
		return "正常承载：按常规节奏监控履约里程碑"
	}
	return "低承载：可承接新增业务体量"
}

func linkageSignals(rep LinkageReport, b Budget) []LinkSignal {
	var out []LinkSignal
	add := func(id, sev, title, detail, decision string, corr, conf float64) {
		out = append(out, LinkSignal{ID: id, Severity: sev, Title: title, Detail: detail,
			Decision: decision, Correlation: corr, Confidence: conf})
	}
	load := rep.LoadRatio
	if load >= 0.8 && rep.CorrCycle >= 0.6 {
		add("LK-1", SeverityCritical,
			"契约负载与执行耗时强正相关",
			fmt.Sprintf("契约负载率 %.0f%%，契约负载×平均耗时相关系数 %.2f：契约规模扩张正在直接拖慢任务执行", load*100, rep.CorrCycle),
			"优先压降在履约契约数（目标 ≤80% 上限），并复核高金额契约的交付节奏",
			rep.CorrCycle, math.Min(1, math.Abs(rep.CorrCycle)))
	} else if load >= 0.8 {
		add("LK-1b", SeverityWarn,
			"契约负载接近上限",
			fmt.Sprintf("在履约契约 %d 份，负载率 %.0f%%（预警线 80%%）", rep.ContractsActive, load*100),
			"启动存量契约结项排期；新增契约改走审批加签",
			0, 0.6)
	}
	if rep.ComplianceAvg > 0 && rep.ComplianceAvg < 70 {
		add("LK-2", SeverityCritical,
			"合规短板正在侵蚀体量承载",
			fmt.Sprintf("在履约契约平均合规分 %.1f（下限 70），低合规契约占比越高，体量扩张的隐性风险越大", rep.ComplianceAvg),
			"冻结新增体量 2 周，集中补齐权责/要素/留痕三块合规缺口",
			0, 0.8)
	}
	if rep.DeliveryAvg > 0 && rep.DeliveryAvg < 0.8 && rep.ContractsActive > 0 {
		add("LK-3", SeverityWarn,
			"履约偏差反向传导至体量指标",
			fmt.Sprintf("平均按期交付率 %.0f%%（下限 80%%），体量上涨未同步带来交付能力提升", rep.DeliveryAvg*100),
			"下调该批契约的体量承载评级，并把交付能力纳入下轮产能评估",
			0, 0.7)
	}
	if rep.CorrFailure >= 0.6 && rep.ContractsActive > 0 {
		add("LK-4", SeverityWarn,
			"契约规模与任务失败率正相关",
			fmt.Sprintf("契约负载×失败率相关系数 %.2f：规模扩张伴随失败率上升", rep.CorrFailure),
			"排查资源承载阈值是否成为瓶颈，优先检查内存/句柄/并发三项",
			rep.CorrFailure, math.Min(1, math.Abs(rep.CorrFailure)))
	}
	if rep.ContractsActive == 0 {
		add("LK-0", SeverityInfo,
			"当前无在履约契约",
			"联动分析退化为纯体量监控；契约侧数据为 0 是事实而非缺口",
			"无需动作；契约进入签署阶段后自动恢复联动",
			0, 1)
	}
	if rep.CorrCycle < 0.6 && load < 0.8 {
		add("LK-OK", SeverityInfo,
			"两类治理数据未见异常联动",
			fmt.Sprintf("负载率 %.0f%%、耗时相关系数 %.2f，均低于联动预警阈值", load*100, rep.CorrCycle),
			"维持现有节奏，按周复核",
			rep.CorrCycle, 0.5)
	}
	sort.SliceStable(out, func(i, j int) bool { return severityRank(out[i].Severity) > severityRank(out[j].Severity) })
	return out
}

// pearson 计算皮尔逊相关系数；样本不足或方差为 0 返回 0。
func pearson(x, y []float64) float64 {
	n := len(x)
	if n != len(y) || n < 2 {
		return 0
	}
	var sx, sy float64
	for i := 0; i < n; i++ {
		sx += x[i]
		sy += y[i]
	}
	mx, my := sx/float64(n), sy/float64(n)
	var num, dx2, dy2 float64
	for i := 0; i < n; i++ {
		dx, dy := x[i]-mx, y[i]-my
		num += dx * dy
		dx2 += dx * dx
		dy2 += dy * dy
	}
	den := math.Sqrt(dx2 * dy2)
	if den == 0 {
		return 0
	}
	return num / den
}

// LinkageToAlerts 把联动信号转为预警（纳入统一预警面）。
func LinkageToAlerts(rep LinkageReport) []Alert {
	var out []Alert
	for _, s := range rep.Signals {
		if s.Severity == SeverityInfo {
			continue
		}
		out = append(out, Alert{
			ID: "LK-" + s.ID, Kind: "linkage." + strings.ToLower(s.ID),
			Severity: s.Severity, Scope: ScopeLinkage, Subject: s.ID,
			Title: s.Title, Detail: s.Detail, Suggestion: s.Decision,
			Evidence: fmt.Sprintf("corr=%.2f confidence=%.2f level=%s", s.Correlation, s.Confidence, rep.Level),
			At:       rep.GeneratedAt,
		})
	}
	return out
}

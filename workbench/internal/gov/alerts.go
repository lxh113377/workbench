package gov

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// 预警严重度。
const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityCritical = "critical"
)

// 预警域。
const (
	ScopeContract = "contract"
	ScopeVolume   = "volume"
	ScopeLinkage  = "linkage"
)

// Alert 是一条治理预警（可落盘、可面板展示、可考核）。
type Alert struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Severity   string `json:"severity"`
	Scope      string `json:"scope"`
	Subject    string `json:"subject"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
	Evidence   string `json:"evidence"` // 机器可复核的量化证据
	At         string `json:"at"`
}

// AlertOptions 控制预警评估的窗口参数。
type AlertOptions struct {
	Now              time.Time
	ExpiringDays     int     // 到期预警窗口（天）
	StageSlaWarnDays int     // 阶段停留预警（天）
	ComplianceMin    float64 // 合规分下限
	DeliveryMin      float64 // 按期交付率下限
}

// DefaultAlertOptions 返回默认窗口。
func DefaultAlertOptions(now time.Time) AlertOptions {
	return AlertOptions{Now: now, ExpiringDays: 30, StageSlaWarnDays: 15, ComplianceMin: 70, DeliveryMin: 0.8}
}

func (o AlertOptions) normalized() AlertOptions {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.ExpiringDays <= 0 {
		o.ExpiringDays = 30
	}
	if o.StageSlaWarnDays <= 0 {
		o.StageSlaWarnDays = 15
	}
	if o.ComplianceMin <= 0 {
		o.ComplianceMin = 70
	}
	if o.DeliveryMin <= 0 || o.DeliveryMin > 1 {
		o.DeliveryMin = 0.8
	}
	return o
}

// ContractAlerts 产出单份契约的全部预警。
func ContractAlerts(c Contract, opt AlertOptions) []Alert {
	opt = opt.normalized()
	at := opt.Now.Format(time.RFC3339)
	var out []Alert
	add := func(kind, sev, title, detail, suggestion, evidence string) {
		out = append(out, Alert{
			ID:     fmt.Sprintf("C-%s-%s", c.ID, strings.ReplaceAll(kind, ".", "-")),
			Kind:   kind, Severity: sev, Scope: ScopeContract, Subject: c.ID,
			Title: title, Detail: detail, Suggestion: suggestion, Evidence: evidence, At: at,
		})
	}

	// 1) 高风险未签条款
	highUnsigned, highAmount := 0, 0.0
	for _, cl := range c.Clauses {
		if cl.RiskLevel >= 3 && !cl.Signed {
			highUnsigned++
			highAmount += cl.Amount
		}
	}
	if highUnsigned > 0 {
		add("contract.clause-risk", SeverityCritical,
			"存在未签署的高风险条款",
			fmt.Sprintf("契约 %s 有 %d 条高风险条款尚未签署确认，涉及金额 %.2f 元", c.Title, highUnsigned, highAmount),
			"回到审核阶段补齐风险条款审签，未签前不得进入履约",
			fmt.Sprintf("highRiskUnsigned=%d amount=%.2f stage=%s", highUnsigned, highAmount, c.Stage))
	}

	// 2) 到期预警 / 已过期未归档
	if exp, ok := ParseDay(c.ExpireAt); ok {
		days := int(exp.Sub(opt.Now).Hours() / 24)
		if c.Stage != StageArchived {
			if days < 0 {
				add("contract.expired-unarchived", SeverityCritical,
					"契约已过期但未归档",
					fmt.Sprintf("契约 %s 到期日 %s，已过期 %d 天，当前阶段 %s", c.Title, c.ExpireAt, -days, c.Stage),
					"立即核对履约完成度并走归档流程，逾期未归档将计入合规考核",
					fmt.Sprintf("expireAt=%s overdueDays=%d stage=%s", c.ExpireAt, -days, c.Stage))
			} else if days <= opt.ExpiringDays {
				sev := SeverityWarn
				if days <= 7 {
					sev = SeverityCritical
				}
				add("contract.expiring", sev,
					"契约临近到期",
					fmt.Sprintf("契约 %s 将于 %s 到期（剩余 %d 天），当前阶段 %s", c.Title, c.ExpireAt, days, c.Stage),
					"启动续签或结项评估，并同步更新履约里程碑",
					fmt.Sprintf("expireAt=%s remainDays=%d stage=%s", c.ExpireAt, days, c.Stage))
			}
		}
	}

	// 3) 阶段停留超期（按该阶段最长 SLA 折算，缺省用窗口）
	dwell := StageDwell(c, opt.Now)
	if c.Stage != StageArchived && dwell > 0 {
		sla := maxStageSLA(c.Stage)
		limit := time.Duration(sla) * time.Hour
		if limit <= 0 {
			limit = time.Duration(opt.StageSlaWarnDays) * 24 * time.Hour
		}
		if dwell > limit {
			add("contract.stage-dwell", SeverityWarn,
				"阶段停留超期",
				fmt.Sprintf("契约 %s 在 %s 阶段已停留 %.1f 天（该阶段 SLA %.1f 天）", c.Title, c.Stage, dwell.Hours()/24, limit.Hours()/24),
				"催办当前节点责任人，或按回退通道打回上一阶段并留痕",
				fmt.Sprintf("stage=%s dwellHours=%.1f slaHours=%.1f", c.Stage, dwell.Hours(), limit.Hours()))
		}
	}

	// 4) 合规分不足
	if score := ComplianceScore(c); score < opt.ComplianceMin {
		add("contract.compliance-low", SeverityWarn,
			"契约合规分不足",
			fmt.Sprintf("契约 %s 合规分 %.1f，低于下限 %.0f；缺失项：%s", c.Title, score, opt.ComplianceMin, strings.Join(limitStrings(MissingDuties(c), 3), "；")),
			"按缺失项逐条补齐权责与要素后再推进阶段",
			fmt.Sprintf("compliance=%.1f min=%.0f missing=%d", score, opt.ComplianceMin, len(MissingDuties(c))))
	}

	// 5) 履约偏差
	if c.Stage == StagePerforming || stageReached(c.Stage, StagePerforming) {
		var bad []string
		if c.DeliveryOnTime > 0 && c.DeliveryOnTime < opt.DeliveryMin {
			bad = append(bad, fmt.Sprintf("按期交付率 %.0f%%", c.DeliveryOnTime*100))
		}
		if c.PaymentOnTime > 0 && c.PaymentOnTime < opt.DeliveryMin {
			bad = append(bad, fmt.Sprintf("按期付款率 %.0f%%", c.PaymentOnTime*100))
		}
		if c.QualityScore > 0 && c.QualityScore < 60 {
			bad = append(bad, fmt.Sprintf("质量评分 %.0f", c.QualityScore))
		}
		if c.Disputes > 0 {
			bad = append(bad, fmt.Sprintf("争议 %d 次", c.Disputes))
		}
		if len(bad) > 0 {
			sev := SeverityWarn
			if len(bad) >= 2 || c.Disputes >= 2 {
				sev = SeverityCritical
			}
			add("contract.perf-deviation", sev,
				"履约指标偏差",
				fmt.Sprintf("契约 %s 履约偏差：%s", c.Title, strings.Join(bad, "、")),
				"要求相对方提交整改计划，同步下调该契约的体量承载评级",
				"deliveryOnTime="+fmt.Sprintf("%.2f paymentOnTime=%.2f quality=%.0f disputes=%d",
					c.DeliveryOnTime, c.PaymentOnTime, c.QualityScore, c.Disputes))
		}
	}
	return out
}

// maxStageSLA 返回该阶段权责表中最长 SLA（小时）。
func maxStageSLA(s ContractStage) int {
	max := 0
	for _, d := range LifecycleDuties() {
		if d.Stage == s && d.SLAHours > max {
			max = d.SLAHours
		}
	}
	return max
}

func limitStrings(ss []string, n int) []string {
	if len(ss) > n {
		return append(append([]string{}, ss[:n]...), fmt.Sprintf("等 %d 项", len(ss)))
	}
	return ss
}

// VolumeAlerts 把指标评估转为体量预警（只报红与黄，绿不产生预警）。
func VolumeAlerts(evals []Eval, at string) []Alert {
	var out []Alert
	for _, e := range evals {
		if e.Level != LevelWarn && e.Level != LevelRed {
			continue
		}
		sev := SeverityWarn
		if e.Level == LevelRed {
			sev = SeverityCritical
		}
		out = append(out, Alert{
			ID:     "V-" + e.Key + "-" + e.Level,
			Kind:   "volume.metric-" + e.Level,
			Severity: sev, Scope: ScopeVolume, Subject: e.Key,
			Title:  fmt.Sprintf("%s %s", DomainName(e.Domain), e.Name),
			Detail: fmt.Sprintf("%s 当前 %.2f%s，警示线 %.2f，红线 %.2f（占用率 %.0f%%）",
				e.Name, e.Value, e.Unit, e.Warn, e.Red, e.Ratio*100),
			Suggestion: volumeSuggestion(e),
			Evidence:   fmt.Sprintf("metric=%s value=%.2f warn=%.2f red=%.2f level=%s", e.Key, e.Value, e.Warn, e.Red, e.Level),
			At:         at,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity == SeverityCritical
		}
		return out[i].Subject < out[j].Subject
	})
	return out
}

func volumeSuggestion(e Eval) string {
	if e.Level == LevelRed {
		return "立即执行自适应调整（降负载或扩容），并在 24 小时内复测；复测仍红则暂停新增任务"
	}
	return "进入观察窗口：连续 2 个采样周期走同一方向即升级为红区处置"
}

// MergeAlerts 合并多组预警并去重（按 ID），红排在前面。
func MergeAlerts(groups ...[]Alert) []Alert {
	seen := map[string]bool{}
	var out []Alert
	for _, g := range groups {
		for _, a := range g {
			if seen[a.ID] {
				continue
			}
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if ri != rj {
			return ri > rj
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarn:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// CountSeverity 统计各级预警数量。
func CountSeverity(alerts []Alert) map[string]int {
	out := map[string]int{SeverityCritical: 0, SeverityWarn: 0, SeverityInfo: 0}
	for _, a := range alerts {
		out[a.Severity]++
	}
	return out
}

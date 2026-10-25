package gov

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// TimeLayout 是治理记录统一使用的天数日期格式。
const TimeLayout = "2006-01-02"

// ParseDay 解析 YYYY-MM-DD（空串返回零值 + false）。
func ParseDay(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(TimeLayout, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// ParseRFC3339 解析 RFC3339 时间（空串返回零值 + false）。
func ParseRFC3339(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// CanTransition 判定阶段迁移是否合法，返回 (是否合法, 归因)。
// 规则：只能前进到紧邻阶段；允许回退到紧邻前一阶段（审核打回/履约异常），归档是终态。
func CanTransition(from, to ContractStage) (bool, string) {
	if from == to {
		return false, "阶段未变化"
	}
	stages := AllStages()
	fi, ti := -1, -1
	for i, st := range stages {
		if st == from {
			fi = i
		}
		if st == to {
			ti = i
		}
	}
	if fi < 0 {
		return false, "未知的当前阶段: " + string(from)
	}
	if ti < 0 {
		return false, "未知的目标阶段: " + string(to)
	}
	if from == StageArchived {
		return false, "归档为终态，需人工开启新版本契约"
	}
	if ti == fi+1 {
		return true, "前进"
	}
	if ti == fi-1 {
		return true, "回退（需留痕说明）"
	}
	return false, fmt.Sprintf("不允许跨阶段迁移 %s → %s", from, to)
}

// Transition 执行一次阶段迁移：校验合法性、补时间戳、追加留痕。
func Transition(c Contract, to ContractStage, actor, at, note string) (Contract, error) {
	ok, why := CanTransition(c.Stage, to)
	if !ok {
		return c, fmt.Errorf("契约 %s 迁移被拒: %s", c.ID, why)
	}
	if strings.TrimSpace(actor) == "" {
		return c, fmt.Errorf("契约 %s 迁移被拒: 必须填写操作人（权责留痕）", c.ID)
	}
	if why == "回退（需留痕说明）" && strings.TrimSpace(note) == "" {
		return c, fmt.Errorf("契约 %s 回退被拒: 回退必须填写原因", c.ID)
	}
	ev := StageEvent{From: c.Stage, To: to, Actor: actor, At: at, Note: note}
	if at == "" {
		ev.At = time.Now().Format(time.RFC3339)
	}
	if ev.Note == "" {
		ev.Note = why
	}
	c.Stage = to
	c.Trail = append(c.Trail, ev)
	c.UpdatedAt = ev.At
	switch to {
	case StageSigned:
		if c.SignedAt == "" {
			c.SignedAt = ev.At
		}
	case StageArchived:
		if c.ArchivedAt == "" {
			c.ArchivedAt = ev.At
		}
	}
	return c, nil
}

// roleFilled 判断某角色是否已指派。
func roleFilled(c Contract, r Role) bool {
	switch r {
	case RoleOwner:
		return strings.TrimSpace(c.Owner) != ""
	case RoleDrafter:
		return strings.TrimSpace(c.Drafter) != ""
	case RoleReviewer:
		return strings.TrimSpace(c.Reviewer) != ""
	case RoleSigner:
		return strings.TrimSpace(c.Signer) != ""
	case RolePerfMonitor:
		return strings.TrimSpace(c.PerfMonitor) != ""
	case RoleArchivist:
		return strings.TrimSpace(c.Archivist) != ""
	case RoleCompliance:
		return strings.TrimSpace(c.ReviewNote) != ""
	}
	return false
}

// MissingDuties 返回该契约在权责表上缺失的必需项（用于合规门禁）。
func MissingDuties(c Contract) []string {
	var out []string
	for _, d := range LifecycleDuties() {
		if !d.Mandatory {
			continue
		}
		if !stageReached(c.Stage, d.Stage) {
			continue // 尚未到达该阶段，不算缺失
		}
		if !roleFilled(c, d.Role) {
			out = append(out, fmt.Sprintf("%s 阶段缺少 %s：%s", d.Stage, d.Role, d.Duty))
		}
	}
	if strings.TrimSpace(c.Title) == "" {
		out = append(out, "缺少契约名称")
	}
	if c.Amount <= 0 {
		out = append(out, "缺少契约金额（金额上限校验依赖它）")
	}
	if _, ok := ParseDay(c.ExpireAt); !ok {
		out = append(out, "缺少有效到期日（预警与归档依赖它）")
	}
	if len(c.Deliverables) == 0 {
		out = append(out, "缺少交付物清单（履约核对依赖它）")
	}
	if len(c.Clauses) == 0 {
		out = append(out, "缺少条款清单（条款风险识别依赖它）")
	}
	return out
}

// stageReached 判断当前阶段是否已达到（或越过）目标阶段。
func stageReached(cur, target ContractStage) bool {
	stages := AllStages()
	ci, ti := -1, -1
	for i, st := range stages {
		if st == cur {
			ci = i
		}
		if st == target {
			ti = i
		}
	}
	return ci >= 0 && ti >= 0 && ci >= ti
}

// ComplianceScore 计算契约合规分（0-100），四段加权 + 风险扣分。
// 权重：权责完整 30 / 要素齐全 30 / 审核质量 20 / 留痕完整 20，再按条款与履约风险扣分。
func ComplianceScore(c Contract) float64 {
	score := 0.0
	// 1) 权责完整（30）：必需角色齐备比例
	total, filled := 0, 0
	for _, d := range LifecycleDuties() {
		if !d.Mandatory || !stageReached(c.Stage, d.Stage) {
			continue
		}
		total++
		if roleFilled(c, d.Role) {
			filled++
		}
	}
	if total > 0 {
		score += 30 * float64(filled) / float64(total)
	} else {
		score += 30
	}
	// 2) 要素齐全（30）：六个必备要素各 5 分
	elem := 0
	if strings.TrimSpace(c.Title) != "" {
		elem++
	}
	if c.Amount > 0 {
		elem++
	}
	if _, ok := ParseDay(c.ExpireAt); ok {
		elem++
	}
	if len(c.Deliverables) > 0 {
		elem++
	}
	if len(c.Clauses) > 0 {
		elem++
	}
	if strings.TrimSpace(c.Counterpart) != "" {
		elem++
	}
	score += 5 * float64(elem)
	// 3) 审核质量（20）：审核人 + 审核意见 + 合规抽查留痕
	rev := 0
	if strings.TrimSpace(c.Reviewer) != "" {
		rev++
	}
	if strings.TrimSpace(c.ReviewNote) != "" {
		rev++
	}
	if hasComplianceTrail(c) {
		rev++
	}
	score += 20 * float64(rev) / 3
	// 4) 留痕完整（20）：Trail 覆盖已到达阶段的迁移
	score += 20 * trailCoverage(c)
	// 扣分项：高风险未签条款 / 争议 / 质量低 / 履约偏差
	score -= deductions(c)
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return round2(score)
}

func hasComplianceTrail(c Contract) bool {
	for _, ev := range c.Trail {
		if strings.Contains(ev.Actor, string(RoleCompliance)) || strings.Contains(ev.Note, "合规") {
			return true
		}
	}
	return false
}

// trailCoverage 返回迁移留痕覆盖率 0-1：每条 trail 的 to 阶段都算一次覆盖。
func trailCoverage(c Contract) float64 {
	if len(c.Trail) == 0 {
		if c.Stage == StageDraft {
			return 1 // 起草阶段尚无迁移，视为覆盖完成
		}
		return 0
	}
	want := 0
	for _, st := range AllStages() {
		if stageReached(c.Stage, st) && st != StageDraft {
			want++
		}
	}
	if want == 0 {
		return 1
	}
	seen := map[ContractStage]bool{}
	for _, ev := range c.Trail {
		seen[ev.To] = true
	}
	got := 0
	for _, st := range AllStages() {
		if st == StageDraft {
			continue
		}
		if stageReached(c.Stage, st) && seen[st] {
			got++
		}
	}
	v := float64(got) / float64(want)
	if v > 1 {
		v = 1
	}
	return v
}

func deductions(c Contract) float64 {
	d := 0.0
	for _, cl := range c.Clauses {
		if cl.RiskLevel >= 3 && !cl.Signed {
			d += 4
		}
	}
	if c.Disputes > 0 {
		d += float64(c.Disputes) * 5
	}
	if c.QualityScore > 0 && c.QualityScore < 60 {
		d += 10
	}
	if c.DeliveryOnTime > 0 && c.DeliveryOnTime < 0.8 {
		d += 8
	}
	if c.PaymentOnTime > 0 && c.PaymentOnTime < 0.8 {
		d += 6
	}
	return d
}

// round2 保留两位小数。
//
// 旧实现 float64(int64(v*100+0.5)) / 100 是「向正无穷舍入」，负数会算错方向：
// -1234.5 → int64(-123449.5) = -123449 → -1234.49（应为 -1234.5）。
// 本包里 pearson() 返回的相关系数取值域是 [-1, 1]，负相关是真正常态
// （面板「负载×耗时相关」直接显示它），所以这不是理论问题。
// 2026-10-22 由 metric_text_test.go 的千分位用例抓到。
func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*100) / 100
}

// StageDwell 返回当前阶段停留时长；now 之前无时间依据返回 0。
func StageDwell(c Contract, now time.Time) time.Duration {
	last := time.Time{}
	if len(c.Trail) > 0 {
		if t, ok := ParseRFC3339(c.Trail[len(c.Trail)-1].At); ok {
			last = t
		}
	}
	if last.IsZero() {
		if t, ok := ParseRFC3339(c.UpdatedAt); ok {
			last = t
		}
	}
	if last.IsZero() || now.Before(last) {
		return 0
	}
	return now.Sub(last)
}

// SortContracts 按到期日升序、同日期按金额降序排序（面板与预警共用口径）。
func SortContracts(cs []Contract) {
	sort.SliceStable(cs, func(i, j int) bool {
		di, oki := ParseDay(cs[i].ExpireAt)
		dj, okj := ParseDay(cs[j].ExpireAt)
		if oki != okj {
			return oki // 有到期日的排前面
		}
		if oki && !di.Equal(dj) {
			return di.Before(dj)
		}
		return cs[i].Amount > cs[j].Amount
	})
}

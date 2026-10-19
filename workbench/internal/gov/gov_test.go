package gov

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------- 生命周期状态机 ----------

func TestLifecycleTransitions(t *testing.T) {
	c := Contract{ID: "C1", Title: "测试契约", Owner: "owner-a", Stage: StageDraft}
	// 跨阶段迁移必须被拒
	if ok, _ := CanTransition(StageDraft, StagePerforming); ok {
		t.Fatal("draft→performing 应被拒绝（禁跨阶段）")
	}
	// 归档终态不可再迁
	if ok, _ := CanTransition(StageArchived, StagePerforming); ok {
		t.Fatal("archived 为终态，不应允许迁出")
	}
	// 正常前进
	nc, err := Transition(c, StageReview, "reviewer-a", "2026-10-18T10:00:00Z", "提交审核")
	if err != nil {
		t.Fatalf("draft→review 失败: %v", err)
	}
	if nc.Stage != StageReview || len(nc.Trail) != 1 || nc.Trail[0].From != StageDraft {
		t.Fatalf("迁移留痕异常: %+v", nc.Trail)
	}
	// 操作人必填（权责留痕）
	if _, err := Transition(nc, StageSigned, "", "", ""); err == nil {
		t.Fatal("缺少操作人应被拒绝")
	}
	// 回退必须有原因
	if _, err := Transition(nc, StageDraft, "owner-a", "", ""); err == nil {
		t.Fatal("回退未填原因应被拒绝")
	}
	if _, err := Transition(nc, StageDraft, "owner-a", "", "条款返工"); err != nil {
		t.Fatalf("回退带原因应通过: %v", err)
	}
}

func TestTransitionSetsTimestamps(t *testing.T) {
	c := Contract{ID: "C2", Stage: StageReview, Owner: "o", Reviewer: "r", Title: "t", Amount: 100,
		ExpireAt: "2027-01-01", Deliverables: []string{"d"}, Clauses: []Clause{{ID: "x", Title: "x", RiskLevel: 1}}}
	c, err := Transition(c, StageSigned, "signer-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SignedAt == "" {
		t.Fatal("签署阶段应自动补 SignedAt")
	}
	c, err = Transition(c, StagePerforming, "pm-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err = Transition(c, StageArchived, "arch-a", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ArchivedAt == "" {
		t.Fatal("归档应自动补 ArchivedAt")
	}
}

func TestComplianceScoreRanges(t *testing.T) {
	// 空契约：要素缺失、权责缺失 ⇒ 分应很低
	empty := Contract{ID: "E", Stage: StageReview}
	low := ComplianceScore(empty)
	if low >= 50 {
		t.Fatalf("空契约合规分应低，实得 %.1f", low)
	}
	// 完备契约：应显著更高
	full := Contract{
		ID: "F", Title: "完备契约", Counterpart: "相对方", Stage: StagePerforming,
		Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p", Archivist: "a",
		Amount: 100000, ExpireAt: "2027-06-30", Deliverables: []string{"交付物A"},
		Clauses:        []Clause{{ID: "c1", Title: "付款", RiskLevel: 2, Signed: true}},
		ReviewNote:     "法务已审",
		DeliveryOnTime: 0.95, PaymentOnTime: 0.95, QualityScore: 88,
		Trail: []StageEvent{
			{From: StageDraft, To: StageReview, Actor: "d", At: "2026-09-01T00:00:00Z"},
			{From: StageReview, To: StageSigned, Actor: "s", At: "2026-09-02T00:00:00Z"},
			{From: StageSigned, To: StagePerforming, Actor: "p", At: "2026-09-03T00:00:00Z"},
		},
	}
	high := ComplianceScore(full)
	if high < 80 {
		t.Fatalf("完备契约合规分应 ≥80，实得 %.1f（缺失=%v）", high, MissingDuties(full))
	}
	if high <= low {
		t.Fatalf("完备契约分数应高于空契约: %.1f vs %.1f", high, low)
	}
}

func TestMissingDuties(t *testing.T) {
	c := Contract{ID: "M", Stage: StageSigned, Title: "t"}
	missing := MissingDuties(c)
	if len(missing) == 0 {
		t.Fatal("缺要素契约应有缺失项")
	}
	found := false
	for _, m := range missing {
		if containsAny(m, "金额") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应报缺金额，实际: %v", missing)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && (indexOf(s, sub) >= 0) {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// ---------- 体量指标体系 ----------

func TestEvaluateThreeDomains(t *testing.T) {
	b := DefaultBudget()
	good := Sample{TasksDaily: 10, ContractsActive: 5, ContractAmount: 1000, OutputBytes: 1 << 20,
		MemMB: 50, Handles: 100, Goroutines: 20, Concurrency: 2, EventsBytes: 1 << 20,
		Backlog: 3, ThroughputDaily: 30, AvgCycleSec: 60, FailureRate: 0.01}
	evals := Evaluate(good, b)
	if len(evals) != 13 {
		t.Fatalf("应有 13 项指标，实得 %d", len(evals))
	}
	domains := map[Domain]int{}
	for _, e := range evals {
		domains[e.Domain]++
		if e.Level != LevelOK {
			t.Fatalf("健康样本应全绿，%s=%s", e.Key, e.Level)
		}
	}
	if domains[DomainBusiness] == 0 || domains[DomainResource] == 0 || domains[DomainOperation] == 0 {
		t.Fatalf("三域必须都有指标: %+v", domains)
	}
	// 红样本
	bad := Sample{TasksDaily: 999, ContractsActive: 999, ContractAmount: 9e9, OutputBytes: 99 << 30,
		MemMB: 9999, Handles: 99999, Goroutines: 99999, Concurrency: 99, EventsBytes: 999 << 20,
		Backlog: 999, ThroughputDaily: 0, AvgCycleSec: 99999, FailureRate: 0.9}
	badEvals := Evaluate(bad, b)
	for _, e := range badEvals {
		if e.Level != LevelRed {
			t.Fatalf("%s 在超限样本下应为红，实得 %s", e.Key, e.Level)
		}
	}
	sums := Summarize(badEvals)
	for _, d := range sums {
		if d.Level != LevelRed {
			t.Fatalf("超限样本下 %s 域应为红，实得 %s", d.Name, d.Level)
		}
	}
}

func TestZeroActivityIsUnverifiedNotRed(t *testing.T) {
	b := DefaultBudget()
	// 全新环境：无任务、无积压 ⇒ 吞吐 0 应记 unverified（不是"产能不足"红）
	evals := Evaluate(Sample{}, b)
	for _, e := range evals {
		if e.Key == "throughputDaily" && e.Level != LevelUnknown {
			t.Fatalf("零活动态吞吐应为 unverified，实得 %s", e.Level)
		}
	}
	// 有任务但吞吐 0 ⇒ 必须判红（引擎在跑却不出活）
	evals2 := Evaluate(Sample{TasksDaily: 10}, b)
	for _, e := range evals2 {
		if e.Key == "throughputDaily" && e.Level != LevelRed {
			t.Fatalf("有任务但零吞吐应判红，实得 %s", e.Level)
		}
	}
}

func TestBudgetNormalizeAndCustom(t *testing.T) {
	var b Budget
	b.Normalize()
	if b.MemLimitMB <= 0 || b.EventsBytesMax <= 0 || b.FailureRateMax <= 0 {
		t.Fatalf("Normalize 未补齐阈值: %+v", b)
	}
	// 自定义阈值必须生效（防「改了没生效」）
	b2 := DefaultBudget()
	b2.MemLimitMB = 100
	s := Sample{MemMB: 120}
	ev := Evaluate(s, b2)
	var found bool
	for _, e := range ev {
		if e.Key == "memMB" {
			found = true
			if e.Level != LevelRed {
				t.Fatalf("自定义内存阈值 100MB 下 120MB 应为红，实得 %s", e.Level)
			}
		}
	}
	if !found {
		t.Fatal("缺少 memMB 指标")
	}
}

func TestHysteresisAdjust(t *testing.T) {
	b := DefaultBudget()
	// 连续 2 次红 ⇒ escalate
	red := Sample{TasksDaily: 500, ContractsActive: 500}
	hist := []Sample{red, red}
	adj := Adjust(hist, b)
	if len(adj) == 0 {
		t.Fatal("连续 2 个红样本应产出调整建议")
	}
	kind := adj[0].Kind
	if kind != "escalate" {
		t.Fatalf("首个建议应为 escalate，实得 %s", kind)
	}
	// 单次红不应触发（滞回）
	if got := Adjust([]Sample{red}, b); len(got) != 0 {
		t.Fatalf("单次红不应触发调整（滞回），实得 %d 条", len(got))
	}
	// 连续 3 次绿且占用低 ⇒ relax（低占用 = 各项远低于阈值，且吞吐达标）
	green := Sample{TasksDaily: 2, ContractsActive: 1, ContractAmount: 1000, OutputBytes: 1 << 20,
		MemMB: 20, Handles: 50, Goroutines: 10, Concurrency: 1, EventsBytes: 1 << 20,
		Backlog: 1, ThroughputDaily: 60, AvgCycleSec: 30, FailureRate: 0.0}
	relaxHist := []Sample{green, green, green}
	adj2 := Adjust(relaxHist, b)
	var hasRelax bool
	for _, a := range adj2 {
		if a.Kind == "relax" {
			hasRelax = true
		}
	}
	if !hasRelax {
		t.Fatal("连续 3 个低占用绿样本应产出 relax 建议")
	}
	// 步长上限
	for _, a := range adj {
		if a.Confidence > 1 || a.Confidence < 0 {
			t.Fatalf("置信度越界: %f", a.Confidence)
		}
	}
}

// ---------- 预警 ----------

func TestContractAlertsHighRiskAndOverdue(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	c := Contract{
		ID: "A1", Title: "高风险契约", Stage: StagePerforming,
		Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p",
		Amount: 100, ExpireAt: "2026-10-10", Deliverables: []string{"x"},
		Clauses:  []Clause{{ID: "c", Title: "违约金", RiskLevel: 3, Signed: false, Amount: 50}},
		Disputes: 2, DeliveryOnTime: 0.5,
	}
	alerts := ContractAlerts(c, DefaultAlertOptions(now))
	kinds := map[string]bool{}
	for _, a := range alerts {
		kinds[a.Kind] = true
		if a.Evidence == "" {
			t.Fatalf("预警缺少可复核证据: %+v", a)
		}
		if a.Suggestion == "" {
			t.Fatalf("预警缺少处置建议: %+v", a)
		}
	}
	for _, want := range []string{"contract.clause-risk", "contract.expired-unarchived", "contract.perf-deviation"} {
		if !kinds[want] {
			t.Fatalf("缺少预警 %s，实际: %+v", want, kinds)
		}
	}
	// 临期（未来 10 天）应命中 expiring
	c2 := Contract{ID: "A2", Title: "临期", Stage: StagePerforming, ExpireAt: "2026-10-28",
		Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p", Amount: 1,
		Deliverables: []string{"x"}, Clauses: []Clause{{ID: "c", Title: "c", RiskLevel: 1, Signed: true}},
		ReviewNote: "ok", DeliveryOnTime: 1, PaymentOnTime: 1, QualityScore: 90,
		Trail: []StageEvent{{From: StageDraft, To: StageReview, Actor: "d", At: "2026-10-01T00:00:00Z"},
			{From: StageReview, To: StageSigned, Actor: "s", At: "2026-10-02T00:00:00Z"},
			{From: StageSigned, To: StagePerforming, Actor: "p", At: "2026-10-03T00:00:00Z"}}}
	a2 := ContractAlerts(c2, DefaultAlertOptions(now))
	var hasExpiring bool
	for _, a := range a2 {
		if a.Kind == "contract.expiring" {
			hasExpiring = true
			if a.At == "" {
				t.Fatal("预警缺少时间戳")
			}
		}
	}
	if !hasExpiring {
		t.Fatalf("10 天后到期应命中临期预警，实际: %+v", a2)
	}
}

func TestMergeAlertsOrderAndDedup(t *testing.T) {
	a := []Alert{{ID: "1", Severity: SeverityWarn}, {ID: "2", Severity: SeverityCritical}}
	b := []Alert{{ID: "2", Severity: SeverityCritical}, {ID: "3", Severity: SeverityInfo}}
	got := MergeAlerts(a, b)
	if len(got) != 3 {
		t.Fatalf("去重后应 3 条，实得 %d", len(got))
	}
	if got[0].Severity != SeverityCritical {
		t.Fatalf("红预警应排最前，实得 %+v", got[0])
	}
	cnt := CountSeverity(got)
	if cnt[SeverityCritical] != 1 || cnt[SeverityWarn] != 1 || cnt[SeverityInfo] != 1 {
		t.Fatalf("计数错误: %+v", cnt)
	}
}

// ---------- 联动 ----------

func TestPearson(t *testing.T) {
	x := []float64{1, 2, 3, 4, 5}
	y := []float64{2, 4, 6, 8, 10}
	if got := pearson(x, y); math.Abs(got-1) > 1e-9 {
		t.Fatalf("完全线性相关应为 1，实得 %f", got)
	}
	if got := pearson([]float64{1}, []float64{1}); got != 0 {
		t.Fatalf("单点样本相关系数应为 0，实得 %f", got)
	}
}

func TestBuildLinkageSignalsAndAllocations(t *testing.T) {
	now := "2026-10-18T12:00:00Z"
	cs := []Contract{
		{ID: "L1", Title: "大额", Stage: StagePerforming, Amount: 900000,
			Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p", Archivist: "a",
			ExpireAt: "2027-01-01", Deliverables: []string{"x"},
			Clauses: []Clause{{ID: "c", Title: "c", RiskLevel: 1, Signed: true}}, ReviewNote: "ok",
			DeliveryOnTime: 0.5, QualityScore: 50,
			Trail: []StageEvent{{From: StageDraft, To: StageReview, Actor: "d", At: "2026-09-01T00:00:00Z"},
				{From: StageReview, To: StageSigned, Actor: "s", At: "2026-09-02T00:00:00Z"},
				{From: StageSigned, To: StagePerforming, Actor: "p", At: "2026-09-03T00:00:00Z"}}},
		{ID: "L2", Title: "小额", Stage: StageSigned, Amount: 100000,
			Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p", Archivist: "a",
			ExpireAt: "2027-02-01", Deliverables: []string{"x"},
			Clauses: []Clause{{ID: "c", Title: "c", RiskLevel: 1, Signed: true}}, ReviewNote: "ok",
			DeliveryOnTime: 1, PaymentOnTime: 1, QualityScore: 90,
			Trail: []StageEvent{{From: StageDraft, To: StageReview, Actor: "d", At: "2026-09-01T00:00:00Z"},
				{From: StageReview, To: StageSigned, Actor: "s", At: "2026-09-02T00:00:00Z"}}},
	}
	// 契约负载上升 + 耗时上升 ⇒ 应命中 LK-1
	var samples []Sample
	for i := 0; i < 6; i++ {
		samples = append(samples, Sample{At: int64(1000 + i), ContractsActive: 95 + i, AvgCycleSec: float64(100 + i*50), FailureRate: 0.01 * float64(i)})
	}
	// 契约上限压到 2 ⇒ 2 份在履约契约即满载（测 LK-1 阈值逻辑本身）
	b := DefaultBudget()
	b.ActiveContractMax = 2
	rep := BuildLinkage(cs, samples, b, 12, now)
	if rep.ContractsActive != 2 {
		t.Fatalf("在履约契约应为 2，实得 %d", rep.ContractsActive)
	}
	if len(rep.Allocations) != 2 {
		t.Fatalf("分摊应覆盖 2 份契约，实得 %d", len(rep.Allocations))
	}
	sumShare := 0.0
	for _, a := range rep.Allocations {
		sumShare += a.Share
		if a.Rating == "" || a.Suggestion == "" {
			t.Fatalf("分摊缺少评级/建议: %+v", a)
		}
	}
	if math.Abs(sumShare-1) > 0.02 {
		t.Fatalf("份额之和应≈1，实得 %.3f", sumShare)
	}
	var hitLK1 bool
	for _, s := range rep.Signals {
		if s.ID == "LK-1" {
			hitLK1 = true
			if s.Decision == "" || s.Detail == "" {
				t.Fatalf("联动信号缺少决策/明细: %+v", s)
			}
		}
	}
	if !hitLK1 {
		t.Fatalf("高负载+高相关应命中 LK-1，实际信号: %+v", rep.Signals)
	}
	if rep.CorrCycle <= 0.6 {
		t.Fatalf("耗时相关系数应 >0.6，实得 %.2f", rep.CorrCycle)
	}
	// 无采样 ⇒ 数据缺口必须显式声明，不得静默 PASS
	rep2 := BuildLinkage(cs, nil, b, 12, now)
	if len(rep2.DataGaps) == 0 {
		t.Fatal("无采样时必须登记数据缺口")
	}
	if rep2.Level == LevelOK {
		t.Fatalf("无采样不应判 ok，实得 %s", rep2.Level)
	}
}

// ---------- 存储 ----------

func TestStoreContractRoundTripAndStateMachine(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "governance"))
	if err != nil {
		t.Fatal(err)
	}
	c := Contract{ID: "S1", Title: "存储契约", Stage: StageDraft, Owner: "o", Amount: 1000,
		Counterpart: "cp", ExpireAt: "2027-03-01", Deliverables: []string{"d"},
		Clauses: []Clause{{ID: "c", Title: "c", RiskLevel: 1}}}
	if _, err := st.UpsertContract(c); err != nil {
		t.Fatal(err)
	}
	// 绕过状态机直接改阶段必须被拒
	c.Stage = StageSigned
	if _, err := st.UpsertContract(c); err == nil {
		t.Fatal("直接改写阶段应被拒绝")
	}
	// 走状态机
	if _, err := st.TransitionContract("S1", StageReview, "r", "提交审核", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.TransitionContract("S1", StageSigned, "s", "", ""); err != nil {
		t.Fatal(err)
	}
	// 重新打开：数据必须还在（持久化验证）
	st2, err := Open(filepath.Join(dir, "governance"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok := st2.Contract("S1")
	if !ok {
		t.Fatal("重新打开后契约丢失")
	}
	if got.Stage != StageSigned || len(got.Trail) != 2 {
		t.Fatalf("重新打开后阶段/留痕不符: stage=%s trail=%d", got.Stage, len(got.Trail))
	}
	// 归档后写归档流水
	if _, err := st2.TransitionContract("S1", StagePerforming, "p", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st2.TransitionContract("S1", StageArchived, "a", "结项", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "governance", fileArchive)); err != nil {
		t.Fatalf("归档流水未落盘: %v", err)
	}
}

func TestStoreSamplesAndBudget(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := st.AddSample(Sample{At: int64(1000 + i), TasksDaily: i}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(st.Samples()); got != 5 {
		t.Fatalf("样本数应为 5，实得 %d", got)
	}
	last, ok := st.LatestSample()
	if !ok || last.TasksDaily != 4 {
		t.Fatalf("最新样本不符: %+v", last)
	}
	// 重开验证 JSONL 持久化
	st2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(st2.Samples()); got != 5 {
		t.Fatalf("重开后样本数应为 5，实得 %d", got)
	}
	// 阈值覆盖必须落盘并回读
	b := DefaultBudget()
	b.MemLimitMB = 123
	if got, err := st2.SetBudget(b); err != nil || got.MemLimitMB != 123 {
		t.Fatalf("SetBudget 失败: %+v err=%v", got, err)
	}
	st3, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st3.Budget().MemLimitMB; got != 123 {
		t.Fatalf("阈值覆盖未持久化，实得 %d", got)
	}
}

func TestSummaryLevelsAndChecklist(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 无采样 ⇒ unverified，且检查项 K4 必须 FAIL（不许静默 PASS）
	sum := st.Summary(time.Now())
	if sum.Level != LevelUnknown {
		t.Fatalf("无采样总等级应为 unverified，实得 %s", sum.Level)
	}
	if len(sum.DataGaps) == 0 {
		t.Fatal("无采样必须登记数据缺口")
	}
	var k4 *CheckItem
	for i := range sum.Compliance.Items {
		if sum.Compliance.Items[i].Code == "K4" {
			k4 = &sum.Compliance.Items[i]
		}
	}
	// 无采样 ⇒ K4 必须是 unverified（既不 PASS 也不 FAIL），且总数里有 unverified 计数
	if k4 == nil || k4.Pass {
		t.Fatalf("无采样时 K4 必须不 PASS（记 unverified），实得 %+v", k4)
	}
	if sum.Compliance.Unverified == 0 {
		t.Fatalf("无采样时 unverified 计数应 > 0，实得 %+v", sum.Compliance)
	}
	// 契约逾期 ⇒ 总等级红
	c := Contract{ID: "X", Title: "逾期契约", Stage: StagePerforming, Amount: 1, ExpireAt: "2020-01-01",
		Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p",
		Deliverables: []string{"d"}, Clauses: []Clause{{ID: "c", Title: "c", RiskLevel: 1, Signed: true}}}
	if _, err := st.UpsertContract(c); err != nil {
		t.Fatal(err)
	}
	_ = st.AddSample(Sample{At: time.Now().UnixMilli(), MemMB: 10, Handles: 10, Goroutines: 10})
	sum2 := st.Summary(time.Now())
	if sum2.Contracts.Overdue != 1 {
		t.Fatalf("逾期契约计数应为 1，实得 %d", sum2.Contracts.Overdue)
	}
	if sum2.Level != LevelRed {
		t.Fatalf("存在逾期契约时总等级应为红，实得 %s", sum2.Level)
	}
}

func TestFormatSummaryContainsAllSections(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.AddSample(Sample{At: time.Now().UnixMilli(), TasksDaily: 1, MemMB: 1, Handles: 1, Goroutines: 1, ThroughputDaily: 1})
	out := FormatSummary(st.Summary(time.Now()))
	for _, want := range []string{"治理等级", "体量", "预警", "自适应调整", "联动", "考核校验"} {
		if !containsAny(out, want) {
			t.Fatalf("CLI 摘要缺少段落 %q\\n%s", want, out)
		}
	}
}

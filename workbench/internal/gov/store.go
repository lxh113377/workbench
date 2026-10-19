package gov

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"workbench/internal/config"
)

// 治理记录的固定文件名（统一放在 <DataRoot>/governance/ 下）。
const (
	fileContracts = "contracts.json"
	fileSamples   = "samples.jsonl"
	fileAlerts    = "alerts.json"
	fileAdjust    = "adjustments.json"
	fileLinkage   = "linkage.json"
	fileArchive   = "archive.jsonl"
	fileBudget    = "budget.json"
)

// Store 负责治理数据的持久化：契约 + 体量样本 + 预警 + 联动报告 + 归档流水。
// 写入策略沿用全项目约定：结构化快照原子写 JSON，事件流追加 JSONL。
type Store struct {
	dir string

	mu        sync.RWMutex
	contracts []Contract
	samples   []Sample
	alerts    []Alert
	adjust    []Adjustment
	linkage   LinkageReport
	budget    Budget
	loadedAt  string
}

// Dir 返回治理数据目录。
func (s *Store) Dir() string { return s.dir }

// Open 打开（或初始化）治理数据目录。
// 目录不存在时自动创建；契约快照缺失时以空集启动，不报错（首次运行是正常态）。
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("治理数据目录为空")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建治理数据目录失败: %w", err)
	}
	s := &Store{dir: dir, budget: DefaultBudget(), loadedAt: time.Now().Format(time.RFC3339)}
	var cs []Contract
	if err := readJSONFile(filepath.Join(dir, fileContracts), &cs); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("读取契约快照失败: %w", err)
		}
	}
	s.contracts = cs
	// 阈值覆盖：存在则以盘上为准（面板可在线调整并留痕）
	var bud Budget
	if err := readJSONFile(filepath.Join(dir, fileBudget), &bud); err == nil && bud != (Budget{}) {
		bud.Normalize()
		s.budget = bud
	}
	if err := s.loadSamplesLocked(); err != nil {
		return nil, err
	}
	var al []Alert
	if err := readJSONFile(filepath.Join(dir, fileAlerts), &al); err == nil {
		s.alerts = al
	}
	var ad []Adjustment
	if err := readJSONFile(filepath.Join(dir, fileAdjust), &ad); err == nil {
		s.adjust = ad
	}
	var lr LinkageReport
	if err := readJSONFile(filepath.Join(dir, fileLinkage), &lr); err == nil {
		s.linkage = lr
	}
	return s, nil
}

func (s *Store) loadSamplesLocked() error {
	path := filepath.Join(s.dir, fileSamples)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("打开体量样本失败: %w", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var out []Sample
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v Sample
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			continue // 单行损坏不毁整份历史，跳过并继续（数据面可容忍）
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("读取体量样本失败: %w", err)
	}
	// 只保留最近 maxSamples 条，防止无限膨胀（体量治理自身的体量纪律）。
	const maxSamples = 5000
	if len(out) > maxSamples {
		out = out[len(out)-maxSamples:]
	}
	s.samples = out
	return nil
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// Contracts 返回契约副本。
func (s *Store) Contracts() []Contract {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Contract, len(s.contracts))
	copy(out, s.contracts)
	return out
}

// Contract 按 ID 取单份契约。
func (s *Store) Contract(id string) (Contract, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.contracts {
		if c.ID == id {
			return c, true
		}
	}
	return Contract{}, false
}

// UpsertContract 新增或更新契约并落盘；返回落盘后的记录。
func (s *Store) UpsertContract(c Contract) (Contract, error) {
	if strings.TrimSpace(c.ID) == "" {
		return c, fmt.Errorf("契约 ID 不能为空")
	}
	if c.Trail == nil {
		c.Trail = []StageEvent{}
	}
	if c.Clauses == nil {
		c.Clauses = []Clause{}
	}
	if c.Deliverables == nil {
		c.Deliverables = []string{}
	}
	if c.Milestones == nil {
		c.Milestones = []Milestone{}
	}
	if c.Currency == "" {
		c.Currency = "CNY"
	}
	if c.Stage == "" {
		c.Stage = StageDraft
	}
	c.UpdatedAt = time.Now().Format(time.RFC3339)
	s.mu.Lock()
	defer s.mu.Unlock()
	replaced := false
	for i := range s.contracts {
		if s.contracts[i].ID == c.ID {
			// 已存在的契约不允许绕过状态机改阶段
			if s.contracts[i].Stage != c.Stage {
				return c, fmt.Errorf("不允许直接改写阶段（%s → %s），请走 transition 通道", s.contracts[i].Stage, c.Stage)
			}
			s.contracts[i] = c
			replaced = true
			break
		}
	}
	if !replaced {
		s.contracts = append(s.contracts, c)
	}
	return c, s.persistContractsLocked()
}

// TransitionContract 通过状态机迁移契约阶段并落盘。
func (s *Store) TransitionContract(id string, to ContractStage, actor, note, at string) (Contract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.contracts {
		if s.contracts[i].ID != id {
			continue
		}
		nc, err := Transition(s.contracts[i], to, actor, at, note)
		if err != nil {
			return s.contracts[i], err
		}
		s.contracts[i] = nc
		if err := s.persistContractsLocked(); err != nil {
			return nc, err
		}
		if to == StageArchived {
			_ = s.appendArchiveLocked(archiveEntry{At: time.Now().Format(time.RFC3339), ContractID: id, Action: "archived", Compliance: ComplianceScore(nc)})
		}
		return nc, nil
	}
	return Contract{}, fmt.Errorf("契约不存在: %s", id)
}

// DeleteContract 删除契约（归档留痕，物理上从快照移除）。
func (s *Store) DeleteContract(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.contracts {
		if s.contracts[i].ID == id {
			s.contracts = append(s.contracts[:i], s.contracts[i+1:]...)
			_ = s.appendArchiveLocked(archiveEntry{At: time.Now().Format(time.RFC3339), ContractID: id, Action: "deleted"})
			return s.persistContractsLocked()
		}
	}
	return fmt.Errorf("契约不存在: %s", id)
}

type archiveEntry struct {
	At         string  `json:"at"`
	ContractID string  `json:"contractId"`
	Action     string  `json:"action"`
	Compliance float64 `json:"compliance,omitempty"`
}

func (s *Store) appendArchiveLocked(e archiveEntry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, fileArchive), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

func (s *Store) persistContractsLocked() error {
	// 契约按到期日排序后落盘，保证快照可读性与面板顺序一致。
	SortContracts(s.contracts)
	return config.WriteJSONAtomic(filepath.Join(s.dir, fileContracts), s.contracts)
}

// AddSample 追加一条体量采样并落 JSONL。
func (s *Store) AddSample(v Sample) error {
	if v.At <= 0 {
		v.At = time.Now().UnixMilli()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(s.dir, fileSamples), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("写体量样本失败: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	s.samples = append(s.samples, v)
	const maxSamples = 5000
	if len(s.samples) > maxSamples {
		s.samples = s.samples[len(s.samples)-maxSamples:]
	}
	return nil
}

// Samples 返回体量采样副本（默认最近 200 条）。
func (s *Store) Samples() []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Sample, len(s.samples))
	copy(out, s.samples)
	if len(out) > 200 {
		out = out[len(out)-200:]
	}
	return out
}

// LatestSample 返回最后一条采样。
func (s *Store) LatestSample() (Sample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.samples) == 0 {
		return Sample{}, false
	}
	return s.samples[len(s.samples)-1], true
}

// Budget 返回当前阈值。
func (s *Store) Budget() Budget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.budget
}

// SetBudget 覆盖阈值并归一化落盘（返回生效值，调用方必须回显）。
func (s *Store) SetBudget(b Budget) (Budget, error) {
	b.Normalize()
	s.mu.Lock()
	s.budget = b
	s.mu.Unlock()
	return b, config.WriteJSONAtomic(filepath.Join(s.dir, fileBudget), b)
}

// SaveAnalysis 落盘预警与调整建议（快照原子写）。
func (s *Store) SaveAnalysis(alerts []Alert, adjust []Adjustment, lr LinkageReport) error {
	s.mu.Lock()
	s.alerts = alerts
	s.adjust = adjust
	s.linkage = lr
	s.mu.Unlock()
	if err := config.WriteJSONAtomic(filepath.Join(s.dir, fileAlerts), alerts); err != nil {
		return err
	}
	if err := config.WriteJSONAtomic(filepath.Join(s.dir, fileAdjust), adjust); err != nil {
		return err
	}
	return config.WriteJSONAtomic(filepath.Join(s.dir, fileLinkage), lr)
}

// Alerts 返回最近一次预警快照。
func (s *Store) Alerts() []Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Alert, len(s.alerts))
	copy(out, s.alerts)
	return out
}

// Adjustments 返回最近一次调整建议。
func (s *Store) Adjustments() []Adjustment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Adjustment, len(s.adjust))
	copy(out, s.adjust)
	return out
}

// Linkage 返回最近一次联动报告。
func (s *Store) Linkage() LinkageReport {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.linkage
}

// Summary 生成面板用的治理总览。
func (s *Store) Summary(now time.Time) Summary {
	s.mu.RLock()
	contracts := append([]Contract(nil), s.contracts...)
	samples := append([]Sample(nil), s.samples...)
	b := s.budget
	alerts := append([]Alert(nil), s.alerts...)
	adjust := append([]Adjustment(nil), s.adjust...)
	s.mu.RUnlock()
	return BuildSummary(contracts, samples, b, alerts, adjust, now)
}

// Summary 是治理总览（面板 / CLI / e2e 验收共用同一结构）。
type Summary struct {
	GeneratedAt  string          `json:"generatedAt"`
	Contracts    ContractStats   `json:"contracts"`
	StageBuckets []StageBucket   `json:"stageBuckets"`
	Volume       VolumeStats     `json:"volume"`
	Domains      []DomainSummary `json:"domains"`
	Evals        []Eval          `json:"evals"`
	Alerts       AlertStats      `json:"alerts"`
	Adjustments  []Adjustment    `json:"adjustments"`
	Linkage      LinkageReport   `json:"linkage"`
	Level        string          `json:"level"`
	Compliance   Checklist       `json:"checklist"`
	DataGaps     []string        `json:"dataGaps"`
}

// ContractStats 是契约聚合统计。
type ContractStats struct {
	Total         int     `json:"total"`
	Draft         int     `json:"draft"`
	Review        int     `json:"review"`
	Signed        int     `json:"signed"`
	Performing    int     `json:"performing"`
	Archived      int     `json:"archived"`
	Active        int     `json:"active"` // 签署 + 履约
	AmountTotal   float64 `json:"amountTotal"`
	AmountActive  float64 `json:"amountActive"`
	ComplianceAvg float64 `json:"complianceAvg"`
	ComplianceLow int     `json:"complianceLow"` // 低于 70 分的份数
	ExpiringSoon  int     `json:"expiringSoon"`
	Overdue       int     `json:"overdue"`
}

// StageBucket 是阶段分布（面板画柱状图用）。
type StageBucket struct {
	Stage ContractStage `json:"stage"`
	Name  string        `json:"name"`
	Count int           `json:"count"`
}

// VolumeStats 是体量聚合统计。
type VolumeStats struct {
	Samples         int     `json:"samples"`
	LatestAt        string  `json:"latestAt"`
	Level           string  `json:"level"`
	TasksDaily      int     `json:"tasksDaily"`
	ContractsActive int     `json:"contractsActive"`
	MemMB           float64 `json:"memMB"`
	Handles         int     `json:"handles"`
	OutputBytes     int64   `json:"outputBytes"`
}

// AlertStats 是预警聚合。
type AlertStats struct {
	Total    int     `json:"total"`
	Critical int     `json:"critical"`
	Warn     int     `json:"warn"`
	Info     int     `json:"info"`
	Top      []Alert `json:"top"`
}

// Checklist 是考核校验结果（要求 4：配套考核校验标准）。
type Checklist struct {
	Items []CheckItem `json:"items"`
	Pass  int         `json:"pass"`
	// Unverified 是"取不到数"的项数：按项目铁律「零数据不记 PASS」，
	// 这类项既不计 PASS 也不计 FAIL，但必须显式可见，否则会退化成
	// "没数据 = 不合格"的误报。
	Unverified int    `json:"unverified"`
	Fail       int    `json:"fail"`
	Level      string `json:"level"`
}

// CheckItem 是单项考核校验。
type CheckItem struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Target   string `json:"target"`
	Actual   string `json:"actual"`
	Pass     bool   `json:"pass"`
	Evidence string `json:"evidence"`
}

func stageName(s ContractStage) string {
	switch s {
	case StageDraft:
		return "起草"
	case StageReview:
		return "审核"
	case StageSigned:
		return "签署"
	case StagePerforming:
		return "履约监控"
	case StageArchived:
		return "归档"
	}
	return string(s)
}

// BuildSummary 汇总全部治理数据（纯函数，便于测试与 CLI/HTTP 复用）。
func BuildSummary(contracts []Contract, samples []Sample, b Budget, alerts []Alert, adjust []Adjustment, now time.Time) Summary {
	b.Normalize()
	sum := Summary{GeneratedAt: now.Format(time.RFC3339), Adjustments: adjust}
	sum.Contracts = ContractStats{Total: len(contracts)}
	counts := map[ContractStage]int{}
	for _, c := range contracts {
		counts[c.Stage]++
		sum.Contracts.AmountTotal += c.Amount
		if activeContract(c) {
			sum.Contracts.Active++
			sum.Contracts.AmountActive += c.Amount
		}
		if score := ComplianceScore(c); score < 70 {
			sum.Contracts.ComplianceLow++
		}
		if exp, ok := ParseDay(c.ExpireAt); ok && c.Stage != StageArchived {
			days := int(exp.Sub(now).Hours() / 24)
			if days < 0 {
				sum.Contracts.Overdue++
			} else if days <= 30 {
				sum.Contracts.ExpiringSoon++
			}
		}
		if c.Stage == StagePerforming {
			sum.Contracts.Performing++
		}
	}
	compSum := 0.0
	for _, c := range contracts {
		compSum += ComplianceScore(c)
	}
	if len(contracts) > 0 {
		sum.Contracts.ComplianceAvg = round2(compSum / float64(len(contracts)))
	}
	for _, st := range AllStages() {
		sum.StageBuckets = append(sum.StageBuckets, StageBucket{Stage: st, Name: stageName(st), Count: counts[st]})
	}
	// 体量
	sum.DataGaps = []string{}
	if len(samples) == 0 {
		sum.Volume.Level = LevelUnknown
		sum.DataGaps = append(sum.DataGaps, "无体量采样 ⇒ 体量域结论为 unverified（不得记 PASS）")
	} else {
		last := samples[len(samples)-1]
		sum.Volume = VolumeStats{
			Samples: len(samples), LatestAt: time.UnixMilli(last.At).Format(time.RFC3339),
			TasksDaily: last.TasksDaily, ContractsActive: last.ContractsActive,
			MemMB: round2(last.MemMB), Handles: last.Handles, OutputBytes: last.OutputBytes,
		}
		sum.Evals = Evaluate(last, b)
		sum.Domains = Summarize(sum.Evals)
		sum.Volume.Level = LevelOK
		for _, d := range sum.Domains {
			if d.Level == LevelRed {
				sum.Volume.Level = LevelRed
				break
			}
			if d.Level == LevelWarn {
				sum.Volume.Level = LevelWarn
			}
		}
	}
	// 预警
	sum.Alerts = AlertStats{Total: len(alerts)}
	cnt := CountSeverity(alerts)
	sum.Alerts.Critical, sum.Alerts.Warn, sum.Alerts.Info = cnt[SeverityCritical], cnt[SeverityWarn], cnt[SeverityInfo]
	if len(alerts) > 5 {
		sum.Alerts.Top = alerts[:5]
	} else {
		sum.Alerts.Top = alerts
	}
	// 联动
	sum.Linkage = BuildLinkage(contracts, samples, b, 12, sum.GeneratedAt)
	// 总等级
	sum.Level = LevelOK
	if len(sum.DataGaps) > 0 {
		sum.Level = LevelUnknown
	}
	if sum.Contracts.Overdue > 0 || sum.Alerts.Critical > 0 {
		sum.Level = LevelRed
	} else if sum.Alerts.Warn > 0 || sum.Volume.Level == LevelWarn || sum.Linkage.Level == LevelWarn {
		if sum.Level != LevelRed {
			sum.Level = LevelWarn
		}
	}
	// 考核校验（要求 4）：考核结果必须反向影响总等级，否则会出现
	// "总等级绿 + 考核 2 项 FAIL"的自相矛盾读数。
	sum.Compliance = BuildChecklist(sum)
	if sum.Compliance.Fail >= 3 {
		sum.Level = LevelRed
	} else if sum.Compliance.Fail > 0 && sum.Level == LevelOK {
		sum.Level = LevelWarn
	}
	return sum
}

// BuildChecklist 依据要求 4 生成可机器校验的考核项。
func BuildChecklist(sum Summary) Checklist {
	ck := Checklist{}
	// add 记录一项「已判定」的考核（pass/fail）。
	add := func(code, name, target, actual string, pass bool, evidence string) {
		ck.Items = append(ck.Items, CheckItem{Code: code, Name: name, Target: target, Actual: actual, Pass: pass, Evidence: evidence})
		if pass {
			ck.Pass++
		} else {
			ck.Fail++
		}
	}
	// addUnknown 记录一项「取不到数」的考核：不计 PASS 也不计 FAIL。
	//
	// 为什么必须存在：体量域无采样时 K4 的数据链路根本无从判定，若草率记 FAIL，
	// 就会把"还没跑"读成"跑坏了"；这与总验收的 unverified 纪律同源。
	addUnknown := func(code, name, target, actual, evidence string) {
		ck.Items = append(ck.Items, CheckItem{Code: code, Name: name, Target: target, Actual: actual, Pass: false, Evidence: evidence})
		ck.Unverified++
	}
	// K1 契约要素齐全率
	elemOK := 0
	for _, c := range sum.Alerts.Top {
		_ = c
	}
	_ = elemOK
	add("K1", "契约权责与要素齐全", "契约数 > 0 且 合规均分 ≥ 70",
		fmt.Sprintf("契约 %d 份 / 均分 %.1f", sum.Contracts.Total, sum.Contracts.ComplianceAvg),
		sum.Contracts.Total > 0 && sum.Contracts.ComplianceAvg >= 70,
		fmt.Sprintf("complianceAvg=%.2f low=%d total=%d", sum.Contracts.ComplianceAvg, sum.Contracts.ComplianceLow, sum.Contracts.Total))
	// K2 到期治理
	add("K2", "到期/逾期契约治理", "逾期 = 0",
		fmt.Sprintf("逾期 %d / 临期 %d", sum.Contracts.Overdue, sum.Contracts.ExpiringSoon), sum.Contracts.Overdue == 0,
		fmt.Sprintf("overdue=%d expiringSoon=%d", sum.Contracts.Overdue, sum.Contracts.ExpiringSoon))
	// K3 体量三域无红
	reds := 0
	for _, d := range sum.Domains {
		if d.Level == LevelRed {
			reds++
		}
	}
	add("K3", "体量三域无红区", "红区指标 = 0",
		fmt.Sprintf("红区 %d 项", reds), len(sum.Domains) > 0 && reds == 0,
		fmt.Sprintf("domains=%d redDomains=%d level=%s", len(sum.Domains), reds, sum.Volume.Level))
	// K4 数据链路完整（有采样才能联动）
	if sum.Volume.Samples == 0 {
		addUnknown("K4", "契约×体量数据链路可用", "采样 ≥ 3 点", "0 点", "samples=0 gaps="+intText(len(sum.DataGaps))+" ⇒ 取不到数，记 unverified")
	} else {
		add("K4", "契约×体量数据链路可用", "采样 ≥ 3 点",
			fmt.Sprintf("%d 点", sum.Volume.Samples), sum.Volume.Samples >= 3,
			fmt.Sprintf("samples=%d gaps=%d", sum.Volume.Samples, len(sum.DataGaps)))
	}
	// K5 自适应调整闭环
	openEsc := 0
	for _, a := range sum.Adjustments {
		if a.Kind == "escalate" {
			openEsc++
		}
	}
	add("K5", "自适应调整有落点", "escalate 建议均已登记处置",
		fmt.Sprintf("待处置 %d 条", openEsc), openEsc == 0,
		fmt.Sprintf("escalate=%d totalAdjust=%d", openEsc, len(sum.Adjustments)))
	// K6 预警闭环
	add("K6", "风险预警闭环", "critical = 0",
		fmt.Sprintf("critical %d", sum.Alerts.Critical), sum.Alerts.Critical == 0,
		fmt.Sprintf("critical=%d warn=%d total=%d", sum.Alerts.Critical, sum.Alerts.Warn, sum.Alerts.Total))
	ck.Level = LevelOK
	if ck.Fail > 0 {
		ck.Level = LevelWarn
	}
	if ck.Fail >= 3 {
		ck.Level = LevelRed
	}
	// 全部项都取不到数 ⇒ 整体结论为 unverified（不得显示绿色）。
	if ck.Fail == 0 && ck.Unverified > 0 && ck.Pass == 0 {
		ck.Level = LevelUnknown
	}
	return ck
}

// Recompute 重算预警 / 调整 / 联动并落盘（调度循环与 HTTP 触发共用）。
func (s *Store) Recompute(now time.Time) ([]Alert, []Adjustment, LinkageReport) {
	s.mu.Lock()
	contracts := append([]Contract(nil), s.contracts...)
	samples := append([]Sample(nil), s.samples...)
	b := s.budget
	s.mu.Unlock()

	opt := DefaultAlertOptions(now)
	var contractAlerts []Alert
	for _, c := range contracts {
		contractAlerts = append(contractAlerts, ContractAlerts(c, opt)...)
	}
	var volumeAlerts []Alert
	var evals []Eval
	if len(samples) > 0 {
		evals = Evaluate(samples[len(samples)-1], b)
		volumeAlerts = VolumeAlerts(evals, now.Format(time.RFC3339))
	}
	adjust := Adjust(samples, b)
	lr := BuildLinkage(contracts, samples, b, 12, now.Format(time.RFC3339))
	linkAlerts := LinkageToAlerts(lr)
	all := MergeAlerts(contractAlerts, volumeAlerts, linkAlerts)
	_ = evals
	_ = s.SaveAnalysis(all, adjust, lr)
	return all, adjust, lr
}

// SortedByStage 返回按阶段分组的契约（面板展示顺序）。
func SortedByStage(cs []Contract) []Contract {
	out := append([]Contract(nil), cs...)
	idx := map[ContractStage]int{}
	for i, st := range AllStages() {
		idx[st] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		if idx[out[i].Stage] != idx[out[j].Stage] {
			return idx[out[i].Stage] < idx[out[j].Stage]
		}
		di, oki := ParseDay(out[i].ExpireAt)
		dj, okj := ParseDay(out[j].ExpireAt)
		if oki && okj && !di.Equal(dj) {
			return di.Before(dj)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// intText 把整数转成十进制字符串（避免为此引入 strconv 到展示路径）。
func intText(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte(48 + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = 45
	}
	return string(b[i:])
}

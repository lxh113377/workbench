// Package gov 实现工作台的「契约治理 × 体量治理」双线治理内核。
//
// 设计目标（对应四条治理要求）：
//  1. 契约全生命周期：起草 → 审核 → 签署 → 履约监控 → 归档，五阶段状态机 +
//     节点权责（RACI 角色）+ 合规评分 + 风险预警（条款风险 / 到期 / 履约偏差）。
//  2. 体量治理指标体系：业务体量 / 资源承载体量 / 运营规模体量 三域量化指标 +
//     阈值（warn/red）+ 滞回（避免抖动）+ 自适应调整建议。
//  3. 数据互通链路：体量样本 → 契约履约数据联动（每份在履约契约按体量占比分摊风险），
//     产出协同决策信号（CORR 序号），供面板与考核使用。
//  4. 落地执行：Adjustment（自适应调整）/ Alert（预警）/ Checklist（考核校验项）
//     三类可执行产物，全部带证据字段，可机器校验。
//
// 本包只做纯计算与状态迁移，不直接做磁盘 IO 之外的副作用；持久化由 store.go 承担。
package gov


// ContractStage 是契约生命周期阶段。
type ContractStage string

const (
	StageDraft     ContractStage = "draft"     // 起草
	StageReview    ContractStage = "review"    // 审核
	StageSigned    ContractStage = "signed"    // 签署
	StagePerforming ContractStage = "performing" // 履约监控
	StageArchived  ContractStage = "archived"  // 归档
)

// AllStages 返回生命周期阶段的固定顺序（用于遍历与校验）。
func AllStages() []ContractStage {
	return []ContractStage{StageDraft, StageReview, StageSigned, StagePerforming, StageArchived}
}

// StageWeight 是各阶段在契约健康分中的权重（履约阶段权重最高，因为风险最集中）。
func StageWeight(s ContractStage) float64 {
	switch s {
	case StageDraft:
		return 0.10
	case StageReview:
		return 0.15
	case StageSigned:
		return 0.20
	case StagePerforming:
		return 0.40
	case StageArchived:
		return 0.15
	default:
		return 0
	}
}

// Role 是契约节点的权责角色（RACI）。
type Role string

const (
	RoleOwner        Role = "owner"        // 负责人：结果问责（A）
	RoleDrafter      Role = "drafter"      // 起草人
	RoleReviewer     Role = "reviewer"     // 审核人
	RoleSigner       Role = "signer"       // 签署/审批人
	RolePerfMonitor  Role = "perf-monitor" // 履约监控人
	RoleArchivist    Role = "archivist"    // 归档人
	RoleCompliance   Role = "compliance"   // 合规官
)

// NodeDuty 描述一个生命周期节点的权责定义（规范表，供文档与校验共用）。
type NodeDuty struct {
	Stage     ContractStage `json:"stage"`
	Role      Role          `json:"role"`
	Duty      string        `json:"duty"`
	SLAHours  int           `json:"slaHours"`  // 该节点最长处理时长（0 = 不设限）
	Mandatory bool          `json:"mandatory"` // 缺失是否判该契约不合规
}

// LifecycleDuties 返回五阶段的标准权责表（唯一真相源：文档与代码共用本表）。
func LifecycleDuties() []NodeDuty {
	return []NodeDuty{
		{StageDraft, RoleDrafter, "按模板起草：标的/金额/期限/交付物/违约责任五要素齐全", 48, true},
		{StageDraft, RoleOwner, "确认契约标的与业务口径，指派审核人", 24, true},
		{StageReview, RoleReviewer, "法务/业务双审：条款风险、金额上限、体量承载能力", 72, true},
		{StageReview, RoleCompliance, "合规校验：审批留痕、权限匹配、关联方披露", 48, true},
		{StageSigned, RoleSigner, "签署审批：与审核结论一致方可签署", 24, true},
		{StageSigned, RoleOwner, "签署登记：归档编号、生效日期、履约节点计划", 24, true},
		{StagePerforming, RolePerfMonitor, "履约监控：按里程碑核对交付物与付款，偏差超阈值即预警", 0, true},
		{StagePerforming, RoleCompliance, "履约合规抽查：不少于每季度一次并留痕", 0, false},
		{StageArchived, RoleArchivist, "归档：正文 + 全部审批留痕 + 履约证据链完整入库", 120, true},
		{StageArchived, RoleOwner, "归档确认：结项结论与遗留风险移交", 72, true},
	}
}

// Clause 是契约条款（用于风险识别）。
type Clause struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	RiskLevel int     `json:"riskLevel"` // 0 无 / 1 低 / 2 中 / 3 高
	Amount    float64 `json:"amount"`    // 该条款涉及金额（元）
	Deadline  string  `json:"deadline"`  // 关键期限 YYYY-MM-DD（可空）
	Signed    bool    `json:"signed"`    // 是否已签署确认
}

// Contract 是一份契约的全生命周期记录。
type Contract struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Counterpart string       `json:"counterpart"` // 相对方
	Stage      ContractStage `json:"stage"`
	Owner      string        `json:"owner"`
	Drafter    string        `json:"drafter"`
	Reviewer   string        `json:"reviewer"`
	Signer     string        `json:"signer"`
	PerfMonitor string       `json:"perfMonitor"`
	Archivist  string        `json:"archivist"`
	Amount     float64       `json:"amount"`    // 契约金额（元）
	Currency   string        `json:"currency"`
	EffectiveAt string       `json:"effectiveAt"` // YYYY-MM-DD 生效
	ExpireAt    string       `json:"expireAt"`    // YYYY-MM-DD 到期
	Deliverables []string    `json:"deliverables"`
	Clauses    []Clause      `json:"clauses"`
	Milestones []Milestone   `json:"milestones"`
	// 履约监控指标
	DeliveryOnTime float64 `json:"deliveryOnTime"` // 按期交付率 0-1
	PaymentOnTime  float64 `json:"paymentOnTime"`  // 按期付款率 0-1
	QualityScore   float64 `json:"qualityScore"`   // 质量评分 0-100
	Disputes       int     `json:"disputes"`        // 争议次数
	ReviewNote     string  `json:"reviewNote"`
	SignedAt       string  `json:"signedAt"`   // 实际签署时间 RFC3339（空 = 未签）
	ArchivedAt     string  `json:"archivedAt"` // 归档时间 RFC3339（空 = 未归档）
	UpdatedAt      string  `json:"updatedAt"`
	// 生命周期留痕：每次 stage 迁移写一条，归档审计可直接用。
	Trail []StageEvent `json:"trail"`
}

// StageEvent 是一次阶段迁移留痕。
type StageEvent struct {
	From  ContractStage `json:"from"`
	To    ContractStage `json:"to"`
	Actor string        `json:"actor"`
	At    string        `json:"at"`
	Note  string        `json:"note"`
}

// Milestone 是履约里程碑。
type Milestone struct {
	Name    string `json:"name"`
	DueDate string `json:"dueDate"`
	Done    bool   `json:"done"`
	DoneAt  string `json:"doneAt"`
}

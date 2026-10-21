// Package model 定义工作台的领域类型。
// 修改字段时必须同步考虑 schemaVersion 迁移。
package model

import (
	"strings"
	"time"
	"unicode/utf8"
)

// TaskState 是任务状态机中的合法状态。
type TaskState string

const (
	StateCandidate   TaskState = "candidate"   // 已生成待评估
	StateEvaluating  TaskState = "evaluating"  // 评估中
	StateRejected    TaskState = "rejected"    // 评估未过阈值
	StateApproved    TaskState = "approved"    // 通过评估，等待入队
	StateQueued      TaskState = "queued"      // 队列中
	StateRunning     TaskState = "running"     // 执行中
	StateDone        TaskState = "done"        // 成功完成
	StateFailed      TaskState = "failed"      // 重试耗尽失败
	StateTimeout     TaskState = "timeout"     // 超时
	StateCancelled   TaskState = "cancelled"   // 用户终止
	StateInterrupted TaskState = "interrupted" // 进程异常退出导致的悬挂
	// StateDegraded 表示任务已产出但**未通过真实执行的验收**（截断、不能运行、
	// 缺 verify 等）。它是有产出的终态：保留内容供人工查看，但不计入"今日产出"、
	// 不触发后继链 —— 修复改造前"截断也算成功"的虚报。
	StateDegraded TaskState = "degraded"
)

// IsTerminal 判断状态是否终态（终态任务不再被调度器选中）。
func (s TaskState) IsTerminal() bool {
	switch s {
	case StateRejected, StateDone, StateFailed, StateCancelled, StateDegraded:
		return true
	}
	return false
}

// IsActive 判断状态是否属于“占用执行槽”的活跃态。
func (s TaskState) IsActive() bool {
	return s == StateRunning || s == StateEvaluating
}

// TransitionAllowed 集中校验状态迁移合法性，非法迁移在 store 层被拒绝。
func TransitionAllowed(from, to TaskState) bool {
	allowed := map[TaskState][]TaskState{
		StateCandidate:  {StateEvaluating, StateRejected, StateApproved, StateCancelled},
		StateEvaluating: {StateApproved, StateRejected, StateFailed, StateCancelled},
		StateApproved:   {StateQueued, StateCancelled, StateRejected},
		StateQueued:     {StateRunning, StateCancelled, StateRejected},
		// StateRunning -> StateQueued：环境性退回（模型池全冷却 / 凭据暂缺）。
		// 这不是失败：任务已被调度器取出、还没有真正开始产出，把它放回队列等
		// 环境恢复是正确的语义。改造前引擎绕过状态机直接改字段（store.Update），
		// 导致审计流水与真实状态不一致、且没有护栏拦得住热循环；补上这条合法迁移后
		// 退回动作会留下 transition 审计，Attempts 也不计入失败。
		StateRunning:     {StateDone, StateFailed, StateTimeout, StateCancelled, StateInterrupted, StateQueued, StateDegraded},
		// StateDegraded -> StateQueued：返工通道（验收失败后带着报错重做）。
		StateDegraded: {StateQueued, StateCancelled, StateRejected},
		// StateInterrupted -> StateFailed：崩溃救援额度耗尽时的判死出口（L4）。
		// 没有这条通道时，"一跑就弄死进程"的任务会被无限重新入队
		// （KeepAlive 每分钟拉起 → 恢复 → 再崩），7×24 无人值守下永不停歇。
		StateInterrupted: {StateQueued, StateFailed, StateCancelled, StateRejected},
		StateTimeout:     {StateQueued, StateFailed, StateCancelled},
		StateFailed:      {StateQueued, StateRejected, StateCancelled},
		// StateRejected 以前是"只进不出"的孤岛：面板上的"重试被拒任务"
		// 会因 rejected -> queued 非法而必然失败（引擎 RetryTask 的允许集里包含 rejected，
		// 但状态机不允许，两者矛盾）。这里补上人工重开通道。
		StateRejected: {StateQueued, StateCandidate, StateCancelled},
	}
	for _, candidate := range allowed[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// Weights 是评估权重配置。
type Weights struct {
	Value       float64 `json:"value"`
	Cost        float64 `json:"cost"`
	Feasibility float64 `json:"feasibility"`
}

// Normalized 返回归一化权重（防御配置被写成非法值）。
func (w Weights) Normalized() Weights {
	v, c, f := w.Value, w.Cost, w.Feasibility
	if v < 0 {
		v = 0
	}
	if c < 0 {
		c = 0
	}
	if f < 0 {
		f = 0
	}
	sum := v + c + f
	if sum <= 0 {
		return Weights{Value: 0.5, Cost: 0.2, Feasibility: 0.3}
	}
	return Weights{Value: v / sum, Cost: c / sum, Feasibility: f / sum}
}

// EvalScores 是一次三维评估的结果。
type EvalScores struct {
	Value       float64  `json:"value"`       // 0-100 产出价值
	Cost        float64  `json:"cost"`        // 0-100 资源消耗（越低越好）
	Feasibility float64  `json:"feasibility"` // 0-100 可行性
	Total       float64  `json:"total"`       // 加权综合分
	Semantic    bool     `json:"semantic"`    // 语义层是否参与
	Degraded    bool     `json:"degraded"`    // 语义层降级
	Reason      string   `json:"reason"`
	Reasons     []string `json:"reasons,omitempty"`
	Gates       []string `json:"gates,omitempty"` // 命中的硬门禁
	Threshold   float64  `json:"threshold"`
	Weights     Weights  `json:"weights"`
	At          int64    `json:"at"`
}

// Template 是任务模板。模板是任务的“先验”，决定任务如何生成与如何被评估。
type Template struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Domain          string         `json:"domain"` // content/code/knowledge/data/solution/report
	Description     string         `json:"description"`
	PromptTemplate  string         `json:"promptTemplate"`
	Params          TemplateParams `json:"params"`
	OutputSpec      OutputSpec     `json:"outputSpec"`
	PriorityWeight  int            `json:"priorityWeight"` // 1-5
	ValuePrior      float64        `json:"valuePrior"`     // 0-100 价值先验
	CostPrior       float64        `json:"costPrior"`      // 0-100 成本先验
	CooldownMinutes int            `json:"cooldownMinutes"`
	DiversityGroup  string         `json:"diversityGroup"`
	Chain           ChainSpec      `json:"chain"`
	Enabled         bool           `json:"enabled"`
	Builtin         bool           `json:"builtin"`
	CreatedAt       int64          `json:"createdAt"`
	UpdatedAt       int64          `json:"updatedAt"`
}

// TemplateParams 描述模板执行参数。
type TemplateParams struct {
	ModelTier    string   `json:"modelTier"`    // quality/balanced/fast
	MaxMinutes   int      `json:"maxMinutes"`   // 单任务超时（分钟）
	OutputFormat string   `json:"outputFormat"` // md/txt/docx
	WebAllowed   bool     `json:"webAllowed"`
	Tags         []string `json:"tags"`
}

// OutputSpec 描述产出约束，同时作为自评的评分依据。
type OutputSpec struct {
	Format   string   `json:"format"`
	Sections []string `json:"sections"`
	MinWords int      `json:"minWords"`
	MinBytes int      `json:"minBytes"`
}

// ChainSpec 描述任务链的延伸规则。
type ChainSpec struct {
	SuccessorTemplates []string `json:"successorTemplates"`
	MaxDepth           int      `json:"maxDepth"`
	Fanout             int      `json:"fanout"`
}

// Task 是一个可执行任务实例。
type Task struct {
	// WorkRoot 是 external（用户目录）模式下该任务的真实工作目录。
	// 项目模式下留空，执行器在数据根的沙箱里跑。
	WorkRoot string `json:"workRoot,omitempty"`
	ID           string    `json:"id"`
	ChainID      string    `json:"chainId"`
	SessionID    string    `json:"sessionId"`
	TemplateID   string    `json:"templateId"`
	Title        string    `json:"title"`
	Prompt       string    `json:"prompt"`
	Domain       string    `json:"domain"`
	Tags         []string  `json:"tags"`
	Priority     int       `json:"priority"`
	State        TaskState `json:"state"`
	Source       string    `json:"source"`           // template/chain/synthesis/manual/autonomy
	GoalID       string    `json:"goalId,omitempty"` // 所属自主目标（仅 source=autonomy 时有值）
	ParentID     string    `json:"parentId"`
	ChainDepth   int       `json:"chainDepth"`
	Attempts     int       `json:"attempts"`
	// CrashRescues 是「崩溃救援」累计轮次（L4）。不能复用 Attempts：
	// Attempts 是单次 runOne 内的重试序号（engine.go 每轮直接赋值 t.Attempts = attempt），
	// 崩溃恢复后重跑会被重置覆盖 —— 拿它当崩溃预算等于没有预算。
	CrashRescues int       `json:"crashRescues,omitempty"`
	MaxAttempts  int       `json:"maxAttempts"`
	Model        string    `json:"model"`
	OutputFormat string    `json:"outputFormat"`
	TimeoutMin   int       `json:"timeoutMin"`

	Scores   *EvalScores `json:"scores,omitempty"`
	Critique *Critique   `json:"critique,omitempty"`
	Rating   int         `json:"rating"`
	Pinned   bool        `json:"pinned"`

	// Verify 是真实执行的验收报告（P3）。Verified 为 true 才计入"今日产出"。
	Verify   *VerifyReport `json:"verify,omitempty"`
	Verified bool          `json:"verified"`
	// RepairRounds 记录已返工轮数（验收失败回灌重做）。
	RepairRounds int `json:"repairRounds,omitempty"`
	// PlanRounds 记录已"重新规划"轮数（方案层面错了，重出方案再执行）。
	//
	// 与 RepairRounds 分开记的原因（这是双角色闭环的核心取舍）：
	//   RepairRounds = 方案不变，执行模型带着报错重做（便宜，先用）；
	//   PlanRounds   = 认定方案错了，重新出方案（贵，后用）。
	// 合成一个计数器就无法实现"返工优先、重规划兜底"的策略，
	// 也无法在面板上说清"这个任务到底卡在实现还是卡在方案"。
	PlanRounds int `json:"planRounds,omitempty"`
	// PlanDegraded 表示最近一轮是"退化直执行"（方案层不可用）。
	// 面板据此区分"方案层真的跑过"与"方案层当时挂了"。
	PlanDegraded bool `json:"planDegraded,omitempty"`
	// PlanSummary 是最近一轮方案的一行摘要（面板展示，不必翻 plans/ 目录）。
	PlanSummary string `json:"planSummary,omitempty"`
	// PlanModel 是最近一轮出方案的模型 id（审计：思考模型与执行模型是否真的是两个）。
	PlanModel string `json:"planModel,omitempty"`

	CreatedAt  int64 `json:"createdAt"`
	UpdatedAt  int64 `json:"updatedAt"`
	StartedAt  int64 `json:"startedAt"`
	FinishedAt int64 `json:"finishedAt"`
	ElapsedMs  int64 `json:"elapsedMs"`

	OutputDir   string `json:"outputDir"`
	OutputFile  string `json:"outputFile"`
	OutputBytes int64  `json:"outputBytes"`
	Error       string `json:"error"`
	ErrorKind   string `json:"errorKind"`
	Summary     string `json:"summary"`
	TokenIn     int    `json:"tokenIn"`
	TokenOut    int    `json:"tokenOut"`
	ToolCalls   int    `json:"toolCalls"`
}

// VerifyReport 是一条产出的真实验收报告（由 internal/verify 产生，此处为持久化镜像）。
//
// 放在 model 包而不是直接复用 verify.Report，是为了避免 model ↔ verify 的循环依赖：
// verify 依赖 artifact，engine 同时依赖两者，持久化类型归 model 最干净。
type VerifyReport struct {
	Round      int            `json:"round"`
	Pass       bool           `json:"pass"`
	FailReason string         `json:"failReason,omitempty"`
	Outcomes   []VerifyOutcome `json:"outcomes,omitempty"`
	At         int64          `json:"at"`
}

// VerifyOutcome 是单条验收命令的结果。
type VerifyOutcome struct {
	Cmd      string `json:"cmd"`
	Exit     int    `json:"exit"`
	Expect   int    `json:"expectExit"`
	Pass     bool   `json:"pass"`
	TimedOut bool   `json:"timedOut,omitempty"`
	Ms       int64  `json:"ms"`
	Stdout   string `json:"stdoutTail,omitempty"`
	Stderr   string `json:"stderrTail,omitempty"`
	Skipped  string `json:"skippedReason,omitempty"`
}

// Critique 是任务完成后由模型自评产出的结构化反馈。
type Critique struct {
	Quality   int      `json:"quality"` // 1-10
	Tags      []string `json:"tags"`
	Strengths []string `json:"strengths"`
	Gaps      []string `json:"gaps"`
	NextIdeas []Idea   `json:"nextIdeas"`
	At        int64    `json:"at"`
	Degraded  bool     `json:"degraded"`
}

// Idea 是自评给出的后继任务草案。
type Idea struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	TemplateID string `json:"templateId,omitempty"`
	Domain     string `json:"domain,omitempty"`
}

// Chain 是一条任务链（同一会话内的递进任务序列）。
type Chain struct {
	ID         string   `json:"id"`
	RootTaskID string   `json:"rootTaskId"`
	SessionID  string   `json:"sessionId"`
	Topic      string   `json:"topic"`
	Depth      int      `json:"depth"`
	MaxDepth   int      `json:"maxDepth"`
	Status     string   `json:"status"` // active/closed
	TaskIDs    []string `json:"taskIds"`
	CreatedAt  int64    `json:"createdAt"`
	UpdatedAt  int64    `json:"updatedAt"`
}

// Event 是 SSE 推送给前端的事件。
type Event struct {
	Type   string `json:"type"`
	At     int64  `json:"at"`
	TaskID string `json:"taskId,omitempty"`
	Data   any    `json:"data,omitempty"`
	Msg    string `json:"msg,omitempty"`
}

// EnginePhase 是引擎整体运行阶段。
type EnginePhase string

const (
	PhaseIdle    EnginePhase = "idle"
	PhaseRunning EnginePhase = "running"
	PhasePaused  EnginePhase = "paused"
	PhaseStopped EnginePhase = "stopped"
)

// Stats 是面板展示的统计快照。
type Stats struct {
	TotalTasks    int     `json:"totalTasks"`
	DoneTasks     int     `json:"doneTasks"`
	FailedTasks   int     `json:"failedTasks"`
	QueuedTasks   int     `json:"queuedTasks"`
	RunningTasks  int     `json:"runningTasks"`
	RejectedTasks int     `json:"rejectedTasks"`
	// DegradedTasks 是"有产出但未通过验收"的任务数（P3）。
	// 它们**不计入** DoneTasks / TodayDone，避免"截断也算成功"的虚报。
	DegradedTasks int `json:"degradedTasks"`
	TodayDone     int     `json:"todayDone"`
	SuccessRate   float64 `json:"successRate"`
	AvgSeconds    float64 `json:"avgSeconds"`
	TotalBytes    int64   `json:"totalBytes"`
	// VerifiedTasks 是通过**真实执行验收**的任务数（P3）。
	// 与 DoneTasks 区分：done 只表示流程跑完，verified 才表示产物真能跑。
	VerifiedTasks int `json:"verifiedTasks"`
	TotalTokenIn  int64   `json:"totalTokenIn"`
	TotalTokenOut int64   `json:"totalTokenOut"`
}

// Now 返回当前毫秒时间戳。
func Now() int64 { return time.Now().UnixMilli() }

// Truncate 按字节截断字符串，并回退到合法 UTF-8 边界，避免切碎中文。
func Truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimSpace(cut)
}
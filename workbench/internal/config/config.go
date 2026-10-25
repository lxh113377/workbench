package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"workbench/internal/retention"
	"workbench/internal/workspace"
)

// SchemaVersion 是配置文件结构版本，用于升级迁移。
// 版本历史：
//
//	1 → 2：看门狗默认 180s -> 420s
//	2 → 3：P3 产物协议开启（appendProtocol / repairRounds）
//	3 → 4：迁移判据改为「字段零值 + 版本上限」（修 v3 被提前写入导致迁移不命中）
//	4 → 5：请求速率闸门（requestsPerMinute / requestBurst）
//	5 → 6：双角色闭环「思考模型只出方案 / agent 模型负责执行」（plan 段）
const SchemaVersion = 6

// EvalConfig 控制三维评估的行为。
type EvalConfig struct {
	Weights       EvalWeights `json:"weights"`
	Threshold     float64     `json:"threshold"`     // 综合分阈值，低于者拒
	SemanticOn    bool        `json:"semanticOn"`    // 是否启用 LLM 语义判分
	RepeatGate    bool        `json:"repeatGate"`    // 重复任务硬门禁
	SimilarityCap float64     `json:"similarityCap"` // 判为重复的相似度上限
	DailyCap      int         `json:"dailyCap"`      // 每日最多生成任务数
	DiversityCap  float64     `json:"diversityCap"`  // 同组占比上限
}

// EvalWeights 是三维权重。
type EvalWeights struct {
	Value       float64 `json:"value"`
	Cost        float64 `json:"cost"`
	Feasibility float64 `json:"feasibility"`
}

// ModelRoute 是一个模型路由项。
type ModelRoute struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	// Lane 是模型所属的**接入车道**（free / kilo / eac / aggregate / custom）。
	//
	// 为什么不能只看 Provider（2026-10-19 现场实测）：our-free-model 插件在 DSH 里
	// 把 OFM 免费、Kilo 免费池、EAC 协付三条车道**统一注册成 provider
	// "our-free-model"**，因此按 provider 分组做冷却会把三条独立线路当成一条 ——
	// 实测 OFM 429 期间 Kilo 车道明明可用，却因为同 provider 被一起停掉。
	// 车道是真正的"故障域"，必须单独传下来。
	Lane      string `json:"lane,omitempty"`
	Tier      string `json:"tier"` // quality/balanced/fast
	Enabled   bool   `json:"enabled"`
	Weight    int    `json:"weight"`    // 同 tier 内的选取权重
	MaxFail   int    `json:"maxFail"`   // 连续失败阈值
	CooldownM int    `json:"cooldownM"` // 熔断冷却分钟
}

// ExecutorConfig 控制子进程执行器。
type ExecutorConfig struct {
	Concurrency    int    `json:"concurrency"`    // 并发执行数 1-6
	DefaultTimeout int    `json:"defaultTimeout"` // 默认单任务超时（分钟）
	MaxRetries     int    `json:"maxRetries"`     // 失败重试次数
	RetryBackoffMs []int  `json:"retryBackoffMs"`
	MaxOutputMB    int    `json:"maxOutputMB"`  // 单任务事件流上限
	StallSeconds   int    `json:"stallSeconds"` // 无进展判定阈值（秒），默认 180
	DSHExe         string `json:"dshExe"`       // 留空则自动探测
	ProfileName    string `json:"profileName"`

	// RetryPolicy 是重试策略（对标 river ClientRetryPolicy / asynq RetryDelayFunc）。
	// 零值字段由 Normalize 补默认，旧配置只写 maxRetries/retryBackoffMs 也能正常工作。
	RetryPolicy RetryPolicyConfig `json:"retryPolicy"`
	// ShutdownGraceSeconds 是优雅停机宽限期（秒）：到期仍在跑的任务被强制取消。
	ShutdownGraceSeconds int `json:"shutdownGraceSeconds"`
	// AppendProtocol 控制是否在任务 prompt 尾部追加产出协议（P3）。
	// 默认 true：模型据此输出「文件清单+文件内容+验收命令」，工作台落盘并真跑。
	AppendProtocol bool `json:"appendProtocol"`
	// RepairRounds 是验收失败后的最大返工轮数（额度无限，默认 3）。
	RepairRounds int `json:"repairRounds"`
	// RequestsPerMinute 是全局请求速率上限（每分钟发起的 DSH 调用次数）。
	//
	// 2026-10-19 生产事故后新增：事故时每分钟 45-70 次请求打爆免费车道，
	// 导致 429 持续 50 分钟。默认 20/分钟：既保证 24/7 持续产出
	// （约 3 分钟一篇文章），又给车道留足余量。设为 0 表示不限速（不推荐）。
	RequestsPerMinute int `json:"requestsPerMinute"`
	// RequestBurst 是允许的瞬时突发请求数（令牌桶容量）。默认 3。
	RequestBurst int `json:"requestBurst"`
}

// RetryPolicyConfig 是重试策略的配置面（对应 internal/retry.Policy）。
type RetryPolicyConfig struct {
	BaseDelayMs    int      `json:"baseDelayMs"`    // 指数退避基准（毫秒）
	MaxDelayMs     int      `json:"maxDelayMs"`     // 单次等待上限（毫秒）
	Jitter         float64  `json:"jitter"`         // 抖动比例 0..1
	RetryableKinds []string `json:"retryableKinds"` // 为空 = 用默认可重试表
}

// ChainConfig 控制链式生成。
type ChainConfig struct {
	Enabled      bool `json:"enabled"`
	MaxDepth     int  `json:"maxDepth"`
	Fanout       int  `json:"fanout"`
	MinQuality   int  `json:"minQuality"`   // 自评低于此值终止链
	ContextChars int  `json:"contextChars"` // 注入后继任务的上下文长度
}

// ServerConfig 控制面板服务。
type ServerConfig struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	OpenBrowser bool   `json:"openBrowser"`
}

// SupervisorConfig 控制自愈与资源治理。
type SupervisorConfig struct {
	Enabled          bool `json:"enabled"`
	MemLimitMB       int  `json:"memLimitMB"`
	HandleLimit      int  `json:"handleLimit"`
	GoroutineLimit   int  `json:"goroutineLimit"`
	SampleSeconds    int  `json:"sampleSeconds"`
	StrikesToRestart int  `json:"strikesToRestart"`
}

// PoolConfig 控制模型池（多接口统一纳管 + 自动探测 + 故障转移）。
type PoolConfig struct {
	Enabled      bool   `json:"enabled"`      // 是否启动模型池服务
	ProbeSeconds int    `json:"probeSeconds"` // 可用性探测周期（秒），30..3600
	SyncSeconds  int    `json:"syncSeconds"`  // 网关清单同步周期（秒），60..7200
	Strategy     string `json:"strategy"`     // priority / round-robin / weighted / least-loaded
	DeepProbe    bool   `json:"deepProbe"`    // 是否发模型级补全探测（会消耗额度，默认关）
}

// PlanConfig 控制「双角色闭环」：思考模型只出方案，agent 模型负责执行。
//
// 立规原话（老大 2026-10-21）：
//
//	「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」
//
// 为什么这必须是配置项而不是硬编码常量：
//  1. 方案模型与执行模型是**两次独立调用**，各自要能被单独指向（思考用大模型、
//     执行用便宜快的模型，或反过来）；
//  2. 车道上可能根本没有"支持 reasoning 的模型"（免费车道多数不支持显式推理档位）——
//     此时 PlannerModels 为空表示"从现有路由表里按 tier 选"，而不是留空即失败；
//  3. 额度无限的前提是方案层可关：小任务直执行更省时间，不能强迫所有任务都先规划。
type PlanConfig struct {
	// Enabled 是否启用方案阶段。false = 退回改造前的单次调用（兼容性要求）。
	Enabled bool `json:"enabled"`
	// PlannerModels 是指定的方案模型（模型 id，按顺序尝试）。
	//
	// 为空 = 由路由器按 PlannerTier 从现有路由表里选。
	// 非空时只在这些模型里选（都不可用则退化直执行并留审计，绝不静默跳过）。
	PlannerModels []string `json:"plannerModels,omitempty"`
	// PlannerTier 是无指定模型时的选取档位（quality/balanced/fast）。
	// 默认 quality —— 方案是"想清楚"，这一步值得用最好的模型。
	PlannerTier string `json:"plannerTier"`
	// PlannerMaxMinutes 是方案调用的单次超时（分钟）。方案比产出短，默认 8。
	PlannerMaxMinutes int `json:"plannerMaxMinutes"`
	// ReplanRounds 是验收失败后**重新规划**的最大轮数（默认 2）。
	//
	// 与 Executor.RepairRounds 的分工（这是本设计的核心取舍）：
	//   - RepairRounds：方案不变，让执行模型带着报错重做（实现层面的错）；
	//   - ReplanRounds：认定方案本身错了，重新出方案再执行（方案层面的错）。
	// 先修实现再改方案：改方案的成本高（等于推翻重来），所以返工轮先用完。
	ReplanRounds int `json:"replanRounds"`
	// AppendPlanToPrompt 是否在执行指令里保留原始任务描述（默认 true）。
	// 关掉只剩方案，适合"方案本身就是完整规格"的场景。
	AppendPlanToPrompt bool `json:"appendPlanToPrompt"`
}

// NormalizePlan 补齐方案阶段配置的零值与越界值。
//
// 单独成函数（而不是塞进 Normalize）的原因：它被 Normalize 与 PlanConfig 的
// 使用方同时需要，且判据集中在方案语义上，便于单测直接钉住。
func NormalizePlan(p *PlanConfig, fromSchema int) {
	if p == nil {
		return
	}
	// 旧安装（schema<6）没这个段：字段全是零值。此时按"开启"迁移 ——
	// 与 appendProtocol/repairRounds 那次迁移同一口径：新能力要能被老用户拿到。
	if fromSchema < 6 && !p.Enabled && len(p.PlannerModels) == 0 && p.PlannerTier == "" {
		p.Enabled = true
	}
	if p.PlannerTier == "" {
		p.PlannerTier = "quality"
	}
	switch strings.ToLower(strings.TrimSpace(p.PlannerTier)) {
	case "quality", "balanced", "fast":
		p.PlannerTier = strings.ToLower(strings.TrimSpace(p.PlannerTier))
	default:
		p.PlannerTier = "quality"
	}
	if p.PlannerMaxMinutes < 1 || p.PlannerMaxMinutes > 60 {
		p.PlannerMaxMinutes = 8
	}
	// 重规划轮数：零值是"未配置"，必须按默认补齐 —— 否则老安装升级后
	// 「返工用尽 -> 重新规划」这一整条闭环支路永远不生效。
	// 实测形态：迁移后 ReplanRounds 仍是 0，shouldReplan 恒 false，
	// 于是方案层面的错永远修不掉，闭环断在最后一段。
	//
	// 关掉双角色闭环的正确开关是 plan.enabled=false，不是把轮数设 0 ——
	// 两个开关语义重叠会让"哪个是权威"变得不可判定。
	if p.ReplanRounds <= 0 {
		p.ReplanRounds = 2
	}
	if p.ReplanRounds > 5 {
		p.ReplanRounds = 5
	}
	// 注意：AppendPlanToPrompt 的零值是 false，但默认应为 true。
	// 用"schema<6 视为未配置"判定，避免用户显式关闭后被反复打开。
	if fromSchema < 6 {
		p.AppendPlanToPrompt = true
	}
}

// GovConfig 控制「契约治理 × 体量治理」双线治理的监控参数。
// 体量量化阈值（Budget）不在这里：它作为治理数据随 governance/budget.json 落盘，
// 便于面板在线调整与版本留痕，避免与运行期配置互相覆盖。
type GovConfig struct {
	Enabled       bool    `json:"enabled"`       // 是否启动治理监控循环
	SampleSeconds int     `json:"sampleSeconds"` // 采样周期（秒），5..3600
	AutoEscalate  bool    `json:"autoEscalate"`  // 红区预警是否写升级审计
	ExpiringDays  int     `json:"expiringDays"`  // 契约到期预警窗口（天）
	ComplianceMin float64 `json:"complianceMin"` // 契约合规分下限
}

// AutonomyConfig 控制「自主任务规划与闭环执行」子系统的行为。
//
// 设计原则：默认开启，但所有能失控的旋钮都在这里，且 Normalize 会强制钳制到安全区间。
// 关掉 Enabled 即回到改造前的固定模板链路（兼容性要求）。
type AutonomyConfig struct {
	Enabled  bool             `json:"enabled"`
	Budget   AutonomyBudget   `json:"budget"`
	Bounds   AutonomyBounds   `json:"bounds"`
	Plan     AutonomyPlan     `json:"plan"`
	Risk     AutonomyRisk     `json:"risk"`
	Rollback AutonomyRollback `json:"rollback"`
}

// AutonomyBudget 是自主循环的预算上限（防失控的三道闸）。
type AutonomyBudget struct {
	MaxTasksPerGoal int `json:"maxTasksPerGoal"` // 单目标最多生成任务数
	MaxStages       int `json:"maxStages"`       // 单目标最多阶段数
	MinQuality      int `json:"minQuality"`      // 阶段自评下限（1-10）
	MaxRollbacks    int `json:"maxRollbacks"`    // 单目标最多回退次数
	StageEvery      int `json:"stageEvery"`      // 每 N 条任务推进一个阶段（预留）
}

// AutonomyBounds 是动态任务的边界约束（B1-B4 的配置面）。
type AutonomyBounds struct {
	AllowedDomains    []string `json:"allowedDomains"`    // 允许自主生成的领域白名单
	MaxTaskWords      int      `json:"maxTaskWords"`      // 单任务字数上限
	MaxDraftsPerRound int      `json:"maxDraftsPerRound"` // 每轮最多草案数（防扇出爆炸）
}

// AutonomyPlan 控制规划器的目标与产出规格。
type AutonomyPlan struct {
	RootTitle     string `json:"rootTitle"`     // 初始项目目标标题
	RootIntent    string `json:"rootIntent"`    // 初始项目目标意图（注入 Prompt）
	DefaultDomain string `json:"defaultDomain"` // 默认领域
	Words         int    `json:"words"`         // 单任务目标字数
	MaxDrafts     int    `json:"maxDrafts"`     // 每轮最多规划草案数
}

// AutonomyRisk 是风险校验阈值。
type AutonomyRisk struct {
	EscalateMinQuality int  `json:"escalateMinQuality"` // 低于此值升级为高危
	BlockOnRepeat      bool `json:"blockOnRepeat"`      // 空转是否直接阻塞（默认 false=仅告警）
}

// AutonomyRollback 控制异常回退行为。
type AutonomyRollback struct {
	OnLowQuality  bool `json:"onLowQuality"`  // 低质量阶段是否回退（默认 true）
	OnCostOverrun bool `json:"onCostOverrun"` // 预算超限是否回退（默认 true，实际按 block 处理）
}

// Config 是工作台的完整配置。
type Config struct {
	SchemaVersion int              `json:"schemaVersion"`
	AutoResume    bool             `json:"autoResume"`   // 启动后自动恢复上次运行
	ResumeOnBoot  bool             `json:"resumeOnBoot"` // 开机自启后是否自动开跑
	AutoStart     bool             `json:"autoStart"`
	OutputRoot    string           `json:"outputRoot"`
	Eval          EvalConfig       `json:"eval"`
	Models        []ModelRoute     `json:"models"`
	Executor      ExecutorConfig   `json:"executor"`
	Chain         ChainConfig      `json:"chain"`
	Server        ServerConfig     `json:"server"`
	Supervisor    SupervisorConfig `json:"supervisor"`
	Gov           GovConfig        `json:"gov"`
	Autonomy      AutonomyConfig   `json:"autonomy"`
	Pool          PoolConfig       `json:"pool"`
	// Plan 是「双角色闭环」配置：思考模型只出方案，agent 模型负责执行。
	// 零值 + 旧 schema 会由 NormalizePlan 迁移成"开启"（见其注释）。
	Plan PlanConfig `json:"plan"`
	// Workspace 是「AI 开发工作目录」配置（可选外部目录模式）。
	// 零值/缺省 = 项目模式（与改造前行为完全一致）。
	Workspace workspace.Config `json:"workspace"`
	// Retention 是运行时产物保留策略（DSH_HOME 副本/workspace 回收）。
	//
	// 2026-10-19 现场审计：24 小时内积累 247 个 DSH_HOME 副本（26.3MB）
	// 且从未回收。按 7×24 估算 1 年后约 3.6GB。
	// 零值 = 默认保留窗口（3/30/90 天）。
	Retention retention.Policy `json:"retention"`
	UpdatedAt int64            `json:"updatedAt"`
	// Warnings 记录 Normalize 期间的配置修正（跨字段矛盾为主）。
	//
	// 为什么需要（2026-10-22 对标 river client.go:528-534）：river 用一条跨字段判据
	// 把 rescueAfter 与 jobTimeout 的关系收敛；本项目此前 31 处钳制全是单字段，
	// 零处跨字段。新增的 normalizeCrossField 把修正写进这里，
	// 让「配置被改过」成为可观测事实，而不是静默行为差异。
	//
	// 语义：只增不减由 addWarning 去重；不入盘（每次 Normalize 重新计算，
	// 避免把一次性告警固化成配置噪音）。
	Warnings []string `json:"warnings,omitempty"`
}

// DefaultConfig 返回开箱即用的默认配置（基于本机实测的模型健康度）。
func DefaultConfig() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		AutoResume:    true,
		ResumeOnBoot:  true,
		AutoStart:     false,
		Eval: EvalConfig{
			Weights:       EvalWeights{Value: 0.5, Cost: 0.2, Feasibility: 0.3},
			Threshold:     60,
			SemanticOn:    true,
			RepeatGate:    true,
			SimilarityCap: 0.9,
			DailyCap:      200,
			DiversityCap:  0.4,
		},
		Models: []ModelRoute{
			{Model: "z-ai/glm-5.3", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
			{Model: "openai/gpt-oss-20b", Provider: "our-free-model", Tier: "fast", Enabled: true, Weight: 60, MaxFail: 3, CooldownM: 30},
			{Model: "z-ai/glm-5.3-flash", Provider: "our-free-model", Tier: "fast", Enabled: true, Weight: 30, MaxFail: 2, CooldownM: 60},
		},
		Executor: ExecutorConfig{
			Concurrency:    2,
			DefaultTimeout: 30,
			MaxRetries:     3,
			RetryBackoffMs: []int{5000, 20000, 60000},
			MaxOutputMB:    32,
			RetryPolicy: RetryPolicyConfig{
				BaseDelayMs: 5000, MaxDelayMs: 600000, Jitter: 0.2,
				// transport_failed / stream_cut 必须在内（2026-10-19 现场补齐）：
				// 网络抖动与流中断是「换模型/重试就能好」的典型故障，
				// 若不在表内，executor 判出的类别到了重试层会被判成不可重试 ——
				// 于是 ETIMEDOUT 一次失败即任务判死，浪费了故障转移的能力。
				RetryableKinds: []string{"timeout", "upstream_failed", "bad_exit", "stalled", "unknown", "transport_failed", "stream_cut"},
			},
			ShutdownGraceSeconds: 20,
			AppendProtocol:       true,
			RepairRounds:         3,
			RequestsPerMinute:    20,
			RequestBurst:         3,
			// 420 秒：OFM 思考期可静默 60–70 秒、极端 300 秒（插件 README 已知边界），
			// 旧默认 180 秒会把"正在深思"误判成卡死（现场 glm-5.3 4 次失败全是 stalled）。
			StallSeconds: 420,
			ProfileName:  "web-desktop",
		},
		Chain:  ChainConfig{Enabled: true, MaxDepth: 5, Fanout: 2, MinQuality: 6, ContextChars: 2000},
		Server: ServerConfig{Host: "127.0.0.1", Port: 17890, OpenBrowser: true},
		Supervisor: SupervisorConfig{
			Enabled: true, MemLimitMB: 700, HandleLimit: 3000, GoroutineLimit: 1500,
			SampleSeconds: 30, StrikesToRestart: 3,
		},
		Gov:      GovConfig{Enabled: true, SampleSeconds: 300, AutoEscalate: true, ExpiringDays: 30, ComplianceMin: 70},
		Autonomy: DefaultAutonomyConfig(),
		// 双角色闭环默认开启：方案阶段用 quality 档模型想清楚，执行阶段按模板 tier 选。
		// ReplanRounds=2：返工（RepairRounds=3）先用完再重新规划 —— 改方案等于推翻重来。
		Plan: PlanConfig{
			Enabled:            true,
			PlannerTier:        "quality",
			PlannerMaxMinutes:  8,
			ReplanRounds:       2,
			AppendPlanToPrompt: true,
		},
		// 模型池默认开启：探测 3 分钟一轮（端点级，不烧额度），清单同步 10 分钟一轮。
		Pool: PoolConfig{Enabled: true, ProbeSeconds: 180, SyncSeconds: 600, Strategy: "priority", DeepProbe: false},
		// 默认项目模式（兼容）：用户不选外部目录时行为与改造前完全一致。
		Workspace: workspace.DefaultConfig(),
		// 保留策略默认开启：不回收会无限堆积（现场 24h 247 个副本 / 26.3MB）。
		Retention: retention.DefaultPolicy(),
	}
}

// DefaultAutonomyConfig 返回自主子系统的开箱默认值。
func DefaultAutonomyConfig() AutonomyConfig {
	return AutonomyConfig{
		Enabled: true,
		Budget: AutonomyBudget{
			MaxTasksPerGoal: 12, MaxStages: 6, MinQuality: 6, MaxRollbacks: 3, StageEvery: 1,
		},
		Bounds: AutonomyBounds{
			AllowedDomains:    []string{"content", "code", "knowledge", "data", "solution", "report"},
			MaxTaskWords:      3000,
			MaxDraftsPerRound: 2,
		},
		Plan: AutonomyPlan{
			RootTitle:     "持续产出与能力沉淀",
			RootIntent:    "围绕个人知识资产持续产出高质量可复用成果：先把主题拆透、写出成体系的长文，再拆成卡片/脚本等可复用形态，最后复盘补齐不足。",
			DefaultDomain: "content",
			Words:         1000,
			MaxDrafts:     2,
		},
		Risk:     AutonomyRisk{EscalateMinQuality: 3, BlockOnRepeat: false},
		Rollback: AutonomyRollback{OnLowQuality: true, OnCostOverrun: true},
	}
}

// Normalize 修正越界配置，保证运行期不会因手改配置而崩。
func (c *Config) Normalize() {
	d := DefaultConfig()
	// 迁移判据必须用"进入 Normalize 时的版本"，否则后面的版本号抬升会让
	// 迁移分支永远不成立（本轮实测踩到：schema 先抬到 3，autonomy 段补默认的分支就死了）。
	fromSchema := c.SchemaVersion
	if c.SchemaVersion <= 0 {
		c.SchemaVersion = SchemaVersion
	}
	if c.Executor.Concurrency < 1 {
		c.Executor.Concurrency = 1
	}
	if c.Executor.Concurrency > 6 {
		c.Executor.Concurrency = 6
	}
	if c.Executor.DefaultTimeout < 1 {
		c.Executor.DefaultTimeout = d.Executor.DefaultTimeout
	}
	if c.Executor.MaxRetries < 0 || c.Executor.MaxRetries > 10 {
		c.Executor.MaxRetries = d.Executor.MaxRetries
	}
	if len(c.Executor.RetryBackoffMs) == 0 {
		c.Executor.RetryBackoffMs = d.Executor.RetryBackoffMs
	}
	if c.Executor.MaxOutputMB < 1 {
		c.Executor.MaxOutputMB = d.Executor.MaxOutputMB
	}
	if c.Executor.ShutdownGraceSeconds < 1 || c.Executor.ShutdownGraceSeconds > 300 {
		c.Executor.ShutdownGraceSeconds = d.Executor.ShutdownGraceSeconds
	}
	if c.Executor.RetryPolicy.BaseDelayMs < 1 {
		c.Executor.RetryPolicy.BaseDelayMs = d.Executor.RetryPolicy.BaseDelayMs
	}
	if c.Executor.RetryPolicy.MaxDelayMs < c.Executor.RetryPolicy.BaseDelayMs {
		c.Executor.RetryPolicy.MaxDelayMs = d.Executor.RetryPolicy.MaxDelayMs
	}
	if c.Executor.RetryPolicy.Jitter < 0 || c.Executor.RetryPolicy.Jitter > 1 {
		c.Executor.RetryPolicy.Jitter = d.Executor.RetryPolicy.Jitter
	}
	// 旧版本默认值 180 秒要主动迁移到新的 420 秒：
	// 配置一旦落盘就会带着旧默认值，只改 DefaultConfig 不影响既有安装。
	// 判据：恰好等于旧默认值（180）视为"用户没改过"，升到新默认；
	// 其它值（含用户显式设置的）一律保留，避免覆盖用户的真实选择。
	if c.SchemaVersion < 2 && c.Executor.StallSeconds == 180 {
		c.Executor.StallSeconds = d.Executor.StallSeconds
	}
	if c.Executor.StallSeconds < 30 || c.Executor.StallSeconds > 3600 {
		c.Executor.StallSeconds = d.Executor.StallSeconds
	}
	// 保留策略零值补齐（2026-10-19 现场缺陷）：
	//
	// retention.go 早就写了 NormalizeRetention（注释：「补齐保留策略的零值，用户不配也能安全运行」），
	// 但 **Normalize() 从不调用它** —— 实测把 config.json 的 retention 写成 0/0/0 后，
	// 读出来的 policy 就是 0/0/0，而不是安全默认 3/30/90。
	//
	// 为什么这比「不回收」更危险：0 天保留期意味着**什么都算过期** ——
	// 一次 --prune --apply 就可能立刻回收大量目录。零值必须被补齐。
	//
	// 与 TaskDirDays 那次同类：字段/函数都齐全，唯独**调用链断了一节**。
	NormalizeRetention(c)
	if c.SchemaVersion < 2 {
		c.SchemaVersion = 2
	}
	// v2 -> v3：P3 产物协议开启（appendProtocol / repairRounds）。
	//
	// 为什么用「字段零值 + schemaVersion 上限」双重判据（2026-10-19 二次教训）：
	// 第一版只看 schemaVersion<3，但现场配置的 schemaVersion 已被写成 3
	// （别的模块先升过版本号），迁移永远不命中 —— 配置里 appendProtocol 仍是
	// false、repairRounds 仍是 0，P3 继续形同虚设。
	//
	// 现在的规则：
	//   - schemaVersion > 3 视为未来版本，不动；
	//   - schemaVersion <= 3 且字段是零值 -> 按默认值打开（旧安装的必经路径）；
	//   - 用户后来显式关闭时，schemaVersion 会被提升到 4（见 Replace/Update），
	//     因此不会被反复覆盖。
	//
	// 判据必须用 **fromSchema（进入时的版本）** 而不是 c.SchemaVersion（2026-10-22 实测缺陷）：
	// 函数开头有一条 `if c.SchemaVersion <= 0 { c.SchemaVersion = SchemaVersion }`，
	// 于是零值配置（{} / 只填了部分字段的外部工具产物 / 半截写入的文件）
	// 进来就被抬到 6，本分支**永远不命中** —— appendProtocol 停在 false、
	// repairRounds 停在 0，P3 产物协议形同虚设。
	// 这正是本文件顶部记过的「版本号被提前抬升导致迁移不命中」的**第三处**复现，
	// 也是上面 fromSchema 注释里写明的同一条规则，只是本分支当时没跟上。
	// 实证：crossfield_test.go 的 TestCrossFieldReplanGuardOnZeroRepair 首轮判红。
	if fromSchema <= 3 {
		if !c.Executor.AppendProtocol {
			c.Executor.AppendProtocol = d.Executor.AppendProtocol
		}
		if c.Executor.RepairRounds <= 0 {
			c.Executor.RepairRounds = d.Executor.RepairRounds
		}
		c.SchemaVersion = 4
	}
	if c.Executor.ProfileName == "" {
		c.Executor.ProfileName = d.Executor.ProfileName
	}
	if c.Executor.RepairRounds < 0 || c.Executor.RepairRounds > 5 {
		c.Executor.RepairRounds = d.Executor.RepairRounds
	}
	// v4 -> v5：请求速率闸门。旧配置没有这两个字段（零值 = 不限速），
	// 那正是 2026-10-19 打爆车道的配置，必须补默认值。
	if c.SchemaVersion <= 4 {
		if c.Executor.RequestsPerMinute <= 0 {
			c.Executor.RequestsPerMinute = d.Executor.RequestsPerMinute
		}
		if c.Executor.RequestBurst <= 0 {
			c.Executor.RequestBurst = d.Executor.RequestBurst
		}
		c.SchemaVersion = 5
	}
	if c.Executor.RequestsPerMinute < 0 {
		c.Executor.RequestsPerMinute = d.Executor.RequestsPerMinute
	}
	if c.Executor.RequestBurst < 1 {
		c.Executor.RequestBurst = d.Executor.RequestBurst
	}
	if c.Executor.RequestBurst > 20 {
		c.Executor.RequestBurst = 20
	}
	// v5 -> v6：双角色闭环（思考模型只出方案 / agent 模型负责执行）。
	//
	// 判据沿用「fromSchema + 零值」双口径（v3 那次教训：只看版本号会被别的模块
	// 提前抬版本害死）。fromSchema 必须用**进入 Normalize 时**的版本，
	// 否则前面的分支已经把 SchemaVersion 抬到 6，这里就永远不成立。
	NormalizePlan(&c.Plan, fromSchema)
	if c.SchemaVersion < 6 {
		c.SchemaVersion = 6
	}
	if c.Eval.Threshold <= 0 || c.Eval.Threshold > 100 {
		c.Eval.Threshold = d.Eval.Threshold
	}
	if c.Eval.SimilarityCap <= 0 || c.Eval.SimilarityCap > 1 {
		c.Eval.SimilarityCap = d.Eval.SimilarityCap
	}
	if c.Eval.DailyCap < 1 {
		c.Eval.DailyCap = d.Eval.DailyCap
	}
	if c.Eval.DiversityCap <= 0 || c.Eval.DiversityCap > 1 {
		c.Eval.DiversityCap = d.Eval.DiversityCap
	}
	w := docWeights(c.Eval.Weights)
	c.Eval.Weights = w
	if c.Chain.MaxDepth < 1 {
		c.Chain.MaxDepth = d.Chain.MaxDepth
	}
	if c.Chain.MaxDepth > 20 {
		c.Chain.MaxDepth = 20
	}
	if c.Chain.Fanout < 0 {
		c.Chain.Fanout = 0
	}
	if c.Chain.Fanout > 4 {
		c.Chain.Fanout = 4
	}
	if c.Chain.ContextChars < 200 {
		c.Chain.ContextChars = d.Chain.ContextChars
	}
	if c.Server.Host == "" {
		c.Server.Host = d.Server.Host
	}
	if c.Server.Port < 1024 || c.Server.Port > 65535 {
		c.Server.Port = d.Server.Port
	}
	if len(c.Models) == 0 {
		c.Models = d.Models
	}
	if c.Supervisor.SampleSeconds < 5 {
		c.Supervisor.SampleSeconds = d.Supervisor.SampleSeconds
	}
	if c.Supervisor.StrikesToRestart < 1 {
		c.Supervisor.StrikesToRestart = d.Supervisor.StrikesToRestart
	}
	if c.Gov.SampleSeconds < 5 || c.Gov.SampleSeconds > 3600 {
		c.Gov.SampleSeconds = d.Gov.SampleSeconds
	}
	if c.Gov.ExpiringDays < 1 || c.Gov.ExpiringDays > 180 {
		c.Gov.ExpiringDays = d.Gov.ExpiringDays
	}
	if c.Gov.ComplianceMin <= 0 || c.Gov.ComplianceMin > 100 {
		c.Gov.ComplianceMin = d.Gov.ComplianceMin
	}
	if c.Pool.ProbeSeconds < 30 || c.Pool.ProbeSeconds > 3600 {
		c.Pool.ProbeSeconds = d.Pool.ProbeSeconds
	}
	if c.Pool.SyncSeconds < 60 || c.Pool.SyncSeconds > 7200 {
		c.Pool.SyncSeconds = d.Pool.SyncSeconds
	}
	switch c.Pool.Strategy {
	case "priority", "round-robin", "weighted", "least-loaded":
	default:
		c.Pool.Strategy = d.Pool.Strategy
	}
	c.normalizeAutonomy(d, fromSchema)
	c.normalizeWorkspace()
	// 跨字段一致性校验（2026-10-22 对标 river client.go:528-534 后新增）。
	// 必须放在所有单字段钳制**之后**：前面的段保证每个字段各自合法，
	// 但字段之间的关系无人保证。river 用一条
	//   if c.JobTimeout > 0 && c.RescueStuckJobsAfter < 1 && c.JobTimeout > c.RescueStuckJobsAfter
	// 就把这类关系收敛了；本项目此前只有单字段钳制，31 处、零处跨字段。
	c.normalizeCrossField(d)
}

// normalizeCrossField 校验并修正**字段之间**的矛盾关系。
//
// 为什么单独立一段而不是塞进 Normalize 主体：跨字段判据的失效形态与单字段不同 ——
// 单字段越界是"这个值不合法"，跨字段矛盾是"每个值都合法、合在一起不 work"。
// 后者更隐蔽（每个字段都过得了自己的钳制），也更难排查（现场看不到任何错误）。
//
// 已实测存在的矛盾（2026-10-22 取证）：
//
//	StallSeconds(420s) < DefaultTimeout(30min)
//	  => 任务超时先于卡死判定触发，看门狗对超时类任务**永不生效**。
//	     river 的对应注释（job_executor.go:392）明确要求
//	     "RescueStuckJobsAfter 必须 > JobTimeout，否则误判"。
//
//	Plan.ReplanRounds(2) >= Executor.RepairRounds(3)
//	  => 返工额度还没用完就重新规划，"先用便宜手段"的设计被推翻。
//	     此前只靠 plan_config_test.go 一条测试断言默认配置的大小关系，
//	     手改配置可突破（测试只测默认值）。
//
//	Plan.PlannerMaxMinutes(8) >= Executor.DefaultTimeout(30)
//	  => 方案调用比整个任务的超时还长，方案阶段必然超时。
//
// 修正策略：抬高一侧而不是压低另一侧（压低会让功能直接失效），
// 并把每一条修正登记到 Config.Warnings，让调用方在启动时说清楚。
func (c *Config) normalizeCrossField(d Config) {
	// 1) 卡死阈值必须**小于**单任务超时，否则看门狗永远抢不到执行机会。
	//
	// 方向说明（本轮第一次写反，被既有测试 TestMigrationV1AlsoFixesWatchdog 抓出）：
	// 看门狗判据是「**活动**停滞」（无 stdout 事件且无产出文件写入），不是「跑得太久」；
	// 而 DefaultTimeout 是硬性墙钟上限。两者是「安全网」与「兜底上限」的关系：
	// 安全网必须先于兜底触发才有效。若 StallSeconds >= 任务超时，
	// 任务总会被硬超时先接管，stalled 分类**永不可达** —— 这是一份死配置，
	// 面板上永远看不到「卡死」，排查时也找不到这条线索。
	stallSec := c.Executor.StallSeconds
	timeoutSec := c.Executor.DefaultTimeout * 60
	if stallSec > 0 && timeoutSec > 0 && int64(stallSec) >= int64(timeoutSec) {
		c.Executor.StallSeconds = timeoutSec / 2
		if c.Executor.StallSeconds < 30 {
			c.Executor.StallSeconds = 30
		}
		c.addWarning("executor.stallSeconds",
			fmt.Sprintf("卡死阈值 %ds 不小于单任务超时 %dmin，看门狗永不可能先于硬超时触发（stalled 死配置）；已钳到 %ds",
				stallSec, c.Executor.DefaultTimeout, c.Executor.StallSeconds))
	}
	// 2) 重新规划轮数必须小于返工轮数（先用便宜手段）。
	// 守卫 RepairRounds > 0：零值配置（未走迁移分支）下该字段尚未补默认，
	// 此时比较没有意义，钳下去反而会把 NormalizePlan 刚补好的默认值打回 0
	// （本轮实测：TestNormalizeRaisesSchemaToSix 因此判红）。
	if c.Plan.Enabled && c.Executor.RepairRounds > 0 {
		if c.Plan.ReplanRounds >= c.Executor.RepairRounds {
			c.Plan.ReplanRounds = c.Executor.RepairRounds - 1
			if c.Plan.ReplanRounds < 0 {
				c.Plan.ReplanRounds = 0
			}
			c.addWarning("plan.replanRounds",
				fmt.Sprintf("重新规划轮数 %d 不小于返工轮数 %d（应先用便宜的返工）；已钳到 %d",
					c.Executor.RepairRounds, c.Executor.RepairRounds, c.Plan.ReplanRounds))
		}
		// 3) 方案调用超时必须短于整个任务的超时（否则方案阶段必然超时）。
		if c.Plan.PlannerMaxMinutes > 0 && c.Executor.DefaultTimeout > 0 &&
			c.Plan.PlannerMaxMinutes >= c.Executor.DefaultTimeout {
			c.Plan.PlannerMaxMinutes = c.Executor.DefaultTimeout / 2
			if c.Plan.PlannerMaxMinutes < 1 {
				c.Plan.PlannerMaxMinutes = 1
			}
			c.addWarning("plan.plannerMaxMinutes",
				fmt.Sprintf("方案调用超时 %dmin 不小于任务超时 %dmin（方案阶段必然超时）；已钳到 %dmin",
					c.Executor.DefaultTimeout, c.Executor.DefaultTimeout, c.Plan.PlannerMaxMinutes))
		}
	}
}

// 为什么没有「每日配额必须大于并发」这条（2026-10-22 实测，本轮被既有测试否掉的判据）：
//
// 第一版写了这条，理由是「一轮并发就把配额吃满」。但实测两条既有测试立刻判红
// （TestDailyCapStopsAutonomySupply / TestStallReasonDailyCap）——
// 它们刻意把 DailyCap 设成很小的值（1~2）来验证「配额耗尽后必须停手」，
// 而我这条判据把它们抬到 200，断言的前提直接消失。
//
// 更根本的问题是语义："一天只产 1 条"是用户的**刻意选择**（配额本来就是
// 用来收紧的旋钮），不是矛盾配置。把它抬上去属于「宁可多改」——
// 跨字段校验只能修**功能不可达**的矛盾，不能替用户调参。
// 保留的三条都满足这个判据：
//   1) stall >= timeout  -> stalled 分类永不可达（死配置）
//   2) replan >= repair  -> 「先用便宜手段」的顺序被推翻（设计意图不可达）
//   3) planTimeout >= taskTimeout -> 方案阶段挤占掉全部执行预算（执行不可达）
// 且三条的修正方向都是「抬高被压死的一侧 / 压低挤占的一侧」，
// 绝不是把用户的收紧选择放大。

// addWarning 登记一条配置修正（去重，按发生顺序累积）。
//
// 与单字段钳制的分工：钳制是静默的，Warnings 让"被钳过"这件事可观测。
// 若没有人读 Warnings，跨字段校验就仍然不可见 —— 那与不校验只差一个日志行。
func (c *Config) addWarning(field, reason string) {
	msg := field + ": " + reason
	for _, w := range c.Warnings {
		if w == msg {
			return
		}
	}
	c.Warnings = append(c.Warnings, msg)
}

// normalizeAutonomy 把自主配置钳制到安全区间。
//
// 容错策略：只有"预算/边界"这类会影响失控风险的字段做强钳制；
// Plan 里的文本字段为空时补默认，但不会覆盖用户显式写的目标意图。
func (c *Config) normalizeAutonomy(d Config, fromSchema int) {
	a := &c.Autonomy
	dv := d.Autonomy

	// schema < 3 的旧配置没有 autonomy 段：整段补默认值（默认开启）。
	// 判据是 SchemaVersion 而不是"字段是否为零值"——用户显式关闭需要能被保留。
	if fromSchema < 3 {
		if !a.Enabled && a.Budget.MaxTasksPerGoal == 0 && a.Plan.RootTitle == "" {
			c.Autonomy = dv
			return
		}
	}
	// 兜底修复（2026-10-19 现场）：schema 已经 >= 3 但 autonomy 段整体为零值时，
	// 说明这份配置是「部分重写/外部工具生成的残缺配置」（实测：压测实例手工写
	// config.json 只填了 executor/supervisor/server，结果 autonomy.enabled=false，
	// 目标树 bootstrap 出一个空 goals.json，自主子系统静默停摆）。
	// 判据：Enabled=false 且 预算/规划文案全空 ⇒ 视为"没配置过"，整段补默认。
	// 注意：用户显式关闭自主时会留下非空预算或文案，不受此分支影响。
	if a.Budget.MaxTasksPerGoal == 0 && a.Plan.RootTitle == "" && a.Bounds.MaxTaskWords == 0 {
		c.Autonomy = dv
		return
	}

	// 预算：任一越界即回默认，避免手改配置把预算放大成"无限循环"。
	if a.Budget.MaxTasksPerGoal <= 0 || a.Budget.MaxTasksPerGoal > 200 {
		a.Budget.MaxTasksPerGoal = dv.Budget.MaxTasksPerGoal
	}
	if a.Budget.MaxStages <= 0 || a.Budget.MaxStages > 50 {
		a.Budget.MaxStages = dv.Budget.MaxStages
	}
	if a.Budget.MinQuality < 1 || a.Budget.MinQuality > 10 {
		a.Budget.MinQuality = dv.Budget.MinQuality
	}
	if a.Budget.MaxRollbacks < 0 || a.Budget.MaxRollbacks > 20 {
		a.Budget.MaxRollbacks = dv.Budget.MaxRollbacks
	}
	if a.Budget.StageEvery < 1 || a.Budget.StageEvery > 10 {
		a.Budget.StageEvery = dv.Budget.StageEvery
	}

	// 边界：白名单为空视为"未配置"，用默认六大领域；非法领域逐个剔除，全非法则回默认。
	if len(a.Bounds.AllowedDomains) == 0 {
		a.Bounds.AllowedDomains = append([]string(nil), dv.Bounds.AllowedDomains...)
	} else {
		valid := make([]string, 0, len(a.Bounds.AllowedDomains))
		for _, dm := range a.Bounds.AllowedDomains {
			for _, ok := range dv.Bounds.AllowedDomains {
				if dm == ok {
					valid = append(valid, dm)
					break
				}
			}
		}
		if len(valid) == 0 {
			valid = append([]string(nil), dv.Bounds.AllowedDomains...)
		}
		a.Bounds.AllowedDomains = valid
	}
	if a.Bounds.MaxTaskWords <= 0 || a.Bounds.MaxTaskWords > 20000 {
		a.Bounds.MaxTaskWords = dv.Bounds.MaxTaskWords
	}
	if a.Bounds.MaxDraftsPerRound <= 0 || a.Bounds.MaxDraftsPerRound > 5 {
		a.Bounds.MaxDraftsPerRound = dv.Bounds.MaxDraftsPerRound
	}

	// 规划：文本为空补默认；被编码破坏的文本也补默认；字数/草案数钳制。
	//
	// 现场缺陷（2026-10-19 实测）：dist 实例的 config.json 里 rootTitle 变成
	// "?????????"、rootIntent 变成一串问号 —— 典型成因是外部客户端用 ASCII
	// 编码调 PUT /api/settings（PowerShell Invoke-RestMethod 传 string body 默认
	// 按 ASCII 发送，中文全部变成 '?'）。这两个字段会被 planner 原样拼进每个
	// 自主任务的 Prompt（"项目核心目标：?????????"），等于把自主决策的锚点废掉，
	// 而系统一声不响继续跑 —— 属于必须拦截的静默失效。
	// 判据：非空，但去掉空白/问号/标点后没有任何字母或 CJK ⇒ 视为损坏，回落默认。
	if a.Plan.RootTitle == "" || corruptedText(a.Plan.RootTitle) {
		a.Plan.RootTitle = dv.Plan.RootTitle
	}
	if a.Plan.RootIntent == "" || corruptedText(a.Plan.RootIntent) {
		a.Plan.RootIntent = dv.Plan.RootIntent
	}
	if a.Plan.DefaultDomain == "" {
		a.Plan.DefaultDomain = dv.Plan.DefaultDomain
	}
	if a.Plan.Words < 100 || a.Plan.Words > 10000 {
		a.Plan.Words = dv.Plan.Words
	}
	if a.Plan.MaxDrafts <= 0 || a.Plan.MaxDrafts > 5 {
		a.Plan.MaxDrafts = dv.Plan.MaxDrafts
	}

	// 风险：质量升级阈值必须在 1-10。
	if a.Risk.EscalateMinQuality < 1 || a.Risk.EscalateMinQuality > 10 {
		a.Risk.EscalateMinQuality = dv.Risk.EscalateMinQuality
	}
}

// corruptedText 判断一段文本是否已被编码破坏（典型形态：整串问号）。
//
// 判据刻意保守，只抓"明显坏掉"的：
//   - 连续 3 个以上问号；或
//   - 去掉空白/问号/中英文标点后**一个字母/CJK 都不剩**（说明原本的文本全丢了）。
//
// 正常中文、英文、中英混排都不会命中；带一个问号的正常句子也不会命中。
func corruptedText(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.Contains(s, "???") {
		return true
	}
	meaningful := 0
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			meaningful++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			meaningful++
		case r >= 0x4E00 && r <= 0x9FFF: // CJK
			meaningful++
		case r >= 0x3400 && r <= 0x4DBF:
			meaningful++
		}
	}
	return meaningful == 0
}

// normalizeWorkspace 钳制工作目录配置。
//
// 关键安全约束：external 模式却缺少合法 root 时**回落项目模式**
// —— 宁可退回兼容行为，也不能让任务在没有确定根的情况下乱写文件。
func (c *Config) normalizeWorkspace() {
	c.Workspace = c.Workspace.Normalized()
	if c.Workspace.Mode == workspace.ModeExternal {
		root := c.Workspace.Root
		if root == "" || !filepath.IsAbs(root) {
			c.Workspace.Mode = workspace.ModeProject
			c.Workspace.Root = ""
		}
	}
}

func docWeights(w EvalWeights) EvalWeights {
	n := struct{ Value, Cost, Feasibility float64 }{w.Value, w.Cost, w.Feasibility}
	v, c, f := n.Value, n.Cost, n.Feasibility
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
		return EvalWeights{Value: 0.5, Cost: 0.2, Feasibility: 0.3}
	}
	return EvalWeights{Value: v / sum, Cost: c / sum, Feasibility: f / sum}
}

// Store 负责配置的读写与内存缓存。
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// NewStore 加载配置；文件不存在时落盘默认配置。
func NewStore(path string) (*Store, error) {
	s := &Store{path: path}
	var cfg Config
	err := ReadJSON(path, &cfg)
	switch {
	case err == nil:
		// 迁移后必须落盘（2026-10-19 现场教训）：
		// Normalize() 里的 schemaVersion 迁移只改内存，若不在启动时写回，
		// 每次重启都会重新迁移但文件永远停在旧值 —— 现场表现为
		// 'schemaVersion 已升到 3，但 appendProtocol 仍是 false、repairRounds 仍是 0'，
		// P3 因此永远不生效。判据：迁移改了任何版本号/字段就写回。
		// 判据不能再逐个字段列举（2026-10-19 第二次踩到）：新增了"损坏文案回落默认"
		// 的分支后，rootTitle/rootIntent 被修好了却因为没有列入比较字段而不落盘，
		// 文件里仍是问号，重启后又修一遍 —— 表现为"内存对了、磁盘永远不对"。
		// 改为**序列化前后整体比较**：只要 Normalize 改了任何字段就写回，
		// 以后再加迁移分支也不会漏。
		before := cfg
		canonicalizeForCompare(&before)
		cfg.Normalize()
		s.cfg = cfg
		after := cfg
		canonicalizeForCompare(&after)
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		if !bytes.Equal(beforeJSON, afterJSON) {
			cfg.UpdatedAt = nowMs()
			s.cfg = cfg
			if perr := s.persistLocked(); perr != nil {
				// 落盘失败不阻断启动：内存里已经是迁移后的值，下次启动会再试。
				return s, nil
			}
		}
	case errors.Is(err, os.ErrNotExist):
		cfg = DefaultConfig()
		cfg.UpdatedAt = nowMs()
		s.cfg = cfg
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	return s, nil
}

// canonicalizeForCompare 把"语义等价但 JSON 形态不同"的差异抹平，用于判断
// Normalize 是否真的改动了什么。
//
// 唯一已知的这类差异是 **nil slice 与空 slice**：默认配置里 AllowedReadRoots 是 nil
// （JSON 为 null），Normalize 会把它变成空 slice（JSON 为 []）。两者语义相同，
// 但序列化字节不同 —— 不做归一化的话每次启动都会认为"配置被改了"而白写一次磁盘，
// 违反"已迁移配置不应无谓重写"。
func canonicalizeForCompare(c *Config) {
	if c.Models == nil {
		c.Models = []ModelRoute{}
	}
	if c.Executor.RetryBackoffMs == nil {
		c.Executor.RetryBackoffMs = []int{}
	}
	if c.Executor.RetryPolicy.RetryableKinds == nil {
		c.Executor.RetryPolicy.RetryableKinds = []string{}
	}
	if c.Workspace.AllowedReadRoots == nil {
		c.Workspace.AllowedReadRoots = []string{}
	}
	if c.Autonomy.Bounds.AllowedDomains == nil {
		c.Autonomy.Bounds.AllowedDomains = []string{}
	}
	// Warnings 不入盘、不参与比较：它是每次 Normalize 重新派生的一次性告警。
	// 不在这里抹平的话，磁盘上（没有该字段）与内存里（跨字段修正产生）
	// 永远存在差异，每次启动都会白写一次磁盘，
	// 违反「已迁移配置不应无谓重写」的老判据。
	c.Warnings = nil
}

// Get 返回配置副本（调用方不可直接改内存状态）。
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.cfg
	cp.Models = append([]ModelRoute(nil), s.cfg.Models...)
	cp.Executor.RetryBackoffMs = append([]int(nil), s.cfg.Executor.RetryBackoffMs...)
	cp.Executor.RetryPolicy.RetryableKinds = append([]string(nil), s.cfg.Executor.RetryPolicy.RetryableKinds...)
	cp.Autonomy.Bounds.AllowedDomains = append([]string(nil), s.cfg.Autonomy.Bounds.AllowedDomains...)
	cp.Workspace.AllowedReadRoots = append([]string(nil), s.cfg.Workspace.AllowedReadRoots...)
	return cp
}

// Update 用 mutator 修改配置并落盘，返回修改后的配置。
func (s *Store) Update(mutator func(*Config)) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mutator(&s.cfg)
	s.cfg.Normalize()
	s.cfg.UpdatedAt = nowMs()
	if err := s.persistLocked(); err != nil {
		return s.cfg, err
	}
	return s.cfg, nil
}

// Replace 整份替换配置（面板保存路径）。
func (s *Store) Replace(cfg Config) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg.SchemaVersion = SchemaVersion
	cfg.Normalize()
	cfg.UpdatedAt = nowMs()
	s.cfg = cfg
	return s.cfg, s.persistLocked()
}

func (s *Store) persistLocked() error {
	return WriteJSONAtomic(s.path, s.cfg)
}

// MarshalConfig 导出 JSON（供面板展示 / 备份）。
func MarshalConfig(c Config) ([]byte, error) { return json.MarshalIndent(c, "", "  ") }

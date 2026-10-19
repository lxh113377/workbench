// Package modelpool 把「所有可用的兼容模型接口」统一纳管成一条可调度队列，
// 提供实时可用性探测、故障转移与全程状态日志（无感切换）。
//
// 与 internal/modelroute 的分工：
//   - modelroute 决定「这一次用谁」（按 tier 的健康加权随机，最终喂给 executor）；
//   - modelpool 决定「池子里有谁、谁还能用」（发现 → 探测 → 降权/禁用/恢复 → 落盘与日志），
//     并把结果投影成 config.ModelRoute 交给 modelroute，二者是「账本」与「选择器」的关系。
//
// 设计对标：Kubernetes EndpointSlice + 健康探针（就绪/存活分离）、Envoy 的 outlier detection
// （连续失败驱逐 + 冷却后半开）、以及 litellm 的路由降级链。全部用标准库实现。
package modelpool

import (
	"strings"
	"time"
)

// State 是一个模型在池子里的可用性状态。
type State string

const (
	StateUnknown  State = "unknown"  // 尚未探测过（新加入的模型）
	StateHealthy  State = "healthy"  // 最近一次探测成功
	StateDegraded State = "degraded" // 能通但慢/偶发失败，仍可调度
	StateDown     State = "down"     // 探测失败，冷却中（不参与调度）
	StateDisabled State = "disabled" // 用户手动停用
)

// Lane 是模型所在的接入车道（对应 OFM 的三条来源 + 自定义）。
type Lane string

const (
	LaneFree      Lane = "free"      // our-free-model 匿名免费车道（模型选择器里的 OFM 分组）
	LaneKilo      Lane = "kilo"      // Kilo 公共免费池
	LaneEAC       Lane = "eac"       // 桌面端 EAC 协付车道
	LaneAggregate Lane = "aggregate" // 聚合渠道（第三方端点 + 自带 Key）
	LaneCustom    Lane = "custom"    // 用户在面板自填的 OpenAI 兼容端点
)

// Format 是上游线协议。OFM 内置网关对外只有 OpenAI Chat Completions，
// 自定义端点可显式声明 anthropic / responses，由探测与转发按需选择路径。
type Format string

const (
	FormatChat      Format = "openai-chat"
	FormatResponses Format = "openai-responses"
	FormatMessages  Format = "anthropic-messages"
)

// Spec 是池子里一个模型的静态定义（可被面板增删改）。
type Spec struct {
	ID            string `json:"id"`            // 全局唯一：车道内模型 id（自定义端点用 <provider>/<model>）
	Label         string `json:"label"`         // 面板显示名
	Provider      string `json:"provider"`      // 供应商标识（our-free-model / kilo / 自定义）
	Lane          Lane   `json:"lane"`
	BaseURL       string `json:"baseUrl"`       // 可空：空表示走 OFM 内置网关（127.0.0.1:8326）
	APIKey        string `json:"apiKey"`        // 只写本机 settings；API 响应只回显指纹
	Model         string `json:"model"`         // 上游模型 id（可能与 ID 不同，如 provider/model）
	Format        Format `json:"format"`
	Tier          string `json:"tier"`          // quality / balanced / fast（透传给 modelroute）
	Group         string `json:"group"`         // 面板分组（OFM / Kilo / EAC / 聚合 / 自定义）
	Priority      int    `json:"priority"`      // 越小越先用（1 = 最高优先级）
	Weight        int    `json:"weight"`        // 同优先级内的调度权重
	Enabled       bool   `json:"enabled"`
	Vision        bool   `json:"vision"`
	ContextWindow int    `json:"contextWindow"`
	MaxOutput     int    `json:"maxOutput"`
	MaxFail       int    `json:"maxFail"`       // 连续失败阈值（达到即冷却）
	CooldownSec   int    `json:"cooldownSec"`   // 基础冷却时长（秒，失败后指数退避）
	Source        string `json:"source"`        // ofm-catalog / ofm-settings / gateway-sync / builtin / user
	Note          string `json:"note"`
	UpdatedAt     int64  `json:"updatedAt"`
	// Routable 表示该模型能被 DSH 的 our-free-model provider 直接调度
	// （网关专属车道只能经 OpenAI 网关调用，不能写进 overlay 的 model 字段）。
	Routable bool `json:"routable"`
	// AutoDisabled 标记「因上游判定不可用而被自动停用」。
	// 与用户手动停用区分开：网关重新广播该模型时应自动恢复启用。
	AutoDisabled bool `json:"autoDisabled"`

	// Runtime 是健康度快照（不落盘，由 Manager 每次写盘后同步），
	// 使调度器只读 Specs 即可判定可用性，避免探测线程与调度线程争同一把锁。
	Runtime *specRuntime `json:"-"`
}

// Health 是一个模型的运行期可用性。
type Health struct {
	ID        string `json:"id"`
	State     State  `json:"state"`
	Success   int    `json:"success"`
	Failure   int    `json:"failure"`
	ConsecFail int   `json:"consecFail"`
	LatencyMs int64  `json:"latencyMs"` // 最近一次探测/调用的耗时（EWMA 由 Prober 维护）
	LastErr     string `json:"lastErr"`
	LastErrKind string `json:"lastErrKind"`
	CheckedAt   int64  `json:"checkedAt"`
	CooldownTo  int64  `json:"cooldownTo"`
	LastUsedAt  int64  `json:"lastUsedAt"`
	ProbeOK     int    `json:"probeOk"`
	ProbeFail   int    `json:"probeFail"`
}

// Available 判断该模型此刻能否被调度（不冷却、非禁用）。
func (h *Health) Available(now int64) bool {
	if h == nil {
		return false
	}
	if h.State == StateDisabled || h.State == StateDown {
		return false
	}
	return h.CooldownTo <= now
}

// file 是池子落盘结构。
type file struct {
	Version   int                `json:"version"`
	UpdatedAt int64              `json:"updatedAt"`
	Specs     []Spec             `json:"specs"`
	Health    map[string]*Health `json:"health"`
	// Strategy 是调度策略。必须落盘：否则 CLI 里 --model-pool strategy 只改内存，
	// 下一个进程（面板或工作台）又用回默认值，"可配置"就成了假象。
	Strategy string `json:"strategy,omitempty"`
}

// logRecord 是一条模型状态日志（JSONL 追加）。
type logRecord struct {
	At        int64  `json:"at"`
	Kind      string `json:"kind"` // probe / switch / success / failure / skip / recover / crud / probe-cycle
	Model     string `json:"model,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	State     string `json:"state,omitempty"`
	Reason    string `json:"reason,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
	Actor     string `json:"actor,omitempty"`
}

// LastState / LastCooldown 是便利字段：让调度器只看 Spec 就能判断可用性，
// 不必来回查 health map（探测线程与调度线程因此不需要共享锁）。
// 由 Manager 在每次 persist/ApplyProbe 时同步写回（见 manager.syncSpecState）。
type specRuntime struct {
	State      State
	CooldownTo int64
}

// HealthState 返回该 Spec 快照里的状态（未同步时视为 unknown）。
func (s Spec) HealthState() State {
	if s.Runtime == nil {
		return StateUnknown
	}
	return s.Runtime.State
}

// HealthCooldown 返回该 Spec 快照里的冷却截止时刻（毫秒）。
func (s Spec) HealthCooldown() int64 {
	if s.Runtime == nil {
		return 0
	}
	return s.Runtime.CooldownTo
}

// LastState / LastCooldown 是便利字段：让调度器只看 Spec 就能判断可用性，
// 不必来回查 health map（探测线程与调度线程因此不需要共享锁）。
// 由 Manager 在每次 persist/ApplyProbe 时同步写回（见 manager.syncSpecState）。

// HealthState 返回该 Spec 快照里的状态（未同步时视为 unknown）。

// HealthCooldown 返回该 Spec 快照里的冷却截止时刻（毫秒）。

func trim(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func nowMs() int64 { return time.Now().UnixMilli() }

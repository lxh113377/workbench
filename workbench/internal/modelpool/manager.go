package modelpool

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"workbench/internal/config"
)

// ── Manager：模型池唯一读写入口 ───────────────────────────────────

// transitionRecord 记录一次「谁切到谁」的故障转移（无感切换的可追溯证据）。
type transitionRecord struct {
	At     int64  `json:"at"`
	TaskID string `json:"taskId,omitempty"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

// Manager 统一管理模型清单、可用性与调度顺序。
//
// 锁边界：所有读写临界区都在 m.mu 内；磁盘写盘与日志追加不持锁（先取快照再写），
// 避免 7x24 运行时探测线程与调度线程互相拖慢。
type Manager struct {
	mu    sync.RWMutex
	specs []Spec
	// order 保存自定义顺序（面板拖动优先级时写入），空表示按 Priority 排序。
	health map[string]*Health
	store  *Store
	// onRoutes 是「池子变化后把新路由表推给引擎」的回调（由 web/main 注入，避免反向依赖）。
	onRoutes func([]config.ModelRoute)
	// log 是最近一次探测摘要（面板展示用，不落盘）。
	lastProbe ProbeSummary
	// gatewayBase 是本机 OFM 网关地址（Discover 得到）。
	gatewayBase string
	cycle       int
	sweepCancel chan struct{}
	sweepOnce   sync.Once
	// pendingLogs 是持锁期间积攒的待写日志（解锁后统一落盘，避免持锁做 IO）。
	pendingLogs []logRecord
	// lastSync 是最近一次网关清单同步的统计。
	lastSync SyncResult
	// gatewayKeySet 标记当前是否拿到了可用的网关密钥。
	gatewayKeySet bool
	// strategy 是调度策略的落盘值（"" 表示未设置，沿用配置默认）。
	// 面板/CLI 的切换必须跨进程生效，否则「可配置」是假的 —— 依据 types.go
	// 里 file.Strategy 的注释与 strategy_persist_test.go 的回归。
	strategy string
	// lastOK 是最近一次**执行成功**的模型（含时刻）。
	// 用途：同一次任务的后续阶段（方案→执行）优先复用刚被证明能产出的模型（T17①）。
	lastOK   string
	lastOKAt int64
}

// NewManager 从磁盘加载（不存在则用 seed 初始化），返回可用实例。
func NewManager(store *Store, seed []Spec) *Manager {
	m := &Manager{store: store, health: map[string]*Health{}}
	f, err := store.Load()
	if f != nil {
		// 先接住已落盘的策略：即便接下来走 seed 分支（清单为空），
		// 也不该丢掉用户上一次的选择。
		m.strategy = f.Strategy
	}
	if err != nil || f == nil || len(f.Specs) == 0 {
		if seed == nil {
			seed = SeedSpecs()
		}
		m.specs = append([]Spec(nil), seed...)
		for i := range m.specs {
			m.specs[i].Enabled = true
			if m.specs[i].UpdatedAt == 0 {
				m.specs[i].UpdatedAt = nowMs()
			}
			m.health[m.specs[i].ID] = &Health{ID: m.specs[i].ID, State: StateUnknown}
		}
		m.mu.Lock()
		m.syncSpecStateLocked()
		m.mu.Unlock()
		m.mu.Lock()
		m.syncSpecStateLocked()
		m.mu.Unlock()
		if seedErr := store.Save(&file{Specs: m.specs, Health: m.health}); seedErr != nil {
			_ = seedErr
		}
		return m
	}
	m.specs = f.Specs
	for i := range m.specs {
		if m.specs[i].MaxFail <= 0 {
			m.specs[i].MaxFail = 3
		}
		if m.specs[i].CooldownSec <= 0 {
			m.specs[i].CooldownSec = 120
		}
	}
	for id, h := range f.Health {
		if h != nil {
			m.health[id] = h
		}
	}
	for _, s := range m.specs {
		if m.health[s.ID] == nil {
			m.health[s.ID] = &Health{ID: s.ID, State: StateUnknown}
		}
		if !s.Enabled && m.health[s.ID].State != StateDown {
			m.health[s.ID].State = StateDisabled
		}
	}
	m.mu.Lock()
	m.syncSpecStateLocked()
	m.mu.Unlock()
	return m
}

// Strategy 返回已落盘的调度策略（"" 表示从未在面板/CLI 切换过，沿用配置默认）。
func (m *Manager) Strategy() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.strategy
}

// SetStrategy 把调度策略写进账本并落盘。
//
// 为什么必须落盘而不是只改内存：工作台是「面板 + CLI + 引擎」多入口结构，
// 每个进程都从 config.json 的 pool.strategy 读初值。用户在面板切到 weighted 后，
// 下一次起 CLI 或重启又回到 priority —— 需求2 的「可配置」就成了假象。
// types.go 里 file.Strategy 的字段注释写的正是这条约束，此前只有字段没有读写路径。
func (m *Manager) SetStrategy(st string) {
	m.mu.Lock()
	if m.strategy == st {
		m.mu.Unlock()
		return
	}
	m.strategy = st
	m.mu.Unlock()
	m.persist()
}

// SetRouteSink 注入「路由表变更」回调（引擎热更新）。
func (m *Manager) SetRouteSink(fn func([]config.ModelRoute)) {
	m.mu.Lock()
	m.onRoutes = fn
	m.mu.Unlock()
	m.NotifyRoutes()
}

// SetGatewayBase 记录本机网关地址（Discover 的产物）。
func (m *Manager) SetGatewayBase(v string) {
	m.mu.Lock()
	m.gatewayBase = v
	m.mu.Unlock()
}

// GatewayBase 返回本机网关地址。
func (m *Manager) GatewayBase() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gatewayBase
}

// Specs 返回全部模型定义（按调度顺序：优先级升序 + 权重降序 + id）。
// 返回值带 Runtime 快照，调度器与面板可直接据此判断可用性。
func (m *Manager) Specs() []Spec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]Spec(nil), m.specs...)
	m.syncRuntime(out)
	sortSpecs(out)
	return out
}

// syncRuntime 为 Spec 副本补 Runtime（不持锁时的只读投影）。
func (m *Manager) syncRuntime(specs []Spec) {
	now := nowMs()
	for i := range specs {
		id := specs[i].ID
		h := m.health[id]
		rt := &specRuntime{State: StateUnknown}
		if h != nil {
			rt.State = h.State
			rt.CooldownTo = h.CooldownTo
			if !specs[i].Enabled {
				rt.State = StateDisabled
			} else if h.CooldownTo > now && h.State != StateDown {
				rt.State = StateDown
			}
		}
		specs[i].Runtime = rt
	}
}

// HealthSnapshot 返回全部健康度（按调度顺序，保证面板与调度器同序）。
func (m *Manager) HealthSnapshot() []Health {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Health, 0, len(m.health))
	for _, s := range m.specs {
		if h, ok := m.health[s.ID]; ok && h != nil {
			cp := *h
			out = append(out, cp)
		}
	}
	return out
}

// Get 返回单个模型定义与健康度。
func (m *Manager) Get(id string) (Spec, Health, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.specs {
		if s.ID == id {
			h := Health{ID: id}
			if cur, ok := m.health[id]; ok && cur != nil {
				h = *cur
			}
			return s, h, true
		}
	}
	return Spec{}, Health{}, false
}

// ── 增删改 ──────────────────────────────────────────────────────

// Upsert 新增或更新一个模型定义（面板「添加/编辑」入口）。
func (m *Manager) Upsert(s Spec, actor string) (Spec, error) {
	s.ID = strings.TrimSpace(s.ID)
	if s.ID == "" {
		return Spec{}, fmt.Errorf("模型 id 不能为空")
	}
	// 注意：OFM 的模型 id 天然带斜杠（z-ai/glm-5.3、opencode/xxx-free），
	// 因此只拒绝空白、控制字符与查询符号；斜杠/冒号是合法字符。
	if strings.ContainsAny(s.ID, " \t\r\n?#") {
		return Spec{}, fmt.Errorf("模型 id 不能包含空白或 ? # 等查询符号")
	}
	if s.Model = firstNonEmpty(strings.TrimSpace(s.Model), s.ID); s.Model == "" {
		return Spec{}, fmt.Errorf("上游模型名不能为空")
	}
	if s.BaseURL = strings.TrimSpace(s.BaseURL); s.BaseURL != "" {
		u, err := url.Parse(s.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Spec{}, fmt.Errorf("BaseURL 必须是 http(s) 绝对地址")
		}
	}
	if s.Format == "" {
		s.Format = FormatChat
	}
	switch s.Format {
	case FormatChat, FormatResponses, FormatMessages:
	default:
		return Spec{}, fmt.Errorf("不支持的线协议: %s", s.Format)
	}
	if s.Tier == "" {
		s.Tier = "balanced"
	}
	switch s.Tier {
	case "quality", "balanced", "fast":
	default:
		return Spec{}, fmt.Errorf("tier 只能是 quality/balanced/fast")
	}
	if s.Lane == "" {
		s.Lane = LaneCustom
	}
	if s.Group == "" {
		s.Group = "自定义"
	}
	if s.Provider == "" {
		s.Provider = "custom"
	}
	if s.Priority <= 0 {
		s.Priority = 100
	}
	if s.Weight <= 0 {
		s.Weight = 50
	}
	if s.MaxFail <= 0 {
		s.MaxFail = 3
	}
	if s.CooldownSec <= 0 {
		s.CooldownSec = 120
	}
	s.Source = firstNonEmpty(s.Source, "user")
	s.UpdatedAt = nowMs()

	m.mu.Lock()
	found := false
	for i := range m.specs {
		if m.specs[i].ID == s.ID {
			// 允许留空 APIKey 表示「不修改」，避免面板回写时把密钥清掉。
			if s.APIKey == "" {
				s.APIKey = m.specs[i].APIKey
			}
			// 保留创建来源，避免每次编辑都变成 user。
			if s.Source == "user" && m.specs[i].Source != "" {
				s.Source = m.specs[i].Source
			}
			m.specs[i] = s
			found = true
			break
		}
	}
	if !found {
		m.specs = append(m.specs, s)
	}
	if m.health[s.ID] == nil {
		m.health[s.ID] = &Health{ID: s.ID, State: StateUnknown}
	}
	if !s.Enabled {
		m.health[s.ID].State = StateDisabled
	} else if m.health[s.ID].State == StateDisabled {
		m.health[s.ID].State = StateUnknown
	}
	m.mu.Unlock()

	m.persist()
	m.log(logRecord{Kind: "crud", Model: s.ID, State: "upsert", Reason: fmt.Sprintf("优先级 %d / 权重 %d / 启用 %v", s.Priority, s.Weight, s.Enabled), Actor: actor})
	m.NotifyRoutes()
	return s, nil
}

// Delete 删除一个模型（面板「删除」入口）。
func (m *Manager) Delete(id, actor string) error {
	m.mu.Lock()
	idx := -1
	for i := range m.specs {
		if m.specs[i].ID == id {
			idx = i
			break
		}
	}
	var removed Spec
	if idx >= 0 {
		removed = m.specs[idx]
		m.specs = append(m.specs[:idx], m.specs[idx+1:]...)
		delete(m.health, id)
	}
	m.mu.Unlock()
	if idx < 0 {
		return fmt.Errorf("模型不存在: %s", id)
	}
	m.persist()
	m.log(logRecord{Kind: "crud", Model: id, State: "delete", Reason: "面板删除", Actor: actor})
	m.NotifyRoutes()
	_ = removed
	return nil
}

// SetEnabled 启用/停用一个模型。
func (m *Manager) SetEnabled(id string, enabled bool, actor string) error {
	m.mu.Lock()
	found := false
	for i := range m.specs {
		if m.specs[i].ID == id {
			m.specs[i].Enabled = enabled
			m.specs[i].UpdatedAt = nowMs()
			found = true
			break
		}
	}
	if found {
		h := m.health[id]
		if h == nil {
			h = &Health{ID: id}
			m.health[id] = h
		}
		if enabled {
			if h.State == StateDisabled {
				h.State = StateUnknown
			}
			h.CooldownTo = 0
			h.ConsecFail = 0
		} else {
			h.State = StateDisabled
		}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("模型不存在: %s", id)
	}
	m.persist()
	state := "enable"
	if !enabled {
		state = "disable"
	}
	m.log(logRecord{Kind: "crud", Model: id, State: state, Actor: actor})
	m.NotifyRoutes()
	return nil
}

// Reorder 调整优先级（面板拖动/上下移）。ids 按「最高优先级在前」排序。
func (m *Manager) Reorder(ids []string, actor string) error {
	m.mu.Lock()
	index := map[string]int{}
	for i, id := range ids {
		index[id] = i + 1
	}
	changed := 0
	for i := range m.specs {
		if p, ok := index[m.specs[i].ID]; ok && m.specs[i].Priority != p {
			m.specs[i].Priority = p
			m.specs[i].UpdatedAt = nowMs()
			changed++
		}
	}
	m.mu.Unlock()
	if changed == 0 {
		return nil
	}
	m.persist()
	m.log(logRecord{Kind: "crud", State: "reorder", Reason: fmt.Sprintf("已调整 %d 条优先级", changed), Actor: actor})
	m.NotifyRoutes()
	return nil
}

// Restore 把模型从冷却/下线状态恢复为未知（面板「重置」按钮）。
func (m *Manager) Restore(id, actor string) error {
	m.mu.Lock()
	found := false
	for i := range m.specs {
		if m.specs[i].ID == id {
			m.specs[i].Enabled = true
			found = true
			break
		}
	}
	if found {
		m.health[id] = &Health{ID: id, State: StateUnknown}
	}
	m.mu.Unlock()
	if !found {
		return fmt.Errorf("模型不存在: %s", id)
	}
	m.persist()
	m.log(logRecord{Kind: "recover", Model: id, State: "restore", Reason: "手动重置冷却与计数", Actor: actor})
	m.NotifyRoutes()
	return nil
}

// ── 可用性反馈（供调度/执行侧回报） ──────────────────────────────

// MarkSuccess 记录一次成功：清空连续失败、回到健康和可用。
func (m *Manager) MarkSuccess(id string, latencyMs int64) {
	m.mu.Lock()
	var recovered bool
	if h := m.ensureLocked(id); h != nil {
		h.Success++
		h.ConsecFail = 0
		h.State = StateHealthy
		h.CooldownTo = 0
		// 真实执行成功是清掉出口级印记（not_served）的唯一自动路径；不清则车道虽已恢复，
		// 旧印记会让下一次无关失败仍被当成出口级事实而永远无法半开（守卫读到陈旧 LastErrKind）。
		h.LastErr = ""
		h.LastErrKind = ""
		h.CheckedAt = nowMs()
		h.LastUsedAt = nowMs()
		if latencyMs > 0 {
			if h.LatencyMs == 0 {
				h.LatencyMs = latencyMs
			} else {
				h.LatencyMs = (h.LatencyMs*2 + latencyMs) / 3
			}
		}
		m.lastOK = id
		m.lastOKAt = nowMs()
		recovered = h.ProbeFail > 0 || h.ConsecFail > 0
	}
	m.mu.Unlock()
	m.persist()
	_ = recovered
}

// LastOKModel 返回最近一次**执行成功**的模型（id, 时刻, 是否存在）。
//
// 为什么需要（round 19 里程碑的下一步 T17①）：计划阶段在 `dots-studio/…` 上第一次产出成功，
// 紧接着的执行阶段却按池子的加权顺序从头试最弱车道（nemotron→boot_failed、glm-5.3→早回收），
// 白烧约 2.5 分钟才可能轮到刚证明可用的那个模型。FailoverRunner 用它把「刚成功的模型」排到链首。
func (m *Manager) LastOKModel() (string, int64, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastOK == "" {
		return "", 0, false
	}
	return m.lastOK, m.lastOKAt, true
}

// MarkFailure 记录一次失败：连续失败达到阈值即冷却并禁用调度。
func (m *Manager) MarkFailure(id, kind, msg string) {
	m.mu.Lock()
	var trip bool
	var cooldown int64
	var consec int
	var maxFail int
	if h := m.ensureLocked(id); h != nil {
		h.Failure++
		h.ConsecFail++
		h.LastErr = trim(kind+": "+msg, 300)
		h.LastErrKind = kind
		h.CheckedAt = nowMs()
		h.LastUsedAt = nowMs()
		consec = h.ConsecFail
		maxFail = 3
		cooldownSec := 120
		for i := range m.specs {
			if m.specs[i].ID == id {
				if m.specs[i].MaxFail > 0 {
					maxFail = m.specs[i].MaxFail
				}
				if m.specs[i].CooldownSec > 0 {
					cooldownSec = m.specs[i].CooldownSec
				}
				break
			}
		}
		// 能力性失败（协议不合规）**一击即冷却**：它不是偶发抖动，而是「这条车道不认产物协议」——
		// 反复试它只会重复被判 no_artifact（round 22 实测同一次尝试内连续 3 条车道因此被拒；round 25 窗口内仍以它为主）。
		// 阈值压到 1，并给比偶发失败更长的冷却，让它尽快退出执行阶段候选、把机会让给可能合规的车道（T18⑦）。
		// 一击即冷却与结构性长冷却统一查 laneDispositions（round 125 · T80）。
		//
		// 收敛前的形态是三处裸字符串判定：no_artifact⇒maxFail1+600s、not_served⇒maxFail1+1800s、
		// 四个结构性 kind（model_unavailable/missing_credential/unreachable/bad_endpoint）⇒长冷却；
		// 而 exec_health.go 里另有一份「探测不得翻案」的列表 —— 同一契约多处实现，本会话已因此出过 3 次线上事故
		// （新增或改名的类别只在一处登记，另一处判定**静默失效**，面板上看不出来）。
		// 现在唯一事实源是那张表；lane_disposition_test 的总覆盖守卫负责「executor 加了 Kind 而这里没决策 ⇒ 判红」。
		// 逐条等价的处置理由（含 600/1800 的量级依据）写在 laneDispositions 的 note 字段里。
		if one, minSec := laneOneStrike(kind); one {
			maxFail = 1
			if minSec > 0 && cooldownSec < minSec {
				cooldownSec = minSec
			}
		}
		long := laneLongCooldown(kind)
		if consec >= maxFail || long {
			mult := consec - maxFail
			if mult < 0 {
				mult = 0
			}
			if mult > 5 {
				mult = 5
			}
			sec := int64(cooldownSec) << uint(mult)
			if sec > 3600 {
				sec = 3600
			}
			h.CooldownTo = nowMs() + sec*1000
			h.State = StateDown
			trip = true
			cooldown = sec
		} else {
			h.State = StateDegraded
		}
	}
	m.mu.Unlock()
	m.persist()
	if trip {
		m.log(logRecord{Kind: "failure", Model: id, State: string(StateDown), Reason: fmt.Sprintf("连续失败 %d 次（阈值 %d），冷却 %d 秒后自动恢复探测", consec, maxFail, cooldown)})
	} else {
		m.log(logRecord{Kind: "failure", Model: id, State: string(StateDegraded), Reason: trim(kind+": "+msg, 200)})
	}
	m.NotifyRoutes()
}

func (m *Manager) ensureLocked(id string) *Health {
	if id == "" {
		return nil
	}
	if h, ok := m.health[id]; ok && h != nil {
		return h
	}
	m.health[id] = &Health{ID: id, State: StateUnknown}
	return m.health[id]
}

// ── 探测 → 状态机 → 日志 ────────────────────────────────────────

// healHalfOpenLocked 把「冷却已到期」的 down 模型放回半开态（unknown），
// 让调度器重新试它一次（等价于 Envoy 的 outlier detection 半开探测）。
// 没有这个动作，模型会永远停在 down，只能靠外部探测把它救回来。
func (m *Manager) healHalfOpenLocked(now int64) {
	for id, h := range m.health {
		if h == nil {
			continue
		}
		if h.State != StateDown || h.CooldownTo <= 0 || h.CooldownTo > now {
			continue
		}
		// 出口级事实（not_served）**不半开**：上游原文写明「本出口不服务该模型」，
		// 冷却到期只说明「等够了」，不说明「现在能用了」。若在这里把它放回 unknown 并清掉
		// LastErrKind，下一次探测就会因「没有出口级印记」而把它标回 healthy，
		// 于是 down(冷却) -> healthy(探测) -> boot_failed(白跑) -> down 的循环继续吃掉整池尝试
		// （round 51 实测：combo:auto-vision 11:45:37 recover -> 11:45:39 probe healthy -> 11:50 boot_failed）。
		// 清除这条印记只允许两条路径：一次**真实执行成功**（MarkSuccess）或手动重置（Restore）。
		if h.LastErrKind == notServedKind {
			continue
		}
		h.State = StateUnknown
		h.CooldownTo = 0
		h.ConsecFail = 0
		h.LastErr = ""
		h.LastErrKind = ""
		if m.pendingLogs == nil {
			m.pendingLogs = make([]logRecord, 0, 4)
		}
		m.pendingLogs = append(m.pendingLogs, logRecord{
			Kind: "recover", Model: id, State: string(StateUnknown),
			Reason: "冷却到期，半开恢复（下一次调用验证）",
		})
	}
}

// healHalfOpenLocked 把「冷却已到期」的 down 模型放回半开态（unknown），
// 让调度器重新试它一次（等价于 Envoy 的 outlier detection 半开探测）。
// 没有这个动作，模型会永远停在 down，只能靠外部探测把它救回来。

// ApplyProbe 把一轮探测结果并入健康度：
//   - 成功：healthy + 解除冷却（恢复会自动回到调度队列）；
//   - 失败：累计连续失败，达阈值转 down（随后按冷却时间自动重新探测）；
//   - 本机网关未开启（gateway_off）：保持 unknown，不误杀模型。
func (m *Manager) ApplyProbe(results []ProbeResult) ProbeSummary {
	summary := Summarize(results)
	now := nowMs()
	m.mu.Lock()
	for _, r := range results {
		h := m.ensureLocked(r.Model)
		if h == nil {
			continue
		}
		was := h.State
		switch r.State {
		case StateHealthy:
			h.ProbeOK++
			// 探测成功只证明「端点可达」，不等于「模型会产出」（round 15 实测：零产出模型的
			// 执行侧连续失败每轮被探测抹掉 ⇒ 首个路由永远落在它身上）。**执行侧失败**与
			// **出口级事实**（上游不服务该模型）都只能由 MarkSuccess（真实执行成功）或
			// 冷却到期的半开恢复清除；结构性故障（不可达/缺凭据/端点坏）仍由探测恢复（对照组测试钉住）。
			//
			// round 43 独立审计铁证：这里原先**无条件** `h.State = StateHealthy` ⇒
			// `combo:auto-*` 的 state 每 ~66 秒被探测从 down 写成 healthy，而 lastErrKind 一直是
			// `not_served`、consecFail 已 39/77/91 ⇒ 每个任务仍要白试 3 条（boot_failed 占全部尝试 71%）。
			if h.State == StateDisabled {
				// 用户手动停用的车道保持 disabled：探测只证明端点可达，不能把它翻回 healthy
				// （round 51 实测：14:20:11 面板停用后，同一毫秒的探测写出 disabled -> healthy 假象）。
			} else if isExecutionFailureKind(h.LastErrKind) {
				// 保持原状态（down/degraded）与计数：**探测不得翻案**。
			} else {
				h.State = StateHealthy
				h.ConsecFail = 0
				h.Success++
				h.CooldownTo = 0
				h.LastErr = ""
				h.LastErrKind = ""
			}
			if r.LatencyMs > 0 {
				h.LatencyMs = r.LatencyMs
			}
		case StateDown:
			h.ProbeFail++
			h.Failure++
			h.ConsecFail++
			h.State = StateDown
			h.LastErr = trim(r.Error, 300)
			h.LastErrKind = r.Kind
			sec := int64(60)
			for i := range m.specs {
				if m.specs[i].ID == r.Model && m.specs[i].CooldownSec > 0 {
					sec = int64(m.specs[i].CooldownSec)
					break
				}
			}
			backoff := h.ProbeFail
			if backoff > 4 {
				backoff = 4
			}
			sec <<= uint(backoff)
			if sec > 1800 {
				sec = 1800
			}
			h.CooldownTo = now + sec*1000
		case StateDegraded:
			h.ProbeFail++
			h.ConsecFail++
			h.State = StateDegraded
			h.LastErr = trim(r.Error, 300)
			h.LastErrKind = r.Kind
		default:
			// unknown：只在历史上没成功过时保留 unknown，避免把已健康的模型打回未知。
			if was == "" || was == StateUnknown {
				h.State = StateUnknown
			}
		}
		h.CheckedAt = now
		if r.LatencyMs > 0 {
			if h.LatencyMs == 0 {
				h.LatencyMs = r.LatencyMs
			} else {
				h.LatencyMs = (h.LatencyMs*3 + r.LatencyMs) / 4
			}
		}
		if was != h.State && r.Model != "" {
			m.appendSwitchLocked(logRecord{
				Kind: "probe", Model: r.Model, From: string(was), To: string(h.State),
				Reason: firstNonEmpty(trim(r.Error, 160), "探测状态变化"), LatencyMs: r.LatencyMs,
			})
		}
	}
	m.lastProbe = summary
	m.cycle++
	m.syncSpecStateLocked()
	// 出口拓扑在**持锁期间**算好（721 行那次 m.log 已在 Unlock 之后，届时不能再裸读 m.specs）。
	// 为什么要记它（round 132 · P0-2 更正）：实测本机池内 38 条车道的 baseUrl 全部是
	// http://127.0.0.1:8326/v1 —— 探测路径上是同一个回环端点。原先这个事实只写在 failover.go 的注释里，
	// 而那条注释的前提（“各车道走不同网络路径”）未经核实 ⇒ 把能算的东西写成不可核对的断言。
	// 注意执行面另有一条路（dsh 子进程自己选上游，见 egress.go 的适用面），
	// 所以 transport_failed 该逐车道冷却还是整池熔断，要先取证执行面的占比（T86），不在此处下结论。
	// 能算的就别靠注释说：把端点数放进每轮都有的低频摘要里，拓扑一旦变化立刻可见。
	// 措辞刻意写「探测面 … → 网关端点」而不是「共用一个出口」（round 133 · T87）：
	// 上一轮我自己就被"共用一个出口"这种写法误导过 —— 它读起来像"整机只有一条出网路",
	// 而 egressKey 算的只是 spec.baseUrl 那一面；执行面由 dsh 子进程自连上游（见 egress.go 的适用面声明）。
	// 文案要标明自己属于哪一面，否则下一个读日志的人会重犯我的错。
	specCount := len(m.specs)
	egressCount := len(egressGroups(m.specs, m.gatewayBase))
	pending := m.drainPendingLocked()
	m.mu.Unlock()
	for _, rec := range pending {
		m.log(rec)
	}
	m.persist()
	m.log(logRecord{Kind: "probe-cycle", Reason: fmt.Sprintf("本轮探测 %d 个：可用 %d / 下线 %d / 未知 %d ｜探测面 %d 条车道 → %d 个网关端点", summary.Total, summary.OK, summary.Down, summary.Unknown, specCount, egressCount)})
	m.NotifyRoutes()
	return summary
}

// syncSpecStateLocked 把 health 快照同步进每个 Spec 的 Runtime 字段。
// 调用方必须持写锁。
func (m *Manager) syncSpecStateLocked() {
	now := nowMs()
	for i := range m.specs {
		id := m.specs[i].ID
		h := m.health[id]
		rt := &specRuntime{State: StateUnknown}
		if h != nil {
			rt.State = h.State
			rt.CooldownTo = h.CooldownTo
			if !m.specs[i].Enabled {
				rt.State = StateDisabled
			} else if h.CooldownTo > now && h.State != StateDown {
				rt.State = StateDown
			}
		}
		m.specs[i].Runtime = rt
	}
}

// drainPendingLocked 取出积攒的日志并清空（调用方持锁）。
func (m *Manager) drainPendingLocked() []logRecord {
	if len(m.pendingLogs) == 0 {
		return nil
	}
	out := m.pendingLogs
	m.pendingLogs = nil
	return out
}

// AppendSwitch 记录一次运行期故障转移（executor 失败 → 换模型）。
func (m *Manager) AppendSwitch(rec transitionRecord) {
	if rec.At == 0 {
		rec.At = nowMs()
	}
	// TaskID 必须转发（2026-10-19 现场缺陷）：调用方已提供，但这里曾漏传，
	// 于是 switch 日志无法归属到任务，排查时重建不出切换链。
	m.log(logRecord{Kind: "switch", TaskID: rec.TaskID, Model: rec.To, From: rec.From, To: rec.To, Reason: rec.Reason})
}

// LastProbe 返回最近一次探测摘要。
func (m *Manager) LastProbe() ProbeSummary {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastProbe
}

// ── 投影成 modelroute 路由表 ────────────────────────────────────

// Routes 把池子投影成 config.ModelRoute（喂给既有调度器）。
// 只有「启用且非 down」的模型进入路由表；enabled 与健康状态分离，便于面板停用而不丢配置。
func (m *Manager) Routes() []config.ModelRoute {
	// 先做一次半开恢复：冷却到期的模型必须重新进入候选，
	// 否则一次误判会让模型永久缺席（现场 6 个模型同时长冷却导致整池停摆）。
	m.mu.Lock()
	before := len(m.pendingLogs)
	m.healHalfOpenLocked(nowMs())
	pending := m.drainPendingLocked()
	_ = before
	m.mu.Unlock()
	for _, rec := range pending {
		m.log(rec)
	}
	snapshot := m.HealthSnapshot()
	order := map[string]Health{}
	for _, h := range snapshot {
		order[h.ID] = h
	}
	m.mu.RLock()
	specs := append([]Spec(nil), m.specs...)
	m.mu.RUnlock()
	sortSpecs(specs)
	now := nowMs()
	out := make([]config.ModelRoute, 0, len(specs))
	for _, s := range specs {
		if !s.Enabled || !s.Routable {
			continue
		}
		h := order[s.ID]
		if h.State == StateDown || h.CooldownTo > now {
			continue
		}
		// Provider 统一写 our-free-model：DSH 的 overlay 只认这个 provider id，
		// 其它值（kilo / gateway / custom）写进去会被执行器判成「未知 provider」。
		out = append(out, config.ModelRoute{
			Model: s.ID, Provider: "our-free-model",
			// Lane 必须传下去（2026-10-19 现场教训）：三条车道在 DSH 里同属
			// provider "our-free-model"，但上游线路完全独立。不传 lane，
			// engine 侧就无法只冷却出问题的车道，只能整池停摆。
			Lane: string(s.Lane),
			Tier: s.Tier, Enabled: true, Weight: s.Weight,
			MaxFail: s.MaxFail, CooldownM: maxInt(1, s.CooldownSec/60),
		})
	}
	return out
}

// NotifyRoutes 把最新路由表推给引擎（无回调时静默）。
func (m *Manager) NotifyRoutes() {
	routes := m.Routes()
	m.mu.RLock()
	sink := m.onRoutes
	m.mu.RUnlock()
	if sink != nil {
		sink(routes)
	}
}

// ── 持久化与日志 ────────────────────────────────────────────────

func (m *Manager) persist() {
	m.mu.RLock()
	f := &file{Specs: append([]Spec(nil), m.specs...), Health: map[string]*Health{}, Strategy: m.strategy}
	for id, h := range m.health {
		if h == nil {
			continue
		}
		cp := *h
		f.Health[id] = &cp
	}
	store := m.store
	m.mu.RUnlock()
	if store != nil {
		_ = store.Save(f)
	}
}

func (m *Manager) log(rec logRecord) {
	m.mu.RLock()
	store := m.store
	m.mu.RUnlock()
	if rec.At == 0 {
		rec.At = nowMs()
	}
	if store != nil {
		store.AppendLog(rec)
	}
}

// appendSwitchLocked 在持锁状态下构造切换记录（避免递归加锁）。
func (m *Manager) appendSwitchLocked(rec logRecord) {
	if rec.At == 0 {
		rec.At = nowMs()
	}
	// 这里只入内存队列，实际写盘由调用方解锁后的 log() 统一处理。
	if m.pendingLogs == nil {
		m.pendingLogs = make([]logRecord, 0, 8)
	}
	m.pendingLogs = append(m.pendingLogs, rec)
}

type netDialer struct{}

// Dial 建立一个 TCP 连接（用于判断本机网关端口是否在监听）。
func (d *netDialer) Dial(addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 800*time.Millisecond)
}

// ── 小工具 ──────────────────────────────────────────────────────

func sortSpecs(in []Spec) {
	sort.SliceStable(in, func(i, j int) bool {
		a, b := in[i], in[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		return a.ID < b.ID
	})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// jsonUnmarshal 是 json.Unmarshal 的薄封装（集中错误信息，便于日志定位）。
func jsonUnmarshal(b []byte, v any) error {
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return nil
}

var _ = errors.Is

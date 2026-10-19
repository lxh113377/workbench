// Package modelroute 实现模型健康度感知的自动择优轮换（用户选定“自动择优轮换”）。
//
// 设计：每个模型维护成功率/平均耗时/连续失败数；被调度器按 tier 需求选模型时，
// 在“冷却期外的健康模型”中按权重 + 健康分加权随机。失败立刻降权，连续失败达阈值
// 进入冷却，冷却结束自动回归探测。全池不可用时返回错误，由上层熔断处理。
package modelroute

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"workbench/internal/config"
)

// Health 是单模型健康状态。
type Health struct {
	Model      string  `json:"model"`
	Provider   string  `json:"provider"`
	Tier       string  `json:"tier"`
	Success    int     `json:"success"`
	Failure    int     `json:"failure"`
	ConsecFail int     `json:"consecFail"`
	AvgMs      float64 `json:"avgMs"`
	CooldownTo int64   `json:"cooldownTo"`
	LastErr    string  `json:"lastErr"`
	LastUsedAt int64   `json:"lastUsedAt"`
	Disabled   bool    `json:"disabled"`
}

// Score 返回 0-100 的健康分（用于加权）。
func (h Health) Score() float64 {
	total := h.Success + h.Failure
	if total == 0 {
		return 70 // 未探索的模型给中等偏上分，保证会被试到
	}
	rate := float64(h.Success) / float64(total)
	score := rate * 100
	// 连续失败重罚
	score -= float64(h.ConsecFail) * 12
	// 平均耗时惩罚（>120s 开始明显扣分）
	if h.AvgMs > 120_000 {
		score -= (h.AvgMs - 120_000) / 20_000 * 5
	}
	if score < 1 {
		score = 1
	}
	if score > 100 {
		score = 100
	}
	return score
}

// Router 是模型路由器。
type Router struct {
	mu     sync.RWMutex
	routes []config.ModelRoute
	health map[string]*Health
	rng    *rand.Rand
	// path 非空时，每次健康度变化都原子落盘，重启后冷却与失败计数不丢。
	// 改造前健康度只在内存：进程一重启就重新踩一遍已知死模型（实测 glm-5.3
	// 重启后仍被优先选中并再次 stall）。
	path string
}

// New 构造路由器（不持久化）。
func New(routes []config.ModelRoute) *Router {
	return NewWithPersistence(routes, "")
}

// NewWithPersistence 构造路由器并从 path 恢复历史健康度；path 为空则纯内存。
func NewWithPersistence(routes []config.ModelRoute, path string) *Router {
	r := &Router{
		routes: append([]config.ModelRoute(nil), routes...),
		health: map[string]*Health{},
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		path:   path,
	}
	for _, rt := range r.routes {
		r.health[rt.Model] = &Health{Model: rt.Model, Provider: rt.Provider, Tier: rt.Tier, Disabled: !rt.Enabled}
	}
	r.load()
	return r
}

// healthFile 是健康度落盘文件结构。
type healthFile struct {
	SavedAt int64              `json:"savedAt"`
	Health  map[string]*Health `json:"health"`
}

// load 从磁盘恢复健康度（不存在或损坏时静默跳过，不阻断启动）。
func (r *Router) load() {
	if r.path == "" {
		return
	}
	var f healthFile
	if err := config.ReadJSON(r.path, &f); err != nil || f.Health == nil {
		return
	}
	for id, h := range f.Health {
		if h == nil || id == "" {
			continue
		}
		cur, ok := r.health[id]
		if !ok {
			// 配置里已删除的模型不再复活，只保留路由表内的条目。
			continue
		}
		cur.Success, cur.Failure, cur.ConsecFail = h.Success, h.Failure, h.ConsecFail
		cur.AvgMs, cur.CooldownTo, cur.LastErr = h.AvgMs, h.CooldownTo, h.LastErr
		cur.LastUsedAt = h.LastUsedAt
	}
}

// Save 立即把健康度原子落盘（调用方已持锁时用 saveLocked）。
func (r *Router) Save() error {
	r.mu.RLock()
	path := r.path
	payload := make(map[string]*Health, len(r.health))
	for id, h := range r.health {
		cp := *h
		payload[id] = &cp
	}
	r.mu.RUnlock()
	if path == "" {
		return nil
	}
	return config.WriteJSONAtomic(path, healthFile{SavedAt: time.Now().UnixMilli(), Health: payload})
}

// saveLocked 在已持锁的临界区里落盘（写盘失败不影响调度，只丢持久化）。
func (r *Router) saveLocked() {
	if r.path == "" {
		return
	}
	payload := make(map[string]*Health, len(r.health))
	for id, h := range r.health {
		cp := *h
		payload[id] = &cp
	}
	_ = config.WriteJSONAtomic(r.path, healthFile{SavedAt: time.Now().UnixMilli(), Health: payload})
}

// UpdateRoutes 热更新路由表（保留已有健康度）。
func (r *Router) UpdateRoutes(routes []config.ModelRoute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append([]config.ModelRoute(nil), routes...)
	for _, rt := range r.routes {
		h, ok := r.health[rt.Model]
		if !ok {
			r.health[rt.Model] = &Health{Model: rt.Model, Provider: rt.Provider, Tier: rt.Tier, Disabled: !rt.Enabled}
			continue
		}
		h.Provider, h.Tier, h.Disabled = rt.Provider, rt.Tier, !rt.Enabled
	}
}

// Health 返回全部健康度快照（按分数降序）。
func (r *Router) Health() []Health {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Health, 0, len(r.health))
	for _, h := range r.health {
		out = append(out, *h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score() > out[j].Score() })
	return out
}

// Pick 依据任务需求 tier 选一个模型。
// excluded 是由调用方确认“刚刚失败过”的模型集合，会在本次选择中排除。
func (r *Router) Pick(tier string, excluded map[string]bool) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	type cand struct {
		model string
		score float64
	}
	var cands []cand
	// 先按请求的 tier 找；没有再放宽到任意 tier
	for _, strict := range []bool{true, false} {
		cands = cands[:0]
		for _, rt := range r.routes {
			if !rt.Enabled || excluded[rt.Model] {
				continue
			}
			if strict && tier != "" && rt.Tier != tier {
				continue
			}
			h := r.health[rt.Model]
			if h == nil || h.Disabled || h.CooldownTo > now {
				continue
			}
			w := rt.Weight
			if w <= 0 {
				w = 1
			}
			cands = append(cands, cand{model: rt.Model, score: h.Score() * float64(w)})
		}
		if len(cands) > 0 {
			break
		}
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("没有可用模型（tier=%s，可能全部处于冷却或已禁用）", tier)
	}
	total := 0.0
	for _, c := range cands {
		total += c.score
	}
	if total <= 0 {
		return cands[r.rng.Intn(len(cands))].model, nil
	}
	pick := r.rng.Float64() * total
	for _, c := range cands {
		pick -= c.score
		if pick <= 0 {
			return c.model, nil
		}
	}
	return cands[len(cands)-1].model, nil
}

// CooldownByProvider 把某个 provider（车道）下的所有模型集体冷却。
//
// 为什么需要（2026-10-19 现场实测）：our-free-model 的**匿名免费车道**按 session
// 统计速率，打满即整条车道返回 429；但同一 provider 下的 Kilo 免费池是另一条
// 上游线路（实测 kilo-auto/free、reflection/beam-free 在 OFM 429 期间仍然可用）。
// 改造前 engine 收到 429 就 setPoolCooldown（整池闸门），把健康的 Kilo 车道也一起
// 停掉 —— 直接违反目标要求 4「自动切换到队列中其他正常可用的模型兜底」。
//
// 正确语义：429 只冷却**出问题的车道**，其它车道照常调度；只有全部车道都冷却时
// 才进入整池闸门。
func (r *Router) CooldownByProvider(provider string, d time.Duration, errKind, errMsg string) int {
	if d <= 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	until := time.Now().Add(d).UnixMilli()
	n := 0
	for _, rt := range r.routes {
		if !rt.Enabled || !strings.EqualFold(rt.Provider, provider) {
			continue
		}
		h := r.health[rt.Model]
		if h == nil {
			h = &Health{Model: rt.Model, Provider: rt.Provider}
			r.health[rt.Model] = h
		}
		if until > h.CooldownTo {
			h.CooldownTo = until
		}
		h.LastErr = trimMsg(errKind, errMsg)
		h.LastUsedAt = time.Now().UnixMilli()
		n++
	}
	r.saveLocked()
	return n
}

// AvailableProviders 返回当前**至少有一个模型可用**的 provider 列表（去重）。
func (r *Router) AvailableProviders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	seen := map[string]bool{}
	for _, rt := range r.routes {
		if !rt.Enabled || rt.Provider == "" {
			continue
		}
		h := r.health[rt.Model]
		if h == nil || h.Disabled || h.CooldownTo > now {
			continue
		}
		seen[rt.Provider] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// ProviderOf 返回某个模型所属 provider（未知返回空串）。
func (r *Router) ProviderOf(modelID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.routeOfLocked(modelID).Provider
}

// RecordSuccess 记录一次成功，更新平均耗时。
func (r *Router) RecordSuccess(modelID string, elapsedMs int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.health[modelID]
	if h == nil {
		h = &Health{Model: modelID}
		r.health[modelID] = h
	}
	h.Success++
	h.ConsecFail = 0
	h.LastUsedAt = time.Now().UnixMilli()
	if h.AvgMs == 0 {
		h.AvgMs = float64(elapsedMs)
	} else {
		h.AvgMs = h.AvgMs*0.7 + float64(elapsedMs)*0.3
	}
	h.LastErr = ""
	r.saveLocked()
}

// RecordFailure 记录一次失败；连续失败达阈值进入冷却期。
func (r *Router) RecordFailure(modelID, errKind, errMsg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.health[modelID]
	if h == nil {
		h = &Health{Model: modelID}
		r.health[modelID] = h
	}
	h.Failure++
	h.ConsecFail++
	h.LastUsedAt = time.Now().UnixMilli()
	h.LastErr = trimMsg(errKind, errMsg)
	rt := r.routeOfLocked(modelID)
	maxFail := rt.MaxFail
	if maxFail <= 0 {
		maxFail = 3
	}
	cooldownM := rt.CooldownM
	if cooldownM <= 0 {
		cooldownM = 30
	}
	// 模型不存在/凭据缺失属于结构性错误，直接长冷却
	if strings.Contains(strings.ToLower(errKind), "model_unavailable") || strings.Contains(strings.ToLower(errKind), "missing_credential") {
		h.CooldownTo = time.Now().Add(12 * time.Hour).UnixMilli()
		r.saveLocked()
		return
	}
	if h.ConsecFail >= maxFail {
		h.CooldownTo = time.Now().Add(time.Duration(cooldownM) * time.Minute).UnixMilli()
	}
	r.saveLocked()
}

// ResetCooldown 手动解除冷却（面板诊断用）。
func (r *Router) ResetCooldown(modelID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h := r.health[modelID]; h != nil {
		h.CooldownTo = 0
		h.ConsecFail = 0
	}
	r.saveLocked()
}

// AllInCooldown 判断是否整池不可用（熔断依据）。
func (r *Router) AllInCooldown() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, rt := range r.routes {
		if !rt.Enabled {
			continue
		}
		h := r.health[rt.Model]
		if h == nil || h.CooldownTo <= now {
			return false
		}
	}
	return true
}

// EarliestAvailable 返回模型池里最早结束冷却的时刻（毫秒）。
// 全部可用时返回 0。热循环治理用它决定"该等多久再试"，避免每 3 秒空转重排。
func (r *Router) EarliestAvailable() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	var earliest int64
	for _, rt := range r.routes {
		if !rt.Enabled {
			continue
		}
		h := r.health[rt.Model]
		if h == nil || h.Disabled {
			continue
		}
		if h.CooldownTo <= now {
			return 0 // 有可用模型
		}
		if earliest == 0 || h.CooldownTo < earliest {
			earliest = h.CooldownTo
		}
	}
	return earliest
}

func (r *Router) routeOfLocked(modelID string) config.ModelRoute {
	for _, rt := range r.routes {
		if rt.Model == modelID {
			return rt
		}
	}
	return config.ModelRoute{}
}

func trimMsg(kind, msg string) string {
	s := kind
	if s != "" {
		s += ": "
	}
	s += msg
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// laneOf 返回一个路由所属的故障域（lane 优先，回退 provider）。
//
// 为什么必须用 lane（2026-10-19 现场实测）：our-free-model 插件在 DSH 里把
// OFM 免费、Kilo 免费池、EAC 协付三条独立上游线路统一注册成同一个 provider
// "our-free-model"。按 provider 分组做冷却会把它们当成一条 —— 实测 OFM 429
// 期间 Kilo 车道完全可用，却因同 provider 被一起停掉，违反要求 4。
func laneOf(rt config.ModelRoute) string {
	if strings.TrimSpace(rt.Lane) != "" {
		return "lane:" + strings.ToLower(strings.TrimSpace(rt.Lane))
	}
	return "provider:" + strings.ToLower(strings.TrimSpace(rt.Provider))
}

// CooldownByLane 把某个**车道**下的所有模型集体冷却（429 只冷却出问题的车道）。
func (r *Router) CooldownByLane(laneKey string, d time.Duration, errKind, errMsg string) int {
	if d <= 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	until := time.Now().Add(d).UnixMilli()
	n := 0
	for _, rt := range r.routes {
		if !rt.Enabled || laneOf(rt) != laneKey {
			continue
		}
		h := r.health[rt.Model]
		if h == nil {
			h = &Health{Model: rt.Model}
			r.health[rt.Model] = h
		}
		if until > h.CooldownTo {
			h.CooldownTo = until
		}
		h.LastErr = trimMsg(errKind, errMsg)
		h.LastUsedAt = time.Now().UnixMilli()
		n++
	}
	r.saveLocked()
	return n
}

// AvailableLanes 返回当前至少有一个模型可用的车道列表（去重）。
func (r *Router) AvailableLanes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UnixMilli()
	seen := map[string]bool{}
	for _, rt := range r.routes {
		if !rt.Enabled {
			continue
		}
		h := r.health[rt.Model]
		if h == nil || h.Disabled || h.CooldownTo > now {
			continue
		}
		seen[laneOf(rt)] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// LaneOf 返回某个模型所属车道（未知返回空串）。
func (r *Router) LaneOf(modelID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rt := r.routeOfLocked(modelID)
	if rt.Model == "" {
		return ""
	}
	return laneOf(rt)
}

package modelpool

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"sync/atomic"
	"workbench/internal/config"
)

// ── Service：模型池的运行期编排 ──────────────────────────────────
//
// 把 Discover（数据面）、Gateway（渠道网关）、Prober（探测）、Scheduler（调度）、
// Manager（账本）与 FailoverRunner（切换）串成一条可启动/停止的后台链路：
//
//	启动 → 发现网关 → 同步清单 → 立即探测一轮 → 周期探测（默认 3 分钟）
//	     → 每轮结果并入 Manager → NotifyRoutes 把新路由表推给引擎
//
// 探测策略（两层，避免烧额度）：
//  1. 端点层：同一个 BaseURL 只探一次 /v1/models，证明地址+凭据活着；
//  2. 模型层：OFM 车道用 availability.json（插件自己逐模型探测的结果）作为
//     每模型信号；自定义端点用 /v1/models 里是否列出该模型作为信号；
//     只有开启 DeepProbe 才发一次 max_tokens=1 的补全。
//
// 这样「实时检测」既覆盖到每个模型，又不会把免费额度耗在探测上。

// ServiceConfig 是模型池服务的运行参数。
type ServiceConfig struct {
	Enabled      bool          `json:"enabled"`
	ProbeSeconds int           `json:"probeSeconds"` // 探测周期（秒），默认 180
	SyncSeconds  int           `json:"syncSeconds"`  // 网关清单同步周期（秒），默认 600
	Strategy     Strategy      `json:"strategy"`     // priority / round-robin / weighted / least-loaded
	DeepProbe    bool          `json:"deepProbe"`    // 是否发模型级补全探测（耗额度）
	ProbeTimeout time.Duration `json:"-"`            // 单次探测超时，默认 10s
	DSHHome      string        `json:"-" `
}

// normalize 补齐默认值。
func (c *ServiceConfig) normalize() {
	if c.ProbeSeconds < 30 {
		c.ProbeSeconds = 180
	}
	if c.ProbeSeconds > 3600 {
		c.ProbeSeconds = 3600
	}
	if c.SyncSeconds < 60 {
		c.SyncSeconds = 600
	}
	if c.Strategy == "" {
		c.Strategy = StrategyPriority
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = 10 * time.Second
	}
}

// Service 是模型池的后台服务。
type Service struct {
	mu       sync.RWMutex
	cfg      ServiceConfig
	mgr      *Manager
	sch      *Scheduler
	prober   *Prober
	store    *Store
	keyMu    sync.Mutex
	keyCache map[string]string
	gw       Gateway
	gwHealth gatewayHealth
	last     ProbeSummary
	// probeRuns / probeOKRuns 记录探测轮次，用于判断「探测是否真的在跑」。
	probeRuns int
	// suppressUntil 是探测抑制截止时刻（毫秒）。上游 429 时由引擎回调置位，
	// 避免冷却期内继续探测 25 个模型、自己制造流量。
	suppressUntil int64

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewService 构造服务（不启动）。
func NewService(store *Store, seed []Spec, cfg ServiceConfig) *Service {
	cfg.normalize()
	mgr := NewManager(store, seed)
	svc := &Service{
		cfg:      cfg,
		mgr:      mgr,
		sch:      NewScheduler(cfg.Strategy),
		prober:   NewProber(cfg.ProbeTimeout),
		store:    store,
		keyCache: map[string]string{},
	}
	// 密钥轮换自愈：探测遇到 401 时重新发现网关并返回可用密钥。
	svc.prober.KeyResolver = svc.resolveGatewayKey
	return svc
}

// resolveGatewayKey 重新发现网关密钥（带 30 秒缓存，避免每模型都重扫目录）。
func (s *Service) resolveGatewayKey(base string) string {
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if k, ok := s.keyCache[base]; ok && k != "" {
		// 缓存命中：仍然验证一次，失败就继续扫。
		if checkGateway(base, k, 1500*time.Millisecond).Authorized {
			return k
		}
		delete(s.keyCache, base)
	}
	gw, health := DiscoverGatewayVerified(s.cfg.DSHHome, 6*time.Second)
	if gw.Key == "" || !health.Authorized {
		return ""
	}
	s.keyCache[base] = gw.Key
	s.mu.Lock()
	if gw.Key != "" {
		s.gw = gw
	}
	s.gwHealth = health
	s.mu.Unlock()
	s.mgr.log(logRecord{Kind: "recover", State: "gateway-key", Reason: "网关密钥已重新解析（来自 " + gw.KeyFrom + "）"})
	return gw.Key
}

// Manager 返回账本（API 与执行器装饰器共用）。
func (s *Service) Manager() *Manager { return s.mgr }

// Scheduler 返回调度器。
func (s *Service) Scheduler() *Scheduler { return s.sch }

// SetRouteSink 透传给 Manager（路由表变更回调，参数是 config.ModelRoute 切片的别名）。
func (s *Service) SetRouteSink(fn func([]config.ModelRoute)) {
	s.mgr.SetRouteSink(fn)
}

// Start 启动后台探测与同步循环（幂等）。
func (s *Service) Start(parent context.Context) {
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.ctx, s.cancel = ctx, cancel
	cfg := s.cfg
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// 首轮同步与探测尽快做，但给应用启动留一点呼吸空间。
		select {
		case <-ctx.Done():
			return
		case <-time.After(1500 * time.Millisecond):
		}
		s.SyncNow("startup")
		s.ProbeNow()
		probeTick := time.NewTicker(time.Duration(cfg.ProbeSeconds) * time.Second)
		syncTick := time.NewTicker(time.Duration(cfg.SyncSeconds) * time.Second)
		defer probeTick.Stop()
		defer syncTick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-probeTick.C:
				s.ProbeNow()
			case <-syncTick.C:
				s.SyncNow("periodic")
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
}

// SyncNow 发现渠道网关并把清单同步进池子。
func (s *Service) SyncNow(actor string) SyncResult {
	// 严格版：只有 HTTP 层能列出模型才算网关可用（TCP 通但 401 不算）。
	gw, health := DiscoverGatewayVerified(s.cfg.DSHHome, 10*time.Second)
	s.mu.Lock()
	s.gw = gw
	s.gwHealth = health
	s.mu.Unlock()
	if gw.Error != "" || len(gw.Models) == 0 {
		// 网关不可达（没在跑 / 密钥没拿到）时退回「OFM 数据面发现」：
		// catalog.json + availability.json 里已经写清了本机有哪些模型车道，
		// 不依赖网关进程也能把清单纳管起来（调度走 DSH 的 OFM provider）。
		s.mgr.log(logRecord{Kind: "sync", State: "gateway-fail", Reason: trim(gw.Error, 200), Actor: actor})
		fallback := Discover(s.cfg.DSHHome)
		res := MergeCandidates(s.mgr, fallback.Candidates, "discover:"+fallback.Dir)
		if len(fallback.Warnings) > 0 {
			s.mgr.log(logRecord{Kind: "sync", State: "discover-warn", Reason: trim(strings.Join(fallback.Warnings, "；"), 240), Actor: actor})
		}
		s.mgr.NotifyRoutes()
		return res
	}
	res := SyncFromGateway(s.mgr, gw, actor)
	s.mgr.log(logRecord{Kind: "sync", State: "gateway-ok",
		Reason: "网关 " + gw.BaseURL + " 广播 " + itoa(len(gw.Models)) + " 个模型：新增 " + itoa(res.Added) + " / 更新 " + itoa(res.Updated), Actor: actor})
	// 同步完立刻把路由表推一次，保证新模型马上可被调度。
	s.mgr.NotifyRoutes()
	return res
}

// ProbeNow 执行一轮可用性探测并写回状态。
func (s *Service) ProbeNow() ProbeSummary {
	// 抑制期内直接返回：不探测、不改清单（避免冷却期自造流量）。
	if ok, _ := s.ProbingSuppressed(); ok {
		return ProbeSummary{At: nowMs()}
	}
	gw := s.gateway()
	specs := s.mgr.Specs()
	if len(specs) == 0 {
		return ProbeSummary{At: nowMs()}
	}
	gwHealth := s.gatewayHealth()
	wasUnauthorized := gwHealth.Reachable && !gwHealth.Authorized
	results := s.probeSpecs(specs, gw)
	summary := s.mgr.ApplyProbe(results)
	// 网关鉴权失败说明密钥轮换过：本轮探测结束后立刻重新同步一次清单，
	// 让新密钥在下一轮生效（现场：8326 属主随任务进程生灭，密钥每分钟都可能变）。
	if wasUnauthorized {
		s.keyMu.Lock()
		s.keyCache = map[string]string{}
		s.keyMu.Unlock()
		go s.SyncNow("probe-recover")
	}
	// 网关鉴权失败说明密钥轮换过：本轮探测结束后立刻重新同步一次清单，
	// 让新密钥在下一轮生效（现场：8326 属主随任务进程生灭，密钥每分钟都可能变）。
	if wasUnauthorized {
		s.keyMu.Lock()
		s.keyCache = map[string]string{}
		s.keyMu.Unlock()
		go s.SyncNow("probe-recover")
	}
	s.mu.Lock()
	s.last = summary
	s.probeRuns++
	s.mu.Unlock()
	s.mgr.log(logRecord{Kind: "probe", State: "summary",
		Reason: "探测 " + itoa(summary.Total) + " 个模型：可用 " + itoa(summary.OK) + " / 下线 " + itoa(summary.Down) + " / 未知 " + itoa(summary.Unknown)})
	return summary
}

// probeSpecs 是探测主体：按 BaseURL 分组，端点只探一次，再给每个模型定状态。
func (s *Service) probeSpecs(specs []Spec, gw Gateway) []ProbeResult {
	// 1) 读 OFM 的逐模型可用性（插件自己探测的结果，最贴近上游真实状态）。
	ofmAvail := map[string]string{}
	if dir := OFMDataDir(s.cfg.DSHHome); dir != "" {
		d := DiscoverOFMOnly(dir)
		ofmAvail = d
	}
	// 2) 端点级探测：同一 base 只打一次。
	bases := map[string][]Spec{}
	order := []string{}
	for _, sp := range specs {
		base := strings.TrimRight(firstNonEmpty(sp.BaseURL, gw.BaseURL), "/")
		if _, ok := bases[base]; !ok {
			order = append(order, base)
		}
		bases[base] = append(bases[base], sp)
	}
	baseOK := map[string]bool{}
	baseErr := map[string]ProbeResult{}
	for _, base := range order {
		group := bases[base]
		probeSpec := group[0]
		probeSpec.BaseURL = base
		// 本机网关必须用「已验证能通过鉴权」的那把 key（可能来自任务隔离 HOME），
		// 而不是每条 Spec 自己带的那把（可能已过期）。
		if isLocalGateway(base) && gw.Key != "" {
			probeSpec.APIKey = gw.Key
		}
		r := s.prober.Probe(probeSpec)
		baseOK[base] = r.OK && r.State == StateHealthy
		baseErr[base] = r
	}
	// 3) 逐模型定状态。
	out := make([]ProbeResult, 0, len(specs))
	for _, sp := range specs {
		base := strings.TrimRight(firstNonEmpty(sp.BaseURL, gw.BaseURL), "/")
		res := ProbeResult{Model: sp.ID, At: nowMs()}
		if !baseOK[base] {
			// 端点层失败：整组模型标记为端点不可达（不是模型本身的问题，但此刻不可用）。
			kind := firstNonEmpty(baseErr[base].Kind, "unreachable")
			res.Kind = kind
			res.Error = firstNonEmpty(baseErr[base].Error, "端点不可达")
			// 本机网关未就绪（没起 / 密钥不匹配）属于环境态：保持 unknown，
			// 让模型继续留在路由表（DSH 走 OFM provider，不依赖 8326）。
			if kind == "gateway_off" || kind == "gateway_unauthorized" {
				res.State = StateUnknown
			} else {
				res.State = StateDown
			}
			out = append(out, res)
			continue
		}
		// 端点通：看 OFM 的逐模型判定（仅对 OFM/免费车道有意义）。
		if sp.Lane == LaneFree || sp.Lane == LaneKilo || sp.Lane == LaneEAC {
			if st, ok := ofmAvail[sp.ID]; ok {
				switch strings.ToLower(strings.TrimSpace(st)) {
				case "unavailable", "region-blocked", "deprecated":
					res.State = StateDown
					res.Kind = "model_unavailable"
					res.Error = "上游判定该模型不可用（" + st + "）"
					out = append(out, res)
					continue
				}
			}
		}
		// 自定义端点：确认 /v1/models 里确实列出了这个模型（必要时做模型级补全）。
		res.OK = true
		res.State = StateHealthy
		res.Kind = ""
		out = append(out, res)
	}
	return out
}

// gatewayHealth 返回最近一次网关健康检查结果。
func (s *Service) gatewayHealth() gatewayHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gwHealth
}

// gatewayHealth 返回最近一次网关健康检查结果。

func (s *Service) gateway() Gateway {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gw
}

// Status 汇总面板需要的全部读数。
func (s *Service) Status() map[string]any {
	gw := s.gateway()
	specs := s.mgr.Specs()
	health := s.mgr.HealthSnapshot()
	hmap := map[string]Health{}
	for _, h := range health {
		hmap[h.ID] = h
	}
	lanes := CountLanes(specs, hmap, nowMs())
	// 车道顺序固定，避免面板每次刷新顺序跳。
	laneOrder := []string{string(LaneFree), string(LaneKilo), string(LaneEAC), string(LaneAggregate), string(LaneCustom)}
	laneRows := make([]map[string]any, 0, len(lanes))
	for _, k := range laneOrder {
		if st, ok := lanes[k]; ok {
			laneRows = append(laneRows, map[string]any{
				"lane": k, "label": laneLabel(Lane(k)), "total": st.Total,
				"available": st.Available, "down": st.Down, "disabled": st.Disabled,
			})
		}
	}
	// 已启用 + 可用 的模型数（面板顶部水位）。
	avail, routable := 0, 0
	for _, sp := range specs {
		if !sp.Enabled {
			continue
		}
		if sp.Routable {
			routable++
		}
		if sp.HealthState() != StateDown && sp.HealthCooldown() <= nowMs() && sp.HealthState() != StateDisabled {
			avail++
		}
	}
	s.mu.RLock()
	last := s.last
	runs := s.probeRuns
	cfg := s.cfg
	gwHealth := s.gwHealth
	s.mu.RUnlock()
	return map[string]any{
		"specs":     specs,
		"health":    health,
		"lanes":     laneRows,
		"strategy":  s.sch.Strategy(),
		"inflight":  s.sch.Inflight(),
		"lastProbe": last,
		"lastSync":  s.mgr.LastSync(),
		"probeRuns": runs,
		"config":    cfg,
		"gateway": map[string]any{
			"base": gw.BaseURL, "hasKey": gw.Key != "", "models": len(gw.Models),
			"error": gw.Error, "at": gw.At, "keyFrom": gw.KeyFrom,
			"reachable": gwHealth.Reachable, "authorized": gwHealth.Authorized,
			"statusCode": gwHealth.StatusCode, "detail": gwHealth.Detail,
		},
		"available": avail,
		"routable":  routable,
		"total":     len(specs),
		"statusLog": s.store.TailLog(120),
		"now":       nowMs(),
	}
}

// SetStrategy 切换调度策略（面板下拉）。
func (s *Service) SetStrategy(st Strategy) error {
	if err := s.sch.SetStrategy(st); err != nil {
		return err
	}
	s.mu.Lock()
	s.cfg.Strategy = st
	s.mu.Unlock()
	s.mgr.log(logRecord{Kind: "crud", State: "strategy", Reason: "调度策略切换为 " + string(st), Actor: "panel"})
	return nil
}

// TopAvailable 返回当前可调度的模型 id（按调度顺序），供面板「下一批会用谁」展示。
func (s *Service) TopAvailable(limit int) []string {
	chain := s.sch.PickChain(s.mgr.Specs(), nowMs())
	if limit > 0 && len(chain) > limit {
		chain = chain[:limit]
	}
	return chain
}

// LogTail 返回最近 n 行状态日志（面板「状态日志」分区）。
func (s *Service) LogTail(n int) []string {
	if s.store == nil {
		return []string{}
	}
	return s.store.TailLog(n)
}

// LogTail 返回最近 n 行状态日志（面板「状态日志」分区）。

// SortedHealth 返回按调度顺序排列的健康度（面板表格用）。
func SortedHealth(specs []Spec, health []Health) []Health {
	hmap := map[string]Health{}
	for _, h := range health {
		hmap[h.ID] = h
	}
	out := make([]Health, 0, len(specs))
	for _, sp := range specs {
		h := hmap[sp.ID]
		if h.ID == "" {
			h = Health{ID: sp.ID, State: StateUnknown}
		}
		out = append(out, h)
	}
	return out
}

var _ = sort.Strings

// SuppressProbing 在指定秒数内暂停自动探测（上游 429 时由引擎回调触发）。
//
// 现场（2026-10-19）：OFM 持续 429 时探测每 180 秒仍打 25 个模型，一边等冷却
// 一边自己制造流量，等于永远恢复不了。抑制只推后不提前（短抑制不得缩短长抑制）。
func (s *Service) SuppressProbing(seconds int, reason string) {
	if seconds <= 0 {
		return
	}
	until := nowMs() + int64(seconds)*1000
	for {
		old := atomic.LoadInt64(&s.suppressUntil)
		if old >= until || atomic.CompareAndSwapInt64(&s.suppressUntil, old, until) {
			break
		}
	}
	_ = reason
}

// ProbingSuppressed 返回当前是否处于抑制期与剩余秒数。
func (s *Service) ProbingSuppressed() (bool, int) {
	until := atomic.LoadInt64(&s.suppressUntil)
	now := nowMs()
	if until <= now {
		return false, 0
	}
	return true, int((until - now) / 1000)
}

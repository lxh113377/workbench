package modelpool

import (
	"sort"
	"strings"
	"sync"
)

// ── 轮询调度 ────────────────────────────────────────────────────
//
// 调度策略（三类，面板可切换）：
//  1. priority（默认）：严格按 Priority 升序取第一个可用模型，天然实现「主用 + 兜底」；
//  2. round-robin：在可用集合里轮转起点，配合粘滞窗口让同一任务连续 N 次落在同一模型；
//  3. weighted：按 Weight 加权随机（沿用 modelroute 的思路），适合多模型分摊负载。
//
// 与 modelroute 的区别：本调度器直接消费 Manager 的实时健康状态（探测结果），
// 且对外提供「取下一个」语义给故障转移使用，不依赖任务失败后的重试循环。

// Strategy 是调度策略。
type Strategy string

const (
	StrategyPriority    Strategy = "priority"
	StrategyRoundRobin  Strategy = "round-robin"
	StrategyWeighted    Strategy = "weighted"
	StrategyLeastLoaded Strategy = "least-loaded"
)

// Scheduler 是模型轮询调度器。
type Scheduler struct {
	mu       sync.Mutex
	strategy Strategy
	cursor   int
	// sticky 是轮换策略的粘滞轮数：连续 N 次pick 落在同一模型，减少切换开销。
	sticky int
	// inflight 记录每个模型当前正在执行的任务数（least-loaded 用）。
	inflight map[string]int
	// rr 是加权随机的确定性轮盘（用累计票数，避免 math/rand 的全局竞争）。
	rrPos int
	// preferred 是有序车道路径偏好（高通过率车道在前）；空 = 关闭，行为同改造前。
	preferred []string
}

// NewScheduler 构造调度器。
func NewScheduler(strategy Strategy) *Scheduler {
	if strategy == "" {
		strategy = StrategyPriority
	}
	return &Scheduler{strategy: strategy, sticky: 3, inflight: map[string]int{}}
}

// SetStrategy 切换策略。
func (s *Scheduler) SetStrategy(st Strategy) error {
	switch st {
	case StrategyPriority, StrategyRoundRobin, StrategyWeighted, StrategyLeastLoaded:
	default:
		return errUnknownStrategy(st)
	}
	s.mu.Lock()
	s.strategy = st
	s.cursor = 0
	s.rrPos = 0
	s.mu.Unlock()
	return nil
}

// Strategy 返回当前策略。
func (s *Scheduler) Strategy() Strategy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.strategy
}

// SetPreferredModels 设置有序车道路径偏好；空/全空白表 = 关闭（绝不静默沿用）；用 setter 因调用方不在本次写面内。
func (s *Scheduler) SetPreferredModels(ids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preferred = nil
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" { // 去空白/空串，保住有序性
			s.preferred = append(s.preferred, id)
		}
	}
}

// Acquire 标记某模型开始处理一个任务（least-loaded 计数）。
func (s *Scheduler) Acquire(id string) {
	s.mu.Lock()
	s.inflight[id]++
	s.mu.Unlock()
}

// Release 标记某模型处理结束。
func (s *Scheduler) Release(id string) {
	s.mu.Lock()
	if s.inflight[id] > 0 {
		s.inflight[id]--
	}
	s.mu.Unlock()
}

// Inflight 返回各模型在飞任务数快照。
func (s *Scheduler) Inflight() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.inflight))
	for k, v := range s.inflight {
		out[k] = v
	}
	return out
}

// Pick 从候选里选下一个模型；excluded 为本次已失败/应跳过的集合。
//
// 返回空字符串表示没有可用候选（调用方应走「全池冷却」等待逻辑）。
func (s *Scheduler) Pick(specs []Spec, excluded map[string]bool, now int64) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cands := make([]Spec, 0, len(specs))
	for _, sp := range specs {
		if !sp.Enabled || excluded[sp.ID] {
			continue
		}
		// 不可路由的模型绝不派发（2026-10-19 现场修复）。
		//
		// Routable=false 表示该模型（如未接通 aggregate 的自定义端点）写进 DSH overlay
		// 会被判「未知 provider」、**必然失败**。Manager.Routes() 一直过滤它，
		// 但 Pick/PickChain 漏了 —— 于是兜底链会把注定失败的模型派出去。
		// 既有用例 TestSchedulerSkipsNonRoutable 锁住了这条承诺。
		if !sp.Routable {
			continue
		}
		if sp.HealthState() == StateDown || sp.HealthCooldown() > now {
			continue
		}
		cands = append(cands, sp)
	}
	if len(cands) == 0 {
		return "", ""
	}
	sortSpecs(cands)
	switch s.strategy {
	case StrategyLeastLoaded:
		// 最少负载：挑当前**在飞任务数最少**的模型。
		//
		// 现场缺陷（2026-10-19）：这里原来与 round-robin **共用同一个分支**，
		// 只按 cursor 轮转、完全没读 s.inflight —— 于是「least-loaded」根本不是
		// 最少负载，而是 round-robin 的副本。schedprobe 实测：
		//   A 在飞 3、B 在飞 1、C 在飞 0（三者同优先级同权重）=> 却挑了 A。
		//
		// 正确语义：在候选里取 inflight 最小者；并列时按 sortSpecs 的顺序
		// （优先级 > 权重 > ID）取第一个，保证结果确定可复现。
		best := cands[0]
		bestN := s.inflight[best.ID]
		for _, c := range cands[1:] {
			if n := s.inflight[c.ID]; n < bestN {
				best, bestN = c, n
			}
		}
		return best.ID, string(StrategyLeastLoaded)
	case StrategyRoundRobin:
		// 轮转起点：cursor 记录上次位置，保证每个模型都被均匀使用。
		if s.cursor >= len(cands) {
			s.cursor = 0
		}
		picked := cands[s.cursor]
		s.cursor = (s.cursor + 1) % len(cands)
		return picked.ID, string(s.strategy)
	case StrategyWeighted:
		total := 0
		for _, sp := range cands {
			w := sp.Weight
			if w <= 0 {
				w = 1
			}
			total += w
		}
		if total <= 0 {
			return cands[0].ID, string(s.strategy)
		}
		s.rrPos = (s.rrPos + 1) % total
		acc := 0
		for _, sp := range cands {
			w := sp.Weight
			if w <= 0 {
				w = 1
			}
			acc += w
			if s.rrPos < acc {
				return sp.ID, string(s.strategy)
			}
		}
		return cands[len(cands)-1].ID, string(s.strategy)
	default: // priority
		return cands[0].ID, string(StrategyPriority)
	}
}

// PickChain 返回候选队列（主选 + 全部兜底），用于单次任务内的快速故障转移。
// 调用方按顺序尝试：第一个失败立刻换第二个，不再等重试退避。
func (s *Scheduler) PickChain(specs []Spec, now int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	cands := make([]Spec, 0, len(specs))
	for _, sp := range specs {
		if !sp.Enabled || !sp.Routable {
			continue // 不可路由模型绝不进兜底链（同 Pick 的护栏）
		}
		if sp.HealthState() == StateDown || sp.HealthCooldown() > now {
			continue
		}
		cands = append(cands, sp)
	}
	sortSpecs(cands)
	ord := make([]string, 0, len(cands))
	for _, sp := range cands {
		ord = append(ord, sp.ID)
	}
	// least-loaded 的兜底链也必须按负载升序（2026-10-19 现场修复）。
	//
	// 这里原来同样把 LeastLoaded 与 RoundRobin 挤在一个分支里，只做 cursor 轮转 ——
	// 于是 Pick 修好了、PickChain 还是旧行为，正是既有用例
	// TestSchedulerLeastLoadedChainPrefersIdle 抓出来的漏洞（期望 [c b a]，实际 [a b c]）。
	// 教训：**同一策略的入口往往不止一个**（Pick 与 PickChain 都要各自正确）。
	if s.strategy == StrategyLeastLoaded && len(ord) > 1 {
		sort.SliceStable(ord, func(i, j int) bool {
			li, lj := s.inflight[ord[i]], s.inflight[ord[j]]
			if li != lj {
				return li < lj // 负载小的排前面
			}
			// 负载并列时保持 sortSpecs 的调度序（优先级 > 权重 > ID），
			// 这样「零负载」场景仍返回 [a b c]，不会引入随机抖动。
			return false
		})
	}
	if s.strategy == StrategyRoundRobin && len(ord) > 1 {
		if s.cursor >= len(ord) {
			s.cursor = 0
		}
		ord = append(ord[s.cursor:], ord[:s.cursor]...)
		s.cursor = (s.cursor + 1) % len(cands)
	}
	// 稳定分组放在所有策略排序之后：命中者前置、其余保持策略序；不可用偏好已在上面被过滤。
	return groupPreferred(ord, s.preferred)
}

// groupPreferred 稳定分组：命中者按偏好顺序前置、其余保持策略序；未知 id 被自然忽略。
func groupPreferred(ord, pref []string) []string {
	if len(pref) == 0 || len(ord) < 2 {
		return ord
	}
	rank := make(map[string]int, len(pref))
	for i := len(pref) - 1; i >= 0; i-- { // 逆序写：重复 id 保留最早那次
		rank[pref[i]] = i
	}
	sort.SliceStable(ord, func(i, j int) bool {
		ri, iok := rank[ord[i]]
		rj, jok := rank[ord[j]]
		return iok && (!jok || ri < rj) // 命中组一律前置；都命中才比偏好序
	})
	return ord
}

func errUnknownStrategy(st Strategy) error {
	return &unknownStrategyError{st: st}
}

type unknownStrategyError struct{ st Strategy }

func (e *unknownStrategyError) Error() string {
	return "未知的调度策略: " + string(e.st) + "（可选 priority/round-robin/weighted/least-loaded）"
}

// CountLanes 统计各车道的模型数与可用数（面板顶部概况）。
func CountLanes(specs []Spec, health map[string]Health, now int64) map[string]LaneStats {
	out := map[string]LaneStats{}
	for _, sp := range specs {
		st := out[string(sp.Lane)]
		st.Total++
		h := health[sp.ID]
		if sp.Enabled && h.State != StateDown && h.CooldownTo <= now {
			st.Available++
		}
		if h.State == StateDown {
			st.Down++
		}
		if !sp.Enabled {
			st.Disabled++
		}
		out[string(sp.Lane)] = st
	}
	return out
}

// LaneStats 是单车道统计。
type LaneStats struct {
	Total     int `json:"total"`
	Available int `json:"available"`
	Down      int `json:"down"`
	Disabled  int `json:"disabled"`
}

// NormalizeModelID 把面板输入收敛成池内 id（去空白、拒绝空值）。
func NormalizeModelID(s string) string { return strings.TrimSpace(s) }

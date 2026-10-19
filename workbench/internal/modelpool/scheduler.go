package modelpool

import (
	"sort"
	"strings"
	"sync"
)

// ── 轮询调度 ────────────────────────────────────────────────────
//
// 调度策略（四类，面板可切换）：
//  1. priority（默认）：严格按 Priority 升序取第一个可用模型，天然实现「主用 + 兜底」；
//  2. round-robin：在可用集合里轮转起点，均匀分摊；
//  3. weighted：按 Weight 加权轮盘（沿用 modelroute 的思路），快模型多跑；
//  4. least-loaded：按 Acquire/Release 维护的在飞任务数取最闲模型，高并发分摊负载。
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
	// inflight 记录每个模型当前正在执行的任务数（least-loaded 用）。
	inflight map[string]int
	// rr 是加权随机的确定性轮盘（用累计票数，避免 math/rand 的全局竞争）。
	rrPos int
}

// NewScheduler 构造调度器。
func NewScheduler(strategy Strategy) *Scheduler {
	if strategy == "" {
		strategy = StrategyPriority
	}
	return &Scheduler{strategy: strategy, inflight: map[string]int{}}
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
	case StrategyRoundRobin:
		// 轮转起点：cursor 记录上次位置，保证每个模型都被均匀使用。
		if s.cursor >= len(cands) {
			s.cursor = 0
		}
		picked := cands[s.cursor]
		s.cursor = (s.cursor + 1) % len(cands)
		return picked.ID, string(s.strategy)
	case StrategyLeastLoaded:
		// 真正按「在飞任务数」选最闲的模型；并列时用调度序（Priority↑ Weight↓ ID）
		// 兜底，因为 cands 已经过 sortSpecs 稳定排序，取第一个最小即等价。
		//
		// 历史缺陷（2026-10-19 活体面板复验发现）：此分支曾与 round-robin 合并，
		// inflight 计数只在 Acquire/Release 与 /api/pool 快照里出现，从未参与选择。
		// 于是面板下拉写着「least-loaded：在飞任务少的优先」、文档同调，
		// 实跑却是轮转 —— 负载均衡策略名存实亡，用户切过去看不出任何差异。
		best := cands[0]
		bestLoad := s.inflight[best.ID]
		for _, sp := range cands[1:] {
			if load := s.inflight[sp.ID]; load < bestLoad {
				best, bestLoad = sp, load
			}
		}
		return best.ID, string(s.strategy)
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
		if !sp.Enabled || sp.HealthState() == StateDown || sp.HealthCooldown() > now {
			continue
		}
		cands = append(cands, sp)
	}
	sortSpecs(cands)
	ord := make([]string, 0, len(cands))
	for _, sp := range cands {
		ord = append(ord, sp.ID)
	}
	if s.strategy == StrategyRoundRobin && len(ord) > 1 {
		if s.cursor >= len(ord) {
			s.cursor = 0
		}
		ord = append(ord[s.cursor:], ord[:s.cursor]...)
		s.cursor = (s.cursor + 1) % len(cands)
	}
	if s.strategy == StrategyLeastLoaded && len(ord) > 1 {
		// 兜底链同样要「最闲的先上」：按在飞任务数升序稳定重排（cands 已是调度序，
		// 并列时保持 Priority↑ Weight↓ ID）。FailoverRunner 用的就是这条链，
		// 不重排的话高并发下会把任务继续压给最忙的模型。
		load := func(id string) int { return s.inflight[id] }
		sort.SliceStable(cands, func(i, j int) bool {
			return load(cands[i].ID) < load(cands[j].ID)
		})
		ord = ord[:0]
		for _, sp := range cands {
			ord = append(ord, sp.ID)
		}
	}
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

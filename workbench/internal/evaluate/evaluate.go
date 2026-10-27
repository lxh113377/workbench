// Package evaluate 实现任务必要性智能评估：规则引擎 + 轻量语义判分，
// 从产出价值、资源消耗、可行性三维度打分，并施加硬门禁（重复/冷却/配额/多样性）。
package evaluate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/templates"
)

// Evaluator 是评估器。它持有"最近产出"的滚动窗口用于新颖度与重复判定。
type Evaluator struct {
	mu      sync.RWMutex
	cfg     config.EvalConfig
	recent  []recentItem // 最近任务（用于重复与多样性）
	chainOK map[string]float64
}

type recentItem struct {
	TaskID      string
	GoalID      string
	Title       string
	Prompt      string
	Domain      string
	Diversity   string
	TemplateID  string
	CreatedAt   int64
	Quality     int
	Fingerprint string
}

// New 构造评估器。
func New(cfg config.EvalConfig) *Evaluator {
	return &Evaluator{cfg: cfg, chainOK: map[string]float64{}}
}

// UpdateConfig 热更新评估配置。
func (e *Evaluator) UpdateConfig(cfg config.EvalConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = cfg
}

// Config 返回当前评估配置。
func (e *Evaluator) Config() config.EvalConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// Input 是一次评估的输入。
type Input struct {
	Task       *model.Task
	Template   *model.Template
	History    []*model.Task // 历史任务（由调用方注入，避免评估器直接依赖 store）
	Now        int64
	DailyCount int
}

// Evaluate 执行一次评估。返回填充好的 Model.EvalScores；拒与否由 Total>=Threshold 与 Gates 决定。
func (e *Evaluator) Evaluate(in Input) *model.EvalScores {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := in.Now
	if now == 0 {
		now = model.Now()
	}
	cfg := e.cfg
	w := model.Weights{Value: cfg.Weights.Value, Cost: cfg.Weights.Cost, Feasibility: cfg.Weights.Feasibility}.Normalized()
	scores := &model.EvalScores{
		Threshold: cfg.Threshold,
		Weights:   w,
		At:        now,
		Reasons:   []string{},
	}

	// ── 硬门禁（先于打分）────────────────────────────────────────
	gates := e.gates(in, now)
	scores.Gates = gates
	if len(gates) > 0 {
		scores.Total = 0
		scores.Reason = strings.Join(gates, "；")
		scores.Value, scores.Cost, scores.Feasibility = 0, 100, 0
		return scores
	}

	// ── 价值 V ───────────────────────────────────────────────────
	valuePrior := 60.0
	if in.Template != nil {
		valuePrior = in.Template.ValuePrior
	}
	novelty := e.novelty(in.Task)
	chainBonus := e.chainBonus(in.Task)
	value := clamp(valuePrior*0.6+novelty*0.3+chainBonus*0.1, 0, 100)
	scores.Value = round1(value)
	scores.Reasons = append(scores.Reasons, fmt.Sprintf("价值先验 %.0f × 新颖度 %.0f × 链收益 %.0f", valuePrior, novelty, chainBonus))

	// ── 成本 C（越大越贵）───────────────────────────────────────
	costPrior := 50.0
	if in.Template != nil {
		costPrior = in.Template.CostPrior
	}
	histAvg := e.historicalCost(in.Task, in.History)
	if histAvg > 0 {
		costPrior = costPrior*0.5 + histAvg*0.5
	}
	loadPenalty := 0.0
	if in.DailyCount > cfg.DailyCap/2 {
		loadPenalty = float64(in.DailyCount) / float64(maxInt(cfg.DailyCap, 1)) * 30
	}
	cost := clamp(costPrior+loadPenalty, 0, 100)
	scores.Cost = round1(cost)
	scores.Reasons = append(scores.Reasons, fmt.Sprintf("成本先验 %.0f + 负载惩罚 %.0f", costPrior, loadPenalty))

	// ── 可行性 F ─────────────────────────────────────────────────
	feas := 90.0
	if in.Template != nil {
		if in.Template.Params.WebAllowed {
			feas -= 5
		}
		if in.Template.OutputSpec.MinWords > 1000 {
			feas -= 5
		}
	}
	if strings.TrimSpace(in.Task.Prompt) == "" {
		feas -= 40
	}
	if in.Task.ChainDepth > 0 && in.Task.ChainDepth >= maxChainDepth(in.Template) {
		feas -= 25
	}
	feas = clamp(feas, 0, 100)
	scores.Feasibility = round1(feas)
	scores.Reasons = append(scores.Reasons, fmt.Sprintf("可行性 %.0f（提示词完整度/产出规格/链深度）", feas))

	// ── 综合分 ───────────────────────────────────────────────────
	total := w.Value*value + w.Cost*(100-cost) + w.Feasibility*feas
	scores.Total = round1(total)
	scores.Semantic = false
	scores.Reason = defaultReason(total, cfg.Threshold)
	return scores
}

// ApplySemantic 把 LLM 语义判分融合进规则分。value/feas 是 0-100，semantic 为 false 表示语义层未参与。
func ApplySemantic(scores *model.EvalScores, value, feas float64, reason string, used bool) {
	if scores == nil || len(scores.Gates) > 0 {
		return
	}
	if used {
		// 语义与规则各占一半，避免任一引擎独断
		scores.Value = round1(scores.Value*0.5 + clamp(value, 0, 100)*0.5)
		scores.Feasibility = round1(scores.Feasibility*0.5 + clamp(feas, 0, 100)*0.5)
		scores.Semantic = true
	}
	if strings.TrimSpace(reason) != "" {
		scores.Reasons = append(scores.Reasons, "语义判分："+reason)
	}
	w := scores.Weights
	total := w.Value*scores.Value + w.Cost*(100-scores.Cost) + w.Feasibility*scores.Feasibility
	scores.Total = round1(total)
	scores.Reason = defaultReason(scores.Total, scores.Threshold)
}

// Approve 判断是否通过阈值。
func Approve(scores *model.EvalScores) bool {
	if scores == nil {
		return false
	}
	if len(scores.Gates) > 0 {
		return false
	}
	return scores.Total >= scores.Threshold
}

// gates 返回命中的硬门禁描述列表。
func (e *Evaluator) gates(in Input, now int64) []string {
	cfg := e.cfg
	var out []string
	t := in.Task

	if cfg.DailyCap > 0 && in.DailyCount >= cfg.DailyCap {
		out = append(out, fmt.Sprintf("已达每日任务上限 %d", cfg.DailyCap))
	}

	if cfg.RepeatGate && t != nil {
		// 同目标豁免（2026-10-19 端到端实测修复）：
		// 自主任务属于同一个项目目标，规划器按「<目标标题> · <阶段焦点>」生成，
		// 标题前缀天然相同、Prompt 的上下文段（项目意图 + 已完成阶段）也天然高度相似，
		// 三维相似度长期落在 0.9+。若不放行，同目标的第 3 个阶段起会被重复门禁
		// 全部拒掉 —— 现场实测：目标进度卡在 2/4，decisions.jsonl 连续 47 条
		// "评估拒绝：与最近任务高度相似（0.92）"、0 产出（自主闭环死锁）。
		// 正确口径：最近窗口里"同目标"的历史条目不作为重复依据；
		// 跨目标的相似产出仍然受门禁约束。
		fp := fingerprint(t.Title, t.Prompt)
		for _, it := range e.recent {
			if it.TaskID == t.ID {
				continue
			}
			// 精确重复 vs 模糊相似必须分开判（2026-10-21 现场缺陷）：
			//   同目标豁免的初衷是「不要因为同项目上下文天然相似而全拒」，
			//   但旧写法把**精确重复**也一起豁免了 —— 同一目标的同一条任务可以
			//   被反复规划、反复进入执行，考核指标上完全看不出来。
			//   现在：精确指纹（标题+Prompt 归一化后完全相同）**任何目标都要拦**；
			//   模糊相似才按目标豁免（同目标阶段任务本就共享上下文）。
			if it.Fingerprint == fp {
				// 精确重复加**时间窗**（round 45 · T29）：原先永久封杀同指纹，而模板任务的 goalId 为空 ⇒
				// 同目标豁免不适用 ⇒ 生成器只要反复产同一批组合，队列就被自己判死（实测 rejected 4970 / queued 1）。
				// 窗内仍拦（防短周期抖动）；窗外放行（那个任务多半还没交付成功，重跑是合理的）。
				win := cfg.ExactDupWindowMinutes
				if win <= 0 {
					win = 60
				}
				if now-it.CreatedAt < int64(win)*60_000 {
					out = append(out, "与最近任务完全重复")
					break
				}
				// 窗外：本条老记录不作为重复依据，继续比对窗口内其它条目（别把后面的相似判据一起跳过）。
				continue
			}
			if sameGoal(it.GoalID, t.GoalID) {
				continue
			}
			// 模糊相似**只比标题**（round 45 · T32，独立排查实锤）：比全量 title+prompt 时，
			// 长脚手架模板的常量段把相似度顶到 0.92~0.997（9 条模板 859 条**永久误杀**），
			// 量的其实是模板常量而不是主题变量。实测全库「只看标题」的 Jaccard 从不为 ≥0.9。
			sim := similarity(t.Title, it.Title)
			if sim >= cfg.SimilarityCap {
				// 模糊判据与精确指纹**同口径加时间窗**：窗外不再拦 ——
				// 否则「同标题不同 prompt」这类组合仍会被永久封杀（队列饿死换个入口复发）。
				win := cfg.ExactDupWindowMinutes
				if win <= 0 {
					win = 60
				}
				if now-it.CreatedAt < int64(win)*60_000 {
					out = append(out, fmt.Sprintf("与最近任务高度相似（%.2f）", sim))
					break
				}
				// 窗外：本条不作重复依据，继续比对窗口内其它条目。
				continue
			}
		}
	}

	if in.Template != nil && t != nil {
		if in.Template.CooldownMinutes > 0 {
			for _, it := range e.recent {
				if it.TemplateID == in.Template.ID && now-it.CreatedAt < int64(in.Template.CooldownMinutes)*60_000 {
					out = append(out, fmt.Sprintf("模板 %s 处于冷却期", in.Template.ID))
					break
				}
			}
		}
	}
	return out
}

// novelty 计算新颖度：与最近任务的最大相似度越低越新颖。
//
// 同目标口径必须与 gates() 保持一致（2026-10-21 现场缺陷，代价是 32 小时零产出）：
//   - 自主任务的 Prompt 由 buildLadderPrompt 生成，**天然注入项目意图 + 已完成阶段**，
//     同目标相邻阶段的全文相似度长期在 0.9 以上；
//   - 旧写法对同目标不做区分，于是「合法的新阶段任务」被判成旧任务的重放：
//     实测 value = 60×0.6 + novelty(0)×0.3 + chain(50)×0.1 = 41，
//     综合分 0.5×41 + 0.2×(100-60) + 0.3×65 = 48 < 阈值 55 => **永不可能通过**；
//   - gates() 里早已按「同目标豁免」放行模糊相似（注释在册），novelty 却没跟上 ——
//     同一套语义两处实现不一致，等于豁免被 novelty 抵消掉（组件齐全、接线断一节）。
//
// 现在的口径：同目标只用**精确指纹**判重放（完全相同 = 新颖度 0），
// 模糊相似不再参与同目标的新颖度计算；跨目标的新颖度判定完全不变。
func (e *Evaluator) novelty(t *model.Task) float64 {
	if t == nil || len(e.recent) == 0 {
		return 80
	}
	text := t.Title + " " + t.Prompt
	fp := fingerprint(t.Title, t.Prompt)
	maxSim := 0.0
	for _, it := range e.recent {
		if sameGoal(it.GoalID, t.GoalID) {
			if it.Fingerprint == fp {
				return 0 // 同一目标下逐字相同的任务 ＝ 重放，无新颖度
			}
			continue
		}
		sim := similarity(text, it.Title+" "+it.Prompt)
		if sim > maxSim {
			maxSim = sim
		}
	}
	return clamp((1-maxSim)*100, 0, 100)
}

// chainBonus 计算链收益：同一链前序任务自评越高，后继价值越高。
// 注意：本函数在 Evaluate 持有写锁的临界区内被调用，因此**不得再取锁**
// （sync.RWMutex 不可重入，否则会永久死锁 —— 这是初版真实踩到的 bug）。
func (e *Evaluator) chainBonus(t *model.Task) float64 {
	if t == nil || t.ChainID == "" {
		return 50
	}
	q, ok := e.chainOK[t.ChainID]
	if !ok {
		return 50
	}
	return clamp(q*10, 0, 100)
}

// SetChainQuality 记录链前序任务的质量（1-10）。
func (e *Evaluator) SetChainQuality(chainID string, quality int) {
	if chainID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.chainOK[chainID] = float64(quality)
}

func (e *Evaluator) historicalCost(t *model.Task, history []*model.Task) float64 {
	if t == nil || len(history) == 0 {
		return 0
	}
	var sum float64
	var n int
	for _, h := range history {
		if h.TemplateID != t.TemplateID || h.ElapsedMs <= 0 {
			continue
		}
		// 把耗时映射到 0-100：10 分钟=50 分
		cost := math.Min(100, float64(h.ElapsedMs)/600_000*50)
		sum += cost
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// Observe 把一次已完成任务加入评估器的滚动窗口（重复/多样性/新颖度的依据）。
func (e *Evaluator) Observe(t *model.Task, quality int, diversity string, maxKeep int) {
	if t == nil {
		return
	}
	if maxKeep <= 0 {
		maxKeep = 200
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recent = append(e.recent, recentItem{
		TaskID: t.ID, Title: t.Title, Prompt: t.Prompt, Domain: t.Domain,
		// GoalID 必须记（2026-10-19 现场缺陷）：gates() 的「同目标豁免」依赖它 ——
		// sameGoal(a,b) 是 a != "" && a == b，漏了 GoalID 就等于豁免恒不生效，
		// 自主任务会被相似度门禁全量拒掉，形成「重复被拒 -> 不入队 -> 3 秒后再试」热循环。
		GoalID:    t.GoalID,
		Diversity: diversity, TemplateID: t.TemplateID, CreatedAt: t.CreatedAt,
		Quality: quality, Fingerprint: fingerprint(t.Title, t.Prompt),
	})
	if len(e.recent) > maxKeep {
		e.recent = e.recent[len(e.recent)-maxKeep:]
	}
}

// Seed 用历史任务重建滚动窗口（启动恢复用）。
func (e *Evaluator) Seed(tasks []*model.Task, maxKeep int) {
	if maxKeep <= 0 {
		maxKeep = 200
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt < tasks[j].CreatedAt })
	e.mu.Lock()
	defer e.mu.Unlock()
	e.recent = e.recent[:0]
	for _, t := range tasks {
		// 只把已完成任务计入新颖度/重复度窗口，未完成的不参与
		if t.State != model.StateDone && t.State != model.StateRunning {
			continue
		}
		q := 0
		if t.Critique != nil {
			q = t.Critique.Quality
		}
		e.recent = append(e.recent, recentItem{
			TaskID: t.ID, Title: t.Title, Prompt: t.Prompt, Domain: t.Domain,
			// GoalID 必须记（同 Observe 的理由）：重启恢复后豁免同样要生效。
			GoalID:     t.GoalID,
			TemplateID: t.TemplateID, CreatedAt: t.CreatedAt, Quality: q,
			Fingerprint: fingerprint(t.Title, t.Prompt),
		})
	}
	if len(e.recent) > maxKeep {
		e.recent = e.recent[len(e.recent)-maxKeep:]
	}
}

// RecentItemsForTest 暴露滚动窗口的快照（供测试断言窗口字段完整性）。
//
// 命名带 ForTest 是为了明确「这不是业务 API」；生产代码不应依赖它。
func (e *Evaluator) RecentItemsForTest() []recentItem {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]recentItem, len(e.recent))
	copy(out, e.recent)
	return out
}

// RecentTitles 返回最近的标题（供后继生成去重）。
func (e *Evaluator) RecentTitles(n int) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if n <= 0 || n > len(e.recent) {
		n = len(e.recent)
	}
	out := make([]string, 0, n)
	for i := len(e.recent) - n; i < len(e.recent); i++ {
		if i < 0 {
			continue
		}
		out = append(out, e.recent[i].Title)
	}
	return out
}

// DomainShare 返回某领域在最近窗口中的占比（多样性门禁用）。
func (e *Evaluator) DomainShare(domain string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if len(e.recent) == 0 {
		return 0
	}
	n := 0
	for _, it := range e.recent {
		if it.Domain == domain {
			n++
		}
	}
	return float64(n) / float64(len(e.recent))
}

// ── 工具函数 ─────────────────────────────────────────────────────

func fingerprint(title, prompt string) string {
	h := sha256.Sum256([]byte(normalize(title) + "|" + normalize(prompt)))
	return hex.EncodeToString(h[:16])
}

func normalize(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(s) {
		if r == ' ' || r == '\t' || r == '\n' || r == '　' || r == '，' || r == '。' || r == ',' || r == '.' {
			if !prevSpace {
				b.WriteRune(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(b.String())
}

// sameGoal 判断两个 recentItem 是否属于同一自主目标。
//
// 现场缺陷（2026-10-19）：没有这条豁免时，同一目标的后续阶段会被「与最近任务
// 高度相似」硬门禁反复拒绝，闭环卡在 47 次连续拒绝。
func sameGoal(a, b string) bool {
	return a != "" && a == b
}

// similarity 用 shingle 集合的 Jaccard 相似度近似语义重复。
func similarity(a, b string) float64 {
	sa := shingles(normalize(a))
	sb := shingles(normalize(b))
	if len(sa) == 0 || len(sb) == 0 {
		return 0
	}
	inter := 0
	for k := range sa {
		if _, ok := sb[k]; ok {
			inter++
		}
	}
	union := len(sa) + len(sb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func shingles(s string) map[string]struct{} {
	out := map[string]struct{}{}
	runes := []rune(s)
	const n = 3
	if len(runes) < n {
		out[s] = struct{}{}
		return out
	}
	for i := 0; i+n <= len(runes); i++ {
		out[string(runes[i:i+n])] = struct{}{}
	}
	return out
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func defaultReason(total, threshold float64) string {
	if total >= threshold {
		return fmt.Sprintf("综合分 %.1f ≥ 阈值 %.0f，通过", total, threshold)
	}
	return fmt.Sprintf("综合分 %.1f < 阈值 %.0f，拒绝", total, threshold)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxChainDepth(t *model.Template) int {
	if t == nil || t.Chain.MaxDepth <= 0 {
		return 5
	}
	return t.Chain.MaxDepth
}

// 保证 templates 包被引用（语义判分提示词复用模板渲染）
var _ = templates.Render

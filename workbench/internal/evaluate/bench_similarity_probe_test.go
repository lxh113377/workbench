package evaluate

// 窗口相似度成本的基准（2026-10-28 性能定位，非业务断言）。
//
// 背景：supplyCandidates 每个补货 tick 对每个 enabled 模板调一次 Evaluate；
// Evaluate 内部 gates() 与 novelty() 都要遍历滚动窗口（生产 Seed 窗口 400 条），
// 每条都调 similarity(a, b) —— 它每次都把**两侧**的 normalize + 3-gram 集合重建一遍。
// 历史侧那 400 条的内容根本不变，却在每次评估里被重算 ⇒「重复计算 + 缓存缺失」。
//
// 三个基准分别钉一层成本：
//   BenchmarkNovelty400    一次 novelty() 扫窗
//   BenchmarkEvaluateFull  一次完整评估（含 historicalCost 扫 5000 条 History）
//   BenchmarkTickSupply    一个补货 tick（24 个模板各评估一次）
//
// 尺寸取自生产实测读数（queue.json 5002 条）：title p50=18 / p90=26，prompt p50=91 / p90=974。

import (
	"fmt"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// mkWindow 造 n 条滚动窗口，正文长度贴近生产 p90。
func mkWindow(n int, promptRunes int) []recentItem {
	items := make([]recentItem, 0, n)
	base := "这是一段任务说明内容示例用于模拟真实模板拼装后的提示词正文长度分布特征"
	var sb strings.Builder
	for sb.Len()/3 < promptRunes {
		sb.WriteString(base)
	}
	body := sb.String()
	for i := 0; i < n; i++ {
		items = append(items, recentItem{
			TaskID:     fmt.Sprintf("task-%d", i),
			Title:      fmt.Sprintf("任务标题示例%d", i),
			Prompt:     body + fmt.Sprintf(" 第%d条", i),
			TemplateID: fmt.Sprintf("tpl-%d", i%24),
			CreatedAt:  int64(1_700_000_000_000) + int64(i)*1000,
		})
	}
	return items
}

func benchEvalCfg() config.EvalConfig {
	return config.EvalConfig{
		Threshold:             60,
		SimilarityCap:         0.9,
		RepeatGate:            true,
		DailyCap:              200,
		DiversityCap:          0.4,
		ExactDupWindowMinutes: 60,
	}
}

func benchHistory(n int) []*model.Task {
	hist := make([]*model.Task, 0, n)
	for i := 0; i < n; i++ {
		hist = append(hist, &model.Task{
			ID:         fmt.Sprintf("h-%d", i),
			TemplateID: fmt.Sprintf("tpl-%d", i%24),
			ElapsedMs:  int64(60_000 + i),
			State:      model.StateDone,
			CreatedAt:  int64(1_700_000_000_000) + int64(i)*1000,
		})
	}
	return hist
}

func BenchmarkNovelty400(b *testing.B) {
	e := New(benchEvalCfg())
	e.recent = mkWindow(400, 900)
	cand := &model.Task{ID: "c", Title: "新任务标题", Prompt: "新任务的提示词正文，长度模拟真实模板拼装后的规模", TemplateID: "tpl-0"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.novelty(cand)
	}
}

func BenchmarkEvaluateFull(b *testing.B) {
	e := New(benchEvalCfg())
	e.recent = mkWindow(400, 900)
	hist := benchHistory(5000)
	cand := &model.Task{ID: "c", Title: "新任务标题", Prompt: "新任务的提示词正文", TemplateID: "tpl-0"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Evaluate(Input{Task: cand, History: hist, Now: 1_700_000_999_999, DailyCount: 10})
	}
}

func BenchmarkTickSupply(b *testing.B) {
	e := New(benchEvalCfg())
	e.recent = mkWindow(400, 900)
	hist := benchHistory(5000)
	cands := make([]*model.Task, 24)
	for i := range cands {
		cands[i] = &model.Task{
			ID:         fmt.Sprintf("c-%d", i),
			Title:      fmt.Sprintf("标题%d 主题词", i),
			Prompt:     "模板拼装后的提示词正文示例，含若干中文字符以贴近真实长度",
			TemplateID: fmt.Sprintf("tpl-%d", i),
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range cands {
			_ = e.Evaluate(Input{Task: c, History: hist, Now: 1_700_000_999_999, DailyCount: 10})
		}
	}
}

// BenchmarkNovelty400Uncached 钉住内存护栏的**下界行为**：正文远超上限（400 条 ×3000 rune
// 且互不相同）⇒ 每条都走现算路径 ⇒ 应与改造前同速，绝不会比旧实现更慢。
// 这条基准是护栏的代价声明：护栏只让长文本回到旧成本，不引入新的更差路径。
func BenchmarkNovelty400Uncached(b *testing.B) {
	e := New(benchEvalCfg())
	e.recent = mkVariedWindow(400, 3000)
	cand := &model.Task{ID: "c", Title: "新任务标题", Prompt: "新任务的提示词正文，长度模拟真实模板拼装后的规模", TemplateID: "tpl-0"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.novelty(cand)
	}
}

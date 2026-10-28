package evaluate

// 相似度缓存的等价性判据（2026-10-28 性能修复的配套测试）。
//
// 被优化的不是算法而是重复计算：gates()/novelty() 原先每条历史比对都要把两侧字符串
// 重新 normalize + 建 3-gram 集合；现在历史侧集合缓存在 recentItem 上、候选侧提到循环外
// 只算一遍。判据因此只回答一个问题：缓存有没有让任何一个分数变化。
// 手段是双跑对照 —— 现行（走缓存）vs 参照（similarity 本体保留未改，正是参照）。
//
// 覆盖面：① 逐值等价（含空串/超短串/纯空白三类边界）；② novelty() 整体返回值等价；
// ③ 窗口对外读数不受新字段影响；④ 负向对照（把缓存改坏必须被抓到 ⇒ 证明判据有牙）。

import (
	"fmt"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func evalCfgForCache() config.EvalConfig { return benchEvalCfg() }

// freshSimTitle / freshSimFull 是无缓存参照：直接调用未改动的 similarity 本体。
func freshSimTitle(candTitle, itTitle string) float64 { return similarity(candTitle, itTitle) }

func freshSimFull(candText string, it *recentItem) float64 {
	return similarity(candText, it.Title+" "+it.Prompt)
}

// TestSimilarityCacheEqualsFreshComputation 主判据：窗口内每条历史，缓存路径
// （titleSh/fullSh + jaccard）与现算路径（similarity）必须逐值相等。
func TestSimilarityCacheEqualsFreshComputation(t *testing.T) {
	e := New(evalCfgForCache())
	e.recent = mkWindow(60, 300)
	// 边界形态：空串、超短（<3 rune 走 shingles 的整串分支）、纯空白（normalize 会归一）
	e.recent = append(e.recent,
		recentItem{TaskID: "edge-empty", Title: "", Prompt: ""},
		recentItem{TaskID: "edge-short", Title: "ab", Prompt: "cd"},
		recentItem{TaskID: "edge-space", Title: "  ", Prompt: "，。 ,."},
	)
	cands := []*model.Task{
		{ID: "c1", Title: "任务标题示例7", Prompt: "这是一段任务说明内容示例用于模拟真实模板拼装后的提示词正文长度分布特征 第7条"},
		{ID: "c2", Title: "完全不同的标题", Prompt: "毫不相干的提示词内容"},
		{ID: "c3", Title: "", Prompt: ""},
	}
	for _, c := range cands {
		shCT := shinglesOf(c.Title)
		shCF := shinglesOf(c.Title + " " + c.Prompt)
		for i := range e.recent {
			it := &e.recent[i]
			if got, want := jaccard(shCT, it.titleSh()), freshSimTitle(c.Title, it.Title); got != want {
				t.Fatalf("标题相似度不等 cand=%s item=%s cached=%.17f fresh=%.17f", c.ID, it.TaskID, got, want)
			}
			if got, want := jaccard(shCF, it.fullSh()), freshSimFull(c.Title+" "+c.Prompt, it); got != want {
				t.Fatalf("全文相似度不等 cand=%s item=%s cached=%.17f fresh=%.17f", c.ID, it.TaskID, got, want)
			}
		}
		// 二次调用必须命中缓存：同一个 map 实例 ⇒ 没重算、也没被重置
		ptrBefore := fmt.Sprintf("%p", e.recent[0].shTitle)
		_ = e.recent[0].titleSh()
		if ptrAfter := fmt.Sprintf("%p", e.recent[0].shTitle); ptrBefore != ptrAfter {
			t.Fatalf("titleSh 第二次调用换了 map 实例（%s -> %s）⇒ 缓存没生效", ptrBefore, ptrAfter)
		}
	}
}

// TestNoveltyEqualsLegacyPath 判据②：novelty() 的返回值必须与不缓存的旧路径一致。
func TestNoveltyEqualsLegacyPath(t *testing.T) {
	mk := func() *Evaluator {
		x := New(evalCfgForCache())
		x.recent = mkWindow(40, 400)
		return x
	}
	cand := &model.Task{ID: "n-1", Title: "任务标题示例9", Prompt: "模板拼装后的提示词正文示例，含若干中文字符以贴近真实长度", TemplateID: "tpl-9", GoalID: "g1"}

	// 参照 = 改造前逐字相同的算法（每次现算 similarity，不做任何缓存）
	eRef := mk()
	var refMaxSim float64
	text := cand.Title + " " + cand.Prompt
	for i := range eRef.recent {
		it := &eRef.recent[i]
		if it.GoalID != "" && it.GoalID == cand.GoalID {
			continue
		}
		if s := similarity(text, it.Title+" "+it.Prompt); s > refMaxSim {
			refMaxSim = s
		}
	}
	refNovelty := clamp((1-refMaxSim)*100, 0, 100)

	e2 := mk()
	e2.mu.Lock()
	cached := e2.novelty(cand)
	e2.mu.Unlock()
	if cached != refNovelty {
		t.Fatalf("novelty 漂移: cached=%.6f ref=%.6f", cached, refNovelty)
	}
}

// TestCacheCorruptionIsDetected 负向对照：把缓存改坏，等价判据必须立刻抓到不一致。
// 若它绿着，说明 jaccard 根本没读缓存 ⇒ 上面的等价判据是恒绿的、没有牙。
func TestCacheCorruptionIsDetected(t *testing.T) {
	e := New(evalCfgForCache())
	e.recent = mkWindow(10, 200)
	cand := &model.Task{ID: "z", Title: "任务标题示例3", Prompt: "x"}
	it := &e.recent[3]
	before := jaccard(shinglesOf(cand.Title), it.titleSh())
	// 注入损坏：让缓存与真实内容脱钩。键长 10 ⇒ 真实 3-gram 永远不可能等于它。
	it.shTitle = map[string]struct{}{"poison-key": {}}
	after := jaccard(shinglesOf(cand.Title), it.titleSh())
	if before == after {
		t.Fatal("负向对照失效：缓存被改坏而相似度没变 ⇒ 等价判据恒绿")
	}
	if fresh := freshSimTitle(cand.Title, it.Title); fresh == after {
		t.Fatal("损坏的缓存仍与现算相等 ⇒ jaccard 没在使用缓存")
	}
}

// TestWindowFieldsStillVisible 判据③：新增缓存字段不得影响窗口对外读数。
func TestWindowFieldsStillVisible(t *testing.T) {
	e := New(evalCfgForCache())
	e.recent = mkWindow(20, 100)
	titles := e.RecentTitles(5)
	if len(titles) != 5 || titles[0] == "" {
		t.Fatalf("RecentTitles 读数异常: %v", titles)
	}
	if got := len(e.RecentItemsForTest()); got != 20 {
		t.Fatalf("窗口条数变了: %d", got)
	}
	if share := e.DomainShare("code"); share < 0 || share > 1 {
		t.Fatalf("DomainShare 越界: %v", share)
	}
}

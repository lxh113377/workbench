package evaluate

// window_contract_test.go -- 评估器窗口查询 API 的契约判据（2026-10-23 补）。
//
// 为什么必须有：逐函数扫描显示 RecentTitles / DomainShare / maxChainDepth 三个函数 0% 覆盖，
// 而它们都不是装饰性代码：
//   ① DomainShare 是**多样性门禁**的输入（DiversityCap 靠它判「同一领域是否过量」）。
//      若算错（分母用错 / 领域比较串味），门禁要么形同虚设（永远放行 → 内容单一化），
//      要么过度拦截（永远拒绝 → 候选不入队 → 热循环）。
//   ② RecentTitles 是「最近窗口」的对外读数（面板/查重提示用）。n 的越界处理若写反，
//      会返回空列表或全量列表，让调用方误判「最近没产出」或「重复泛滥」。
//   ③ maxChainDepth 是模板链深的兜底（模板没配时默认 5）。兜底值错了会让链
//      要么过浅（产出断链）要么过深（无限延伸刷量）。

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func newWindowEvaluator(t *testing.T) *Evaluator {
	t.Helper()
	e := New(config.EvalConfig{})
	// 窗口内容：content x2、code x1、knowledge x1（各带可辨识标题）
	for i, spec := range []struct{ domain, title string }{
		{"content", "第一篇"},
		{"content", "第二篇"},
		{"code", "代码一"},
		{"knowledge", "知识一"},
	} {
		e.Observe(&model.Task{ID: "w-" + string(rune('a'+i)), Title: spec.title, Domain: spec.domain}, 7, spec.domain, 200)
	}
	return e
}

// TestDomainShareExactFractions 钉死占比计算：命中数/窗口总数，空窗口为 0。
func TestDomainShareExactFractions(t *testing.T) {
	e := newWindowEvaluator(t)
	cases := []struct {
		domain string
		want   float64
	}{
		{"content", 0.5},    // 2/4
		{"code", 0.25},      // 1/4
		{"knowledge", 0.25}, // 1/4
		{"absent", 0},       // 0/4：不存在的领域必须是 0 而不是 1 或 NaN
	}
	for _, c := range cases {
		got := e.DomainShare(c.domain)
		if got < c.want-1e-9 || got > c.want+1e-9 {
			t.Fatalf("DomainShare(%q) = %v, want %v", c.domain, got, c.want)
		}
	}
}

// TestDomainShareEmptyWindowIsZero 钉死空窗口返回 0。
// 若返回 NaN（0/0），调用方拿 NaN 做比较时所有比较都是 false：
// 既不会触发 DiversityCap 的拦截，也会让面板显示 NaN —— 静默且难查。
func TestDomainShareEmptyWindowIsZero(t *testing.T) {
	e := New(config.EvalConfig{})
	got := e.DomainShare("content")
	if got != 0 {
		t.Fatalf("空窗口 DomainShare 必须为 0（NaN 会让比较恒 false，门禁静默失效），got %v", got)
	}
}

// TestDomainShareAfterWindowRollover 钉死滚动窗口的一致性。
// Observe 超过 maxKeep 会截断旧项；占比必须按**当前窗口**算，不能按累计历史算。
func TestDomainShareAfterWindowRollover(t *testing.T) {
	e := New(config.EvalConfig{})
	// 先灌 3 条 content，再灌 2 条 code，maxKeep=2 => 窗口只剩最近 2 条 code。
	for i := 0; i < 3; i++ {
		e.Observe(&model.Task{ID: "c" + string(rune('a'+i)), Title: "c", Domain: "content"}, 7, "content", 2)
	}
	for i := 0; i < 2; i++ {
		e.Observe(&model.Task{ID: "k" + string(rune('a'+i)), Title: "k", Domain: "code"}, 7, "code", 2)
	}
	if got := e.DomainShare("code"); got != 1.0 {
		t.Fatalf("滚出窗口后 code 应占 100%%（窗口仅剩 2 条 code），got %v", got)
	}
	if got := e.DomainShare("content"); got != 0 {
		t.Fatalf("已滚出窗口的 content 不应再计入占比，got %v", got)
	}
}

// TestRecentTitlesSemantics 钉死「最近 n 条」的取值与越界语义。
func TestRecentTitlesSemantics(t *testing.T) {
	e := newWindowEvaluator(t)
	// n=4 取全部，且顺序按观察顺序（从旧到新，最后一个最新）。
	all := e.RecentTitles(4)
	if len(all) != 4 {
		t.Fatalf("n=4 应返回 4 条，got %d: %v", len(all), all)
	}
	if all[len(all)-1] != "知识一" {
		t.Fatalf("返回顺序应为观察顺序（最新在末尾），got %v", all)
	}
	// n=2 取最近两条。
	last2 := e.RecentTitles(2)
	if len(last2) != 2 || last2[0] != "代码一" || last2[1] != "知识一" {
		t.Fatalf("n=2 应返回最近两条 [代码一 知识一]，got %v", last2)
	}
	// n 大于窗口长度：按窗口长度返回（不得 panic / 不得返回空）。
	over := e.RecentTitles(999)
	if len(over) != 4 {
		t.Fatalf("n 超过窗口长度时应返回全部 4 条，got %d", len(over))
	}
	// n<=0：按实现口径返回全部（文档化行为，不得返回 nil 的歧义状态）。
	nonpos := e.RecentTitles(0)
	if len(nonpos) != 4 {
		t.Fatalf("n=0 应按实现口径返回全部，got %d", len(nonpos))
	}
}

// TestRecentTitlesEmptyWindow 钉死空窗口安全。
func TestRecentTitlesEmptyWindow(t *testing.T) {
	e := New(config.EvalConfig{})
	got := e.RecentTitles(5)
	if len(got) != 0 {
		t.Fatalf("空窗口应返回空列表，got %v", got)
	}
}

// TestMaxChainDepthFallback 钉死链深兜底值 5。
// 兜底错了的后果：过浅 => 产出断链；过深 => 无限延伸刷量。
func TestMaxChainDepthFallback(t *testing.T) {
	if got := maxChainDepth(nil); got != 5 {
		t.Fatalf("nil 模板必须兜底 5，got %d", got)
	}
	if got := maxChainDepth(&model.Template{}); got != 5 {
		t.Fatalf("未配链深的模板必须兜底 5，got %d", got)
	}
	if got := maxChainDepth(&model.Template{Chain: model.ChainSpec{MaxDepth: -3}}); got != 5 {
		t.Fatalf("负链深必须兜底 5（负数会让链立即封顶），got %d", got)
	}
	if got := maxChainDepth(&model.Template{Chain: model.ChainSpec{MaxDepth: 9}}); got != 9 {
		t.Fatalf("显式链深必须被尊重，got %d", got)
	}
}

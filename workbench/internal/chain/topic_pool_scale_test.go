package chain

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/templates"
)

// windowCapacity 是评估器滚动窗口容量（engine.go 多处 Observe(..., 400)）。
const windowCapacity = 400

// TestTopicPoolScalesBeyondWindow：T32-c(1)。每域 >=42 条工作空间口径选题，且唯一 (模板,主题)
// 组合数 = Σ_模板 |域池| >= 3×滚动窗口 —— 修前必红（每域 5 条 / 组合数 145 < 1200）。
func TestTopicPoolScalesBeyondWindow(t *testing.T) {
	const perDomain = 42
	if len(topicsByDomain) < 8 {
		t.Fatalf("选题池只有 %d 个域（<8）—— 判据受理面不足", len(topicsByDomain))
	}
	for d, pool := range topicsByDomain {
		if len(pool) < perDomain {
			t.Errorf("域 %q 主题数 %d < %d —— 组合数不足，滚动窗口会覆盖全池", d, len(pool), perDomain)
			continue
		}
		seen := map[string]bool{}
		for _, topic := range pool {
			if strings.TrimSpace(topic) == "" || seen[topic] {
				t.Errorf("域 %q 空选题或重复项: %q（重复项不增加组合数）", d, topic)
			}
			seen[topic] = true
		}
	}
	all := templates.Defaults()
	if len(all) == 0 {
		t.Fatal("内置模板为 0 —— 判据会空集全绿")
	}
	combos := 0
	for _, tpl := range all {
		pool, ok := topicsByDomain[tpl.Domain]
		if !ok {
			pool = topicsByDomain["content"] // 生产回落路径：缺池取 content
		}
		combos += len(pool)
	}
	if combos < 3*windowCapacity {
		t.Errorf("唯一 (模板,主题) 组合数 %d < 3×窗口 %d —— 结构性饥饿未解除", combos, 3*windowCapacity)
	}
}

// TestPickTopicSkipsExcluded：T32-c(2)。排除近期主题后不得返回被排除项；池尽返回 ""，RootTask 跳过（nil）。
func TestPickTopicSkipsExcluded(t *testing.T) {
	g := &Generator{pick: newRandPicker()}
	pool := topicsByDomain["quality"]
	if len(pool) < 2 {
		t.Fatalf("quality 池 %d 条，无法构造排除实验", len(pool))
	}
	excluded, last := pool[:len(pool)-1], pool[len(pool)-1]
	for i := 0; i < 200; i++ {
		if got := g.pickTopic("quality", excluded...); got != last {
			t.Fatalf("排除 %d 条后第 %d 次返回 %q，want %q —— 排除未生效", len(excluded), i, got, last)
		}
	}
	if got := g.pickTopic("quality", pool...); got != "" {
		t.Fatalf("池尽应返回空串表示跳过，得到 %q", got)
	}
	cfg := config.ChainConfig{Enabled: true, MaxDepth: 5, MinQuality: 4, ContextChars: 2000}
	g2, reg := newTestGen(t, cfg)
	tpl := mustTpl(t, reg, "quality-audit")
	task := g2.RootTask(tpl, 1700000000, excluded...)
	if task == nil || !strings.HasSuffix(task.Title, last) {
		t.Fatalf("RootTask 未用唯一可选主题: %+v", task)
	}
	if g2.RootTask(tpl, 0, pool...) != nil {
		t.Fatal("池尽时 RootTask 必须返回 nil（跳过），不得重产近重复选题")
	}
}

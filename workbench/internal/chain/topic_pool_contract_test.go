package chain

import (
	"strings"
	"testing"

	"workbench/internal/templates"
)

// TestEveryTemplateDomainHasTopicPool 钉住「模板域集合 ⊆ 选题池键集合」。
//
// 为什么需要（2026-10-26 五轴审查实测，报告 docs/工作空间聚焦与模板域覆盖-五轴审查-2026-10-26.md §1.2）：
// 模板库有 7 个域，而 topicsByDomain 只覆盖 6 个 —— **quality 缺键**。
// pickTopic（chain.go:197-209）在 !ok 时**静默回落** topicsByDomain["content"]，
// 于是「工程质量体检 / 五轴审查 / 判据规格设计 / 判据有效性验证 / 工程卫生清理」这 5 条模板
// 被调度时拿到的是「AI 时代的个人效率」这类**内容选题**：任务标题形如
// 「五轴审查报告 · 远程协作的隐性成本」，与工作空间项目一点关系都没有。
//
// 这条缺陷的形态是「集合对账」族：**域集合与池键集合没有任何判据对齐**，加一个域忘加池就静默降级。
// 本测试是该族的下限断言（防空转）：解析面必须真跑起来，否则空集比较会全绿。
func TestEveryTemplateDomainHasTopicPool(t *testing.T) {
	all := templates.Defaults()
	if len(all) == 0 {
		t.Fatal("内置模板为 0 条 —— 解析面没跑起来，本判据会空集全绿（防空转）")
	}
	domains := map[string]bool{}
	for _, tpl := range all {
		domains[tpl.Domain] = true
	}
	if len(domains) < 2 {
		t.Fatalf("只解析到 %d 个领域（<2）—— 解析面漂移，判据不可信", len(domains))
	}
	for domain := range domains {
		pool, ok := topicsByDomain[domain]
		if !ok || len(pool) == 0 {
			t.Errorf("领域 %q 无选题池 —— pickTopic 会静默回落 content 选题，该域任务的主题与被测项目无关", domain)
			continue
		}
		for i, topic := range pool {
			if strings.TrimSpace(topic) == "" {
				t.Errorf("领域 %q 的第 %d 条选题为空串", domain, i)
			}
		}
	}
	t.Logf("领域↔选题池对齐：模板 %d 条 / 领域 %d 个 / 池键 %d 个", len(all), len(domains), len(topicsByDomain))
}

// TestQualityTopicDoesNotFallBackToContent 是上面那条的行为面探针：
// 直接调真实 pickTopic("quality", "")，断言结果**不得**出现在 content 池里。
// 修前必红（拿到的就是 content 选题）、修后必绿；把「回落」这件事从代码阅读变成可观测事实。
func TestQualityTopicDoesNotFallBackToContent(t *testing.T) {
	if _, ok := topicsByDomain["content"]; !ok {
		t.Fatal("content 池不存在 —— 对照组不成立的实验等于没做")
	}
	g := &Generator{pick: newRandPicker()}
	for i := 0; i < 20; i++ {
		got := g.pickTopic("quality", "")
		for _, c := range topicsByDomain["content"] {
			if got == c {
				t.Fatalf("quality 选题落回 content 池：%q —— 工程质量任务的主题将与被测项目无关", got)
			}
		}
	}
}

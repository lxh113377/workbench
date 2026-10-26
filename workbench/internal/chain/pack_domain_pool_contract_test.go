package chain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// packPath 是工程模板包在仓库里的位置（与 internal/templates 的契约测试同一份资产）。
const packPath = "../../../docs/quality-packs/工程模板包-v1.json"

// TestPackDomainsHaveTopicPool 钉住「模板包用到的每个领域都必须有非空选题池」。
//
// 为什么需要：round 1 实测的 P0 就是这个形态 —— quality 域在模板库里存在、但 topicsByDomain 缺键，
// pickTopic（chain.go）静默回落 content 选题，于是工程质量任务的 {topic} 变成「AI 时代的个人效率」。
// 模板包新增了 frontend 领域（前端工程审查 / 构建与性能调优），若只加模板不加池，同一条缺陷会原样复发。
// 本判据把它挡在导入之前：判据先红（缺 frontend 池）、补齐后转绿。
func TestPackDomainsHaveTopicPool(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash(packPath))
	if err != nil {
		t.Fatalf("读不到工程模板包 %s: %v", packPath, err)
	}
	var pack []struct {
		ID     string `json:"id"`
		Domain string `json:"domain"`
	}
	if err := json.Unmarshal(raw, &pack); err != nil {
		t.Fatalf("模板包不是合法 JSON: %v", err)
	}
	if len(pack) == 0 {
		t.Fatal("模板包解析出 0 条 —— 判据会空集全绿，先修解析面")
	}
	seen := map[string]bool{}
	checked := 0
	for _, p := range pack {
		if p.Domain == "" {
			t.Errorf("模板 %s 没有 domain", p.ID)
			continue
		}
		if seen[p.Domain] {
			continue
		}
		seen[p.Domain] = true
		checked++
		pool, ok := topicsByDomain[p.Domain]
		if !ok || len(pool) == 0 {
			t.Errorf("模板包领域 %q 没有选题池 —— pickTopic 会静默回落 content 选题，该面任务的主题与被测项目无关", p.Domain)
			continue
		}
		for i, topic := range pool {
			if len(topic) == 0 {
				t.Errorf("领域 %q 第 %d 条选题为空串", p.Domain, i)
			}
		}
	}
	if checked < 2 {
		t.Fatalf("只检查到 %d 个领域（期望 >=2）—— 判据失去受理面", checked)
	}
}

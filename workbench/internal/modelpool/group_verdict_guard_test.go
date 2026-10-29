package modelpool

import (
	"os"
	"strings"
	"testing"
)

// TestModelLevelFailureNotPropagatedToGroup 钉住「模型级失败不得整组套用」（round 68 的事故回归护栏）。
//
// 事故（2026-10-29）：端点层探测只取组内第一条 spec 代表整个 base，而 baseOK 用它的 State 判定；
// group[0] 恰好是 z-ai/glm-5.3（网关只服务 -flash 变体）⇒ 它判 not_listed ⇒ 同 base 的 38 条车道全被判 down，
// pool.json 里 38 条 lastErr 一字不差（诊断字段「试过=[z-ai/glm-5.3 …]」对每条车道都一样），整池黑障循环。
//
// 为什么钉源文本：该分支要求「真实网关 + 组内多条 spec + 首条模型级失败」三者同时成立，
// 用单测造它等于重造网关；而回归点就是这两行（模型级失败要逐模型探测、不得整组套用）。
func TestModelLevelFailureNotPropagatedToGroup(t *testing.T) {
	src, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatalf("读 service.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, `k == "not_listed" || k == "model_rejected"`) {
		t.Fatal("缺少「模型级失败」识别：端点层结论会被整组套用，38/38 全灭事故会复发")
	}
	if !strings.Contains(body, "r := s.prober.Probe(sp)") {
		t.Fatal("模型级失败必须改为逐模型探测（用各自 id + variants），不能沿用组代表 spec 的结论")
	}
}

package modelpool

import (
	"os"
	"strings"
	"testing"
)

// TestModelIDVariantsCoverGatewayPrefix 钉住「清单 id 形态」的三种变体（round 67）。
//
// 事故背景（2026-10-29）：网关把车道广播成 `our-free-model/<id>`，而代码某处只比对裸 id ⇒
// 本机 38/38 车道被判 not_listed、整池 30 分钟黑障、任务一次未发出。
func TestModelIDVariantsCoverGatewayPrefix(t *testing.T) {
	s := Spec{ID: "lane-1", Model: "z-ai/glm-5.3-flash"}
	got := modelIDVariants(s)
	need := []string{"z-ai/glm-5.3-flash", "our-free-model/z-ai/glm-5.3-flash", "our-free-model-region/z-ai/glm-5.3-flash"}
	for _, n := range need {
		found := false
		for _, g := range got {
			if g == n {
				found = true
			}
		}
		if !found {
			t.Fatalf("变体缺少 %q（实得 %v）", n, got)
		}
	}
	// 已带前缀的 id 不得再叠前缀。
	s2 := Spec{ID: "lane-2", Model: "our-free-model/kilo-auto/free"}
	for _, g := range modelIDVariants(s2) {
		if strings.HasPrefix(g, "our-free-model/our-free-model/") {
			t.Fatalf("重复叠前缀：%q", g)
		}
	}
}

// TestModelListedMatchesPrefixedForm 是清单匹配的行为单测：带前缀形态必须命中。
func TestModelListedMatchesPrefixedForm(t *testing.T) {
	body := []byte(`{"data":[{"id":"our-free-model/z-ai/glm-5.3-flash"},{"id":"deepseek-official/deepseek-flash"}]}`)
	if listed, known := modelListed(body, "z-ai/glm-5.3-flash"); !known || listed {
		t.Fatalf("裸 id 不应命中（known=%v listed=%v）", known, listed)
	}
	if listed, known := modelListed(body, modelIDVariants(Spec{ID: "x", Model: "z-ai/glm-5.3-flash"})...); !known || !listed {
		t.Fatalf("带前缀变体必须命中（known=%v listed=%v）", known, listed)
	}
}

// TestProbeRetryPathUsesVariants 是接线守卫：密钥轮换重试路径必须走 modelIDVariants。
//
// 为什么钉源文本：该分支要求「第一次 401 + KeyResolver 给出新钥匙 + 第二次 200」，
// 在单测里造这套 HTTP 时序成本高；而这次的回归恰恰是一行实参（s.Model, s.ID ⇒ modelIDVariants(s)），
// 文本不变量足够钉死，且与被修的那行一一对应。
func TestProbeRetryPathUsesVariants(t *testing.T) {
	src, err := os.ReadFile("probe.go")
	if err != nil {
		t.Fatalf("读 probe.go 失败: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "modelListed(body2, s.Model, s.ID)") {
		t.Fatal("轮换重试路径又退回只比对裸 id —— 网关前缀形态会再次被判 not_listed（38/38 全灭事故）")
	}
	if !strings.Contains(body, "modelListed(body2, modelIDVariants(s)...)") {
		t.Fatal("轮换重试路径必须用 modelIDVariants 做清单比对")
	}
}

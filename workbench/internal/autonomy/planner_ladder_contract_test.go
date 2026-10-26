package autonomy

import (
	"strings"
	"testing"

	"workbench/internal/config"
)

// TestEveryAllowedDomainHasOwnLadder 钉住「自主白名单里的每个领域都必须有自己的推进阶梯」。
//
// 为什么需要（2026-10-26 round 7 实测）：domainLadder 只覆盖 content/code/knowledge/data/solution/report，
// 而 ladderFor（planner.go）对未知领域**静默回退 content 阶梯** —— 于是 domain=quality 的目标落到
// 「立框架：产出该主题的总纲与核心观点清单」这种内容写作阶段（--autonomy plan 实测输出即如此），
// 与「提升工作空间项目的工程质量」南辕北辙；frontend 域同理。
// 形态与 round 1 的「模板域 ↔ 选题池缺键」同族：集合对账没有判据，加一个域忘配阶梯就静默降级。
func TestEveryAllowedDomainHasOwnLadder(t *testing.T) {
	allowed := config.DefaultAutonomyConfig().Bounds.AllowedDomains
	if len(allowed) == 0 {
		t.Fatal("自主白名单为空 —— 判据失去受理面")
	}
	if len(domainLadder) == 0 {
		t.Fatal("domainLadder 为空 —— 解析面没跑起来")
	}
	for _, d := range allowed {
		if strings.TrimSpace(d) == "" {
			t.Errorf("白名单里有空领域名")
			continue
		}
		l, ok := domainLadder[d]
		if !ok || len(l) == 0 {
			t.Errorf("领域 %q 在白名单里但没有自己的阶梯 —— ladderFor 会静默回退 content 阶梯", d)
			continue
		}
		if len(l) < 3 {
			t.Errorf("领域 %q 的阶梯只有 %d 步（<3）—— 阶段推进两三轮就耗尽", d, len(l))
		}
		for i, step := range l {
			if strings.TrimSpace(step) == "" {
				t.Errorf("领域 %q 第 %d 步为空串", d, i)
			}
		}
	}
}

// TestQualityLadderIsEngineeringNotContent 是行为面探针：直接问 ladderFor 要质量/前端域的阶梯，
// 断言它不是 content 阶梯（回退态），且必须含工程动作词（体检/修复/判据）。
// 对照组用 content 第一步：对照组不成立的实验等于没做（本仓在册判据标准）。
func TestQualityLadderIsEngineeringNotContent(t *testing.T) {
	contentFirst := domainLadder["content"][0]
	if strings.TrimSpace(contentFirst) == "" {
		t.Fatal("content 阶梯为空 —— 对照组不成立的实验等于没做")
	}
	for _, d := range []string{"quality", "frontend"} {
		l := ladderFor(d)
		if len(l) == 0 {
			t.Fatalf("ladderFor(%q) 返回空", d)
		}
		if l[0] == contentFirst {
			t.Errorf("领域 %q 仍回退 content 阶梯（第一步 = %q）—— 自主任务会产出内容写作而不是工程动作", d, l[0])
		}
		joined := strings.Join(l, " ")
		if !strings.Contains(joined, "体检") && !strings.Contains(joined, "修复") && !strings.Contains(joined, "判据") {
			t.Errorf("领域 %q 的阶梯里没有任何工程动作词（体检/修复/判据）：%q", d, joined)
		}
	}
}

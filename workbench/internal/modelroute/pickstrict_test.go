package modelroute

// PickStrict 的判据（2026-10-28：方案阶段不再跨档位借执行车道）。
//
// 背景（实测）：活实例 plannerTier="quality" 而路由表 18 条车道全是 balanced/fast。
// 旧行为下 Pick 会静默放宽到任意档位，于是方案阶段拿执行车道出方案：24h 内方案占全部
// 尝试时长 30.1%，其中 94% 白烧（成功 8/140）；产物面 z-ai/glm-5.3 与 glm-5.3-flash
// 合计 29 次尝试留下 0 份 blueprint。本文件钉住"严格档位"这一新语义。

import (
	"strings"
	"testing"

	"workbench/internal/config"
)

// TestPickStrictDoesNotWidenTier 是核心判据：指定档位没有车道时必须报错，
// 而**同一个 Router**上用 Pick 还能拿到车道（这一对就是负向对照 —— 两者若行为相同，
// 说明 PickStrict 没起作用，判据没牙）。
func TestPickStrictDoesNotWidenTier(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "exec-a", Tier: "balanced", Enabled: true, Weight: 100},
		{Model: "exec-b", Tier: "fast", Enabled: true, Weight: 100},
	})

	// 对照面：宽松入口必须仍能拿到车道（执行阶段依赖这个兜底，不能被我改坏）。
	if got, err := r.Pick("quality", nil); err != nil || got == "" {
		t.Fatalf("Pick 必须保留跨档位兜底（执行阶段依赖它），实际 got=%q err=%v", got, err)
	}

	// 严格面：同一状态下必须报错，且错误里点名档位（否则运维只知道"没模型"，
	// 不知道是"这个档位没配车道"—— 那正是本轮要消灭的静默成因）。
	got, err := r.PickStrict("quality", nil)
	if err == nil {
		t.Fatalf("档位 quality 无车道时必须报错，实际返回 %q（说明仍在静默换档）", got)
	}
	if !strings.Contains(err.Error(), "quality") {
		t.Fatalf("错误信息必须点名档位，实际: %v", err)
	}
}

// TestPickStrictPicksWithinTier 证明严格入口不是"永远报错"，档位内有车道就正常选。
func TestPickStrictPicksWithinTier(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "planner-a", Tier: "quality", Enabled: true, Weight: 100},
		{Model: "exec-a", Tier: "balanced", Enabled: true, Weight: 100},
	})
	got, err := r.PickStrict("quality", nil)
	if err != nil {
		t.Fatalf("档位内有车道却报错: %v", err)
	}
	if got != "planner-a" {
		t.Fatalf("必须选档位内的车道，实际 %q", got)
	}
}

// TestPickStrictStillHonoursExcluded 严格入口必须同样尊重 excluded（冷却排除不能丢）。
func TestPickStrictStillHonoursExcluded(t *testing.T) {
	r := New([]config.ModelRoute{
		{Model: "planner-a", Tier: "quality", Enabled: true, Weight: 100},
		{Model: "planner-b", Tier: "quality", Enabled: true, Weight: 100},
	})
	got, err := r.PickStrict("quality", map[string]bool{"planner-a": true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "planner-b" {
		t.Fatalf("被排除的车道不得再选，实际 %q", got)
	}
	if _, err := r.PickStrict("quality", map[string]bool{"planner-a": true, "planner-b": true}); err == nil {
		t.Fatal("档位内车道全被排除时必须报错")
	}
}

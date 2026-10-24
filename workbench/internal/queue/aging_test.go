package queue

import (
	"testing"

	"workbench/internal/model"
)

// TestQueueAgingPreventsStarvation 钉住防饿死 aging 的两个核心性质（2026-10-25）。
//
// 现场动机：7×24 自主循环持续产任务，纯严格优先级下低优任务可被高优持续流入永久饿死。
// 生产优先级确实异质（1-5），不是恒相等 —— 故这是真实缺陷，测试必须落在非饱和区。
//
// 用注入时钟构造**等待档位不同**的任务（避开"全部饱和后退化回严格优先级"这一既有区）。
func TestQueueAgingPreventsStarvation(t *testing.T) {
	const nowMs = int64(1_800_000_000_000) // 固定基准，可复算
	q := New()
	q.now = func() int64 { return nowMs }

	mkAt := func(id string, prio int, created int64) *model.Task {
		return &model.Task{ID: id, Priority: prio, CreatedAt: created, State: model.StateQueued, Title: id}
	}
	// low：prio1，已等 30 分钟 => boost = 30min/5min = 6，封顶 5 => 有效优先级 6
	q.Push(mkAt("low-waited", 1, nowMs-30*60*1000))
	// high：prio5，刚到 => 有效 5。low 等待后有效 6 > 5 => low 应先出（饿死被解除）。
	q.Push(mkAt("high-fresh", 5, nowMs))
	// mid：prio3，刚到 => 有效 3
	q.Push(mkAt("mid-fresh", 3, nowMs))

	order := []string{}
	for {
		got := q.Pop()
		if got == nil {
			break
		}
		order = append(order, got.ID)
	}
	want := []string{"low-waited", "high-fresh", "mid-fresh"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("饿死未被解除：order=%v, want=%v（等待 30 分钟的 prio1 应反超刚到的 prio5）", order, want)
	}
}

// TestQueueAgingBoundedWorstCase 钉住"有界"这一设计：boost 封顶 agingMaxBoost，
// 故 prio1 追平 prio5 所需等待是**有限**的（=(5-1+1)*agingStepMs 步，够它反超）。
// 若有人改成无界累加，本测试的"两个都远超封顶仍按基础优先级定序"会判红。
func TestQueueAgingBoundedWorstCase(t *testing.T) {
	const nowMs = int64(1_800_000_000_000)
	q := New()
	q.now = func() int64 { return nowMs }

	// 两者等待都远超封顶（+100 步），有效优先级 = 基础 + 5，差值仍由基础优先级决定。
	q.Push(&model.Task{ID: "p1-ancient", Priority: 1, CreatedAt: nowMs - 100*agingStepMs, State: model.StateQueued, Title: "x"})
	q.Push(&model.Task{ID: "p5-ancient", Priority: 5, CreatedAt: nowMs - 100*agingStepMs, State: model.StateQueued, Title: "y"})

	got := q.Pop()
	if got == nil || got.ID != "p5-ancient" {
		t.Fatalf("两者都饱和时应退化回严格优先级：期望 p5-ancient 先，实际 %v", got)
	}
	// 有界的直接后果：agingMaxBoost=5 时 prio1 最多到 6，恰好比 fresh prio5 高 1，
	// 但比 prio6… 不存在（上限 5）—— 保证"追赶"而非"无界插队"。
	if agingMaxBoost != 5 {
		t.Fatalf("agingMaxBoost 期望 5（够 prio1 反超 fresh prio5 一级），实际 %d", agingMaxBoost)
	}
}

// TestEffectivePriorityBounded 直接测纯函数 effectivePriority 的分档。
func TestEffectivePriorityBounded(t *testing.T) {
	const nowMs = int64(1_800_000_000_000)
	cases := []struct {
		name   string
		prio   int
		waitMs int64
		want   int
	}{
		{"fresh", 3, 0, 3},
		{"one-step", 3, agingStepMs, 4},
		{"two-step", 3, 2 * agingStepMs, 5},
		{"capped", 3, 100 * agingStepMs, 3 + agingMaxBoost},
		{"future-clamped", 3, -5000, 3}, // CreatedAt 在 now 之后：等待按 0 处理
	}
	for _, c := range cases {
		got := effectivePriority(&model.Task{Priority: c.prio, CreatedAt: nowMs - c.waitMs}, nowMs)
		if got != c.want {
			t.Fatalf("%s: prio=%d wait=%dms => 有效 %d, 期望 %d", c.name, c.prio, c.waitMs, got, c.want)
		}
	}
}

package engine

import (
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestSupplyRecentTitlesCountsUndeliveredDrafts 钉住供料侧 Recent 的集合口径（round 127 · T82）。
//
// 现场（2026-10-30 07:08→08:34，agent 根 queue.json 实读）：目标 goal-1792985630633 的标题恒为
//
//	「提升工作空间项目的全栈工程质量 · 小步修（正确性/第1轮）」
//
// 07:08 连拒 5 次、07:48 放出 1 条、08:22 再放 1 条、08:34 又连拒 5 次 —— 约 35 分钟一个循环。
// 根因：planner 的轮转号数 ctx.Recent 里「小步修（」出现次数，而 recentTitles 只收 State==Done；
// 该阶段从未交付成功 ⇒ 计数恒 0 ⇒ 标题恒定 ⇒ 被精确指纹判重 ⇒ 触发拒绝熔断。
//
// 边界（四条，缺一即判据不完整）：同目标未交付草案要算进来；跨目标不得污染；
// Done 不限目标照旧；goalID 为空时退化成"只看 Done"（不给假新颖度）。
func TestSupplyRecentTitlesCountsUndeliveredDrafts(t *testing.T) {
	const goal = "goal-A"
	const other = "goal-B"
	const t1 = "提升工作空间项目的全栈工程质量 · 小步修（正确性/第1轮）"
	all := []*model.Task{
		{ID: "1", GoalID: goal, Title: t1, State: model.StateRejected},
		{ID: "2", GoalID: goal, Title: t1, State: model.StateRejected},
		{ID: "3", GoalID: goal, Title: t1, State: model.StateCandidate},
		{ID: "4", GoalID: other, Title: "别的目标 · 小步修（可读性/第9轮）", State: model.StateRejected},
		{ID: "5", GoalID: "", Title: "模板任务 · 深度长文", State: model.StateDone},
		{ID: "6", GoalID: goal, Title: "提升工作空间项目的全栈工程质量 · 立项", State: model.StateDone},
		nil,
		{ID: "7", GoalID: goal, Title: "   ", State: model.StateQueued},
	}
	got := supplyRecentTitles(all, goal, 30)
	// 同目标三条未交付草案 + 两条 Done（不限目标）都应在集合里
	if n := strings.Count(strings.Join(got, "\n"), "小步修（"); n != 3 {
		t.Fatalf("同目标未交付草案必须计入轮转基数，实得 %d 条（现场根因正是这里为 0）：\n%v", n, got)
	}
	for _, x := range got {
		if strings.Contains(x, "可读性") {
			t.Fatalf("跨目标草案不得污染本目标的轮转计数：%v", got)
		}
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "深度长文") {
		t.Fatalf("Done 任务应照旧纳入（不限目标）：%v", got)
	}
	// goalID 为空 ⇒ 只看 Done（无活跃目标时不得凭空造新颖度）
	g2 := supplyRecentTitles(all, "", 30)
	if len(g2) != 2 {
		t.Fatalf("goalID 为空时应只收 Done（2 条），实得 %d：%v", len(g2), g2)
	}
	// 条数上限要生效（供料每 tick 都跑，无界会随历史增长）
	if len(supplyRecentTitles(all, goal, 1)) != 1 {
		t.Fatal("limit 未生效")
	}
	// 空白标题不进集合（否则 planner 会数到无意义条目）
	for _, x := range got {
		if strings.TrimSpace(x) == "" {
			t.Fatalf("空白标题不应进集合：%v", got)
		}
	}
}

package evaluate

// novelty_samegoal_test.go 守护「同目标新阶段不得被新颖度判死」（2026-10-21 现场）。
//
// 现场数字（逐条可从 dist/workbench-output/queue.json 核对）：
//   被拒草案的得分明细是 "价值先验 60 × 新颖度 0 × 链收益 50"
//   => value = 60*0.6 + 0*0.3 + 50*0.1 = 41
//   => total = 0.5*41 + 0.2*(100-60) + 0.3*65 = 48 < 阈值 55   **恒不通过**
// 也就是说：只要新颖度被判成 0，这条自主任务在任何参数下都进不了队。
//
// 而新颖度为什么会是 0：自主 Prompt 由 buildLadderPrompt 生成，固定注入
// 「项目核心目标 + 已完成阶段」，同目标相邻阶段全文相似度天然 0.9+。
// gates() 早已按「同目标豁免」处理这种相似（注释在册），novelty() 当时没跟上 ——
// 同一语义两处口径不一致，豁免被新颖度项目抵消，等于没修。
//
// 判据：
//   1) 同目标、内容不同的新阶段任务：新颖度必须 > 0，且能通过阈值；
//   2) 同目标、逐字相同的重放：新颖度必须 = 0（防重放能力不能被削掉）；
//   3) 跨目标的相似任务：照旧按相似度惩罚（不因这条放宽而失守）。

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func sameGoalCfg() config.EvalConfig {
	c := config.DefaultConfig().Eval
	c.Threshold = 55 // 与生产 config.json 一致
	return c
}

// TestSameGoalNewStageIsNotKilledByNovelty 是现场分数的直接回归。
func TestSameGoalNewStageIsNotKilledByNovelty(t *testing.T) {
	ev := New(sameGoalCfg())
	// 已完成的上一阶段（同目标）
	prev := &model.Task{
		ID: "task-prev", GoalID: "goal-A", ChainID: "chain-A",
		Title:  "工作台自身质量提升 · 深化：复盘 · 深化：复盘 · 深化：",
		Prompt: sameGoalPrompt(5),
		State:  model.StateDone, CreatedAt: model.Now(),
	}
	ev.Observe(prev, 7, "content", 200)
	ev.SetChainQuality("chain-A", 7)

	// 新一轮阶段任务：目标是下一个阶段，标题带新焦点
	next := &model.Task{
		ID: "task-next", GoalID: "goal-A", ChainID: "chain-A",
		Title:  "工作台自身质量提升 · 复盘",
		Prompt: sameGoalPrompt(6),
		CreatedAt: model.Now(),
	}
	scores := ev.Evaluate(Input{Task: next, Now: model.Now()})
	if len(scores.Gates) > 0 {
		t.Fatalf("同目标新阶段不该命中硬门禁（精确重复才该拦）：%v", scores.Gates)
	}
	if scores.Value < 50 {
		t.Fatalf("同目标新阶段的价值分被新颖度判死：value=%.1f（现场 41 分即本缺陷）", scores.Value)
	}
	if !Approve(scores) {
		t.Fatalf("同目标新阶段必须能通过阈值：total=%.1f threshold=%.1f reasons=%v",
			scores.Total, scores.Threshold, scores.Reasons)
	}
}

// TestSameGoalExactReplayStillRejected 断言防重放能力不得被削弱。
func TestSameGoalExactReplayStillRejected(t *testing.T) {
	ev := New(sameGoalCfg())
	prev := &model.Task{
		ID: "task-r1", GoalID: "goal-B",
		Title: "同一条草案", Prompt: "完全相同的提示词内容",
		State: model.StateDone, CreatedAt: model.Now(),
	}
	ev.Observe(prev, 7, "content", 200)
	replay := &model.Task{
		ID: "task-r2", GoalID: "goal-B",
		Title: "同一条草案", Prompt: "完全相同的提示词内容",
		CreatedAt: model.Now(),
	}
	scores := ev.Evaluate(Input{Task: replay, Now: model.Now()})
	if !containsGate(scores.Gates, "完全重复") {
		t.Fatalf("同目标逐字重放必须被硬门禁拦下，实际 gates=%v", scores.Gates)
	}
	if Approve(scores) {
		t.Fatal("重放任务不得通过评估")
	}
	if n := ev.novelty(replay); n != 0 {
		t.Fatalf("重放任务的新颖度必须为 0，实际 %.1f", n)
	}
}

// TestCrossGoalSimilarityStillPenalized 断言跨目标相似度照旧生效。
func TestCrossGoalSimilarityStillPenalized(t *testing.T) {
	ev := New(sameGoalCfg())
	prev := &model.Task{
		ID: "task-x1", GoalID: "goal-X",
		Title: "深度长文 · AI 效率", Prompt: "请写一篇关于AI效率的深度文章，包含结论与建议",
		State: model.StateDone, CreatedAt: model.Now(),
	}
	ev.Observe(prev, 8, "content", 200)
	cross := &model.Task{
		ID: "task-x2", GoalID: "goal-Y",
		Title: "深度长文 · AI 效率", Prompt: "请写一篇关于AI效率的深度文章，包含结论与建议",
		CreatedAt: model.Now(),
	}
	if n := ev.novelty(cross); n > 5 {
		t.Fatalf("跨目标的近乎相同任务新颖度必须接近 0，实际 %.1f", n)
	}
	scores := ev.Evaluate(Input{Task: cross, Now: model.Now()})
	if Approve(scores) {
		t.Fatalf("跨目标的高度相似任务不得通过（相似度 %.2f），gates=%v",
			sameCfgCap(scores), scores.Gates)
	}
}

// containsGate 判断门禁描述里是否含某片段（避免引入 strings 到本文件）。
func containsGate(gates []string, sub string) bool {
	for _, g := range gates {
		if len(g) >= len(sub) && indexOf(g, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func sameCfgCap(s *model.EvalScores) float64 {
	if s == nil {
		return 0
	}
	return s.Total
}

// sameGoalPrompt 复刻自主规划器 buildLadderPrompt 的形状：固定注入「项目核心目标 +
// 当前阶段 + 已完成阶段」。同目标相邻阶段只有阶段序号与"已完成阶段"清单不同，
// 全文相似度因此天然很高 —— 这正是现场新颖度被判成 0 的结构性原因。
func sameGoalPrompt(stage int) string {
	base := `你是自主执行本项目的 AI。项目核心目标：持续提升工作台质量。` + newline +
		`当前阶段性目标（第 ` + itoaSmall(stage) + ` 阶段）：复盘：对照原目标检查覆盖度与遗漏` + newline + newline +
		`已完成阶段：` + newline
	for i := 1; i <= stage; i++ {
		base += `- 第` + itoaSmall(i) + `阶段 阶段产出（质量 7/10，通过）` + newline
	}
	return base
}

// newline / itoaSmall 让上面的 Prompt 构造不依赖反斜杠转义（避免测试文件被编辑器转义弄坏）。
const newline = string(rune(10))

func itoaSmall(n int) string {
	if n < 0 {
		return "0"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0' + n/10)) + string(rune('0'+n%10))
}

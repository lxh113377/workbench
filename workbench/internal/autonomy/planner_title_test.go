package autonomy

// planner_title_test.go 守护「长目标标题下标题必须仍能区分阶段」（2026-10-21 现场）。
//
// 现场（dist 生产实例连续 12 小时"运行中但今日产出 0"）：
//   当日 2000 条自主草案，**标题与 Prompt 完全逐字节相同**（unique titles = 1），
//   全部被评估器以"综合分 48 < 阈值 55"拒绝，不入队、不产出；
//   2000 条拒绝把 dailyCap 吃满后，整台工作台当天彻底停工。
//
// 机理（两个单位混用的叠加）：
//   1) 目标标题随"深化"代际复利增长，现场已到 31 rune / 78 字节；
//   2) 规划器用 model.Truncate(s, MaxTitleRunes) 截标题，而 model.Truncate 的
//      n 是**字节**，MaxTitleRunes 是**rune**上限（80）—— 中文 3 字节/rune，
//      78 + " · 阶段6"(+11B) = 89B 截到 80B 后回退到合法边界，正好停在 78B：
//      追加的阶段/焦点后缀**被整体吃掉**，标题退化成目标标题本身；
//   3) 阶梯分支与兜底分支都这样，于是每轮都产出同一条标题；
//   4) 评估器的 recent 窗口里有上一条 done 的同名同 Prompt 任务 => 新颖度 0
//      => 价值 41 / 成本 60 / 可行 65 => 综合 48 < 55 => 拒绝 => 3 秒后再来一次。
//
// 判据：不同阶段/焦点必须产出**不同**标题，且标题长度按 rune 计不超过 MaxTitleRunes。

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// prodGoalTitle 是 2026-10-21 生产现场的真实目标标题（31 rune / 78 字节）。
const prodGoalTitle = "工作台自身质量提升 · 深化：复盘 · 深化：复盘 · 深化："

func TestProdTitleIsLongEnoughToReproduce(t *testing.T) {
	n := utf8.RuneCountInString(prodGoalTitle)
	if n < 25 {
		t.Fatalf("回归样本必须是长标题（现场 31 rune），当前 %d", n)
	}
	if len(prodGoalTitle)+len(" · 阶段6") <= MaxTitleRunes {
		t.Fatalf("样本必须超过字节上限才会复现：%d 字节", len(prodGoalTitle))
	}
}

// TestLadderTitleKeepsFocusForLongGoal 证明：长目标标题下，阶梯分支的
// 焦点后缀必须保留（修复前被字节截断整体吃掉，退化成目标标题本身）。
func TestLadderTitleKeepsFocusForLongGoal(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 500, MaxDrafts: 1, StageEvery: 1})
	goal := &Goal{ID: "g-long", Title: prodGoalTitle, Intent: "持续提升工作台质量", Domain: "content"}
	// 阶段 5（超出阶梯长度）走上界钳制 -> 焦点 = 阶梯最后一级"复盘…"
	drafts := p.Plan(PlanContext{Goal: goal, StageIndex: 5})
	if len(drafts) == 0 {
		t.Fatal("长标题目标也必须能规划出草案")
	}
	got := drafts[0].Title
	if got == goal.Title {
		t.Fatalf("标题退化成目标标题本身（阶段信息丢失）：%q", got)
	}
	if !strings.Contains(got, "复盘") {
		t.Fatalf("标题必须保留阶段焦点，实际 %q", got)
	}
	if n := utf8.RuneCountInString(got); n > MaxTitleRunes {
		t.Fatalf("标题按 rune 计超限：%d > %d", n, MaxTitleRunes)
	}
}

// TestFallbackTitleUniqueForLongGoal 证明：兜底分支在长标题 + 近期已出现同名时
// 仍必须产出唯一标题（修复前退化为目标标题本身，去重循环 20 轮后照样重名）。
func TestFallbackTitleUniqueForLongGoal(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 500, MaxDrafts: 1, StageEvery: 1})
	goal := &Goal{ID: "g-fb", Title: prodGoalTitle, Intent: "持续提升工作台质量", Domain: "content"}
	// 让阶梯分支与自评分支都不产出（近期已有同名），只剩兜底路径。
	recent := []string{
		goal.Title + " · 复盘",
		goal.Title,
	}
	drafts := p.Plan(PlanContext{Goal: goal, StageIndex: 5, Recent: recent})
	if len(drafts) == 0 {
		t.Fatal("兜底路径必须保证不断线（至少产出一条草案）")
	}
	got := drafts[0].Title
	if got == goal.Title {
		t.Fatalf("兜底标题退化成目标标题本身：%q", got)
	}
	if duplicated(got, recent) {
		t.Fatalf("兜底标题与近期任务重复（会被评估器判重拒掉）：%q", got)
	}
	if n := utf8.RuneCountInString(got); n > MaxTitleRunes {
		t.Fatalf("兜底标题按 rune 计超限：%d > %d", n, MaxTitleRunes)
	}
}

// TestDeriveNextGoalTitleDoesNotCompoundUnbounded 证明：目标代际推导不得无限复利
// 叠加"深化：复盘"（现场正是这样把标题顶到 78 字节，从而触发上面的截断退化）。
func TestDeriveNextGoalTitleDoesNotCompoundUnbounded(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 500})
	prev := &Goal{ID: "g-chain", Title: prodGoalTitle, Intent: "持续提升", Domain: "content"}
	d := p.DeriveNextGoal(prev, []string{"覆盖面不够"}, 1_700_000_000_000, GoalDefaults{MinStages: 3, MinQuality: 6, MinWords: 1200})
	if d == nil {
		t.Fatal("应推导出下一目标")
	}
	if n := utf8.RuneCountInString(d.Title); n > MaxTitleRunes {
		t.Fatalf("推导出的目标标题按 rune 计超限：%d > %d", n, MaxTitleRunes)
	}
	if c := strings.Count(d.Title, "深化："); c > 1 {
		t.Fatalf("目标标题仍在复利叠加（深化：出现 %d 次）：%q", c, d.Title)
	}
}

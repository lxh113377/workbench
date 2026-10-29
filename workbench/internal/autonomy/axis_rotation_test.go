package autonomy

import (
	"strings"
	"testing"
)

// TestQualityStage0RotatesFiveAxes 钉住「阶段 0 体检按五轴轮转」（round 62 · T61）。
//
// 同时钉住一条被测试当场抓出的约束：焦点段内层不得再用 "·"（duplicated 取「最后一个 · 之后」）。
//
// 为什么钉它：目标卡在第 0 阶段时，阶梯标题恒定 ⇒ 评估器按重复门禁反复拒绝
// （2026-10-28 23:58 实测同秒 5 连拒 ⇒ 熔断让位模板、目标永不推进）。
// 轮转让每一轮标题互不相同，且正好覆盖目标要的五轴。
func TestQualityStage0RotatesFiveAxes(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 400, MaxDrafts: 2, DefaultDomain: "content"})
	goal := &Goal{Title: "提升工作空间项目的全栈工程质量", Domain: "quality", Intent: "提升工作空间项目的全栈工程质量"}

	titleOf := func(recent []string) string {
		for _, d := range p.Plan(PlanContext{Goal: goal, StageIndex: 0, Recent: recent}) {
			if strings.Contains(d.Title, "体检（") {
				return d.Title
			}
		}
		return ""
	}

	// ① 首轮 = 正确性
	t1 := titleOf(nil)
	if !strings.Contains(t1, "体检（正确性/第1轮）") {
		t.Fatalf("首轮应为「体检（正确性/第1轮）」，实得 %q", t1)
	}
	// ② 已有 1 条 ⇒ 换轴（可读性），这就是破「恒定标题被判重」的关键
	t2 := titleOf([]string{t1})
	if !strings.Contains(t2, "体检（可读性/第1轮）") {
		t.Fatalf("第二轮应轮转到可读性，实得 %q", t2)
	}
	if t2 == t1 {
		t.Fatal("轮转失效：两轮标题相同 ⇒ 会退回「恒定标题被反复拒绝」的死循环")
	}
	// ③ 走完五轴回到第一轴时，轮次号必须递增（否则第 6 轮又会撞重）
	recent := []string{t1, t2}
	recent = append(recent, titleOf(recent))
	recent = append(recent, titleOf(recent))
	recent = append(recent, titleOf(recent))
	t6 := titleOf(recent)
	if !strings.Contains(t6, "体检（正确性/第2轮）") {
		t.Fatalf("第 6 轮应为正确性·第2轮（轮次递增），实得 %q", t6)
	}
	// ④ prompt 必须带上本轴，模型才知道这轮只做这一轴
	for _, d := range p.Plan(PlanContext{Goal: goal, StageIndex: 0}) {
		if strings.Contains(d.Title, "体检（") && !strings.Contains(d.Prompt, "只做「正确性」这一轴") {
			t.Fatalf("分轴体检的 prompt 未限定本轴：%s", d.Prompt[:120])
		}
	}
}

// TestNonQualityLadderUnchanged 是回归护栏：非 quality 域不得被五轴轮转影响。
func TestNonQualityLadderUnchanged(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 300, MaxDrafts: 2, DefaultDomain: "content"})
	goal := &Goal{Title: "写一篇技术文章", Domain: "content", Intent: "产出技术文章"}
	for _, d := range p.Plan(PlanContext{Goal: goal, StageIndex: 0}) {
		if strings.Contains(d.Title, "体检（") {
			t.Fatalf("content 域不应出现五轴体检标题：%q", d.Title)
		}
	}
}

// TestEveryQualityStepRotates 钉住「轮转不只作用于体检」（round 66）。
//
// 现场复发形态（2026-10-29 15:54）：目标推进到阶段 2 后，「小步修」标题恒定 ⇒ 评估器 5 连拒 ⇒ 熔断 ⇒ 22 分钟零供料。
// T61 当初只修了阶段 0 的「体检」，换个阶段就复发 —— 本用例就是那次复发的回归护栏。
func TestEveryQualityStepRotates(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 400, MaxDrafts: 2, DefaultDomain: "content"})
	goal := &Goal{Title: "提升工作空间项目的全栈工程质量", Domain: "quality", Intent: "提升工作空间项目的全栈工程质量"}
	titleOf := func(recent []string) string {
		for _, d := range p.Plan(PlanContext{Goal: goal, StageIndex: 2, Recent: recent}) {
			if strings.Contains(d.Title, "小步修（") {
				return d.Title
			}
		}
		return ""
	}
	t1 := titleOf(nil)
	if t1 == "" {
		t.Fatal("阶段 2 应产出「小步修（轴/第N轮）」形式的标题")
	}
	t2 := titleOf([]string{t1})
	if t2 == "" || t2 == t1 {
		t.Fatalf("阶段 2 的第二轮必须换轴换标题（否则复发「恒定标题被连拒」）：t1=%q t2=%q", t1, t2)
	}
	for _, d := range p.Plan(PlanContext{Goal: goal, StageIndex: 2}) {
		if strings.Contains(d.Title, "小步修（") && !strings.Contains(d.Prompt, "只做「") {
			t.Fatal("阶段 2 的 prompt 未限定本轴 —— 分轴推进就只是标题装饰")
		}
	}
}

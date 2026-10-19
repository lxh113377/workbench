package autonomy

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// testCfg 返回一份用于测试的自主配置（预算放大到不会误触）。
func testCfg() config.AutonomyConfig {
	c := config.DefaultAutonomyConfig()
	c.Budget.MaxTasksPerGoal = 50
	c.Budget.MaxStages = 20
	c.Budget.MinQuality = 6
	c.Budget.MaxRollbacks = 3
	c.Plan.Words = 500
	return c
}

func TestValidateDraftRejectsDangerousContent(t *testing.T) {
	c := testCfg()
	cases := []struct {
		name string
		d    Draft
		code string
	}{
		{"空标题", Draft{Title: "", Prompt: "请产出一篇文章", Domain: "content"}, "title.empty"},
		{"空Prompt", Draft{Title: "测试标题", Prompt: "", Domain: "content"}, "prompt.empty"},
		{"需要人工登录", Draft{Title: "填写表单并提交", Prompt: "请登录网站后填写表单并提交", Domain: "content"}, "semantic.manualRequired"},
		{"无交付物动词", Draft{Title: "随便想想", Prompt: "随便聊聊这个话题的方方面面，把想到的都说出来，不用特别在意结构，也不用给结论。", Domain: "content"}, "prompt.noDeliverable"},
		{"领域越界", Draft{Title: "测试标题", Prompt: "请产出关于测试的内容", Domain: "hacking"}, "domain.notAllowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := ValidateDraft(tc.d, c)
			if r.OK {
				t.Fatalf("应被拒绝但通过了: %+v", r.Draft)
			}
			found := false
			for _, v := range r.Violations {
				if v.Code == tc.code && v.Fatal {
					found = true
				}
			}
			if !found {
				t.Fatalf("缺少期望的违规码 %s，实际 %+v", tc.code, r.Violations)
			}
		})
	}
}

func TestValidateDraftTruncatesLongText(t *testing.T) {
	c := testCfg()
	longTitle := ""
	for i := 0; i < MaxTitleRunes+20; i++ {
		longTitle += "甲"
	}
	r := ValidateDraft(Draft{Title: longTitle, Prompt: "请产出一篇结构完整的文章，要求包含核心结论、关键论据与可执行建议，语言专业但不晦涩。", Domain: "content"}, c)
	if !r.OK {
		t.Fatalf("超长标题应自动裁剪而非拒绝: %+v", r.Violations)
	}
	if n := runeLen(r.Draft.Title); n > MaxTitleRunes {
		t.Fatalf("标题裁剪后仍超长: %d", n)
	}
	if len(r.Violations) == 0 {
		t.Fatal("裁剪必须留痕（Violations 非空）")
	}
}

func TestValidateDraftFillsDefaults(t *testing.T) {
	c := testCfg()
	r := ValidateDraft(Draft{Title: "内容测试", Prompt: "请产出关于效率工具的分析报告，要求包含核心结论、关键论据与可执行建议，语言专业。", Domain: ""}, c)
	if !r.OK {
		t.Fatalf("应通过: %+v", r.Violations)
	}
	if r.Draft.Domain != "content" {
		t.Fatalf("空领域应回退默认领域，实际 %q", r.Draft.Domain)
	}
	if r.Draft.Words <= 0 {
		t.Fatalf("字数应补默认值，实际 %d", r.Draft.Words)
	}
}

func TestPlannerPrefersCritiqueIdeas(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 600, MaxDrafts: 2, StageEvery: 1})
	goal := &Goal{ID: "g1", Title: "效率提升", Intent: "提升个人工作效率", Domain: "content"}
	drafts := p.Plan(PlanContext{
		Goal: goal, StageIndex: 0,
		NextIdeas: []model.Idea{{Title: "为效率方法补三个反例", Prompt: "请产出效率方法的三类常见反例与纠正建议，含结论。", Domain: "content"}},
	})
	if len(drafts) == 0 {
		t.Fatal("应至少规划出一条草案")
	}
	if drafts[0].Title != "为效率方法补三个反例" {
		t.Fatalf("应优先采纳自评给出的后继想法，实际首条 %q（rationale=%s）", drafts[0].Title, drafts[0].Rationale)
	}
}

func TestPlannerLadderAdvancesWithStage(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 600, MaxDrafts: 1})
	goal := &Goal{ID: "g2", Title: "知识地图", Intent: "梳理分布式系统知识体系", Domain: "knowledge"}
	d0 := p.Plan(PlanContext{Goal: goal, StageIndex: 0})
	if len(d0) == 0 {
		t.Fatal("第 0 阶段应产出草案")
	}
	goal.Stages = []Stage{{Index: 0, Title: d0[0].Title, TaskID: "t0", Passed: true}}
	d1 := p.Plan(PlanContext{Goal: goal, StageIndex: 1})
	if len(d1) == 0 {
		t.Fatal("第 1 阶段应产出草案")
	}
	if d1[0].Prompt == d0[0].Prompt {
		t.Fatal("不同阶段应产出不同任务（阶梯推进），实际 Prompt 完全一致")
	}
	if d1[0].Title == d0[0].Title {
		t.Fatalf("不同阶段标题应变化，实际都是 %q", d1[0].Title)
	}
}

func TestPlannerSkipsDuplicateTitles(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 600, MaxDrafts: 1})
	goal := &Goal{ID: "g3", Title: "效率提升", Intent: "提升效率", Domain: "content"}
	d := p.Plan(PlanContext{Goal: goal, StageIndex: 0})
	if len(d) == 0 {
		t.Fatal("首轮应产出")
	}
	dup := p.Plan(PlanContext{Goal: goal, StageIndex: 0, Recent: []string{d[0].Title}})
	if len(dup) > 0 && dup[0].Title == d[0].Title {
		t.Fatalf("近期已有同标题任务时不应再规划相同标题: %q", dup[0].Title)
	}
}

func TestDeriveNextGoalRotatesDomainAndCriteria(t *testing.T) {
	p := NewPlanner(PlanConfig{Words: 600})
	prev := &Goal{ID: "g4", Title: "深度长文", Intent: "写深度文章", Domain: "content",
		Stages: []Stage{{Index: 0, Title: "a", TaskID: "t1", Passed: true, Quality: 8},
			{Index: 1, Title: "b", TaskID: "t2", Passed: true, Quality: 8}}}
	d := p.DeriveNextGoal(prev, []string{"论据不够扎实"}, 1000, GoalDefaults{MinStages: 3, MinQuality: 6, MinWords: 2000})
	if d == nil {
		t.Fatal("应推导出下一目标")
	}
	if d.Domain == "" || d.Intent == "" || len(d.Criteria) == 0 {
		t.Fatalf("下一目标字段不完整: %+v", d)
	}
	if d.Intent == "" || !contains(d.Intent, "论据不够扎实") {
		t.Fatalf("下一目标意图应吸收上一阶段不足，实际 %q", d.Intent)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

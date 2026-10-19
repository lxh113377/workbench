package autonomy

import (
	"testing"

	"workbench/internal/model"
)

func TestCountWordsCJKAndEnglish(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"你好世界", 4},
		{"hello world", 2},
		{"你好 world 世界", 5}, // 2 汉字 + 1 英文词 + 2 汉字
		{"你好，世界！", 4},
		{"a,b,c", 3},
	}
	for _, c := range cases {
		if got := CountWords(c.in); got != c.want {
			t.Fatalf("CountWords(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestEvaluateAllCriteriaPassed(t *testing.T) {
	goal := &Goal{
		ID: "g1", Title: "测试目标", Status: GoalActive,
		Criteria: []Criterion{
			{ID: "c1", Desc: "完成 2 个阶段", Kind: CriterionTaskDone, Threshold: 2},
			{ID: "c2", Desc: "均分不低于 7", Kind: CriterionMinQuality, Threshold: 7},
			{ID: "c3", Desc: "总字数不少于 200", Kind: CriterionMinWords, Threshold: 200},
		},
		Stages: []Stage{
			{Index: 0, TaskID: "t1", Quality: 8, Words: 150, Passed: true},
			{Index: 1, TaskID: "t2", Quality: 8, Words: 150, Passed: true},
		},
	}
	res := Evaluate(AcceptanceInput{Goal: goal})
	if !res.Passed {
		t.Fatalf("应通过验收，实际 %s（criteria=%+v）", res.Reason, res.Criteria)
	}
	if res.DoneCount != 2 || res.TotalWords != 300 {
		t.Fatalf("统计错误: done=%d words=%d", res.DoneCount, res.TotalWords)
	}
}

func TestEvaluateFailsWithReason(t *testing.T) {
	goal := &Goal{
		ID: "g2", Title: "测试目标",
		Criteria: []Criterion{
			{ID: "c1", Desc: "完成 3 个阶段", Kind: CriterionTaskDone, Threshold: 3},
		},
		Stages: []Stage{{Index: 0, TaskID: "t1", Quality: 8, Words: 100, Passed: true}},
	}
	res := Evaluate(AcceptanceInput{Goal: goal})
	if res.Passed {
		t.Fatal("阶段不足时不应通过")
	}
	if res.Reason == "" {
		t.Fatal("不通过必须给出可解释原因")
	}
	if res.Criteria[0].Detail == "" {
		t.Fatal("每条标准都要有现值/阈值说明（Detail）")
	}
}

func TestEvaluateIgnoresUnpassedStages(t *testing.T) {
	goal := &Goal{
		ID: "g3", Title: "测试目标",
		Criteria: []Criterion{{ID: "c1", Kind: CriterionTaskDone, Threshold: 2}},
		Stages: []Stage{
			{Index: 0, TaskID: "t1", Quality: 8, Words: 100, Passed: true},
			{Index: 1, TaskID: "t2", Quality: 2, Words: 100, Passed: false},
		},
	}
	res := Evaluate(AcceptanceInput{Goal: goal})
	if res.DoneCount != 1 {
		t.Fatalf("未通过的阶段不应计入进度，DoneCount = %d", res.DoneCount)
	}
	if res.Passed {
		t.Fatal("只有 1 个通过阶段，不应通过要求 2 个的验收")
	}
}

func TestManualCriterionDoesNotBlockAutomation(t *testing.T) {
	goal := &Goal{
		ID: "g4", Title: "测试目标",
		Criteria: []Criterion{
			{ID: "auto", Kind: CriterionTaskDone, Threshold: 1},
			{ID: "human", Kind: CriterionManual, Threshold: 1},
		},
		Stages: []Stage{{Index: 0, TaskID: "t1", Quality: 9, Words: 100, Passed: true}},
	}
	res := Evaluate(AcceptanceInput{Goal: goal})
	if !res.Passed {
		t.Fatalf("人工判据不应阻塞自动验收: %s", res.Reason)
	}
}

func TestJudgeStageThresholds(t *testing.T) {
	task := &model.Task{ID: "t1", Critique: &model.Critique{Quality: 5}}
	v := JudgeStage(task, 6)
	if v.Pass {
		t.Fatal("质量 5 < 下限 6 应判不通过")
	}
	if v.Reason == "" {
		t.Fatal("不通过必须给原因")
	}
	task.Critique.Quality = 7
	v = JudgeStage(task, 6)
	if !v.Pass {
		t.Fatal("质量 7 ≥ 下限 6 应通过")
	}
	if JudgeStage(nil, 6).Pass {
		t.Fatal("空任务不应通过")
	}
}

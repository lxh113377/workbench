package autonomy

import (
	"testing"

	"workbench/internal/model"
)

// TestEvaluateVerifiedCriteria 覆盖 P3 接线后的自主验收判据：
// "字数够、自评高"不再足以让目标通过，必须有真实执行验收通过的产出。
func TestEvaluateVerifiedCriteria(t *testing.T) {
	mkTask := func(id string, verified bool) *model.Task {
		return &model.Task{ID: id, Domain: "code", Verified: verified}
	}
	goal := &Goal{
		ID: "g1",
		Stages: []Stage{
			{Index: 0, TaskID: "t1", Words: 1200, Quality: 8, Passed: true},
			{Index: 1, TaskID: "t2", Words: 1200, Quality: 8, Passed: true},
			{Index: 2, TaskID: "t3", Words: 1200, Quality: 8, Passed: true},
		},
		Criteria: []Criterion{
			{ID: "c-done", Kind: CriterionTaskDone, Threshold: 3},
			{ID: "c-verified", Kind: CriterionVerified, Threshold: 2},
		},
	}
	// 3 个阶段都"完成"，但只有 1 个真实验收通过 -> 不应通过
	in := AcceptanceInput{
		Goal:  goal,
		Tasks: []*model.Task{mkTask("t1", true), mkTask("t2", false), mkTask("t3", false)},
	}
	res := Evaluate(in)
	if res.VerifiedCount != 1 {
		t.Fatalf("VerifiedCount = %d, want 1", res.VerifiedCount)
	}
	if res.Passed {
		t.Fatal("只有 1 个验收通过时目标不应通过（verified 判据未满足）")
	}

	// 2 个通过 -> 应通过
	in.Tasks = []*model.Task{mkTask("t1", true), mkTask("t2", true), mkTask("t3", false)}
	res = Evaluate(in)
	if res.VerifiedCount != 2 {
		t.Fatalf("VerifiedCount = %d, want 2", res.VerifiedCount)
	}
	if !res.Passed {
		t.Fatalf("2 个验收通过应满足阈值: %s", res.Reason)
	}
}

// TestEvaluateVerifiedRatio 覆盖比例判据。
func TestEvaluateVerifiedRatio(t *testing.T) {
	goal := &Goal{
		ID: "g2",
		Stages: []Stage{
			{Index: 0, TaskID: "a", Words: 100, Quality: 7, Passed: true},
			{Index: 1, TaskID: "b", Words: 100, Quality: 7, Passed: true},
			{Index: 2, TaskID: "c", Words: 100, Quality: 7, Passed: true},
			{Index: 3, TaskID: "d", Words: 100, Quality: 7, Passed: true},
		},
		Criteria: []Criterion{{ID: "c-ratio", Kind: CriterionVerifiedRatio, Threshold: 0.5}},
	}
	res := Evaluate(AcceptanceInput{
		Goal: goal,
		Tasks: []*model.Task{
			{ID: "a", Verified: true}, {ID: "b", Verified: true},
			{ID: "c", Verified: false}, {ID: "d", Verified: false},
		},
	})
	if res.VerifiedRatio != 0.5 {
		t.Fatalf("VerifiedRatio = %v, want 0.5", res.VerifiedRatio)
	}
	if !res.Passed {
		t.Fatalf("50%% 通过率应满足 0.5 阈值: %s", res.Reason)
	}
}

// TestDefaultCriteriaIncludesVerified 保证新建目标默认就带真实验收判据。
func TestDefaultCriteriaIncludesVerified(t *testing.T) {
	crits := DefaultCriteria(GoalDefaults{MinStages: 3, MinQuality: 6, MinWords: 2400}, "code")
	found := false
	for _, c := range crits {
		if c.Kind == CriterionVerified {
			found = true
			if c.Threshold < 1 {
				t.Fatalf("verified 判据阈值过小: %v", c.Threshold)
			}
		}
	}
	if !found {
		t.Fatal("默认验收标准必须包含 CriterionVerified（否则作文也能过目标验收）")
	}
}

// TestDefaultCriteriaVerifiedIsAchievable 守护一个真实产品缺口：
// 绝对计数型 c-verified（要求 ≥60% 阶段"真实验收通过"）在真实通道上恒不可达
// （模型对产物协议遵从率不稳定），会把目标永远卡在未完成，goalDone/nextGoal 永不出现。
// 判据：默认标准里的"真实验收"必须由比例制 + 最低 1 个两句表达，且比例阈值不能是 1.0。
func TestDefaultCriteriaVerifiedIsAchievable(t *testing.T) {
	crits := DefaultCriteria(GoalDefaults{MinStages: 4, MinQuality: 6, MinWords: 2000}, "content")
	var ratio *Criterion
	var floor *Criterion
	for i := range crits {
		switch crits[i].Kind {
		case CriterionVerifiedRatio:
			ratio = &crits[i]
		case CriterionVerified:
			floor = &crits[i]
		}
	}
	if ratio == nil {
		t.Fatal("默认标准必须包含 CriterionVerifiedRatio（比例制，可达成）")
	}
	if ratio.Threshold <= 0 || ratio.Threshold >= 1 {
		t.Fatalf("比例阈值必须在 (0,1) 内，实际 %v（1.0 = 要求全部真实通过，实测不可达）", ratio.Threshold)
	}
	if floor == nil || floor.Threshold != 1 {
		t.Fatalf("必须保留「至少 1 个真实验收通过」的下限，实际 %+v", floor)
	}
}

package config

import "testing"

// TestPlanStageMaxMinutesDefaults 钉住「方案阶段总预算」的默认与钳制（round 43 · T23）。
//
// 为什么必须单测：这个字段的零值有两种都可能被写错的解释 ——
//   - 当成「0 分钟预算」⇒ 方案阶段被瞬间掐死，闭环永远退化直执行；
//   - 当成「无预算」⇒ 单次 8 分钟 × N 条车道的旧缺陷（把任务预算吃光、执行阶段跑不到）原样复发。
//
// 老安装升级后字段必为 0，所以「零值 = 补默认」是硬要求。
func TestPlanStageMaxMinutesDefaults(t *testing.T) {
	p := PlanConfig{}
	NormalizePlan(&p, SchemaVersion)
	if p.StageMaxMinutes != 4 {
		t.Fatalf("零值应补默认 4 分钟，实际 %d", p.StageMaxMinutes)
	}
	for _, bad := range []int{-3, 0, 61, 999} {
		q := PlanConfig{StageMaxMinutes: bad}
		NormalizePlan(&q, SchemaVersion)
		if q.StageMaxMinutes != 4 {
			t.Fatalf("越界值 %d 应回落 4，实际 %d", bad, q.StageMaxMinutes)
		}
	}
	q := PlanConfig{StageMaxMinutes: 2}
	NormalizePlan(&q, SchemaVersion)
	if q.StageMaxMinutes != 2 {
		t.Fatalf("合法值 2 被改写为 %d", q.StageMaxMinutes)
	}
}

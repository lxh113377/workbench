package engine

import (
	"testing"
	"time"
)

// TestPlanStageBudget 钉住「方案阶段总预算」的口径（round 43 · T23）。
//
// 由来（实测）：方案阶段单次上限 8 分钟 × N 条车道，一个 15 分钟窗口就被一条任务的方案阶段吃光，
// 执行阶段（唯一产物出口）一次都没跑到。阶段预算用来把这些时间还给执行阶段。
func TestPlanStageBudget(t *testing.T) {
	cases := []struct {
		name    string
		minutes int
		want    time.Duration
	}{
		{"未配置（0）⇒ 4 分钟兜底", 0, 4 * time.Minute},
		{"负数 ⇒ 4 分钟兜底", -5, 4 * time.Minute},
		{"超范围（61）⇒ 4 分钟兜底", 61, 4 * time.Minute},
		{"合法值 1 原样", 1, time.Minute},
		{"合法值 4 原样", 4, 4 * time.Minute},
		{"上界 60 原样", 60, 60 * time.Minute},
	}
	for _, c := range cases {
		if got := planStageBudget(c.minutes); got != c.want {
			t.Errorf("%s: planStageBudget(%d) = %v, want %v", c.name, c.minutes, got, c.want)
		}
	}
}

package main

// acceptance_a7_test.go 守护 A7 的三态判定顺序（2026-10-19 现场缺陷）。
//
// 现场：生产实例只积累 1.65 小时，句柄 220→450，AnalyzeTrend 判 leak，A7 直接 FAIL。
// 但序列摊开看是**减速并趋于平台**（斜率 171/342/64，末 54 样本恒在 423–452）——
// 典型预热，不是无界泄漏。用不足 24h 的数据（含预热段）断定泄漏，违背 A7 自身前提：
// 判据要求覆盖 ≥24h，失败结论也必须建立在 ≥24h 之上。
//
// 判据矩阵（spanHours × verdict）：
//   leak   + <24h  => unverified（数据不足 ≠ 已证泄漏）
//   leak   + >=24h => fail
//   ok     + >=24h => pass
//   ok     + <24h  => unverified
//   unknown/x      => unverified

import (
	"testing"
)

// TestA7LeakBelow24hIsUnverified 是本次现场缺陷的回归测试。
func TestA7LeakBelow24hIsUnverified(t *testing.T) {
	// 直接验证判定顺序的语义：不足 24h 时不得 FAIL。
	// 说明：这里不构造完整 CSV（那属于 supervisor 包的单测），只锁定 acceptance 的
	// 分支前提 —— 把 spanHours 与 verdict 的四种组合显式列出来断言决策结果。
	type c struct {
		verdict string
		span    float64
		want    checkState
	}
	cases := []c{
		{"leak", 1.65, stateUnverified},
		{"leak", 24.0, stateFail},
		{"ok", 24.0, statePass},
		{"ok", 5.0, stateUnverified},
		{"unknown", 30.0, stateUnverified},
	}
	for _, tc := range cases {
		got := classifySoakVerdict(tc.verdict, tc.span)
		if got != tc.want {
			t.Fatalf("verdict=%s span=%.2f => %s, want %s", tc.verdict, tc.span, got, tc.want)
		}
	}
}

package gov

import (
	"strings"
	"testing"
)

// TestFailureRateMetricDeclaresCumulativeScope 钉住「累计口径指标必须标注口径」。
//
// 一手缺陷（2026-10-23 风险预警走查，真实现场）：失败率 = 累计失败/(累计完成+累计失败)，
// **无时间窗口**。实测：近 1 小时 0 失败、近 24 小时 287 条历史失败，累计恒 79%；
// 而治理台文案写「当前 79%」并建议「立即降负载或扩容」—— 把历史陈旧数据当成当前恶化，
// 会误导决策（因早已恢复的故障去扩容）。修：指标名加「（累计）」+ Note 口径提醒。
func TestFailureRateMetricDeclaresCumulativeScope(t *testing.T) {
	metrics := Evaluate(Sample{FailureRate: 0.79}, DefaultBudget())
	var fr *Eval
	for i := range metrics {
		if metrics[i].Key == "failureRate" {
			fr = &metrics[i]
			break
		}
	}
	if fr == nil {
		t.Fatal("找不到 failureRate 指标")
	}
	// ① 指标名必须显式标注累计口径
	if !strings.Contains(fr.Name, "累计") {
		t.Errorf("failureRate 指标名未标注「累计」口径：%q（会被误读为当前状态）", fr.Name)
	}
	// ② Note 必须说明它是全时段累计，并指引结合近期失败数
	if fr.Note == "" {
		t.Fatal("failureRate 缺少 Note 口径提醒")
	}
	if !strings.Contains(fr.Note, "全时段累计") {
		t.Errorf("Note 未说明「全时段累计」：%q", fr.Note)
	}
	if !strings.Contains(fr.Note, "近期") {
		t.Errorf("Note 未指引「结合近期失败数」：%q", fr.Note)
	}
}

// TestFailureRateDisposalMentionsCumulative 钉住处置文案不得只写「当前」。
//
// 处置文案是用户做决策的直接依据，必须能自我说明口径。
func TestFailureRateDisposalMentionsCumulative(t *testing.T) {
	e := Eval{Key: "failureRate", Name: "任务失败率（累计）", Unit: "比例", Direction: DirHigh, Value: 0.79}
	txt := adjustAction(e, DefaultBudget(), true)
	if !strings.Contains(txt, "累计") {
		t.Errorf("处置文案未标注累计口径：%q", txt)
	}
	if strings.Contains(txt, "当前 79") && !strings.Contains(txt, "累计") {
		t.Errorf("处置文案把累计值说成「当前」：%q", txt)
	}
}

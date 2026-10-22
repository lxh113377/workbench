package gov

import (
	"math"
	"strings"
	"testing"
)

// TestNumberTextGrouping 钉住千分位与两位小数上限。
func TestNumberTextGrouping(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{4000000, "4,000,000"},
		{5000000, "5,000,000"},
		{4096, "4,096"},
		{4.800000000000001, "4.8"},    // 6*0.8 的 IEEE754 噪声
		{0.06999999999999999, "0.07"}, // 0.1*0.7 的噪声
		{22.713333333, "22.71"},
		{-1234.5, "-1,234.5"},
		{0, "0"},
	}
	for _, c := range cases {
		if got := numberText(c.in); got != c.want {
			t.Errorf("numberText(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
	for _, bad := range []float64{math.NaN(), math.Inf(1)} {
		if got := numberText(bad); got != "-" {
			t.Errorf("非有限数应给占位 - ，实得 %q", got)
		}
	}
}

// TestPercentTextScale 钉住百分数量级：≥10 收整数、<10 留一位小数。
func TestPercentTextScale(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{51, "51%"},
		{510, "510%"},
		{7.0, "7.0%"},
		{0.5, "0.5%"},
		{9.95, "10.0%"}, // 边界：四舍五入后进位但仍带小数位
		{99.6, "100%"},
	}
	for _, c := range cases {
		if got := percentText(c.in); got != c.want {
			t.Errorf("percentText(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

// TestMetricValueTextSwallowsRatioUnit 钉住本轮的原始缺陷形态：
// 比例类指标不得输出 "0.51比例"（单位粘连 + 量级读不出）。
func TestMetricValueTextSwallowsRatioUnit(t *testing.T) {
	if got := metricValueText(0.51, "比例"); got != "51%" {
		t.Fatalf("比例类应换算成 51%%，实得 %q（原缺陷形态是 \"0.51比例\"）", got)
	}
	if got := metricValueText(6, "个"); got != "6个" {
		t.Fatalf("非比例类必须保留单位后缀，实得 %q", got)
	}
	if got := metricValueText(4000000, "元"); got != "4,000,000元" {
		t.Fatalf("大数需千分位，实得 %q", got)
	}
	// 阈值与值同量纲：否则「当前 51%，警示线 0.07」根本没法比
	if got := metricNumberText(0.07, "比例"); got != "7.0%" {
		t.Fatalf("比例类阈值也必须换算成百分数，实得 %q", got)
	}
}

// TestUsagePhraseDirectionAware 钉住占用率的语义方向：
// 低方向指标（日吞吐，越小越坏）不得报「占用率 400%」——那会让人以为超载，
// 真实含义是「只达到红线要求的 25%」。
func TestUsagePhraseDirectionAware(t *testing.T) {
	high := Eval{Key: "memMB", Name: "常驻内存", Unit: "MB", Direction: DirHigh, Value: 700, Red: 700, Warn: 560}
	if got := usagePhrase(high); !strings.Contains(got, "达红线 100%") {
		t.Fatalf("高方向应报「达红线 100%%」，实得 %q", got)
	}
	low := Eval{Key: "throughputDaily", Name: "日吞吐量", Unit: "个/日", Direction: DirLow, Value: 2, Red: 8, Warn: 9.6}
	got := usagePhrase(low)
	if strings.Contains(got, "400%") {
		t.Fatalf("低方向不得输出反向措辞（原缺陷：占用率 400%%），实得 %q", got)
	}
	if !strings.Contains(got, "仅为红线要求的 25%") {
		t.Fatalf("低方向应报「仅为红线要求的 25%%」，实得 %q", got)
	}
	// 红线未设时不能算出 Infinity/NaN 上面板
	zero := Eval{Key: "x", Direction: DirHigh, Value: 5, Red: 0}
	if got := usagePhrase(zero); got == "" || strings.Contains(got, "Inf") || strings.Contains(got, "NaN") {
		t.Fatalf("红线为 0 时需安全兜底，实得 %q", got)
	}
}

// TestVolumeAlertsDetailReadable 端到端钉住预警文案：把三类不可读形态作为反向断言。
// TestVolumeAlertsDetailNotRedundantWithTitle 钉住「Detail 不得重复 Title 的主语」。
// 面板把 Title（粗体）与 Detail 并列渲染，Detail 再以指标名开头就是同一行念两遍
// （2026-10-22 走查实测：「并发执行数 并发执行数 当前 6个…」）。
func TestVolumeAlertsDetailNotRedundantWithTitle(t *testing.T) {
	evals := []Eval{
		{Key: "concurrency", Name: "并发执行数", Unit: "个", Domain: DomainResource, Direction: DirHigh, Value: 6, Warn: 4.8, Red: 6, Level: LevelRed, Ratio: 1},
		{Key: "failureRate", Name: "任务失败率", Unit: "比例", Domain: DomainOperation, Direction: DirHigh, Value: 0.51, Warn: 0.07, Red: 0.1, Level: LevelRed, Ratio: 5.1},
	}
	for _, a := range VolumeAlerts(evals, "2026-10-22") {
		if strings.HasPrefix(a.Detail, a.Title) || strings.HasPrefix(a.Detail, strings.TrimSuffix(a.Title, "")) {
			t.Errorf("%s：Detail 以 Title 开头（面板同一行念两遍）：%s", a.Subject, a.Detail)
		}
		for _, name := range []string{"并发执行数", "任务失败率"} {
			if strings.Contains(a.Detail, name) {
				t.Errorf("%s：Detail 重复指标名 %q：%s", a.Subject, name, a.Detail)
			}
		}
	}
}

func TestVolumeAlertsDetailReadable(t *testing.T) {
	evals := []Eval{
		{Key: "failureRate", Name: "任务失败率", Unit: "比例", Domain: DomainOperation, Direction: DirHigh, Value: 0.51, Warn: 0.07, Red: 0.1, Level: LevelRed, Ratio: 5.1},
		{Key: "throughputDaily", Name: "日吞吐量", Unit: "个/日", Domain: DomainOperation, Direction: DirLow, Value: 2, Warn: 9.6, Red: 8, Level: LevelRed, Ratio: 4},
		{Key: "concurrency", Name: "并发执行数", Unit: "个", Domain: DomainResource, Direction: DirHigh, Value: 6, Warn: 4.8, Red: 6, Level: LevelRed, Ratio: 1},
	}
	alerts := VolumeAlerts(evals, "2026-10-22")
	if len(alerts) != 3 {
		t.Fatalf("三项红区应产出 3 条预警，实得 %d", len(alerts))
	}
	for _, a := range alerts {
		if strings.Contains(a.Detail, "比例") {
			t.Errorf("%s：Detail 残留单位词「比例」（原缺陷形态 0.51比例）：%s", a.Subject, a.Detail)
		}
		if strings.Contains(a.Detail, "0.51") || strings.Contains(a.Detail, "0.07") {
			t.Errorf("%s：Detail 仍是裸小数，未换算百分数：%s", a.Subject, a.Detail)
		}
		if strings.Contains(a.Detail, "占用率 0%") {
			t.Errorf("%s：占用率算成 0（除零或方向错）：%s", a.Subject, a.Detail)
		}
	}
	// 抽查两条关键文案的实际形态
	var fr, tp string
	for _, a := range alerts {
		if a.Subject == "failureRate" {
			fr = a.Detail
		}
		if a.Subject == "throughputDaily" {
			tp = a.Detail
		}
	}
	if !strings.Contains(fr, "当前 51%") || !strings.Contains(fr, "警示线 7.0%") || !strings.Contains(fr, "红线 10%") {
		t.Errorf("失败率 Detail 量纲不一致：%s", fr)
	}
	if !strings.Contains(tp, "仅为红线要求的 25%") {
		t.Errorf("日吞吐（低方向）Detail 措辞反向：%s", tp)
	}
}

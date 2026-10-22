package gov

import (
	"math"
	"strconv"
	"strings"
)

// 指标读数的人可读格式化（2026-10-22 前端走查实测补）。
//
// 一手代价：治理台「风险预警」卡的 Detail 文案由本包 fmt.Sprintf 直接拼数字，
// 于是出现三类不可读形态：
//   ① 单位粘连："任务失败率 当前 0.51比例"（比例类把单位词直接接在小数后）；
//   ② 量级读不出：0.51 的失败率、0.07 的警示线，要心算才知道是 51% 超 7% 红线；
//   ③ 占用率措辞反向：低方向指标（日吞吐，越低越坏）显示"占用率 400%"，
//      而它真实含义是"只达到红线要求的 25%"。
// 同一轮里「量化指标」卡已在展示层修过（app.js 的 govNum），但 Detail 是服务端
// 生成的字符串，展示层修不到它 —— 所以格式化必须落在数据源头，让所有消费面一致。

// numberText 把数字转成「最多两位小数 + 千分位分组」的字符串。
func numberText(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	s := strconv.FormatFloat(round2(v), 'f', -1, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i:]
	}
	if len(intPart) < 4 {
		return sign(neg) + intPart + fracPart
	}
	var b strings.Builder
	pre := len(intPart) % 3
	for i := 0; i < len(intPart); i++ {
		if i > 0 && (i-pre)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	return sign(neg) + b.String() + fracPart
}

func sign(neg bool) string {
	if neg {
		return "-"
	}
	return ""
}

// percentText 百分数读数：≥10% 收整数（510% 不必带小数），<10% 留一位小数（7.0%）。
func percentText(pct float64) string {
	if math.IsNaN(pct) || math.IsInf(pct, 0) {
		return "-"
	}
	if math.Abs(pct) >= 10 {
		return numberText(math.Round(pct)) + "%"
	}
	return strconv.FormatFloat(math.Round(pct*10)/10, 'f', 1, 64) + "%"
}

// metricValueText 是「值 + 单位」形态：比例类换算成百分数并吞掉"比例"这个单位词
// （否则得到 "0.51比例"），其余保持原单位后缀。
func metricValueText(v float64, unit string) string {
	if unit == "比例" {
		return percentText(v * 100)
	}
	return numberText(v) + unit
}

// metricNumberText 是「只格式化数字、不带单位」形态（阈值列用），
// 但比例类同样换算成百分数 —— 警示线与值必须同量纲，否则"当前 51%，警示线 0.07"无法比较。
func metricNumberText(v float64, unit string) string {
	if unit == "比例" {
		return percentText(v * 100)
	}
	return numberText(v)
}

// usagePhrase 把「相对红线的位置」说成人话，并按方向区分语义：
//
//	高方向（越大越坏）→ "达红线 510%"；
//	低方向（越小越坏）→ "仅为红线要求的 25%"（原样报 400% 会让人以为超载）。
func usagePhrase(e Eval) string {
	if e.Red <= 0 || math.IsNaN(e.Value) || math.IsInf(e.Value, 0) {
		return "红线未设，无法算占用"
	}
	pct := e.Value / e.Red * 100
	if e.Direction == DirHigh {
		return "达红线 " + percentText(pct)
	}
	return "仅为红线要求的 " + percentText(pct)
}

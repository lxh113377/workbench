package supervisor

// trend.go — metrics.csv 趋势判定（A7「7×24 长期稳定性」的机器判据）。
//
// 背景：A7 需要的不是「metrics.csv 文件存在」，而是「能从采样序列判定内存/句柄
// 是否单调上升（泄漏）」。改造前只有 WriteCSV 写文件，没有任何判读能力，
// 所以 24h 压测只能靠人肉看数字。
//
// 判据设计（刻意保守，宁可 unknown 也不虚报 ok —— 与治理体系「零数据不记 PASS」一致）：
//   - 采样数 < 3 或时间跨度 < 10 分钟  => unknown
//   - 尾部 1/3 均值显著高于头部 且 最小二乘斜率为正 => leak
//   - 其余 => ok
//
// 为什么用「首尾均值 + 斜率」双条件而不是单一斜率：Go 的 GC 会让 heap 呈锯齿，
// 单看斜率容易被一次 GC 前的尖峰带偏；双条件同时成立才算泄漏。

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// TrendPoint 是一个采样点（只保留判定需要的字段）。
type TrendPoint struct {
	At      int64   // 毫秒时间戳
	HeapMB  float64 // 堆内存
	Handles int     // 句柄数
}

// TrendReport 是一次趋势判定的结果。
type TrendReport struct {
	Metric       string  `json:"metric"`
	N            int     `json:"n"`
	SpanMin      float64 `json:"spanMin"`
	SlopePerHour float64 `json:"slopePerHour"`
	HeadAvg      float64 `json:"headAvg"`
	TailAvg      float64 `json:"tailAvg"`
	Verdict      string  `json:"verdict"` // ok / leak / unknown
	Reason       string  `json:"reason"`
}

// MinSoakSamples / MinSoakSpanMin 是判定「有资格下结论」的最低门槛。
const (
	MinSoakSamples = 3
	MinSoakSpanMin = 10.0
)

// AnalyzeTrend 对一串采样值做趋势判定（纯函数，便于单测）。
//
// growthRatio 是「尾部均值相对头部均值允许的最大增幅」，<=0 时取 0.25。
func AnalyzeTrend(points []TrendPoint, metric string, growthRatio float64) TrendReport {
	r := TrendReport{Metric: metric, Verdict: "unknown"}
	r.N = len(points)
	if len(points) < MinSoakSamples {
		r.Reason = fmt.Sprintf("采样不足（需要至少 %d 个点，实际 %d）", MinSoakSamples, len(points))
		return r
	}
	pts := append([]TrendPoint(nil), points...)
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].At < pts[j].At })
	r.SpanMin = float64(pts[len(pts)-1].At-pts[0].At) / 60000.0
	if r.SpanMin < MinSoakSpanMin {
		r.Reason = fmt.Sprintf("时间跨度不足（需要 ≥ %.0f 分钟，实际 %.1f）", MinSoakSpanMin, r.SpanMin)
		return r
	}
	if growthRatio <= 0 {
		growthRatio = 0.25
	}

	vals := make([]float64, len(pts))
	for i, p := range pts {
		if metric == "handles" {
			vals[i] = float64(p.Handles)
		} else {
			vals[i] = p.HeapMB
		}
	}
	// 最小二乘斜率（单位：值 / 小时）
	n := float64(len(pts))
	var sx, sy, sxx, sxy float64
	base := pts[0].At
	for i, p := range pts {
		x := float64(p.At-base) / 3600000.0
		y := vals[i]
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	if den := n*sxx - sx*sx; den != 0 {
		r.SlopePerHour = (n*sxy - sx*sy) / den
	}
	third := len(vals) / 3
	if third < 1 {
		third = 1
	}
	r.HeadAvg = avgFloat(vals[:third])
	r.TailAvg = avgFloat(vals[len(vals)-third:])

	growing := r.HeadAvg > 0 && r.TailAvg > r.HeadAvg*(1+growthRatio)
	if growing && r.SlopePerHour > 0 {
		r.Verdict = "leak"
		r.Reason = fmt.Sprintf("尾部均值比头部高 %.1f%%（%.2f → %.2f）且斜率为正 %.2f/小时",
			(r.TailAvg/r.HeadAvg-1)*100, r.HeadAvg, r.TailAvg, r.SlopePerHour)
		return r
	}
	r.Verdict = "ok"
	r.Reason = fmt.Sprintf("无明显单调上升（头 %.2f → 尾 %.2f，斜率 %.2f/小时）", r.HeadAvg, r.TailAvg, r.SlopePerHour)
	return r
}

func avgFloat(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	var s float64
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

// ReadMetricsCSV 读取 supervisor 写出的 metrics.csv（at,heap_mb,alloc_mb,goroutine,handles,num_gc）。
//
// 容错：表头缺失/列数不足的行跳过并计数（不因一行坏数据丢整份采样）；
// 文件不存在返回明确错误（不返回空切片假装成功）。
func ReadMetricsCSV(path string) ([]TrendPoint, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("打开 metrics.csv 失败: %w", err)
	}
	defer f.Close()
	rd := csv.NewReader(f)
	rd.FieldsPerRecord = -1
	rows, err := rd.ReadAll()
	if err != nil {
		return nil, 0, fmt.Errorf("解析 metrics.csv 失败: %w", err)
	}
	var out []TrendPoint
	skipped := 0
	for i, row := range rows {
		if i == 0 && len(row) > 0 && strings.EqualFold(strings.TrimSpace(row[0]), "at") {
			continue // 表头
		}
		if len(row) < 5 {
			skipped++
			continue
		}
		at, e1 := strconv.ParseInt(strings.TrimSpace(row[0]), 10, 64)
		heap, e2 := strconv.ParseFloat(strings.TrimSpace(row[1]), 64)
		handles, e3 := strconv.Atoi(strings.TrimSpace(row[4]))
		if e1 != nil || e2 != nil || e3 != nil {
			skipped++
			continue
		}
		out = append(out, TrendPoint{At: at, HeapMB: heap, Handles: handles})
	}
	return out, skipped, nil
}

// SoakVerdict 汇总一份压测报告的最终结论（供 CLI 退出码映射）。
//
// 退出码约定与治理子系统一致：0 = ok，2 = leak（红），3 = unknown（未验证）。
// 绝不把 unknown 当 ok —— 零数据不记 PASS。
func SoakVerdict(reports []TrendReport) (string, int) {
	if len(reports) == 0 {
		return "unknown", 3
	}
	unknown := false
	for _, r := range reports {
		switch r.Verdict {
		case "leak":
			return "leak", 2
		case "unknown":
			unknown = true
		}
	}
	if unknown {
		return "unknown", 3
	}
	return "ok", 0
}

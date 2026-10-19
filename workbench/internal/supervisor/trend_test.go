package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func pts(heap []float64, handles []int) []TrendPoint {
	out := make([]TrendPoint, 0, len(heap))
	for i := range heap {
		h := 0
		if i < len(handles) {
			h = handles[i]
		}
		out = append(out, TrendPoint{At: int64(i) * 3600000, HeapMB: heap[i], Handles: h})
	}
	return out
}

// TestAnalyzeTrendUnknownWhenInsufficient 守护「零数据不记 PASS」：
// 采样不足或跨度不足时必须 unknown，不能判 ok。
func TestAnalyzeTrendUnknownWhenInsufficient(t *testing.T) {
	if r := AnalyzeTrend(nil, "heap", 0.25); r.Verdict != "unknown" {
		t.Fatalf("零采样必须 unknown，实际 %s", r.Verdict)
	}
	if r := AnalyzeTrend(pts([]float64{1, 2}, nil), "heap", 0.25); r.Verdict != "unknown" {
		t.Fatalf("采样数不足必须 unknown，实际 %s", r.Verdict)
	}
	// 点数够（5 个）但每小时一个点=跨度 4 小时 > 10 分钟；改用 1 分钟间隔使其跨度不足
	var short []TrendPoint
	for i := 0; i < 5; i++ {
		short = append(short, TrendPoint{At: int64(i) * 60000, HeapMB: float64(i)})
	}
	if r := AnalyzeTrend(short, "heap", 0.25); r.Verdict != "unknown" {
		t.Fatalf("跨度不足必须 unknown，实际 %s（span=%.1f）", r.Verdict, r.SpanMin)
	}
}

// TestAnalyzeTrendDetectsHeapLeak 守护正向判据：线性上升必须判 leak。
func TestAnalyzeTrendDetectsHeapLeak(t *testing.T) {
	var heap []float64
	for i := 0; i < 24; i++ {
		heap = append(heap, 10+float64(i)*2) // 每小时 +2MB
	}
	r := AnalyzeTrend(pts(heap, nil), "heap", 0.25)
	if r.Verdict != "leak" {
		t.Fatalf("线性上升必须判 leak，实际 %s（slope=%.2f head=%.2f tail=%.2f）",
			r.Verdict, r.SlopePerHour, r.HeadAvg, r.TailAvg)
	}
	if r.SlopePerHour < 1.9 || r.SlopePerHour > 2.1 {
		t.Fatalf("斜率应约 2/h，实际 %.2f", r.SlopePerHour)
	}
}

// TestAnalyzeTrendOKForFlatAndSawtooth 守护反向判据：平稳与 GC 锯齿不能误报泄漏。
func TestAnalyzeTrendOKForFlatAndSawtooth(t *testing.T) {
	var flat []float64
	for i := 0; i < 24; i++ {
		flat = append(flat, 20)
	}
	if r := AnalyzeTrend(pts(flat, nil), "heap", 0.25); r.Verdict != "ok" {
		t.Fatalf("平线应判 ok，实际 %s", r.Verdict)
	}
	var saw []float64
	for i := 0; i < 24; i++ {
		if i%2 == 0 {
			saw = append(saw, 30)
		} else {
			saw = append(saw, 20)
		}
	}
	if r := AnalyzeTrend(pts(saw, nil), "heap", 0.25); r.Verdict != "ok" {
		t.Fatalf("GC 锯齿应判 ok，实际 %s", r.Verdict)
	}
}

// TestAnalyzeTrendHandlesMetric 句柄维度同样可判（复用同一套判据）。
func TestAnalyzeTrendHandlesMetric(t *testing.T) {
	var hs []int
	for i := 0; i < 24; i++ {
		hs = append(hs, 300+i*20)
	}
	r := AnalyzeTrend(pts(make([]float64, 24), hs), "handles", 0.25)
	if r.Verdict != "leak" {
		t.Fatalf("句柄单调上升必须判 leak，实际 %s", r.Verdict)
	}
}

// TestAnalyzeTrendSortsInput 乱序输入也必须得到同样结论。
func TestAnalyzeTrendSortsInput(t *testing.T) {
	p := []TrendPoint{
		{At: 3 * 3600000, HeapMB: 16},
		{At: 0, HeapMB: 10},
		{At: 2 * 3600000, HeapMB: 14},
		{At: 1 * 3600000, HeapMB: 12},
	}
	if r := AnalyzeTrend(p, "heap", 0.25); r.Verdict != "leak" {
		t.Fatalf("乱序输入也应判 leak，实际 %s", r.Verdict)
	}
}

// TestReadMetricsCSVRoundTrip 覆盖文件解析：表头跳过、坏行跳过并计数、文件缺失报错。
func TestReadMetricsCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.csv")
	body := "at,heap_mb,alloc_mb,goroutine,handles,num_gc\n" +
		"1000,3.50,2.10,21,318,14\n" +
		"2000,4.00,2.40,22,320,15\n" +
		"bad-row,xxx\n" + // 坏行必须被跳过并计数，不能拖垮整份采样
		"3000,5.00,3.00,23,330,16\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	points, skipped, err := ReadMetricsCSV(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("应解析出 3 个点，实际 %d", len(points))
	}
	if skipped != 1 {
		t.Fatalf("应跳过 1 行坏数据，实际 %d", skipped)
	}
	if points[1].HeapMB != 4.0 || points[1].Handles != 320 {
		t.Fatalf("解析值不正确: %+v", points[1])
	}
	// 文件不存在必须报错（不能返回空切片假装成功）
	if _, _, err := ReadMetricsCSV(filepath.Join(dir, "missing.csv")); err == nil {
		t.Fatal("文件不存在时必须返回错误")
	}
}

// TestSoakVerdictExitCodes 守护退出码口径：ok=0 / leak=2 / unknown=3，绝不把 unknown 当 ok。
func TestSoakVerdictExitCodes(t *testing.T) {
	if v, c := SoakVerdict(nil); v != "unknown" || c != 3 {
		t.Fatalf("空报告必须 unknown/3，实际 %s/%d", v, c)
	}
	if v, c := SoakVerdict([]TrendReport{{Verdict: "ok"}}); v != "ok" || c != 0 {
		t.Fatalf("全 ok 应 ok/0，实际 %s/%d", v, c)
	}
	if v, c := SoakVerdict([]TrendReport{{Verdict: "ok"}, {Verdict: "leak"}}); v != "leak" || c != 2 {
		t.Fatalf("含 leak 必须 leak/2，实际 %s/%d", v, c)
	}
	if v, c := SoakVerdict([]TrendReport{{Verdict: "ok"}, {Verdict: "unknown"}}); v != "unknown" || c != 3 {
		t.Fatalf("含 unknown 必须 unknown/3（不得降级为 ok），实际 %s/%d", v, c)
	}
}

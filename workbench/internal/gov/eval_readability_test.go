package gov

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestEvaluateHasNoFloatNoise 钉住「13 项指标的 Value/Warn/Red 不得带 IEEE754 噪声」。
//
// 一手代价（2026-10-22 前端走查实测）：治理台「量化指标」卡把阈值原样拼进 HTML，
// 而阈值多为 max*0.8 一类算式得出 —— 不 round 就把浮点噪声铺到面板上：
//
//	开发执行数 警示 4.800000000000001（= 6*0.8）
//	任务失败率 警示 0.06999999999999999（= 0.1*0.7）
//
// 20 位裸浮点不可读，属于「面板读数必须诚实且可扫」这条底线的破口。
// 判据取「字符串表示最多两位小数」，与 round2 的精度一致；新增指标若忘记 round 即判红。
func TestEvaluateHasNoFloatNoise(t *testing.T) {
	// 样本必须包含「本身带长小数」的字段，否则判据测不出「Value 忘记 round」：
	// 只填 2 / 448 / 10 这类整数读数时 round2 前后无差别（变异验证 M2 实测漏判过）。
	//   OutputBytes 经 bytesToMiB 除 1048576 → 11.7733…；
	//   ContractAmount 用 0.1+0.2 的经典浮点和 → 0.30000000000000004。
	sample := Sample{
		TasksDaily: 2, Handles: 448, Goroutines: 10, Concurrency: 6,
		MemMB: 22.713333333, // 必须是长小数：诊断实测 float32 字段与整数输入经 round2 前后无差别，
		// 样本太干净会让「Value 忘记 round」这一类回归测不出来（M2 首轮漏判）。
		OutputBytes: 12345678, Backlog: 3,
	}
	evals := Evaluate(sample, DefaultBudget())
	if len(evals) == 0 {
		t.Fatal("Evaluate 未产出任何指标（判据失去受理面）")
	}
	checked := 0
	for _, e := range evals {
		for _, f := range []struct {
			name string
			val  float64
		}{
			{"Value", e.Value}, {"Warn", e.Warn}, {"Red", e.Red}, {"Ratio", e.Ratio},
		} {
			if math.IsNaN(f.val) || math.IsInf(f.val, 0) {
				t.Errorf("%s.%s = %v（非有限数不可上面板）", e.Key, f.name, f.val)
				continue
			}
			s := strconv.FormatFloat(f.val, 'f', -1, 64)
			if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > 2 {
				t.Errorf("%s.%s = %s 小数位 %d > 2 —— 浮点噪声会原样显示在治理台（须 round2）",
					e.Key, f.name, s, len(s)-i-1)
			}
			checked++
		}
	}
	if checked < 40 {
		t.Fatalf("只检查了 %d 个读数，13 项指标 × 4 字段应 ≥ 52 —— 判据覆盖面不足", checked)
	}
}

// TestEvaluateRatioMatchesDirection 钉住占用率方向语义：高方向指标接近红线时 ratio 应接近 1，
// 零活动/无数据时 ratio 必须为 0（不能拿 0 除出 NaN 上面板）。
func TestEvaluateRatioMatchesDirection(t *testing.T) {
	b := DefaultBudget()
	hot := Evaluate(Sample{TasksDaily: b.TaskDailyMax - 1, Handles: 448, Goroutines: 10, MemMB: 22.71, Concurrency: 6, OutputBytes: 12345678, Backlog: 3}, b)
	var found bool
	for _, e := range hot {
		if e.Key == "tasksDaily" {
			found = true
			if !(e.Ratio > 0.9 && e.Ratio <= 1.5) {
				t.Fatalf("接近红线时日产出占用率应≈1，实得 %.4f", e.Ratio)
			}
			if e.Level == LevelUnknown {
				t.Fatalf("有采样且接近红线却判 unverified")
			}
		}
	}
	if !found {
		t.Fatal("未找到 tasksDaily 指标")
	}
	// 资源域全零 = 没测到，必须 unverified 且 ratio=0（不得显示成绿）
	dead := Evaluate(Sample{}, b)
	for _, e := range dead {
		if e.Domain == DomainResource && e.Level != LevelUnknown {
			t.Errorf("资源域 %s 在零采样下判成 %s，应为 unverified", e.Key, e.Level)
		}
		if math.IsNaN(e.Ratio) || math.IsInf(e.Ratio, 0) {
			t.Errorf("%s.Ratio 非有限数（%v）会打爆面板", e.Key, e.Ratio)
		}
	}
}

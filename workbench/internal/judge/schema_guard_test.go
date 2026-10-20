package judge

import (
	"testing"

	"workbench/internal/executor"
)

// TestParseCritiqueRejectsForeignJSON 守护缺陷 D10（2026-10-20 实测）：
// extractJSON 抓的是"第一个平衡 JSON"。模型回答里若含**别的** JSON（产物协议
// manifest、工具输出片段），改造前 json.Unmarshal 到 Score 会全字段零值却返回
// nil error —— 自评质量被误判为 0、degraded=false 让上层以为这是模型的诚实评分。
// 后果链：质量 0 → ShouldExtend 判否 → 链不延伸；自主验收按 quality 门槛判失败 → 闭环不推进。
// 判据用 key 存在性而非数值范围：quality=0 是合法评分，必须放过。
func TestParseCritiqueRejectsForeignJSON(t *testing.T) {
	manifest := `{"files":[{"path":"ok.md"}],"verify":[{"cmd":"x","expectExit":0,"timeout":30}]}`
	_, err := parseCritique(&executor.Result{FinalText: manifest})
	if err == nil {
		t.Fatal("非自评 schema 的 JSON 必须判为解析失败（否则质量分被误判为 0，D10 回归）")
	}
}

// TestParseCritiqueAcceptsZeroQuality 反向守护：收紧判据不得把合法的 0 分自评拒掉。
func TestParseCritiqueAcceptsZeroQuality(t *testing.T) {
	cq, err := parseCritique(&executor.Result{FinalText: `{"quality":0,"tags":["弱"]}`})
	if err != nil {
		t.Fatalf("quality=0 是合法评分，不应被拒: %v", err)
	}
	if cq.Quality != 0 {
		t.Fatalf("Quality = %d, want 0", cq.Quality)
	}
}

// TestParseCritiqueAcceptsMinimalSelfReview 守护字段稀疏但合法的自评仍被接受。
func TestParseCritiqueAcceptsMinimalSelfReview(t *testing.T) {
	cq, err := parseCritique(&executor.Result{FinalText: `{"quality":6}`})
	if err != nil {
		t.Fatalf("仅含 quality 的合法自评不应被拒: %v", err)
	}
	if cq.Quality != 6 {
		t.Fatalf("Quality = %d, want 6", cq.Quality)
	}
}

// TestParseCritiqueClampsOutOfRange 守护越界钳制（回归既有行为，收紧时不得改坏）。
func TestParseCritiqueClampsOutOfRange(t *testing.T) {
	hi, err := parseCritique(&executor.Result{FinalText: `{"quality":99}`})
	if err != nil || hi.Quality != 10 {
		t.Fatalf("上界钳制失效: %+v err=%v", hi, err)
	}
	lo, err := parseCritique(&executor.Result{FinalText: `{"quality":-5}`})
	if err != nil || lo.Quality != 0 {
		t.Fatalf("下界钳制失效: %+v err=%v", lo, err)
	}
}

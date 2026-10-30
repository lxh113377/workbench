package autonomy

import "testing"

// TestStepRotationCountsDistinctTitles 钉住「轮转号数不同标题、不数次数」（round 127 · T82 配套）。
//
// 现场（2026-10-30，agent 根 queue.json 实读）：goal-1792985630633 的「小步修」草案攒到 102 条，
// 但**只有 1 个不同标题**（`小步修（正确性/第1轮）`，其中 90 条被拒）。引擎喂入的是最近 30 条定长窗口：
//
//	· 按次数数 ⇒ n 恒为 30（窗口被同一个标题灌满）⇒ 每轮算出同一个「第7轮」⇒ 一旦那标题也进窗口，
//	  又变回恒定标题被连拒 —— 等于把 T82 修的问题换个位置复现；
//	· 按去重数 ⇒ n=1 ⇒ 立刻换到「可读性/第1轮」（实测该标题历史 0 次），并随新标题出现稳定递进。
func TestStepRotationCountsDistinctTitles(t *testing.T) {
	const a = "提升工作空间项目的全栈工程质量 · 小步修（正确性/第1轮）"
	const b = "提升工作空间项目的全栈工程质量 · 小步修（可读性/第1轮）"
	same := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		same = append(same, a)
	}
	if got := stepRotation(same, "小步修"); got != 1 {
		t.Fatalf("30 条同一标题必须只算 1（数次数会饱和）：%d", got)
	}
	mixed := append(append([]string{}, same...), b, b, b)
	if got := stepRotation(mixed, "小步修"); got != 2 {
		t.Fatalf("两种标题应算 2：%d", got)
	}
	// 别的步的标题不得混入（按步隔离，否则阶段切换会把轮次算歪）
	other := []string{"提升工作空间项目的全栈工程质量 · 立项（正确性/第1轮）"}
	if got := stepRotation(other, "小步修"); got != 0 {
		t.Fatalf("别的步的标题不得计入：%d", got)
	}
	// 空白 token 恒 0（空判据不许产出"看起来在轮转"的假象）
	if got := stepRotation(same, ""); got != 0 {
		t.Fatalf("空 token 应为 0：%d", got)
	}
}

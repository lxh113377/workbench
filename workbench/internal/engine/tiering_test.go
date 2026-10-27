package engine

import "testing"

// TestEffectiveMinWords 钉住「分档降载」的口径（round 41 · T17③）。
//
// 由来：40 轮实测里**没有任何一次**合规产物真正落地 —— 免费车道在「一次交约 1000 字 + 多文件 + 验收命令」
// 的要求下几乎必然零产出。首轮先要小而能验收的交付物，后续轮次再回到模板体量。
func TestEffectiveMinWords(t *testing.T) {
	cases := []struct {
		name    string
		base    int
		attempt int
		want    int
	}{
		{"首轮压到最小可验收量级", 1000, 0, 200},
		{"首轮：base 本来就更小时不动", 120, 0, 120},
		{"首轮：base 恰为 200 时不动", 200, 0, 200},
		{"后续轮回到模板体量", 1000, 1, 1000},
		{"后续轮：小 base 同样不动", 120, 2, 120},
		{"base 非法（0/负数）走 300 兜底后再分档", 0, 0, 200},
		{"base 非法且后续轮：兜底 300 原样返回", -5, 1, 300},
	}
	for _, c := range cases {
		if got := effectiveMinWords(c.base, c.attempt); got != c.want {
			t.Errorf("%s: effectiveMinWords(%d,%d) = %d, want %d", c.name, c.base, c.attempt, got, c.want)
		}
	}
}

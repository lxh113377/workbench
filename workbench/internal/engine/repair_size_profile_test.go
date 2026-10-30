package engine

import (
	"strings"
	"testing"
)

// TestRepairSizeProfileMeasuresRawBodies 钉住 T91 第一步的测量口径：
// 体积画像必须直接量「即将被嵌进 prompt 的那些 body」，不得依赖解析 ```file: 围栏。
//
// 为什么特别强调这点（round 137 我自己的实测翻车）：我先用正则抠「上一版产出」段来算占比，
// 结果被嵌套内容里的同名围栏截断 —— 算出 278,403 B，而该段真实是 687,472 B。
// 解析式测量在"被测物自己含分隔符"时会假小 ⇒ 用它定阈值就会定错。
func TestRepairSizeProfileMeasuresRawBodies(t *testing.T) {
	prev := []artifactFile{
		{Path: "ARCHITECTURE.md", Body: strings.Repeat("a", 600)},
		// 故意让 body 里含 ```file: 与 ``` 围栏：解析式测量会被它骗，原始字节测量不会
		{Path: "CHANGELOG.md", Body: "```file:nested.md\n" + strings.Repeat("b", 5000) + "\n```"},
		{Path: "empty.md", Body: ""},
	}
	// 模拟真实拼接：prompt = 固定头 + 全部 body 原样嵌入
	var b strings.Builder
	b.WriteString(strings.Repeat("h", 1500))
	for _, f := range prev {
		b.WriteString("```file:" + f.Path + "\n" + f.Body + "\n```\n")
	}
	prof := repairSizeProfile(prev, b.String())
	if prof.largestPath != "CHANGELOG.md" {
		t.Fatalf("最大文件应认 CHANGELOG.md（含嵌套围栏的那个），实得 %q", prof.largestPath)
	}
	// 直接拿 body 的 len 作对照，不去猜拼接后的长度（我先前写成一串 len("```...") 相加，
	// 既触发 Go 非法转义，又是"自己算一遍可能算错"的多余复杂度）。
	if prof.largest != len(prev[1].Body) {
		t.Fatalf("largest 应等于该文件 body 的真实字节数 %d，实得 %d", len(prev[1].Body), prof.largest)
	}
	sum := 0
	for _, f := range prev {
		sum += len(f.Body)
	}
	if prof.prevTotal != sum {
		t.Fatalf("prevTotal 应为全部 body 原始字节合计 %d，实得 %d ⇒ 说明我在做解析式测量（会被嵌套围栏截断）", sum, prof.prevTotal)
	}
	if prof.total != len(b.String()) {
		t.Fatalf("total 应为整份 prompt 字节数 %d，实得 %d", len(b.String()), prof.total)
	}
	// 空输入不给假数据
	e := repairSizeProfile(nil, "x")
	if e.total != 1 || e.prevTotal != 0 || e.largest != 0 || e.largestPath != "" {
		t.Fatalf("空 prev 应给零值画像，实得 %+v", e)
	}
}

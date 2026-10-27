package engine

import "testing"

// TestHasMarkdownHeading 钉住 markdown 标题判定（round 43 · T25）。
//
// 由来（独立审计抓到的真实缺陷）：原实现用 `strings.Split(s, "\\n")` —— Go 字面量里那是「反斜杠 + n」
// 两个字符而不是换行 ⇒ 无论文本多规范都切不出一行 ⇒ `hasMarkdownHeading` 恒 false，
// 于是「带 # 标题的短产物」照样落进「产出过短（<200 字）」的失败判据。
func TestHasMarkdownHeading(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"一级标题（多行）", "# 标题\n正文正文", true},
		{"二级标题", "前言\n## 二级标题\n内容", true},
		{"三级标题", "### 三\n内容", true},
		{"无标题", "正文\n更多正文", false},
		{"空串", "", false},
		{"井号无空格（不是标题）", "#标题", false},
	}
	for _, c := range cases {
		if got := hasMarkdownHeading(c.text); got != c.want {
			t.Errorf("%s: hasMarkdownHeading(%q) = %v, want %v", c.name, c.text, got, c.want)
		}
	}
}

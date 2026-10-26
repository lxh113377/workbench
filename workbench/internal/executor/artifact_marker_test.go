package executor

import "testing"

// TestHasArtifactBlock 钉住产物块的判定标记（round 21 · T18）。
func TestHasArtifactBlock(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"带协议块", "说明\n<<<ARTIFACTS>>>\n```manifest", true},
		{"无协议块（过程独白）", "我先扫描工作目录。", false},
		{"空文本", "", false},
		{"只有 END 没有 START", "\n<<<END>>>\n", false},
	}
	for _, c := range cases {
		if got := HasArtifactBlock(c.text); got != c.want {
			t.Errorf("%s: HasArtifactBlock = %v, want %v", c.name, got, c.want)
		}
	}
}

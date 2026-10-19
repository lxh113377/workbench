package artifact

import (
	"strings"
	"testing"
)

// TestParseLooseFenceStyle 覆盖真实模型输出形态（现场 glm-5.3 实测）：
// 外层三重反引号、内层一重反引号的 "\`manifest" / "\`file:path" 标记。
// 严格层会因围栏错位判结构缺陷；loose 层必须能取出文件且不误收垃圾。
func TestParseLooseFenceStyle(t *testing.T) {
	f3 := strings.Repeat(fenceMarker, 3)
	f1 := fenceMarker
	final := "Here's the script.\n\n<<<ARTIFACTS>>>\n" + f3 + "\n" +
		f1 + "manifest\n" +
		"{\"files\":[{\"path\":\"hello.py\",\"lang\":\"python\"}],\n" +
		" \"verify\":[{\"cmd\":\"python hello.py\",\"expectExit\":0,\"timeout\":60}]}\n" +
		f3 + "\n" +
		f1 + "file:hello.py\n" +
		"print(\"hello from workbench\")\n" +
		f3 + "\n<<<END>>>\n"
	res := Parse(final, Limits{})
	if !res.HasFiles() {
		t.Fatalf("应解析出文件: level=%s problems=%v", res.Level, res.Problems)
	}
	if res.Level != LevelLoose {
		t.Fatalf("应走 loose 层，实际 %s", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "hello.py" {
		t.Fatalf("文件清单不对: %+v", res.Files)
	}
	if !strings.Contains(string(res.Files[0].Body), "hello from workbench") {
		t.Fatalf("内容不对: %q", string(res.Files[0].Body))
	}
	if strings.Contains(string(res.Files[0].Body), fenceMarker) {
		t.Fatalf("正文不应残留围栏: %q", string(res.Files[0].Body))
	}
}

// TestParseLooseRejectsTraversal 覆盖安全边界：loose 层不能成为越权后门。
func TestParseLooseRejectsTraversal(t *testing.T) {
	f1 := fenceMarker
	final := "<<<ARTIFACTS>>>\n" + f1 + "file:../evil.sh\nrm -rf /\n<<<END>>>\n"
	res := Parse(final, Limits{})
	if res.HasFiles() {
		t.Fatalf("越界路径必须被拒: %+v", res.Files)
	}
	if len(res.Problems) == 0 {
		t.Fatalf("应留下结构缺陷说明")
	}
}

// TestParseLooseLimits 覆盖大小限制：loose 层同样受单文件/总量上限约束。
func TestParseLooseLimits(t *testing.T) {
	f1 := fenceMarker
	big := strings.Repeat("x", 5000)
	final := "<<<ARTIFACTS>>>\n" + f1 + "file:big.txt\n" + big + "\n<<<END>>>\n"
	res := Parse(final, Limits{MaxFileBytes: 100, MaxTotalBytes: 1000})
	if res.HasFiles() {
		t.Fatalf("超限文件必须被拒: %+v", res.Files)
	}
}

// TestParseStrictStillWins 覆盖降级顺序：标准输出仍走 strict，不因新增 loose 层而改判。
func TestParseStrictStillWins(t *testing.T) {
	// 用 mkDoc 的标准拼法（围栏完全配对），必须走 strict。
	final := mkDoc(
		"{\"files\":[{\"path\":\"a.md\"}],\"verify\":[{\"cmd\":\"pwsh -Command exit 0\",\"expectExit\":0}]}",
		testFence+"file:a.md\n# hi\n"+testFence,
	)
	res := Parse(final, Limits{})
	if res.Level != LevelStrict {
		t.Fatalf("标准形态应走 strict，实际 %s", res.Level)
	}
	if !res.HasFiles() {
		t.Fatalf("应解析出文件")
	}
}

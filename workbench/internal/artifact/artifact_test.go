package artifact

import (
	"strings"
	"testing"
)

const testFence = "```"

func mkDoc(manifest, body string) string {
	return "some prose\n\n<<<ARTIFACTS>>>\n" + testFence + "manifest\n" + manifest + "\n" + testFence + "\n" + body + "\n<<<END>>>\n"
}

func TestParseStrict(t *testing.T) {
	raw := mkDoc(
		`{"files":[{"path":"tool.py","lang":"python"}],"verify":[{"cmd":"python tool.py --help","expectExit":0}]}`,
		testFence+"file:tool.py\nprint('hi')\n"+testFence,
	)
	res := Parse(raw, Limits{})
	if res.Level != LevelStrict {
		t.Fatalf("level = %s, want strict", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "tool.py" {
		t.Fatalf("files = %+v", res.Files)
	}
	if string(res.Files[0].Body) != "print('hi')" {
		t.Fatalf("body = %q", string(res.Files[0].Body))
	}
	if len(res.Files[0].SHA256) != 64 {
		t.Fatalf("sha256 未计算: %q", res.Files[0].SHA256)
	}
	if len(res.Manifest.Verify) != 1 {
		t.Fatalf("verify = %+v", res.Manifest.Verify)
	}
	if !res.OK() {
		t.Fatalf("应无结构缺陷, problems=%v", res.Problems)
	}
}

func TestParseUnclosedFenceReported(t *testing.T) {
	raw := mkDoc(
		`{"files":[{"path":"a.py"}],"verify":[{"cmd":"python a.py","expectExit":0}]}`,
		testFence+"file:a.py\nprint(1)\nprint(2)", // 未闭合
	)
	res := Parse(raw, Limits{})
	if res.OK() {
		t.Fatal("未闭合围栏必须被判为缺陷（截断检测）")
	}
	found := false
	for _, p := range res.Problems {
		if strings.Contains(p, "未闭合") {
			found = true
		}
	}
	if !found {
		t.Fatalf("problems 应包含未闭合说明: %v", res.Problems)
	}
}

func TestParseMissingVerifyReported(t *testing.T) {
	raw := mkDoc(
		`{"files":[{"path":"a.py"}]}`,
		testFence+"file:a.py\nprint(1)\n"+testFence,
	)
	res := Parse(raw, Limits{})
	if res.OK() {
		t.Fatal("没有 verify 必须被判为缺陷（无法证明可运行）")
	}
}

func TestParseManifestContentMismatch(t *testing.T) {
	raw := mkDoc(
		`{"files":[{"path":"declared.py"}],"verify":[{"cmd":"python declared.py","expectExit":0}]}`,
		testFence+"file:other.py\nprint(1)\n"+testFence,
	)
	res := Parse(raw, Limits{})
	if res.OK() {
		t.Fatal("manifest 与正文不一致必须被判为缺陷")
	}
	joined := strings.Join(res.Problems, " | ")
	if !strings.Contains(joined, "声明了文件但正文缺失") {
		t.Fatalf("problems = %v", res.Problems)
	}
}

func TestParsePathTraversalRejected(t *testing.T) {
	for _, bad := range []string{"../evil.py", "/abs/evil.py", "C:/evil.py", "a/../../evil.py"} {
		raw := mkDoc(
			`{"files":[{"path":"`+bad+`"}],"verify":[{"cmd":"python x","expectExit":0}]}`,
			testFence+"file:"+bad+"\nprint(1)\n"+testFence,
		)
		res := Parse(raw, Limits{})
		if len(res.Files) != 0 {
			t.Fatalf("路径 %q 必须被拒绝，实际解析出 %d 个文件", bad, len(res.Files))
		}
	}
}

func TestParseMarkedFallback(t *testing.T) {
	raw := "直接给文件：\n" + testFence + "file:hello.py\nprint('x')\n" + testFence + "\n运行：python hello.py"
	res := Parse(raw, Limits{})
	if res.Level != LevelMarked {
		t.Fatalf("level = %s, want marked", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "hello.py" {
		t.Fatalf("files = %+v", res.Files)
	}
	if len(res.Manifest.Verify) == 0 {
		t.Fatal("标记层应尽量捕获 verify 命令")
	}
}

func TestParseHeuristicFallback(t *testing.T) {
	raw := "这里给出实现：\nhello.py:\n" + testFence + "python\nprint('x')\n" + testFence
	res := Parse(raw, Limits{})
	if res.Level != LevelHeuristic {
		t.Fatalf("level = %s, want heuristic", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "hello.py" {
		t.Fatalf("files = %+v", res.Files)
	}
}

func TestParseNoArtifacts(t *testing.T) {
	res := Parse("这只是一段普通文章，没有任何文件。", Limits{})
	if res.HasFiles() {
		t.Fatal("不应解析出文件")
	}
	if res.Level != LevelNone {
		t.Fatalf("level = %s, want none", res.Level)
	}
	if len(res.Problems) == 0 {
		t.Fatal("必须报告缺陷（触发返工）")
	}
}

func TestParseDuplicatePath(t *testing.T) {
	body := testFence + "file:a.py\nprint(1)\n" + testFence + "\n" + testFence + "file:a.py\nprint(2)\n" + testFence
	raw := mkDoc(`{"files":[{"path":"a.py"}],"verify":[{"cmd":"python a.py","expectExit":0}]}`, body)
	res := Parse(raw, Limits{})
	joined := strings.Join(res.Problems, " | ")
	if !strings.Contains(joined, "重复") {
		t.Fatalf("problems = %v", res.Problems)
	}
}

func TestParseFileSizeLimit(t *testing.T) {
	big := strings.Repeat("x", 5000)
	raw := mkDoc(
		`{"files":[{"path":"big.txt"}],"verify":[{"cmd":"pwsh -Command exit 0","expectExit":0}]}`,
		testFence+"file:big.txt\n"+big+"\n"+testFence,
	)
	res := Parse(raw, Limits{MaxFileBytes: 1000})
	if len(res.Files) != 0 {
		t.Fatal("超限文件必须被拒")
	}
}

func TestSafePath(t *testing.T) {
	ok := []string{"a.py", "dir/a.md", "./dir/a.md", "a/b/c.txt"}
	for _, p := range ok {
		if _, err := SafePath(p); err != nil {
			t.Fatalf("%q 应合法: %v", p, err)
		}
	}
	bad := []string{"", "..", "../a", "a/../b", "/a", "C:\\a", "a/../../b"}
	for _, p := range bad {
		if _, err := SafePath(p); err == nil {
			t.Fatalf("%q 应被拒绝", p)
		}
	}
}

func TestProtocolInstructionNonEmpty(t *testing.T) {
	if !strings.Contains(ProtocolInstruction, "<<<ARTIFACTS>>>") {
		t.Fatal("协议说明必须含 ARTIFACTS 标记")
	}
	if !strings.Contains(ProtocolInstruction, "verify") {
		t.Fatal("协议说明必须含 verify 要求")
	}
}

package engine

import (
	"testing"

	"workbench/internal/executor"
)

// TestAttemptEvidence 钉住「失败取证两件套」的抽取口径（T15-④）。
func TestAttemptEvidence(t *testing.T) {
	// nil 安全：FailoverRunner 上抛时 res 可能为 nil，这条路径也要能落账（否则整个台账写不进去）。
	if p, n := attemptEvidence(nil); p != "" || n != 0 {
		t.Fatalf("nil 结果应返回空值，实得 (%q,%d)", p, n)
	}
	got := &executor.Result{StderrPath: "D:\\t\\stderr.txt", FinalText: "我先扫描工作目录，了解项目结构。"}
	p, n := attemptEvidence(got)
	if p != "D:\\t\\stderr.txt" {
		t.Fatalf("stderrPath = %q", p)
	}
	// 按 rune 计数（与引擎既有 countWordsEngine 同口径）：中文句子 16 字就是 16，不是 48 字节。
	if want := len([]rune(got.FinalText)); n != want {
		t.Fatalf("producedRunes = %d, want %d", n, want)
	}
	// 空产出读数为 0（与「模型没说话」区分开）。
	if _, n := attemptEvidence(&executor.Result{}); n != 0 {
		t.Fatalf("空产出应得 0，实得 %d", n)
	}
}

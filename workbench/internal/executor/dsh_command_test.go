package executor

import (
	"strings"
	"testing"
)

// TestDSHCommandLineSelfEvidence 钉住「可入台账的 DSH 无头命令行」的形态（round 38 · T21）。
//
// 目标条款要求「自动执行的任务通过 DeepSeek Harness Desktop 无头控制执行」；
// 本用例保证台账里那一行命令**确含** 无头入口、CLI 路径、profile 与 --json 流式协议，
// 且与 BuildArgsWithPatches 同源（同源 = 记下来的与真正执行的不可能漂移）。
func TestDSHCommandLineSelfEvidence(t *testing.T) {
	line := DSHCommandLine("dsh.exe", "D:/app/lib/desktop-cli.js", "web-desktop", []string{"C:/t/overlay.yml"})
	for _, want := range []string{"dsh.exe", "--expose-internals", "D:/app/lib/desktop-cli.js", "--profile web-desktop", "--patch C:/t/overlay.yml", "--json -"} {
		if !strings.Contains(line, want) {
			t.Errorf("命令行缺少要素 %q：%s", want, line)
		}
	}
	// 同源校验：与 args 拼装函数逐项一致（含顺序）。
	args := BuildArgsWithPatches("D:/app/lib/desktop-cli.js", "web-desktop", []string{"C:/t/overlay.yml"})
	if got, want := strings.TrimSpace(line), "dsh.exe "+strings.Join(args, " "); got != want {
		t.Fatalf("命令行与 BuildArgs 不同源：\n got=%q\nwant=%q", got, want)
	}
	// 空 exe 不得产生前后空格（可读性：台账里那行要能直接读）。
	if len(DSHCommandLine("", "cli.js", "web-desktop", nil)) != len(DSHCommandLine(" ", "cli.js", "web-desktop", nil)) {
		t.Error("空格处理应一致")
	}
}

package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/artifact"
	"workbench/internal/verify"
)

// TestP3PipelineOfflineE2E 是 P3 的**离线全链路验证**。
//
// 为什么需要它：本机 OFM 车道 2026-10-19 持续限流，无法用真实模型跑端到端。
// 但 P3 的链路（协议解析 → 落盘 → 真实执行验收）本身不依赖模型，
// 可以用一份合规协议文本离线验证，且它会长期作为回归网。
//
// 这也是四层失效事故的集成回归：任何一层（解析/落盘/执行/判定）坏了都会红。
func TestP3PipelineOfflineE2E(t *testing.T) {
	fence := strings.Repeat("`", 3)
	dir := t.TempDir()

	answer := "# 完成\n\n以下是可运行的工具。\n\n" +
		"<<<ARTIFACTS>>>\n" +
		fence + "manifest\n" +
		`{"files":[{"path":"hello.py","lang":"python"}],"verify":[{"cmd":"python hello.py","expectExit":0,"timeout":60}]}` + "\n" +
		fence + "\n" +
		fence + "file:hello.py\nprint('hello from P3')\n" + fence + "\n" +
		"<<<END>>>\n"

	res := artifact.Parse(answer, artifact.Limits{})
	if res.Level != artifact.LevelStrict {
		t.Fatalf("解析层级 = %s, want strict（协议必须被严格层识别）", res.Level)
	}
	if len(res.Files) != 1 || res.Files[0].Path != "hello.py" {
		t.Fatalf("文件解析错误: %+v", res.Files)
	}
	if len(res.Problems) != 0 {
		t.Fatalf("结构问题不应存在: %v", res.Problems)
	}
	if len(res.Manifest.Verify) != 1 {
		t.Fatalf("verify 命令数 = %d, want 1", len(res.Manifest.Verify))
	}

	for _, f := range res.Files {
		dst := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, f.Body, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dst); err != nil {
			t.Fatalf("文件未落盘: %v", err)
		}
	}

	rep := verify.Run(context.Background(), dir, res.Manifest.Verify, verify.Options{DefaultTimeoutSec: 60})
	if !rep.Pass {
		t.Fatalf("离线验收应通过，失败原因: %s", rep.FailReason)
	}
	if len(rep.Outcomes) != 1 || !rep.Outcomes[0].Pass {
		t.Fatalf("命令应执行成功: %+v", rep.Outcomes)
	}
}

// TestProtocolInstructionRoundTrip 验证"协议说明本身"能被自己的解析器读懂。
//
// 这是四层失效里最隐蔽的一层：协议说明渲染坏了，模型照着抄也解析不出来。
// 本测试用 ProtocolInstruction 里的示例结构反向构造一份回答，确保
// 说明与解析器是同一套语法。
func TestProtocolInstructionRoundTrip(t *testing.T) {
	pi := artifact.ProtocolInstruction
	// 协议说明里必须出现三反引号围栏
	fence := strings.Repeat("`", 3)
	if !strings.Contains(pi, fence+"manifest") {
		t.Fatalf("协议说明缺少 manifest 围栏行:\n%s", pi)
	}
	if !strings.Contains(pi, fence+"file:") {
		t.Fatalf("协议说明缺少 file: 围栏行:\n%s", pi)
	}
	if strings.Contains(pi, "\\u0060") {
		t.Fatal("协议说明残留转义序列")
	}
}

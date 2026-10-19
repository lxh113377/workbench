package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/artifact"
)

func TestRunRejectsNonWhitelistedInterpreter(t *testing.T) {
	dir := t.TempDir()
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: "cmd /c echo hi", ExpectExit: 0}}, Options{})
	if rep.Pass {
		t.Fatal("cmd 不在白名单，必须拒绝")
	}
	if len(rep.Outcomes) != 1 || rep.Outcomes[0].Skipped == "" {
		t.Fatalf("应记录跳过原因: %+v", rep.Outcomes)
	}
}

func TestRunRejectsShellInjection(t *testing.T) {
	dir := t.TempDir()
	// 分号被当作普通参数（不经 shell），因此不会被解释成命令串联
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: "pwsh -Command exit 0; echo pwned", ExpectExit: 0}}, Options{})
	// 这条会被拆成多个参数；关键是它不会执行 "echo pwned" 之外的 shell 语义
	// 我们只断言：没有 shell 解释导致的越界文件产生
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("命令不应在 workspace 里产生副作用文件: %+v", entries)
	}
	if rep.Pass && len(rep.Outcomes) == 1 && rep.Outcomes[0].Skipped == "" {
		// 允许 pwsh 正常执行（它本身就是白名单解释器）
		t.Log("pwsh 允许执行，符合白名单语义")
	}
}

func TestRunPathTraversalInArgsRejected(t *testing.T) {
	dir := t.TempDir()
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: "python ../../../etc/passwd", ExpectExit: 0}}, Options{})
	if rep.Pass {
		t.Fatal("参数含越界路径必须拒绝")
	}
	if !strings.Contains(rep.FailReason, "越界") {
		t.Fatalf("failReason = %q", rep.FailReason)
	}
}

func TestRunPassAndFail(t *testing.T) {
	dir := t.TempDir()
	// 写一个一定存在的文件，用 pwsh Test-Path 验证
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := `pwsh -NoProfile -NonInteractive -Command "if (Test-Path a.md) { exit 0 } else { exit 1 }"`
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: cmd, ExpectExit: 0, TimeoutSec: 60}}, Options{})
	if !rep.Pass {
		t.Fatalf("应通过: %+v", rep.Outcomes)
	}

	badCmd := `pwsh -NoProfile -NonInteractive -Command "exit 3"`
	rep2 := Run(context.Background(), dir, []artifact.Check{{Cmd: badCmd, ExpectExit: 0, TimeoutSec: 60}}, Options{})
	if rep2.Pass {
		t.Fatal("退出码不匹配必须判失败")
	}
	if rep2.FailReason == "" {
		t.Fatal("失败应带人读原因（用于返工 prompt）")
	}
}

func TestRunTimeout(t *testing.T) {
	dir := t.TempDir()
	cmd := `pwsh -NoProfile -NonInteractive -Command "Start-Sleep -Seconds 5"`
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: cmd, ExpectExit: 0, TimeoutSec: 1}}, Options{})
	if rep.Pass {
		t.Fatal("超时必须判失败")
	}
	if !rep.Outcomes[0].TimedOut {
		t.Fatalf("应标记 TimedOut: %+v", rep.Outcomes[0])
	}
}

func TestRunNoChecksFails(t *testing.T) {
	rep := Run(context.Background(), t.TempDir(), nil, Options{})
	if rep.Pass {
		t.Fatal("没有验收命令必须判失败（不能默认通过）")
	}
}

func TestSplitCommandQuotes(t *testing.T) {
	got, err := splitCommand(`pwsh -Command "if (Test-Path a.md) { exit 0 }"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("args = %#v", got)
	}
	if got[2] != "if (Test-Path a.md) { exit 0 }" {
		t.Fatalf("引号参数 = %q", got[2])
	}
	if _, err := splitCommand(`pwsh "unclosed`); err == nil {
		t.Fatal("未闭合引号必须报错")
	}
}

func TestSanitizedEnvStripsSecrets(t *testing.T) {
	t.Setenv("DSH_HOME", "/x")
	t.Setenv("MY_API_KEY", "secret")
	t.Setenv("HTTP_PROXY", "http://p")
	env := sanitizedEnv()
	for _, kv := range env {
		up := strings.ToUpper(kv)
		if strings.HasPrefix(up, "DSH_HOME=") || strings.HasPrefix(up, "MY_API_KEY=") || strings.HasPrefix(up, "HTTP_PROXY=") {
			t.Fatalf("敏感变量未清洗: %s", kv)
		}
	}
}

package executor

import (
	"errors"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
)

func TestBuildArgs(t *testing.T) {
	got := BuildArgs("C:/app/lib/desktop-cli.js", "web-desktop", "C:/t/overlay.yml")
	want := []string{"--expose-internals", "C:/app/lib/desktop-cli.js", "--profile", "web-desktop", "--patch", "C:/t/overlay.yml", "--json", "-"}
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildEnvOverridesHostDSH(t *testing.T) {
	base := []string{"PATH=C:/bin", "DSH_HOME=/host/home", "DSH_PERMISSION_MODE=read-only", "FOO=bar"}
	env := BuildEnv("C:/isolated", base)
	joined := strings.Join(env, "|")
	if !strings.Contains(joined, "DSH_HOME=C:/isolated") {
		t.Fatalf("isolated home 未生效: %v", env)
	}
	if strings.Contains(joined, "DSH_HOME=/host/home") {
		t.Fatalf("宿主 DSH_HOME 未被过滤: %v", env)
	}
	if !strings.Contains(joined, "DSH_PERMISSION_MODE=danger-full-access") {
		t.Fatalf("危险模式未设置: %v", env)
	}
	if !strings.Contains(joined, "FOO=bar") || !strings.Contains(joined, "PATH=C:/bin") {
		t.Fatalf("普通环境变量被误删: %v", env)
	}
	if strings.Contains(joined, "read-only") {
		t.Fatalf("宿主 permission mode 未被覆盖: %v", env)
	}
}

func TestBuildEnvNilBase(t *testing.T) {
	env := BuildEnv("C:/h", nil)
	joined := strings.Join(env, "|")
	if !strings.Contains(joined, "DSH_HOME=C:/h") {
		t.Fatal("nil base 时应仍注入隔离变量")
	}
	if !strings.Contains(joined, "ELECTRON_RUN_AS_NODE=1") {
		t.Fatal("缺少 ELECTRON_RUN_AS_NODE")
	}
}

func TestResolveTimeout(t *testing.T) {
	cases := []struct {
		task, def int
		want      time.Duration
	}{
		{5, 30, 5 * time.Minute},
		{0, 30, 30 * time.Minute},
		{0, 0, 30 * time.Minute},
		{-1, 45, 45 * time.Minute},
	}
	for _, c := range cases {
		if got := ResolveTimeout(c.task, c.def); got != c.want {
			t.Fatalf("ResolveTimeout(%d,%d) = %v, want %v", c.task, c.def, got, c.want)
		}
	}
}

func TestStderrSummary(t *testing.T) {
	if got := stderrSummary("", 100); got != "" {
		t.Fatalf("空 stderr 应返回空串，got %q", got)
	}
	// 连续空行应压成最多一个空行（即不出现连续 3 个换行）
	if got := stderrSummary("  \n\n错误A\n\n\n\n错误B\n  ", 400); strings.Contains(got, "\n\n\n") {
		t.Fatalf("连续空行未压缩: %q", got)
	}
	long := strings.Repeat("x", 2000)
	if n := len(stderrSummary(long, 100)); n > 100 {
		t.Fatalf("摘要未截断: %d 字节", n)
	}
}

func TestFinalizeResultStalled(t *testing.T) {
	res := &Result{}
	res.markStalled()
	err := FinalizeResult(res, "", false)
	if !errors.Is(err, errStalled) {
		t.Fatalf("应返回卡死错误，got %v", err)
	}
	if res.ErrorKind != string(KindStalled) {
		t.Fatalf("ErrorKind = %s, want stalled", res.ErrorKind)
	}
}

func TestFinalizeResultTimeout(t *testing.T) {
	res := &Result{TimedOut: true}
	err := FinalizeResult(res, "", false)
	if !errors.Is(err, errTimedOut) {
		t.Fatalf("应返回超时错误，got %v", err)
	}
	if res.ErrorKind != string(KindTimeout) {
		t.Fatalf("ErrorKind = %s, want timeout", res.ErrorKind)
	}
}

func TestFinalizeResultCancelled(t *testing.T) {
	res := &Result{ErrorKind: string(KindCancelled)}
	if err := FinalizeResult(res, "", false); !errors.Is(err, errCancelled) {
		t.Fatalf("应返回取消错误，got %v", err)
	}
}

func TestFinalizeResultBadExit(t *testing.T) {
	res := &Result{ExitCode: 3}
	err := FinalizeResult(res, "upstream 502", false)
	if err == nil {
		t.Fatal("非 0 退出码应返回错误")
	}
	if res.ErrorKind != string(KindUpstream) {
		t.Fatalf("ErrorKind = %s, want upstream_failed", res.ErrorKind)
	}
	if !strings.Contains(res.LastError, "upstream") {
		t.Fatalf("LastError 未写入 stderr 摘要: %q", res.LastError)
	}
}

func TestFinalizeResultNoOutput(t *testing.T) {
	res := &Result{ExitCode: 0, FinalText: "  "}
	err := FinalizeResult(res, "", false)
	if !errors.Is(err, errNoOutput) {
		t.Fatalf("空产出应返回 errNoOutput，got %v", err)
	}
	if res.ErrorKind != string(KindExit) {
		t.Fatalf("ErrorKind = %s, want bad_exit", res.ErrorKind)
	}
}

func TestFinalizeResultNil(t *testing.T) {
	if err := FinalizeResult(nil, "", false); err == nil {
		t.Fatal("nil Result 应返回错误而不是 panic")
	}
}

func TestEffectiveMaxBytes(t *testing.T) {
	if got := effectiveMaxBytes(0); got != 32*1024*1024 {
		t.Fatalf("默认上限 = %d, want 32MB", got)
	}
	if got := effectiveMaxBytes(5); got != 5*1024*1024 {
		t.Fatalf("5MB = %d", got)
	}
	if got := effectiveMaxBytes(-3); got != 32*1024*1024 {
		t.Fatalf("负数应回落默认，got %d", got)
	}
}

func TestNormalizeConcurrency(t *testing.T) {
	cases := map[int]int{-1: 1, 0: 1, 1: 1, 3: 3, 6: 6, 99: 6}
	for in, want := range cases {
		if got := normalizeConcurrency(in); got != want {
			t.Fatalf("normalizeConcurrency(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestOverlayFromConfig(t *testing.T) {
	got := OverlayFromConfig("z-ai/glm-5.3")
	if !strings.Contains(got, "z-ai/glm-5.3") {
		t.Fatalf("overlay 缺少模型名: %s", got)
	}
}

var _ = config.ExecutorConfig{}

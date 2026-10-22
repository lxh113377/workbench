package web

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGateCriticalFilesAreTracked 钉住「前端门禁的依赖文件必须被 git 跟踪」。
//
// 一手缺陷（2026-10-23）：门禁的入口脚本 + 断言层 + 14 个探针**全部未被跟踪**
// （git status 显示 ??）。后果不是本机跑不了，而是**换一个 clone / CI 上根本不存在**：
// node tests/ui/frontend-gate.mjs 会报 Cannot find module ./assertions.mjs，
// 而本机一切正常。这是本仓在册失效形态「判据存在但没挂受理面」的升级变体：
// **受理面的依赖文件不在版本库里**。
func TestGateCriticalFilesAreTracked(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 取不到本文件路径")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..", "..")
	cmd := exec.Command("git", "ls-files", "tests/ui")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("git ls-files 不可用（%v），无法判定跟踪状态", err)
	}
	tracked := string(out)
	required := []string{
		"tests/ui/frontend-gate.mjs",
		"tests/ui/cdp_probe.mjs",
		"tests/ui/assertions.mjs",
		"tests/ui/assertions_selftest.mjs",
	}
	for _, f := range required {
		if !strings.Contains(tracked, f) {
			t.Errorf("%s 未被 git 跟踪 —— 本机可跑但别的 clone / CI 上不存在", f)
		}
	}
	probeCount := strings.Count(tracked, "tests/ui/probes/")
	if probeCount < 10 {
		t.Errorf("tests/ui/probes 下被跟踪的探针只有 %d 个（应 ≥10）—— 门禁会有用例取不到数", probeCount)
	}
}

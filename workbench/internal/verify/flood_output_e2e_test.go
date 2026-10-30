package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/artifact"
)

// TestRunFloodingOutputIsNotBootFailure 是上面那条契约判据的**端到端对照**：
// 真起一个子进程刷远超 MaxOutputBytes 的输出，断言结果按"命令自身退出码"判定，
// 而不是被记成「启动失败」（round 142 线上实测该误分类：exit=-1 / skipped=启动失败: short write）。
//
// 用脚本文件而不是内联命令：内联命令要在 pwsh/python 两套引号语法间周旋，容易把测试自身的错误混进来。
func TestRunFloodingOutputIsNotBootFailure(t *testing.T) {
	ip := pickInterp(t)
	dir := t.TempDir()
	var cmd string
	switch ip.kind {
	case "pwsh":
		script := "1..40000 | ForEach-Object { 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' }" + "\n" + "exit 0" + "\n"
		if err := os.WriteFile(filepath.Join(dir, "flood.ps1"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd = ip.exe + " -NoProfile -NonInteractive -File flood.ps1"
	case "python":
		script := "import sys" + "\n" + "for _ in range(40000):" + "\n" + "    sys.stdout.write('a'*40 + '\\n')" + "\n" + "sys.exit(0)" + "\n"
		if err := os.WriteFile(filepath.Join(dir, "flood.py"), []byte(script), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd = ip.exe + " flood.py"
	default:
		t.Skip("本机无 pwsh/python，端到端洪泛用例跳过")
	}
	const maxOut = 4096
	rep := Run(context.Background(), dir, []artifact.Check{{Cmd: cmd, ExpectExit: 0, TimeoutSec: 60}},
		Options{DefaultTimeoutSec: 60, MaxOutputBytes: maxOut})
	ocr := rep.Outcomes[0]
	if ocr.Skipped != "" {
		t.Fatalf("子进程刷出超过 MaxOutputBytes 的输出时不该被记成启动失败：skipped=%q exit=%d（这正是线上误分类的形态）", ocr.Skipped, ocr.Exit)
	}
	if ocr.Exit != 0 || !ocr.Pass {
		t.Fatalf("命令自身 exit 0，输出超限只应被截断：exit=%d pass=%v timeout=%v skipped=%q",
			ocr.Exit, ocr.Pass, ocr.TimedOut, ocr.Skipped)
	}
}

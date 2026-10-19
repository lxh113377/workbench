package verify

import (
	"os/exec"
	"runtime"
	"strconv"
	"testing"
)

// interp 是本机可用解释器探针（issue #2 平台解耦）。
//
// 改造前测试硬编码 pwsh —— Linux 容器 exec: "pwsh" not found，
// 真实执行路径（通过/失败/超时）在容器里根本没被跑过。
// 现在按 pwsh → python3 → python → node 顺序探测，
// 命令按探测结果构造；Windows 上全缺时硬失败（禁止 Skip 洗绿）。
type interp struct {
	exe  string
	kind string // pwsh | python | node
}

func pickInterp(t *testing.T) interp {
	t.Helper()
	for _, c := range []interp{{"pwsh", "pwsh"}, {"python3", "python"}, {"python", "python"}, {"node", "node"}} {
		if _, err := exec.LookPath(c.exe); err == nil {
			return c
		}
	}
	if runtime.GOOS == "windows" {
		t.Fatal("Windows 测试机必须至少有 pwsh/python/node 之一 —— 真实执行用例不许静默跳过")
	}
	t.Skip("本机无 pwsh/python3/python/node，真实执行用例跳过（非 Windows 才允许）")
	return interp{}
}

// checkFileCmd 生成"文件存在则退出码 0，否则 1"的验收命令。
func (ip interp) checkFileCmd(name string) string {
	switch ip.kind {
	case "pwsh":
		return `pwsh -NoProfile -NonInteractive -Command "if (Test-Path ` + name + `) { exit 0 } else { exit 1 }"`
	case "python":
		return ip.exe + ` -c "import sys,os; sys.exit(0 if os.path.exists('` + name + `') else 1)"`
	default:
		return ip.exe + ` -e "process.exit(require('fs').existsSync('` + name + `')?0:1)"`
	}
}

// sleepCmd 生成持续 sec 秒不退出的命令（超时用例）。
func (ip interp) sleepCmd(sec int) string {
	switch ip.kind {
	case "pwsh":
		return `pwsh -NoProfile -NonInteractive -Command "Start-Sleep -Seconds ` + strconv.Itoa(sec) + `"`
	case "python":
		return ip.exe + ` -c "import time; time.sleep(` + strconv.Itoa(sec) + `)"`
	default:
		return ip.exe + ` -e "const t=Date.now(); while(Date.now()-t<` + strconv.Itoa(sec*1000) + `){}"`
	}
}

// failCmd 生成固定退出码 code 的命令（失败契约用例）。
func (ip interp) failCmd(code int) string {
	switch ip.kind {
	case "pwsh":
		return `pwsh -NoProfile -NonInteractive -Command "exit ` + strconv.Itoa(code) + `"`
	case "python":
		return ip.exe + ` -c "import sys; sys.exit(` + strconv.Itoa(code) + `)"`
	default:
		return ip.exe + ` -e "process.exit(` + strconv.Itoa(code) + `)"`
	}
}

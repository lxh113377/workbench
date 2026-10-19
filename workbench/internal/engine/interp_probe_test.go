package engine

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"runtime"
	"strings"
	"testing"
)

// 解释器探针（issue #2 平台解耦）：engine 的产物验收链路会经 verify.Run 真实执行
// manifest 里的命令。改造前这些命令硬编码 pwsh，Linux 容器里 exec 失败 ——
// 真实"验收通过"路径在容器从未跑过。现在按 pwsh → python3 → python → node 探测，
// 全缺时：Windows 硬失败（不许静默跳过），其它平台 Skip。
type testInterp struct {
	exe  string
	kind string
}

func pickTestInterp(t *testing.T) testInterp {
	t.Helper()
	for _, c := range []testInterp{{"pwsh", "pwsh"}, {"python3", "python"}, {"python", "python"}, {"node", "node"}} {
		if _, err := exec.LookPath(c.exe); err == nil {
			return c
		}
	}
	if runtime.GOOS == "windows" {
		t.Fatal("Windows 测试机必须至少有 pwsh/python/node 之一 —— 产物验收用例不许静默跳过")
	}
	t.Skip("本机无可用解释器，产物验收真实执行用例跳过")
	return testInterp{}
}

// fileCheckCmd 生成"file 存在则 0 否则 1"的验收命令（白名单解释器内）。
func (ip testInterp) fileCheckCmd(file string) string {
	switch ip.kind {
	case "pwsh":
		return `pwsh -NoProfile -NonInteractive -Command "if (Test-Path ` + file + `) { exit 0 } else { exit 1 }"`
	case "python":
		return ip.exe + ` -c "import sys,os; sys.exit(0 if os.path.exists('` + file + `') else 1)"`
	default:
		return ip.exe + ` -e "process.exit(require('fs').existsSync('` + file + `')?0:1)"`
	}
}

// jsonMarshalLit 把字符串编码成 JSON 字面量（含首尾引号）。
func jsonMarshalLit(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

// artifactsFinal 组装一份合规产物协议全文：正文 + 单文件 + 单验收命令。
// 用 json.Marshal 生成 manifest，避免手写转义在跨解释器命令里出错。
func artifactsFinal(t *testing.T, ip testInterp, body, file, fileContent string) string {
	t.Helper()
	manifest := `{"files":[{"path":"` + file + `"}],"verify":[{"cmd":CMD,"expectExit":0,"timeout":30}]}`
	cmdJSON, err := jsonMarshalLit(ip.fileCheckCmd(file))
	if err != nil {
		t.Fatalf("命令 JSON 编码失败: %v", err)
	}
	manifest = strings.Replace(manifest, "CMD", cmdJSON, 1)
	fence := strings.Repeat(string(rune(96)), 3)
	return body + "\n\n<<<ARTIFACTS>>>\n" + fence + "manifest\n" + manifest + "\n" + fence + "\n" +
		fence + "file:" + file + "\n" + fileContent + "\n" + fence + "\n<<<END>>>\n"
}

// scriptCase 返回一份"可真实执行"的脚本产物三元组（路径/内容/验收命令），
// 按探测到的解释器分派 —— p3 离线全链路用例用它验证"跑真脚本"而非只查文件存在。
func (ip testInterp) scriptCase() (path, content, cmd string) {
	switch ip.kind {
	case "python":
		return "hello.py", "print('hello from P3')\n", ip.exe + " hello.py"
	case "node":
		return "hello.js", "console.log('hello from P3')\n", "node hello.js"
	default:
		return "hello.ps1", "Write-Output 'hello from P3'\nexit 0\n",
			ip.exe + " -NoProfile -NonInteractive -File hello.ps1"
	}
}

// failScriptCaseExit 是参数化版本：脚本以指定非零码退出（降级契约用 7）。
func (ip testInterp) failScriptCaseExit(code int) (path, content, cmd string) {
	c := strconv.Itoa(code)
	switch ip.kind {
	case "python":
		return "a.py", "raise SystemExit(" + c + ")\n", ip.exe + " a.py"
	case "node":
		return "a.js", "process.exit(" + c + ")\n", ip.exe + " a.js"
	default:
		return "a.ps1", "exit " + c + "\n", ip.exe + " -NoProfile -NonInteractive -File a.ps1"
	}
}

// failScriptCase 返回一份"真实执行且必然以退出码 2 失败"的脚本三元组，
// 用于失败契约用例（改造前硬编码 python a.py，Linux 容器无 python 别名时
// 失败原因是"启动失败"而不是"退出码不符"——判据被偷换）。
func (ip testInterp) failScriptCase() (path, content, cmd string) {
	switch ip.kind {
	case "python":
		return "a.py", "raise SystemExit(2)\n", ip.exe + " a.py"
	case "node":
		return "a.js", "process.exit(2)\n", ip.exe + " a.js"
	default:
		return "a.ps1", "exit 2\n", ip.exe + " -NoProfile -NonInteractive -File a.ps1"
	}
}

// failArtifactsFinal 组装一份"文件能落盘、但验收脚本以 code 退出码失败"的协议全文，
// 解释器自适应（issue #2）。降级契约用例用它，替代改造前硬编码 python bad.py。
func failArtifactsFinal(t *testing.T, ip testInterp, body string, code int) string {
	t.Helper()
	sPath, sContent, sCmd := ip.failScriptCaseExit(code)
	cmdLit, err := jsonMarshalLit(sCmd)
	if err != nil {
		t.Fatal(err)
	}
	manifest := `{"files":[{"path":"` + sPath + `"}],"verify":[{"cmd":` + cmdLit + `,"expectExit":0,"timeout":30}]}`
	fence := strings.Repeat(string(rune(96)), 3)
	return body + "\n\n<<<ARTIFACTS>>>\n" + fence + "manifest\n" + manifest + "\n" + fence + "\n" +
		fence + "file:" + sPath + "\n" + sContent + fence + "\n<<<END>>>\n"
}

package main

// headless_test.go 提供「测试期间绝不弹窗」的统一约定（2026-10-21 老大指令：
// 「不要给我跳出来窗口，无头执行」）。
//
// 背景：本包的黑盒测试要构建**真实二进制**并起实例（静默契约、CLI 分发表、
// 退出缺口等）。此前这些 spawn 都是裸 exec.Command，于是：
//   1) 被测 EXE 若为 CONSOLE 子系统或走到打开面板分支，桌面会闪黑窗/弹 Edge；
//   2) t.TempDir 里的实例还会真的把面板推到用户桌面（老大反复反馈"又跳窗了"）。
// 这属于"测试污染桌面"，与被测逻辑无关，必须从环境层根除。
//
// 两条硬约定（本文件把约定变成可复用的助手，避免每处各写一份再漏）：
//   A. 所有子进程 spawn 必须走 hiddenTestCmd —— 隐藏窗口 + CREATE_NO_WINDOW；
//   B. 所有子进程环境必须带 WB_HEADLESS=1 —— browser.Open 直接不动手，
//      被测二进制即使走到"弹面板"分支也不会真的拉起 Edge。

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// hiddenTestCmd 构造一个不会弹窗的测试子进程命令。
//
// HideWindow 处理"程序自己申请了窗口"的情况；CREATE_NO_WINDOW 处理
// "CONSOLE 子系统程序被分配控制台"的情况 —— 两者都要，缺一个就会闪窗。
func hiddenTestCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	cmd.Env = headlessTestEnv()
	return cmd
}

// headlessTestEnv 返回带无头标记的完整环境（继承当前环境，只覆盖/追加开关）。
//
// 名字带 Test 与生产侧的 headlessEnv() 区分：前者返回 env 切片（喂给子进程），
// 后者返回 bool（判定本进程）。同名不同义是最容易埋坑的重复，坚决不共用一个名字。
func headlessTestEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if len(kv) > len(headlessEnvMarker) && kv[:len(headlessEnvMarker)+1] == headlessEnvMarker+"=" {
			out = append(out, headlessEnvMarker+"=1")
			replaced = true
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, headlessEnvMarker+"=1")
	}
	return out
}

// ---- 测试自建目录与进程回收（2026-10-22 处置 CI 连续假红的根因） ----
//
// 为什么需要（实测根因）：远端 CI 自 2026-10-22 08:46(+08) 起连续 8 次 red，失败点全部是
// 同型的一句 ——
//
//	--- FAIL: TestSilentDoesNotOpenPanel
//	    testing.go:1619: TempDir RemoveAll cleanup: unlinkat ...\\TestSilentDoesNotOpenPanel*\\002:
//	    The process cannot access the file because it is being used by another process.
//
// 其中一次判红的提交是**纯文档**提交（docs/changelog），说明红与被推送内容无关：
// 是本包契约测试自己的收尾方式在 Windows 上不可靠 ——
// t.TempDir() 的自动清理用 RemoveAll 删目录，而 Windows 上**只要还有进程把它当工作目录
// 就删不掉**；删不掉会被 testing.go 记成测试 FAIL，于是「契约判据全绿、CI 却连红 8 次」。
// 只 Kill 直接子进程收不回脱离进程树的后代（本仓有「后台实例爆炸」在册），
// 故这里给三条可复用能力：收进程树、按 EXE 路径精确回收、目录清理不判红但留证。

// killTree 杀掉进程及其**子进程树**（Windows 走 taskkill /T /F）。
func killTree(p *os.Process) {
	if p == nil {
		return
	}
	if runtime.GOOS == "windows" {
		// 先按父子链收整棵树（父进程还活着时树才可解析），再 Kill 兜底。
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
	}
	_ = p.Kill()
	_, _ = p.Wait()
}

// reapSpawnedExe 按**可执行文件绝对路径**回收本测试起过的实例（含已脱离进程树的后代）。
//
// 精确性：被测 EXE 位于本测试自建的临时根目录下，按该路径匹配只可能命中本测试起的实例，
// 不会误伤用户正在跑的工作台（路径经环境变量传入，避免拼命令时的转义问题 ——
// 本仓有过「命令里双引号被三层解析吃掉」的实测事故）。
func reapSpawnedExe(exe string) {
	if runtime.GOOS != "windows" || exe == "" {
		return
	}
	script := `$p = @(Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" | Where-Object { $_.ExecutablePath -eq $env:WB_TEST_EXE }); foreach ($x in $p) { Stop-Process -Id $x.ProcessId -Force -ErrorAction SilentlyContinue }`
	cmd := hiddenTestCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(cmd.Env, "WB_TEST_EXE="+exe)
	_, _ = cmd.Output()
}

// trackSpawnedExe 注册「测试结束按路径回收该 EXE」的清理。
//
// 注册顺序要紧：它必须**晚于** newSpawnDir 注册（t.Cleanup 后进先出），
// 这样回收进程发生在删目录之前。
func trackSpawnedExe(t *testing.T, exe string) {
	t.Helper()
	t.Cleanup(func() { reapSpawnedExe(exe) })
}

// newSpawnDir 建一个「子进程要当工作目录用」的测试目录，并注册自管清理。
//
// 与 t.TempDir() 的区别：t.TempDir 的清理失败会被记成测试 FAIL（见文件头实测），
// 这里改为「重试 + 留证」，见 removeSpawnDirWithin 的说明。
func newSpawnDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "wb-spawn-")
	if err != nil {
		t.Fatalf("建测试工作目录失败: %v", err)
	}
	t.Cleanup(func() { removeSpawnDirWithin(t, dir, 5*time.Second) })
	return dir
}

// removeSpawnDirWithin 尽力删净目录，返回是否删净。
//
// **不判红**（这是本函数存在的全部理由）：契约判据在调用它之前已经跑完并入库，
// 此处失败只说明「环境里还有进程占着该目录」，与被测契约无关；把它判红会把
// 「测试环境残留」伪装成「契约被破坏」—— 实测代价是 CI 连续 8 次假红。
// 但也不静默：删不掉时打印残留进程快照，让下一次出现时能直接定位残留者。
func removeSpawnDirWithin(t *testing.T, dir string, wait time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		if err := os.RemoveAll(dir); err == nil || !pathExists(dir) {
			return true
		}
		if time.Now().After(deadline) {
			t.Logf("测试目录未能删净（仍有进程占用？）: %s\n%s", dir, processSnapshot(dir))
			return false
		}
		sleepMs(100)
	}
}

// pathExists 判断路径是否还在（RemoveAll 返回错误后复核，避免把「已删净」读成「删不动」）。
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// processSnapshot 抓一份**只与本次临时目录相关**的进程快照（残留实例取证用）。
//
// 过滤口径刻意收紧到「CommandLine 命中该目录」：泛抓 workbench/msedge 会把用户桌面上
// 正在跑的 Edge 全部倒进日志（噪声淹没线索），而残留者必然带着这个唯一临时路径。
func processSnapshot(dir string) string {
	script := `$p = @(Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -and $_.CommandLine -like ('*'+$env:WB_TEST_DIR+'*') } | Select-Object ProcessId,ParentProcessId,Name,CommandLine); if ($p) { $p | Format-List | Out-String } else { '(无进程的 CommandLine 命中该目录)' }`
	cmd := hiddenTestCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(cmd.Env, "WB_TEST_DIR="+dir)
	out, err := cmd.Output()
	if err != nil {
		return "(进程快照取不到: " + err.Error() + ")"
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		return s
	}
	return "(无进程的 CommandLine 命中该目录)"
}

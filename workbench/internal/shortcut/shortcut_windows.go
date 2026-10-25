//go:build windows

package shortcut

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"workbench/internal/winhide"
)

const psScript = `$ErrorActionPreference = "Stop"
$exe = [string]$args[0]
$workDir = [string]$args[1]
$desktop = [Environment]::GetFolderPath("Desktop")
$startMenu = [Environment]::GetFolderPath("Programs")
$targets = @(
  (Join-Path $desktop "个人AI工作台.lnk"),
  (Join-Path $startMenu "个人AI工作台.lnk")
)
$shell = New-Object -ComObject WScript.Shell
$ok = 0
foreach ($lnkPath in $targets) {
  try {
    $lnk = $shell.CreateShortcut($lnkPath)
    $lnk.TargetPath = $exe
    $lnk.WorkingDirectory = $workDir
    $lnk.IconLocation = "$exe,0"
    $lnk.WindowStyle = 1
    $lnk.Description = "个人 AI 自动化工作台"
    # 显式清空 Arguments（2026-10-19 现场缺陷：曾被写死 "--silent"，
    # 而 --silent 的语义是「服务化实例：不弹面板」，于是双击后进程起来了、
    # 端口也在，桌面上却什么都不出现，表现就是「点了没反应 / 打不开」）。
    # CreateShortcut 打开的是**已有** .lnk 时字段是读旧值，只删赋值不清空
    # 会让老 lnk 里的残留 Arguments 继续生效 —— 必须显式置空。
    $lnk.Arguments = ""
    $lnk.Save()
    # 回读校验：必须同时看 TargetPath 与 Arguments（只看前者查不出 --silent 残留）
    $check = $shell.CreateShortcut($lnkPath)
    if ($check.TargetPath -eq $exe -and $check.Arguments -eq "") { $ok++ } else { Write-Error "回读不一致: $lnkPath (TargetPath=$check.TargetPath Arguments=$check.Arguments)" }
  } catch { Write-Error $_.Exception.Message }
}
if ($ok -ge 1) { Write-Output "SHORTCUT_OK $ok" } else { Write-Error "未能创建任何快捷方式"; exit 1 }`

// Create 创建桌面与开始菜单快捷方式，并做 TargetPath 回读校验。
//
// 安全约束（现场缺陷，2026-10-19）：快捷方式必须指向**稳定安装位置**的 EXE。
// 改造前直接指向 os.Executable()，于是任何从临时目录/压测副本启动的实例
// （如 runtime\soak-24h\wbsoak24.exe）都会把老大的桌面快捷方式改写过去 ——
// 实测截图取证：桌面「个人AI工作台」被指向了压测进程，快捷方式语义被破坏。
// 规则：只有位于 <安装根>\workbench.exe 的实例才允许写快捷方式；其它位置的副本
// 一律跳过（返回 nil，不报错），避免把开发/压测副本污染成"正式入口"。
func Create() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	if !isCanonicalExe(exe) {
		return nil
	}
	workDir := filepath.Dir(exe)
	tmp, err := os.CreateTemp("", "wb_shortcut_*.ps1")
	if err != nil {
		return err
	}
	scriptPath := tmp.Name()
	defer os.Remove(scriptPath)
	// PowerShell 5.1 读取无 BOM 的 UTF-8 中文会乱码，必须写 BOM。
	if _, err := tmp.WriteString("\xEF\xBB\xBF" + psScript); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath, exe, workDir)
	cmd.SysProcAttr = winhide.SysProcAttr()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("创建快捷方式失败: %v (%s)", err, truncate(string(out), 400))
	}
	if !strings.Contains(string(out), "SHORTCUT_OK") {
		return fmt.Errorf("快捷方式回读校验未通过: %s", truncate(string(out), 400))
	}
	return nil
}

// isCanonicalExe 判断某个 EXE 是否是"正式安装位置"的可执行文件。
//
// 判据：文件名必须是 workbench.exe（而非 wbsoak24.exe / wbh.exe 这类副本名），
// 且它的父目录下必须有 runtime 或 workbench-output 之一（说明是同目录的数据根）。
// 这样既能挡住临时目录副本，也不会误伤用户把整个目录搬到别的盘。
func isCanonicalExe(exe string) bool {
	if !strings.EqualFold(filepath.Base(exe), "workbench.exe") {
		return false
	}
	root := filepath.Dir(exe)
	for _, sub := range []string{"runtime", "workbench-output"} {
		if st, err := os.Stat(filepath.Join(root, sub)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// truncate 把字符串限制到最多 n 个**字符**（rune），不产生残缺 UTF-8 序列。
//
// 为什么不用字节切片（2026-10-26 round 187 实测修正）：本函数只用于**错误信息**
// （创建快捷方式失败 / 回读校验未通过两处，都是把 powershell 的输出截到 400）。
// 旧实现按字节切：中文 PowerShell 错误（本程序面向 zh-CN 机器，几乎必然含中文）被
// 从 rune 中间切断，产出非法 UTF-8 —— 探针实测 "utf8.Valid=false、末字节 229"。
// 该串随后进 fmt.Errorf → JSON/SSE，Go 会把非法字节替换成 U+FFFD：面板上就是乱码问号。
// 同族后果本会话刚处理过一次（CHANGELOG 第一行带 3 个 U+FFFD 且无任何判据发现）。
//
// 语义保持不变的部分：不加省略号、且**幂等**（截过的串再截不变）—— 现有测试钉住这两点，
// 因为错误信息会在多层包装里被反复截，加省略号会让长度越截越短、破坏幂等。
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// Guard 判断当前 EXE 是否位于正式安装位置（同目录有 runtime/ 或 workbench-output/）。
//
// 背景：桌面快捷方式曾被测试副本劫持（wbsoak24.exe / app-cache 临时目录），
// 用 Guard() 把"非正式位置的实例不得改快捷方式"这条规则变成代码。
func Guard() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	return isCanonicalExe(exe)
}

//go:build windows

package shortcut

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
    $lnk.Save()
    # 回读校验
    $check = $shell.CreateShortcut($lnkPath)
    if ($check.TargetPath -eq $exe) { $ok++ } else { Write-Error "回读不一致: $lnkPath" }
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
	cmd.SysProcAttr = hiddenAttr()
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
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

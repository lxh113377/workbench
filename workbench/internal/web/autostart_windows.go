//go:build windows

package web

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const valueName = "PersonalWorkbench"

// autostartEnabled 查询开机自启是否已登记。
//
// 同样必须隐藏窗口：本函数在**每次打开设置面板**时都会被调用（渲染开关状态），
// 还会在 setAutostart 末尾回读校验时调用一次 ——
// 不隐藏的话，老大来回切页面就会持续闪黑窗（reg.exe 是 CONSOLE 程序）。
func autostartEnabled() (bool, error) {
	cmd := exec.Command("reg", "query", runKey, "/v", valueName)
	cmd.SysProcAttr = hiddenProcAttr()
	out, err := cmd.Output()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), valueName), nil
}

// setAutostart 开启或关闭开机自启（写入 HKCU Run，无需管理员权限）。
func setAutostart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	// 必须隐藏窗口（2026-10-19 现场修复）：
	// reg.exe 是 CONSOLE(3) 子系统程序，而这里是**面板上的用户动作** ——
	// 不隐藏的话，老大每点一次「开机自启」就会闪一个黑窗。
	// 与 dshprofile.hiddenProcAttr / shortcut.hiddenAttr / executor.hiddenSysProcAttr 同款语义。
	if enabled {
		cmd := exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", `"`+exe+`" --silent`, "/f")
		cmd.SysProcAttr = hiddenProcAttr()
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("写入开机自启失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	} else {
		cmd := exec.Command("reg", "delete", runKey, "/v", valueName, "/f")
		cmd.SysProcAttr = hiddenProcAttr()
		if out, err := cmd.CombinedOutput(); err != nil && !strings.Contains(string(out), "unable to find") {
			return fmt.Errorf("删除开机自启失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	}
	now, _ := autostartEnabled()
	if now != enabled {
		return fmt.Errorf("开机自启设置回读不一致（期望 %v，实际 %v）", enabled, now)
	}
	return nil
}

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
func autostartEnabled() (bool, error) {
	out, err := exec.Command("reg", "query", runKey, "/v", valueName).Output()
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
	if enabled {
		cmd := exec.Command("reg", "add", runKey, "/v", valueName, "/t", "REG_SZ", "/d", `"`+exe+`" --silent`, "/f")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("写入开机自启失败: %v (%s)", err, strings.TrimSpace(string(out)))
		}
	} else {
		cmd := exec.Command("reg", "delete", runKey, "/v", valueName, "/f")
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
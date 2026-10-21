//go:build !windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// headless 判断是否处于"无头执行"（不弹任何窗口）。
//
// 与 Windows 实现同口径：WB_HEADLESS=1/true/yes/on 或存在 CI 环境变量。
func headless() bool {
	if v := os.Getenv("WB_HEADLESS"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
			return true
		}
	}
	return os.Getenv("CI") != ""
}

// Open 在非 Windows 平台调用系统默认 opener（无头模式下不启动）。
func Open(url string) error {
	if headless() {
		return nil
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	return nil
}

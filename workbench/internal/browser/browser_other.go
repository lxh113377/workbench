//go:build !windows

package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

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

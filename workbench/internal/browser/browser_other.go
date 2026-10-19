//go:build !windows

package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open 在非 Windows 平台调用系统默认 opener。
func Open(url string) error {
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

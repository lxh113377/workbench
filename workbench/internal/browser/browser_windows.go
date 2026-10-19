//go:build windows

package browser

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Open 用 Edge 的 --app 模式打开（无地址栏，接近原生窗口）；Edge 缺失时回落默认浏览器。
func Open(url string) error {
	if edge := findEdge(); edge != "" {
		cmd := exec.Command(edge, "--app="+url, "--window-size=1280,880")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	return fallback(url)
}

func findEdge() string {
	candidates := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	return ""
}

func fallback(url string) error {
	if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	return nil
}

//go:build windows

package browser

import (
	"fmt"
	"os/exec"
	"workbench/internal/winhide"
)

// Open 用 Edge 的 --app 模式打开（无地址栏，接近原生窗口）；Edge 缺失时回落默认浏览器。
//
// 无头开关（2026-10-21 老大指令「不要跳窗，无头执行」）：
// WB_HEADLESS=1 时**不启动任何浏览器**，只做判定并返回。
// 为什么需要：测试与无人值守场景会通过真实二进制拉起实例，
// 若代码路径无条件调起 Edge --app，桌面就会在跑测试时反复弹窗。
// 这类"测试污染桌面"的行为与被测逻辑无关，必须可以从环境层关掉。
func Open(url string) error {
	if headless() {
		return nil
	}
	if edge := findEdge(); edge != "" {
		cmd := exec.Command(edge, "--app="+url, "--window-size=1280,880")
		// 隐藏属性：Edge 自身是 GUI 进程，HideWindow 主要作用于它可能分配的
		// 控制台/启动提示；配合 CREATE_NO_WINDOW 保证不闪壳窗口。
		cmd.SysProcAttr = winhide.SysProcAttr()
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
	// 回落路径同样隐藏（rundll32 会短暂分配控制台）。
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = winhide.SysProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("打开浏览器失败: %w", err)
	}
	return nil
}

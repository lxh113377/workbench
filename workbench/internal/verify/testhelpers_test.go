package verify

import (
	"context"
	"os/exec"
	"time"
)

// testCtx 给一个带余量的上下文（测试用，避免把 cancel 丢掉造成泄漏告警）。
func testCtx(d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	_ = cancel
	return ctx
}

// lookShell 确认本机有 PowerShell 系可执行文件。
func lookShell() (string, error) {
	for _, exe := range []string{"powershell.exe", "pwsh.exe", "powershell", "pwsh"} {
		if p, err := exec.LookPath(exe); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

//go:build !windows

package tray

import "time"

// Run 在非 Windows 平台退化为阻塞等待（保持进程存活）。
func Run(opts Options) {
	for {
		time.Sleep(time.Hour)
	}
}

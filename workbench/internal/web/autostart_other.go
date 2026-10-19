//go:build !windows

package web

import "fmt"

func autostartEnabled() (bool, error) { return false, nil }

func setAutostart(enabled bool) error {
	return fmt.Errorf("当前平台不支持开机自启设置")
}

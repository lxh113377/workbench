//go:build !windows

package supervisor

// handleCount 在非 Windows 平台不可用。
func handleCount() int { return 0 }

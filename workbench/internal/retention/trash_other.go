//go:build !windows

package retention

import (
	"os"
)

// Trash 的非 Windows 实现：**永久删除**。
//
// 这是有意的降级：跨平台回收站（FreeDesktop Trash）实现复杂且行为不一致，
// 而本项目当前只针对 Windows 交付。为避免误用，这里明确标注语义差异：
//   - Windows: 进回收站，可反悔（硬约束 5）；
//   - 其它平台: 直接删除，不可反悔。
//
// 若未来要支持其它平台，应实现 FreeDesktop Trash 规范后再放开。
func Trash(path string) error {
	return os.RemoveAll(path)
}

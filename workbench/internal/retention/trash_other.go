//go:build !windows

package retention

import (
	"fmt"
	"os"
	"path/filepath"
)

// Trash 的非 Windows 实现：**永久删除**（先过安全阀）。
//
// 这是有意的降级：跨平台回收站（FreeDesktop Trash）实现复杂且行为不一致，
// 而本项目当前只针对 Windows 交付。为避免误用，这里明确标注语义差异：
//   - Windows: 进回收站，可反悔（硬约束 5）；
//   - 其它平台: 直接删除，不可反悔。
//
// 2026-10-19 平台解耦（issue #2）：改造前这里直接 os.RemoveAll，
// 危险路径判定只存在于 Windows 分支 —— 实测 Trash("/") 在 Linux 上会递归删掉文件系统根。
// 现在与 Windows 同序：先存在性检查，再过 refuseDangerousPath，最后才删。
//
// 若未来要支持其它平台，应实现 FreeDesktop Trash 规范后再放开"可反悔"语义。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, serr := os.Lstat(abs); serr != nil {
		return fmt.Errorf("目标不存在: %w", serr)
	}
	if err := refuseDangerousPath(abs); err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

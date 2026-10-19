//go:build windows

package retention

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// Windows 回收站 API（SHFileOperationW + FOF_ALLOWUNDO）。
//
// 为什么不用 os.RemoveAll：项目硬约束 5「所有删除走回收站」。
// 误删用户数据（尤其是只按 mtime 判断的自动清理）必须可反悔。
//
// 注意：仅本包使用。别处若需要"永久删除"应显式声明理由。

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

// FO_* 与 FOF_* 常量。
const (
	foDelete = 0x0003
	fofAllowUndo = 0x0040
	fofNoConfirmation = 0x0010
	fofSilent = 0x0004
	fofNoErrorUI = 0x0400
)

// shFileOp 是 SHFileOperationW 的输入结构（Windows SDK）。
type shFileOp struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted  int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// Trash 把文件/目录移入 Windows 回收站。
//
// 注意：pFrom 必须是以双 NUL 结尾的 UTF-16 字符串。
// 若回收站不可用（例如域策略禁用），返回错误，调用方应记录而不是永久删除。
func Trash(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	// 前置校验：必须存在（不存在就不必删）。
	if _, serr := os.Lstat(abs); serr != nil {
		return fmt.Errorf("目标不存在: %w", serr)
	}
	// 安全阀：refuseDangerousPath（平台无关实现见 danger.go，非 Windows 的 Trash 同样经过它）。
	if err := refuseDangerousPath(abs); err != nil {
		return err
	}

	from, err := syscall.UTF16FromString(abs)
	if err != nil {
		return err
	}
	// UTF16FromString 已带一个 NUL；再补一个形成双 NUL。
	from = append(from, 0)

	op := shFileOp{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI,
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	rc, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if rc != 0 {
		return fmt.Errorf("SHFileOperationW 返回 0x%x", rc)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("操作被中止（用户或系统取消了回收）")
	}
	return nil
}

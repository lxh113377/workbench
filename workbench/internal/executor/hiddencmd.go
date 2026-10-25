package executor

// hiddencmd.go -- 统一的「静默启动子进程」助手（2026-10-19 现场）。
//
// 现场缺陷：linkDir 用 `exec.Command("cmd", "/c", "mklink", "/J", dst, src)` 建 junction，
// 而 cmd.exe 是 CONSOLE(3) 子系统程序，且这里**没有设置任何窗口隐藏标志** ——
// linkDir 在**每个任务**做准备时都会执行（挂 profiles junction），
// 于是每跑一个任务就可能闪一次控制台窗口。
//
// 对比：同一包里连 DSH 主进程（executor.go）都用 winhide.SysProcAttr()
// 带 HideWindow + CREATE_NO_WINDOW；唯独这处裸奔。
//
// 本助手把这些标志收敛到一处，避免再次「某个 spawn 点忘了加」。
import (
	"os/exec"
	"workbench/internal/winhide"
)

const createNoWindowFlag = 0x08000000 // CREATE_NO_WINDOW

// hiddenCmdWith 构造一个**不会弹出控制台窗口**的子进程命令。
//
// 语义：
//   - HideWindow        -> STARTF_USESHOWWINDOW + SW_HIDE（即便目标分配了控制台也隐藏）；
//   - CREATE_NO_WINDOW  -> 从一开始就不给控制台程序分配控制台。
//
// 两者同时给上，才在「GUI 目标 / CONSOLE 目标」两种情况下都不闪窗。
// 平台差异由 sysproc_windows.go / sysproc_other.go 提供。
func hiddenCmdWith(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = winhide.SysProcAttr()
	return cmd
}

// hiddenCmd 是 hiddenCmdWith 的短名（测试与调用点都用它）。
func hiddenCmd(name string, args ...string) *exec.Cmd {
	return hiddenCmdWith(name, args...)
}

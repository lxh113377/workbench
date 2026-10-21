//go:build windows

package tray

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	shell32              = syscall.NewLazyDLL("shell32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	procAppendMenuW      = user32.NewProc("AppendMenuW")
	procTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	procSetForegroundW   = user32.NewProc("SetForegroundWindow")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procDestroyMenu      = user32.NewProc("DestroyMenu")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procLoadIconW        = user32.NewProc("LoadIconW")
	procCreateIconIndir  = user32.NewProc("CreateIconIndirect")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procShellNotifyIcon  = shell32.NewProc("Shell_NotifyIconW")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	// RegisterWindowMessageW：注册 "TaskbarCreated" 广播消息。
	// Explorer 重启后所有托盘图标都会消失，Shell 会广播这条消息让程序自己重加，
	// 不接这条消息就表现为"用着用着图标没了"（2026-10-19 老大现场反馈）。
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procExtractIconExW         = shell32.NewProc("ExtractIconExW")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
)

const (
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmApp          = 0x8000
	wmRButtonUp    = 0x0205
	wmLButtonUp    = 0x0202
	wmCommand      = 0x0111
	nimAdd         = 0x00000000
	nimDelete      = 0x00000002
	nimModify      = 0x00000001
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	menuString     = 0x00000000
	menuSeparator  = 0x00000800
	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	// 托盘消息窗口的扩展样式（2026-10-20 现场：任务视图里堆满"个人 AI 工作台"卡片）。
	//
	// 为什么必须有这两个：
	//   WS_EX_TOOLWINDOW (0x80)  —— 工具窗口不进 Alt-Tab / 任务视图。缺了它，每个实例
	//                              的托盘消息窗都会被当成一个普通应用窗口，在任务视图里
	//                              生成一张同名空白卡片；实例退出后卡片也不会自己消失。
	//   （不要加 WS_EX_NOACTIVATE：托盘右键菜单要靠 SetForegroundWindow 才能正常收起）
	wsExToolWindow = 0x00000080
	// 消息窗用 WS_POPUP 而不是 0：配合 WS_EX_TOOLWINDOW，Shell 才会把它彻底当工具窗。
	wsPopup = 0x80000000

	// SetWindowPos 的 SWP 标志：隐藏 + 不激活 + 不重排 Z 序。
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpHideWindow = 0x0080

	// 消息窗标题（2026-10-20 现场：任务视图里塞满"个人 AI 工作台"卡片）。
	//
	// 托盘主窗口是**纯消息窗口**，它没有可见内容、也不该被用户看到/理解；
	// 一旦它漏进 Alt-Tab / 任务视图（样式丢失或第三方 Shell 插件强行收录），
	// 标题就会变成一张"关不掉的空卡片"。所以标题里显式标注它是内部消息窗，
	// 就算哪天又被收录，用户看到的也是"内部消息窗口（可忽略）"而不是一个像应用的入口。
	trayMsgWindowTitle = "个人 AI 工作台（内部消息窗口，请忽略）"

	// 窗口类名（提成常量：tests/instance_guard.ps1 等取证工具按它找托盘窗，
	// 单元测试也据此守护，改名必须同步改工具，不能悄悄漂移）。
	trayWindowClassName = "PersonalWorkbenchTrayClass"
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
}

type point struct{ X, Y int32 }

type msgStruct struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

var (
	currentOpts *Options
	hwndMain    syscall.Handle
	iconHandle  syscall.Handle
	menuOpen    = uint16(1001)
	menuStart   = uint16(1002)
	menuPause   = uint16(1003)
	menuResume  = uint16(1004)
	menuStop    = uint16(1005)
	menuQuit    = uint16(1006)
)

// trayWindowExStyle 返回托盘消息窗的扩展样式。
//
// 提成函数而不是在 CreateWindowExW 调用点直接写字面量，是为了让守护测试能**真的调用它**：
// 只断言常量 wsExToolWindow == 0x80 防不住最常见的回退方式 ——
// 常量还好好写着 0x80，调用点却被人「顺手改回 0」。那种回退在常量断言面前是全绿的。
// 这里把「实际会传进 API 的那个值」变成可调用入口，测试直接验证入口的返回值，
// 调用点再也绕不过去。
func trayWindowExStyle() uint32 {
	return wsExToolWindow
}

// hideTrayWindow 把托盘消息窗设为永不显示。
//
// 同理提成函数：让守护测试能验证「创建后确实会走隐藏」这条路径存在且参数正确，
// 而不是只验证 swpHideWindow 这个常量本身是 0x80。
func hideTrayWindow(hwnd syscall.Handle) {
	procSetWindowPos.Call(uintptr(hwnd), 0, 0, 0, 0, 0, swpNoZOrder|swpNoActivate|swpHideWindow)
}

// Run 在独立 OS 线程上创建托盘并进入消息循环（阻塞）。
func Run(opts Options) {
	currentOpts = &opts
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString(trayWindowClassName)
	wcw := wndClassEx{
		CbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     syscall.Handle(hInst),
		LpszClassName: className,
	}
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcw))); r == 0 {
		fmt.Println("托盘：RegisterClassExW 失败")
		select {}
	}
	title, _ := syscall.UTF16PtrFromString(trayMsgWindowTitle)
	// 必须是 WS_EX_TOOLWINDOW(0x80) 的消息窗口，否则每个实例都会在任务视图 / Alt-Tab
	// 里留下一张"个人 AI 工作台"卡片 —— 2026-10-20 老大现场：「我这里看到的是什么,关不掉」，
	// 任务视图右侧十几张同名白卡，全是历史实例的托盘窗口（纯消息窗，没有可见内容，也关不掉）。
	//
	// 实测取证（2026-10-20 22:5x，pid 41448）：
	//   tray hwnd=18615588 style=0x04C00000 exstyle=0x00000100 TOOLWINDOW=False
	//   → 任务栏/任务视图把它当普通应用窗口收录。
	// 修法（两处都要）：
	//   ① 这里传 exStyle=WS_EX_TOOLWINDOW：工具窗不进 Alt-Tab / 任务视图；
	//      注意**不要**加 WS_EX_NOACTIVATE —— 托盘右键菜单依赖 SetForegroundWindow(自身)
	//      才能正常弹出/失焦自动收起，加了 NOACTIVATE 会让菜单卡在屏上关不掉。
	//   ② 创建后 SetWindowPos 用 SWP_HIDEWINDOW 确保永不显示（消息窗只需要能收消息）。
	// 注释留证，防止后人又"顺手改回 0"。
	h, _, _ := procCreateWindowExW.Call(
		uintptr(trayWindowExStyle()), uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		uintptr(wsPopup), 0, 0, 0, 0, 0, 0, uintptr(hInst), 0,
	)
	if h == 0 {
		fmt.Println("托盘：CreateWindowExW 失败")
		select {}
	}
	hwndMain = syscall.Handle(h)
	// 消息窗只需能收消息，永不显示（WS_POPUP + 不 ShowWindow 已经是隐藏态，
	// 这里再显式 SWP_HIDEWINDOW 兜一层，防止某些 Shell 版本把它当潜在可见窗收录）。
	hideTrayWindow(hwndMain)

	icon := loadAppIcon()
	iconHandle = icon
	data := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwndMain,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmApp + 1,
		HIcon:            icon,
	}
	copy(data.SzTip[:], syscall.StringToUTF16(opts.Tooltip))
	if !addNotifyIcon(&data) {
		// 首次注册失败 → 诊断 +（可能）自愈后重试一次。详见 integrity_windows.go。
		// 自愈做不到时把原因上报（否则用户只看到"没有图标"，无从排查）。
		if healed, reason := tryRaiseIntegrity(); healed && addNotifyIcon(&data) {
			// 修好了：继续走消息循环
		} else {
			warn(opts.OnWarn, "托盘图标注册失败: "+reason)
		}
	}
	taskbarCreated = registerTaskbarCreatedMessage()

	var msg msgStruct
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		// Explorer 重启 → 重新注册图标（否则图标永久消失）。
		if taskbarCreated != 0 && msg.Message == taskbarCreated {
			addNotifyIcon(&data)
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

// loadAppIcon 取托盘图标：优先用 EXE 自带的图标资源（第 0 个），取不到再退 IDI_APPLICATION。
//
// 现场（2026-10-19 老大："状态栏也没有图标"）：原实现只有 IDI_APPLICATION 兜底，
// 于是即使图标注册成功，托盘里也是一张"通用白板图"（实测截图：一个灰白方块），
// 老大根本认不出那是工作台。现在改成从自己身上取图标，托盘与桌面快捷方式图标一致。
func loadAppIcon() syscall.Handle {
	if h := extractOwnIcon(); h != 0 {
		return h
	}
	id, _, _ := procLoadIconW.Call(0, 32512) // IDI_APPLICATION 兜底
	return syscall.Handle(id)
}

// extractOwnIcon 用 ExtractIconExW 取本 EXE 的第 0 个图标；取不到返回 0。
func extractOwnIcon() syscall.Handle {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	p, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0
	}
	large := make([]syscall.Handle, 1)
	small := make([]syscall.Handle, 1)
	n, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(p)),
		0,
		uintptr(unsafe.Pointer(&large[0])),
		uintptr(unsafe.Pointer(&small[0])),
		1,
	)
	if n == 0 || large[0] == 0 {
		return 0
	}
	return large[0]
}

// taskbarCreated 保存 "TaskbarCreated" 的窗口消息号（0 表示注册失败）。
var taskbarCreated uint32

// registerTaskbarCreatedMessage 注册 Explorer 广播的 TaskbarCreated 消息。
func registerTaskbarCreatedMessage() uint32 {
	name, err := syscall.UTF16PtrFromString("TaskbarCreated")
	if err != nil {
		return 0
	}
	id, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	return uint32(id)
}

// warn 把诊断信息交给调用方（拿不到回调时退回 stdout，GUI 模式下不影响桌面）。
func warn(cb func(string), msg string) {
	if cb != nil {
		cb(msg)
		return
	}
	fmt.Println(msg)
}

// addNotifyIcon 注册托盘图标；返回 false 表示 Shell_NotifyIcon 报告失败。
//
// 失败不是"进程内部错误"而是**权限裁决**：实测 2026-10-19，Low 完整性级别的进程
// 被 UIPI 拒绝向 Medium 的 Explorer 注册图标，GetLastError 报 "Access is denied"。
// 所以本函数只回报结果，处置交给调用方（自愈 or 放弃），自身无副作用。
func addNotifyIcon(data *notifyIconData) bool {
	r, _, _ := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(data)))
	return r != 0
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmApp + 1:
		switch lParam {
		case wmLButtonUp:
			if currentOpts != nil && currentOpts.OnOpen != nil {
				go currentOpts.OnOpen()
			}
		case wmRButtonUp:
			showMenu(hwnd)
		}
		return 0
	case wmCommand:
		handleCommand(uint16(wParam & 0xFFFF))
		return 0
	case wmDestroy:
		data := notifyIconData{CbSize: uint32(unsafe.Sizeof(notifyIconData{})), HWnd: hwndMain, UID: 1}
		procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
		return 0
	case wmClose:
		// 关闭窗口不退出，引擎继续在后台跑
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return r
}

func showMenu(hwnd syscall.Handle) {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	appendItem(menu, menuOpen, "打开面板")
	appendItem(menu, menuStart, "启动引擎")
	appendItem(menu, menuPause, "暂停")
	appendItem(menu, menuResume, "继续")
	appendItem(menu, menuStop, "停止引擎")
	procAppendMenuW.Call(menu, menuSeparator, 0, 0)
	appendItem(menu, menuQuit, "退出工作台")
	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundW.Call(uintptr(hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	if cmd != 0 {
		handleCommand(uint16(cmd))
	}
	procPostMessageW.Call(uintptr(hwnd), 0, 0, 0)
}

func appendItem(menu uintptr, id uint16, text string) {
	t, _ := syscall.UTF16PtrFromString(text)
	procAppendMenuW.Call(menu, menuString, uintptr(id), uintptr(unsafe.Pointer(t)))
}

func handleCommand(id uint16) {
	if currentOpts == nil {
		return
	}
	switch id {
	case menuOpen:
		if currentOpts.OnOpen != nil {
			go currentOpts.OnOpen()
		}
	case menuStart:
		if currentOpts.OnStart != nil {
			go currentOpts.OnStart()
		}
	case menuPause:
		if currentOpts.OnPause != nil {
			go currentOpts.OnPause()
		}
	case menuResume:
		if currentOpts.OnResume != nil {
			go currentOpts.OnResume()
		}
	case menuStop:
		if currentOpts.OnStop != nil {
			go currentOpts.OnStop()
		}
	case menuQuit:
		if currentOpts.OnQuit != nil {
			go currentOpts.OnQuit()
		}
	}
}

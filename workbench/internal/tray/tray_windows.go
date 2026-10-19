//go:build windows

package tray

import (
	"fmt"
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

// Run 在独立 OS 线程上创建托盘并进入消息循环（阻塞）。
func Run(opts Options) {
	currentOpts = &opts
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, _ := syscall.UTF16PtrFromString("PersonalWorkbenchTrayClass")
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
	title, _ := syscall.UTF16PtrFromString(opts.Title)
	h, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, uintptr(hInst), 0,
	)
	if h == 0 {
		fmt.Println("托盘：CreateWindowExW 失败")
		select {}
	}
	hwndMain = syscall.Handle(h)

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
	procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&data)))

	var msg msgStruct
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func loadAppIcon() syscall.Handle {
	id, _, _ := procLoadIconW.Call(0, 32512) // IDI_APPLICATION 兜底
	return syscall.Handle(id)
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

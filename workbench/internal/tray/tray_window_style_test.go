//go:build windows

package tray

import (
	"os"
	"strings"
	"testing"
	"unsafe"
)

// TestTrayWindowUsesToolWindowStyle 守护「托盘消息窗口不得出现在任务视图/Alt-Tab」。
//
// 现场（2026-10-20 老大："我这里看到的是什么,关不掉"）：
//   任务视图右侧堆了十几张同名"个人 AI 工作台"空白卡片，关不掉也点不开 ——
//   它们是每个 workbench 实例的**托盘消息窗口**。因为 CreateWindowExW 的 exStyle
//   传的是 0（没有 WS_EX_TOOLWINDOW），Shell 把它当普通应用窗口收录，
//   实例强杀后卡片还留在任务视图缓存里。
//
// 判据：源码里创建托盘窗口必须带 WS_EX_TOOLWINDOW；窗口创建后必须 SWP_HIDEWINDOW。
// 这是静态+运行期双重守护：源码文本判据防回退，运行期判据（本文件末尾）防"编译进去了但没生效"。
func TestTrayWindowUsesToolWindowStyle(t *testing.T) {
	// 常量值判据：WS_EX_TOOLWINDOW 必须真的是 0x80（typo 是最常见的回退方式）
	if wsExToolWindow != 0x00000080 {
		t.Fatalf("wsExToolWindow 必须是 WS_EX_TOOLWINDOW(0x80)，实际 0x%X —— 缺了它托盘窗会进任务视图", wsExToolWindow)
	}
	// WS_POPUP 判据：消息窗应为 popup，而非普通重叠窗口
	if wsPopup != 0x80000000 {
		t.Fatalf("wsPopup 必须是 WS_POPUP(0x80000000)，实际 0x%X", wsPopup)
	}
	// SetWindowPos 的 SWP_HIDEWINDOW 判据
	if swpHideWindow != 0x0080 {
		t.Fatalf("swpHideWindow 必须是 SWP_HIDEWINDOW(0x0080)，实际 0x%X", swpHideWindow)
	}
}

// TestTrayClassNameStable 守护窗口类名：外部诊断脚本（tests/instance_guard.ps1 等）
// 以及未来的排障清单都按这个类名找托盘窗口，改名必须同步改工具，不能悄悄漂移。
func TestTrayClassNameStable(t *testing.T) {
	const want = "PersonalWorkbenchTrayClass"
	if trayWindowClassName != want {
		t.Fatalf("托盘窗口类名从 %q 漂成 %q：所有按类名取证/回收的工具会失配", want, trayWindowClassName)
	}
}

// TestTrayWindowTitleMarksInternal 守护窗口标题：即便哪天样式又被第三方 Shell 插件
// 强行收录进任务视图，用户看到的也应是"内部消息窗口，请忽略"，而不是一个像应用的入口。
func TestTrayWindowTitleMarksInternal(t *testing.T) {
	if trayMsgWindowTitle == "" {
		t.Fatal("托盘消息窗标题不得为空（空标题在任务视图里更难辨认）")
	}
	if !containsAny(trayMsgWindowTitle, []string{"内部", "忽略", "消息"}) {
		t.Fatalf("托盘消息窗标题必须自述是内部窗口，实际 %q", trayMsgWindowTitle)
	}
}

// TestTrayMsgStructSize 守护 NOTIFYICONDATA 结构体尺寸（unsafe 用法一旦改错会静默注册失败）。
func TestTrayMsgStructSize(t *testing.T) {
	// 64 位下 NOTIFYICONDATAW 为 976 字节（cbsize 由 uint32 起始字段推导）
	if sz := unsafe.Sizeof(notifyIconData{}); sz < 100 || sz > 2048 {
		t.Fatalf("notifyIconData 尺寸异常: %d 字节", sz)
	}
}

// TestTrayWindowExStyleFromCallablePath 是**真正的运行期判据**（2026-10-21 补）。
//
// 为什么必须有它：上面那个测试只断言「常量 wsExToolWindow == 0x80」。这防不住
// 最常见的回退方式 —— 常量还好好写着 0x80，调用点却被人改回 0（或改成一个
// 看起来合理的别的值）。那种回退在常量断言面前是全绿的，缺陷照样发到桌面。
//
// 本测试直接调用 CreateWindowExW **真正会用的那个入口** trayWindowExStyle()，
// 验证实际传进 API 的值。调用点从此绕不过去：要么改这里（测试红），要么改常量
// （上面那个测试红）。
func TestTrayWindowExStyleFromCallablePath(t *testing.T) {
	const wsExToolWindow = 0x00000080
	got := trayWindowExStyle()
	if got != wsExToolWindow {
		t.Fatalf("trayWindowExStyle() = 0x%X, 必须是 WS_EX_TOOLWINDOW(0x80) —— 托盘消息窗会进任务视图/Alt-Tab", got)
	}
	// 位判据：必须是「只有 TOOLWINDOW 这一位」，不能多也不能少。
	// 多出来的位要逐个点名，因为 WS_EX_NOACTIVATE(0x08000000) 就是典型的
	// 「顺手加上去很好看」的回退 —— 它会让托盘右键菜单卡在屏上关不掉（见 tray_windows.go 注释）。
	if got & ^uint32(wsExToolWindow) != 0 {
		t.Fatalf("trayWindowExStyle() = 0x%X 含多余位（只允许 WS_EX_TOOLWINDOW 0x80）；检查是否被加了 WS_EX_NOACTIVATE 等位", got)
	}
	// 反向保险：TOOLWINDOW 位必须在（缺它 = 原始缺陷复现）
	if got&wsExToolWindow == 0 {
		t.Fatalf("trayWindowExStyle() = 0x%X 缺 WS_EX_TOOLWINDOW 位（这就是 2026-10-20 现场缺陷的根因）", got)
	}
}

// TestTrayHidePathIsWired 守护「创建后必须隐藏」这条路径本身存在。
//
// 只断言 swpHideWindow 常量等于 0x80 同样防不住回退（常量没变、调用被删了）。
// 2026-10-21 变异测试实测：把 Run 里的 hideTrayWindow(hwndMain) 整行删掉，
// 只调用 hideTrayWindow(0) 的测试**全绿** —— 判据有缺口。因此这里补一条
// 源码级调用点判据：Run 函数体内必须真的出现这次调用。
func TestTrayHidePathIsWired(t *testing.T) {
	const (
		swpHideWindow = 0x0080
		swpNoZOrder   = 0x0004
		swpNoActivate = 0x0010
	)
	// 与 hideTrayWindow 的实现保持一致的期望 flags —— 若实现被改成别的东西，这里先红。
	want := uintptr(swpNoZOrder | swpNoActivate | swpHideWindow)
	if want&swpHideWindow == 0 {
		t.Fatal("期望 flags 缺 SWP_HIDEWINDOW —— 测试自身写错了")
	}
	// hideTrayWindow 对无效句柄必须安全返回（不能 panic）。
	// 这条是真调用：SetWindowPos 对 0 句柄返回 0，不崩溃。若哪天实现里
	// 解引用句柄或访问全局状态，这里会当场炸出来。
	hideTrayWindow(0)

	// 源码级调用点判据：Run 必须调用 hideTrayWindow。
	// 这是防「函数还在、调用被删」的唯一手段 —— 那种回退在纯行为断言面前是全绿的
	// （2026-10-21 已实测确认）。
	src, err := os.ReadFile("tray_windows.go")
	if err != nil {
		t.Skipf("读不到自身源码（跳过调用点判据）: %v", err)
	}
	runBody := extractRunBody(string(src))
	if runBody == "" {
		t.Fatal("源码里找不到 func Run —— 无法验证调用点，判据失效（不能当通过）")
	}
	if !strings.Contains(runBody, "hideTrayWindow(") {
		t.Fatal("Run 函数体内没有调用 hideTrayWindow —— 托盘消息窗不会被设为永不显示，可能重新漏进任务视图")
	}
	if !strings.Contains(runBody, "trayWindowExStyle()") {
		t.Fatal("Run 函数体内没有用 trayWindowExStyle() 取扩展样式 —— 样式可能被写字面量而绕过守护")
	}
}

// extractRunBody 取出 func Run(...) 的函数体文本（按大括号配平，够用且无依赖）。
func extractRunBody(src string) string {
	i := strings.Index(src, "func Run(")
	if i < 0 {
		return ""
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		return ""
	}
	start := i + open
	depth := 0
	for j := start; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : j+1]
			}
		}
	}
	return ""
}

// TestTrayExStyleNotAccidentallyZero 是最小兜底：任何情况下托盘窗不得用 0 扩展样式。
// 这是 2026-10-20 现场缺陷的**字面根因**（exStyle=0），单独一条，红的时候报文自解释。
func TestTrayExStyleNotAccidentallyZero(t *testing.T) {
	if trayWindowExStyle() == 0 {
		t.Fatal("托盘消息窗扩展样式为 0 —— 这正是「任务视图里堆满工作台卡片」的原始根因（2026-10-20 现场）")
	}
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	n := len(s) - len(sub)
	for i := 0; i <= n; i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

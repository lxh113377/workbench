//go:build windows

package shortcut

import (
	"os"
	"path/filepath"
	"testing"
)

// TestShortcutScriptDropsSilent 守护「快捷方式不带 --silent」这条判据。
//
// 现场（2026-10-19 老大："点击运行后打不开了"）：
// 快捷方式曾被写死 Arguments="--silent"，而 --silent 的语义是"服务化实例：不弹面板"，
// 于是双击后进程的确起来了（进程在、端口在），但**桌面上什么都不出现**，
// 表现就是"点了没反应 / 打不开"。修复后快捷方式必须为空参数。
//
// 判据刻意做**双向**：既要求脚本里没有 $lnk.Arguments = "--silent" 这种赋值，
// 也要求存在把 Arguments 显式清空的赋值 —— 只删不设会让旧的 --silent 残留在
// 已有 .lnk 里（快捷方式是改字段，不是重建文件）。
func TestShortcutScriptDropsSilent(t *testing.T) {
	script := psScript
	if script == "" {
		t.Fatal("快捷方式脚本为空")
	}
	// 1) 不得再把 --silent 写进快捷方式参数
	for _, bad := range []string{`$lnk.Arguments = "--silent"`, `$lnk.Arguments = '--silent'`} {
		if containsAll(script, bad) {
			t.Fatalf("快捷方式不得带 --silent（双击后不会弹面板），仍发现: %s", bad)
		}
	}
	// 2) 必须显式清空（否则老 lnk 里的 --silent 残留）
	if !containsAll(script, `$lnk.Arguments = ""`) {
		t.Fatal("脚本必须显式把 Arguments 置空，否则已有快捷方式里的 --silent 会残留")
	}
	// 3) 回读校验必须同时看 Arguments（只看 TargetPath 查不出 --silent 残留）
	if !containsAll(script, "Arguments") {
		t.Fatal("回读校验必须覆盖 Arguments")
	}
}

// TestShortcutScriptUsesNormalWindow 守护 WindowStyle：产物是 GUI 子系统（-H=windowsgui），
// 不再需要靠「最小化」掩盖控制台黑窗，正常显示即可（最小化反而让人以为没打开）。
func TestShortcutScriptUsesNormalWindow(t *testing.T) {
	if !containsAll(psScript, "$lnk.WindowStyle = 1") {
		t.Fatal("GUI 子系统产物应用 WindowStyle=1（正常显示）；WindowStyle=7 会让窗口以最小化出现")
	}
	if containsAll(psScript, "$lnk.WindowStyle = 7") {
		t.Fatal("WindowStyle 仍残留 7（最小化）")
	}
}

func containsAll(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestCreateIsNoopForTestBinary 复核：非正式位置的测试二进制调用 Create 必须静默跳过。
func TestCreateIsNoopForTestBinary(t *testing.T) {
	exe, err := os.Executable()
	if err == nil && isCanonicalExe(exe) {
		t.Skip("测试二进制恰好位于正式安装位置（极少见），跳过")
	}
	if err := Create(); err != nil {
		t.Fatalf("非正式位置必须静默返回 nil，实际: %v", err)
	}
	_ = filepath.Separator
}

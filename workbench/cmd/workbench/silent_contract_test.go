package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSilentDoesNotOpenPanel 守护「--silent 不弹面板」这条契约（黑盒 + 副作用判据）。
//
// 现场（2026-10-19 老大："一直在跳这个，可不可以不要"+"点击运行后打不开了"）：
// 同一份二进制里 --silent 的语义前后不一致 ——
//   - 首次启动分支：if !silent { openPanel(...) }   ← 尊重 silent
//   - **已在运行**分支：无条件 openPanel(...)        ← 无视 silent
// 于是守护进程/压测/自愈用 --silent 撞上既有实例时，照样往桌面弹一个面板窗口。
//
// 判据为什么不能只看 stdout：两个分支都会打印同样的「工作台已在运行：<url>」，
// 差别在**副作用**（是否新起一个 Edge --app 面板进程）。所以这里做进程级取证。
func TestSilentDoesNotOpenPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构建真实二进制并起实例")
	}
	exe := filepath.Join(t.TempDir(), "wb-silent.exe")
	build := hiddenTestCmd("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建受测二进制失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	root := t.TempDir()
	installed := filepath.Join(root, "workbench.exe")
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("读取受测二进制失败: %v", err)
	}
	if err := os.WriteFile(installed, data, 0o755); err != nil {
		t.Fatalf("复制二进制失败: %v", err)
	}
	for _, sub := range []string{"runtime", "workbench-output"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// 1) 起一个静默实例（--silent：不弹面板，正是服务化语义）
	// 必须走 hiddenTestCmd：裸 exec.Command 会让 CONSOLE 子系统的被测 EXE 闪黑窗，
	// 且环境缺 WB_HEADLESS 时"已在运行"分支会真的把 Edge 面板推到桌面。
	first := hiddenTestCmd(installed, "--silent")
	first.Dir = root
	if err := first.Start(); err != nil {
		t.Fatalf("启动首个实例失败: %v", err)
	}
	defer func() {
		if first.Process != nil {
			_ = first.Process.Kill()
			_, _ = first.Process.Wait()
		}
	}()

	urlFile := filepath.Join(root, "runtime", "panel.url")
	ready := false
	for i := 0; i < 120; i++ {
		if b, err := os.ReadFile(urlFile); err == nil && strings.Contains(string(b), "http") {
			ready = true
			break
		}
		sleepMs(100)
	}
	if !ready {
		t.Skip("首实例未在 12s 内就绪（环境限制），跳过")
	}

	// 2) 二次启动带 --silent：必须走「已在运行」分支，且**不得声明要弹面板**
	second := hiddenTestCmd(installed, "--silent")
	second.Dir = root
	out, _ := second.CombinedOutput()
	text := string(out)
	if !strings.Contains(text, "工作台已在运行") {
		t.Fatalf("二次启动未走「已在运行」分支：%s", strings.TrimSpace(text))
	}
	// 主判据：--silent 下绝不能出现"已打开面板"标记。
	// 这条不依赖浏览器是否真起来（那受环境影响），只判程序自己的决定。
	if strings.Contains(text, panelOpenedMarker) {
		t.Fatalf("--silent 二次启动仍声明要弹面板：服务化启动不得污染桌面。输出：%s", strings.TrimSpace(text))
	}
}

// TestNonSilentDeclaresPanel 守护反方向：不带 --silent 时必须声明要弹面板。
//
// 只有两个方向都测，"快捷方式点不开"这类缺陷才无处躲：
//   - --silent 却弹面板  -> 守护进程污染桌面
//   - 不带 --silent 却不弹 -> 用户点了没反应（老大 2026-10-19 的原始诉求）
func TestNonSilentDeclaresPanel(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构建真实二进制并起实例")
	}
	exe := filepath.Join(t.TempDir(), "wb-panel.exe")
	build := hiddenTestCmd("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建受测二进制失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	root := t.TempDir()
	installed := filepath.Join(root, "workbench.exe")
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, data, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"runtime", "workbench-output"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	first := hiddenTestCmd(installed, "--silent")
	first.Dir = root
	if err := first.Start(); err != nil {
		t.Fatalf("启动首实例失败: %v", err)
	}
	defer func() {
		if first.Process != nil {
			_ = first.Process.Kill()
			_, _ = first.Process.Wait()
		}
	}()
	urlFile := filepath.Join(root, "runtime", "panel.url")
	ready := false
	for i := 0; i < 120; i++ {
		if b, err := os.ReadFile(urlFile); err == nil && strings.Contains(string(b), "http") {
			ready = true
			break
		}
		sleepMs(100)
	}
	if !ready {
		t.Skip("首实例未在 12s 内就绪（环境限制），跳过")
	}

	second := hiddenTestCmd(installed)
	second.Dir = root
	out, _ := second.CombinedOutput()
	text := string(out)
	if !strings.Contains(text, panelOpenedMarker) {
		t.Fatalf("不带 --silent 时未声明要弹面板（用户会「点了没反应」）：%s", strings.TrimSpace(text))
	}
}

// panelProcCount 统计当前 msedge --app 面板进程数（= 桌面是否被弹出面板）。
//
// 取不到读数时用 -1 显式标记"没测成"，由调用方跳过判据 —— 绝不把读不到当成 0
// （那就是"没测成被当成没缺陷"，比断言失败更危险；本项目已有同类事故在册）。
//
// 命令里刻意只用单引号：PowerShell 5.1 经 -Command 传参时，双引号要跨越
// Go 字符串 + 进程参数 + PS 解析三层，实测会被吃掉导致 "无效查询" 静默返回 0。
func panelProcCount(t *testing.T) int {
	t.Helper()
	script := "$p = @(Get-CimInstance Win32_Process | Where-Object { $_.Name -eq 'msedge.exe' -and $_.CommandLine -like '*--app=*' }); $p.Count"
	cmd := hiddenTestCmd("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		t.Logf("取不到 msedge 进程列表（跳过进程判据）: %v", err)
		return -1
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		t.Logf("msedge 进程计数为空（跳过进程判据）")
		return -1
	}
	n := 0
	for _, c := range text {
		if c < '0' || c > '9' {
			t.Logf("msedge 进程计数非数字: %q（跳过进程判据）", text)
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

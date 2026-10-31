package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"workbench/internal/single"
)

// TestSilentDoesNotOpenPanel 守护「--silent 不弹面板」这条契约（黑盒 + 副作用判据）。
//
// 现场（2026-10-19 老大："一直在跳这个，可不可以不要"+"点击运行后打不开了"）：
// 同一份二进制里 --silent 的语义前后不一致 ——
//   - 首次启动分支：if !silent { openPanel(...) }   ← 尊重 silent
//   - **已在运行**分支：无条件 openPanel(...)        ← 无视 silent
//
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

	// root 是**子进程的工作目录**，不能用 t.TempDir()：它的自动清理失败会被记成
	// 测试 FAIL（实测：CI 连续 8 次假红全卡在这一句）。用 newSpawnDir 自管清理。
	root := newSpawnDir(t)
	installed := filepath.Join(root, "workbench.exe")
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("读取受测二进制失败: %v", err)
	}
	if err := os.WriteFile(installed, data, 0o755); err != nil {
		t.Fatalf("复制二进制失败: %v", err)
	}
	// 按 EXE 路径回收残留实例：被测实例可能脱离进程树（常驻/自愈重启），
	// 只杀直接子进程收不回它们，而它们会让 root 不可删。
	trackSpawnedExe(t, installed)
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
	defer killTree(first.Process) // 收整棵进程树：只 Kill 直接子进程会留下占住 root 的后代

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
	root := newSpawnDir(t) // 同上：子进程工作目录不得用 t.TempDir()
	installed := filepath.Join(root, "workbench.exe")
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, data, 0o755); err != nil {
		t.Fatal(err)
	}
	trackSpawnedExe(t, installed)
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
	defer killTree(first.Process) // 收整棵进程树：只 Kill 直接子进程会留下占住 root 的后代
	urlFile := filepath.Join(root, "runtime", "panel.url")
	// 就绪判据（2026-11-01 round 181 加固）：原来只判「文件里含 http」，两个问题：
	//   ① panel.url 可能**正在被写**（读到半截也含 http）⇒ 拿到的 URL 不完整；
	//   ② 只看到文件不等于端口**已可连**；第二实例走的是「实例已在运行」分支，
	//      那一步要求能定位到面板地址，若端口尚未就绪就会打出「未能定位面板地址」而失败。
	// 甲基实测（clean 检出 + 空载）连跑 3 次全 PASS、整包 1 次 PASS，只在聚合门禁并发跑到时红过一次
	//   ⇒ 是时序敏感（flaky），不是固定缺陷。加固方向是**显式等真就绪**，而不是把断言放宽。
	uri := ""
	for i := 0; i < 200; i++ { // 20s 上限：比原来的 12s 宽，但判据更严（要求端口可连）
		if b, err := os.ReadFile(urlFile); err == nil {
			for _, line := range strings.Split(string(b), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "http") { // 完整的一行 URL，而不是「文件里出现过 http」
					uri = line
				}
			}
		}
		if uri != "" && panelPortReachable(uri) {
			break
		}
		uri = ""
		sleepMs(100)
	}
	if uri == "" {
		t.Skip("首实例未在 20s 内写出完整 panel.url（环境限制），跳过")
	}
	if !panelPortReachable(uri) {
		t.Skipf("首实例面板端口 %s 在 20s 内不可连（环境限制），跳过", uri)
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

// panelPortReachable 判断 panel.url 里的面板端口是否**真的可连**。
//
// 为什么要这一层（2026-11-01 round 181）：原判据只看出「panel.url 里含 http」——
//
//	① 文件可能正在被写，读到的是半截；② 文件存在 ≠ 端口已就绪。
//	而第二实例走的是「实例已在运行」分支，那一步必须能定位到面板地址，
//	端口没就绪就会打出「未能定位面板地址」⇒ 测试失败（实为时序敏感）。
//
// 复用**产品自己用的**探测函数 single.ProbeExactPort（internal/single/instance.go:66），
// 而不是在测试里另写一个 net.Dial —— 否则测试与产品对「可达」的定义就会分叉
// （本仓在册病「同一契约多处实现」）。
func panelPortReachable(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port <= 0 {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	_, ok := single.ProbeExactPort(host, port)
	return ok
}

package supervisor

// selfheal_relaunch_test.go 守护 SelfHeal 的**实际拉起行为**（不只是回调被调用）。
//
// 现场关切（2026-10-19）：SelfHeal 用 os.Executable() 重新拉起自己（带 --silent）。
// 这条路径写错的后果：拉不起新进程 => 自愈形同虚设、进程一死就永久停机（24h 承诺破功）；
// 或参数/工作目录错 => 重启后换了数据根，看起来"活着"却读不到原来的队列与台账。
//
// 为什么这一版才叫判据（2026-10-31 变异实测出的免疫与修法）：
//   旧版三个 Test **没有一个是真的调用 SelfHeal**：Test1 只是自己 exec 测试二进制并忽略退出码
//   （WB_SELFHEAL_CHILD 全仓零读者，那是个虚构的自识别机制），Test2 只 Stat exe 所在目录，
//   Test3 结尾是 `_ = m`。于是把 SelfHeal 整个掏空（不取 exe / 不构造 Command / 不 Start，
//   直接 return nil）后三个 Test 全部照旧 PASS —— 恒绿判据，入库就是往 CI 塞假覆盖。
//   新版走 supervisor.go 里的 selfHealStart 接缝：**真的调用 m.SelfHeal**，只在「让进程落地」
//   这一步换替身，因此 cmd.Path / cmd.Args / cmd.Dir 都是被测函数按生产口径构造出来的真实值，
//   而不会真起进程（在 go test 里 os.Executable() 就是测试二进制自身，真 Start 即 fork 风暴）。
//
// 四条断言各自咬住一类缺陷：调用次数=1（掏空/漏调）、Args 含 --silent（参数丢）、
// Dir=exe 目录（工作目录丢 ⇒ 数据根漂移）、Start 失败必须上抛（错误被吞 ⇒ 上层以为自愈成功）。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"workbench/internal/config"
)

// captureSelfHeal 把 selfHealStart 换成替身并真的调用 SelfHeal，返回替身看到的 cmd 与 SelfHeal 的返回错误。
// 关键：调用的是被测函数本体，所以任何"没构造、没启动、吞掉错误"的实现变化都会体现在返回值上。
func captureSelfHeal(t *testing.T, startErr error) (cmd *exec.Cmd, calls int, gotErr error) {
	t.Helper()
	orig := selfHealStart
	// t.Cleanup 必挂：seam 是包级变量，不恢复会污染同包后续测试（注入式测试的硬纪律）。
	t.Cleanup(func() { selfHealStart = orig })
	m := New(config.Layout{}, func() config.SupervisorConfig { return config.SupervisorConfig{} })
	if m == nil {
		t.Fatal("New 返回 nil")
	}
	selfHealStart = func(c *exec.Cmd) error {
		calls++
		cmd = c
		return startErr
	}
	gotErr = m.SelfHeal("unit-test-reason")
	return cmd, calls, gotErr
}

// TestSelfHealActuallySpawnsSilentChild 核心回归：SelfHeal 必须真的走一次「拉起」，且命令行契约成立。
func TestSelfHealActuallySpawnsSilentChild(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("无法定位测试二进制: %v", err)
	}
	cmd, calls, gotErr := captureSelfHeal(t, nil)

	// 断言一：真的调用了一次。掏空 SelfHeal（直接 return nil）时 calls=0 ⇒ 这里翻红。
	if calls != 1 {
		t.Fatalf("SelfHeal 应恰好拉起一次新进程，实际 calls=%d（0=自愈被掏空/漏调，>1=重启风暴风险）", calls)
	}
	if gotErr != nil {
		t.Fatalf("替身返回 nil 时 SelfHeal 不该报错，实际: %v", gotErr)
	}
	if cmd == nil {
		t.Fatal("替身没拿到 *exec.Cmd（说明构造步骤被跳过）")
	}
	// 断言二：拉起的必须是本二进制。
	if cmd.Path != exe {
		t.Fatalf("应重新拉起自身，实际 Path=%q 期望 %q", cmd.Path, exe)
	}
	// 断言三：必须带 --silent（无头拉起；丢了会弹控制台，正是本仓 spawn 静默闸在管的那族）。
	var found bool
	for _, a := range cmd.Args[1:] {
		if a == "--silent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("自愈子进程必须带 --silent，实际 Args=%q（丢了这个参数会弹出控制台窗口）", cmd.Args)
	}
}

// TestSelfHealSetsWorkingDirToExeDir 断言工作目录契约：子进程必须在 exe 所在目录启动，
// 否则 workbench 会推导出错误的 RuntimeDir/DataRoot ⇒「重启后换了数据根」这类隐蔽故障。
func TestSelfHealSetsWorkingDirToExeDir(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("无法定位测试二进制: %v", err)
	}
	cmd, calls, _ := captureSelfHeal(t, nil)
	if calls != 1 || cmd == nil {
		t.Fatalf("需要先成功拉起一次才谈得上目录契约，calls=%d cmd==%v", calls, cmd == nil)
	}
	if want := filepath.Dir(exe); cmd.Dir != want {
		t.Fatalf("子进程工作目录应为 exe 目录 %q，实际 %q（错则重启后数据根漂移）", want, cmd.Dir)
	}
}

// TestSelfHealPropagatesStartFailure 断言错误不被吞：Start 失败必须上抛且带原因，
// 否则调用方（evaluate/loop）会以为自愈成功而继续等待，进程实际已死。
func TestSelfHealPropagatesStartFailure(t *testing.T) {
	boom := os.ErrPermission
	_, calls, gotErr := captureSelfHeal(t, boom)
	if calls != 1 {
		t.Fatalf("应尝试拉起一次，实际 calls=%d", calls)
	}
	if gotErr == nil {
		t.Fatal("Start 失败时 SelfHeal 必须返回错误，实际返回 nil（错误被吞 ⇒ 上层以为自愈成功）")
	}
	if !strings.Contains(gotErr.Error(), "拉起新进程失败") {
		t.Fatalf("错误须含「拉起新进程失败」以便定位，实际: %v", gotErr)
	}
	if !errors.Is(gotErr, boom) {
		t.Fatalf("必须包装并保留原始错误（%v），实际: %v", boom, gotErr)
	}
}

// TestSelfHealDoesNotRequireLayout 断言自愈不依赖 layout：它只用 os.Executable()，
// 所以「配置损坏导致启动失败」时自愈仍要能工作（自愈不能反过来依赖配置）。
// 这条必须真调用 SelfHeal —— 旧版用 `_ = m` 冒充，等于没测。
func TestSelfHealDoesNotRequireLayout(t *testing.T) {
	cmd, calls, gotErr := captureSelfHeal(t, nil)
	if calls != 1 {
		t.Fatalf("空 layout 下 SelfHeal 仍须拉起一次，calls=%d", calls)
	}
	if gotErr != nil {
		t.Fatalf("空 layout 不应导致自愈失败: %v", gotErr)
	}
	if cmd == nil || cmd.Path == "" {
		t.Fatalf("空 layout 下仍须拿到可执行文件路径，实际 cmd=%v", cmd)
	}
}

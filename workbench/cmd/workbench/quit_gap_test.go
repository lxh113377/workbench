package main

// quit_gap_test.go 守护「存在程序化的整体退出入口」（2026-10-19 现场）。
//
// 现场：老大要 7x24，我为了换二进制必须让运行中的实例真正退出。
// 实测发现产品**没有任何程序化的退出入口**：
//   - tray 菜单「退出工作台」是 WM_COMMAND 1006，需要 UI 自动化；
//   - signal.Notify(os.Interrupt/SIGTERM) 对无控制台的 GUI 进程收不到；
//   - --stop 只停「引擎」，不停「进程」—— 这是它的正确语义，但不够用。
// => 只能强杀进程。这违反「零人工干预」：换二进制/运维重启都成了人工动作。
//
// 正确做法：加一个 --quit 子命令，走与 main() 里 signal 处理**同一条**优雅路径
//   （eng.Stop -> srv.Close -> 退出），让运维与自动化都能无人工地让实例停下。
//
// 判据：dispatch 认识 --quit；且该分支存在（不回落成 GUI 启动）。

import (
	"os"
	"strings"
	"testing"
)

// TestQuitSubcommandIsDispatched 断言 main.go 的 dispatch 里有 --quit 分支。
//
// 用源码契约而不是真跑：真跑 --quit 会把测试进程的父进程弄停，
// 且会抢单实例锁。dispatch 的存在性用文本断言最稳（与 main_dispatch_test.go 同法）。
func TestQuitSubcommandIsDispatched(t *testing.T) {
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `case "--quit"`) {
		t.Fatal("main.go 必须有 --quit 分支：产品缺程序化退出入口，7x24 运维只能强杀进程")
	}
	if !strings.Contains(src, "runQuitCLI") {
		t.Fatal("--quit 分支必须调用 runQuitCLI（而不是回落成 GUI 启动）")
	}
}

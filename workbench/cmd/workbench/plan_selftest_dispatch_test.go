package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// --plan-selftest 的分发契约测试。
//
// 为什么需要（照抄 --ui-probe 事故的教训）：
//   2026-10-19 实测，`case "--ui-probe"` 那一格在并发改写 main.go 时整段消失，
//   源码能编译、测试全绿，但二进制跑该参数会静默回落成 GUI 启动。
//   双角色闭环的自检通道如果也被吞掉，后果更隐蔽：没人会发现"闭环已经没法离线验证"，
//   只会觉得"最近没跑过自检而已"。
//
// 判据：真实二进制跑 --plan-selftest 必须打印 PASS 文案并退出 0；
// 不得出现 "工作台已在运行"（那是回落成 GUI 的标志）。
func TestMainDispatchKeepsPlanSelftestFlag(t *testing.T) {
	if testing.Short() {
		t.Skip("需要构建真实二进制")
	}
	exe := filepath.Join(t.TempDir(), "wb-plan.exe")
	build := hiddenTestCmd("go", "build", "-o", exe, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("构建受测二进制失败: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	cmd := hiddenTestCmd(exe, "--plan-selftest")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		t.Fatalf("--plan-selftest 应退出 0，实际 %v\n%s", err, strings.TrimSpace(text))
	}
	if strings.Contains(text, "工作台已在运行") {
		t.Fatalf("--plan-selftest 回落成 GUI 启动（分发表缺这一格）：%s", strings.TrimSpace(text))
	}
	if !strings.Contains(text, "双角色闭环自检 PASS") {
		t.Fatalf("--plan-selftest 未进入自检通道（期望 PASS 文案）：%s", strings.TrimSpace(text))
	}
	// 逐条判据都要在输出里（不能只打印一句 PASS 就了事）。
	for _, want := range []string{
		"方案阶段先于执行阶段",
		"第 2 次调用拿到执行指令",
		"执行指令携带方案声明的验收命令",
		"已进入重新规划",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("自检输出缺少判据 %q：%s", want, strings.TrimSpace(text))
		}
	}

	// 自检不得留下临时目录（它自己负责清理）。
	if strings.Contains(text, "[FAIL]") {
		t.Fatalf("自检内部有失败判据：%s", strings.TrimSpace(text))
	}
}

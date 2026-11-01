package main

import "testing"

// TestParseSilentFlag 钉住 runApp 的启动模式解析（2026-11-01 round 206 首块回归网）。
//
// 为什么先建这块网：runApp 337 行、内部 115 条语句**覆盖率实测 0%**、含 18 return / 9 defer /
// 5 goroutine ⇒ 零覆盖下拆分无法验证行为等价（见 docs/五轴审查-分级-2026-11-01.md 的 P1-1）。
// 按「先建网、后拆墙」的顺序，先把输入输出最明确的一块 —— 命令行参数 → silent 布尔 —— 抽成纯函数。
//
// 语义来源（本函数原注释）：--silent 表达「服务化启动」，--headless 表达「本次运行根本不该出现任何窗口」，
// 两者都置 silent=true；保留两个名字是因为既有计划任务/守护脚本已用 --silent 在跑，改名会静默失效。
func TestParseSilentFlag(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"无参数", nil, false},
		{"--silent", []string{"--silent"}, true},
		{"--headless（--silent 的显式子集）", []string{"--headless"}, true},
		{"两者都给", []string{"--silent", "--headless"}, true},
		{"顺序无关", []string{"--headless", "--silent"}, true},
		{"无关参数不误判", []string{"--diagnose"}, false},
		{"前缀相近但不是（--silently 不得命中）", []string{"--silently"}, false},
		{"大小写敏感（--Silent 不得命中）", []string{"--Silent"}, false},
	}
	for _, c := range cases {
		if got := parseSilentFlag(c.args); got != c.want {
			t.Errorf("%s: parseSilentFlag(%v) = %v，期望 %v", c.name, c.args, got, c.want)
		}
	}
	// 下限守卫：用例少于 8 条说明本判据被删残（同族：空集恒绿）
	if len(cases) < 8 {
		t.Fatalf("用例只有 %d 条，判据受理面退化", len(cases))
	}
	t.Logf("启动模式解析 %d 条用例全部通过（--silent / --headless 双名语义）", len(cases))
}

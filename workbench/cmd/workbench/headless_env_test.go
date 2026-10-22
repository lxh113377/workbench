package main

// headless_env_test.go -- 无头环境判定判据（2026-10-23 补）。
//
// 为什么必须有：headlessEnv() 是「绝不弹窗」的**环境层硬保证**，此前 0% 覆盖。
// 它的失效形态是本仓最贵的现场事故：2026-10-19 老大反复反馈「一直在跳这个，可不可以不要」
// —— 守护进程/压测/CI 用 --silent 撞上既有实例时照样往桌面推 Edge --app 窗口。
// 双层语义（本文件只测环境层）：代码层 shouldOpenPanel(silent) 声明「要不要弹」，
// 环境层 headlessEnv() 声明「准不准弹」，**两者都放行才真的弹**；环境层覆盖代码层。
// 若环境层判错：
//   - 漏判（该 true 却 false）=> CI/计划任务里真的弹窗，把无人值守机器搞挂；
//   - 误判（该 false 却 true）=> 用户点快捷方式没反应（「点了打不开」那条老投诉）。

import "testing"

// TestHeadlessEnvMarkerValues 钉死标记的取值口径（大小写/空白不敏感，四种真值）。
func TestHeadlessEnvMarkerValues(t *testing.T) {
	t.Setenv("CI", "") // 隔离 CI 变量，只测 marker 单独生效
	for _, v := range []string{"1", "true", "TRUE", "True", " 1 ", "yes", "YES", "on", "On", "\ttrue\n"} {
		t.Setenv(headlessEnvMarker, v)
		if !headlessEnv() {
			t.Fatalf("WB_HEADLESS=%q 应判为无头（大小写与空白必须不敏感）", v)
		}
	}
}

// TestHeadlessEnvFalseyValues 钉死反方向：明确关闭值不得判无头。
// 若把 "0"/"false" 也判成无头，用户显式关掉无头却仍然不弹面板 —— 就是「点了没反应」。
func TestHeadlessEnvFalseyValues(t *testing.T) {
	t.Setenv("CI", "")
	for _, v := range []string{"0", "false", "no", "off", "", "   ", "disabled"} {
		t.Setenv(headlessEnvMarker, v)
		if headlessEnv() {
			t.Fatalf("WB_HEADLESS=%q 不应判为无头（显式关闭必须能弹面板）", v)
		}
	}
}

// TestHeadlessEnvCIAnyNonEmptyIsHeadless 钉死 CI 口径：CI 变量**任意非空**即无头。
// 为什么不是只认 CI=true：各 CI 平台取值不一（true/1/yes/构建号）。
// 保守地「有就不弹」是对的 —— CI 上弹窗毫无意义且会把流水线挂死。
func TestHeadlessEnvCIAnyNonEmptyIsHeadless(t *testing.T) {
	t.Setenv(headlessEnvMarker, "") // marker 关闭，只靠 CI 触发
	for _, v := range []string{"true", "1", "yes", "build-1234", "github", "ANY"} {
		t.Setenv("CI", v)
		if !headlessEnv() {
			t.Fatalf("CI=%q（非空）应判为无头", v)
		}
	}
}

// TestHeadlessEnvCIEmptyDoesNotTrigger 钉死反向：CI 为空/纯空白不得判无头。
// 少了这条，一份 "CI= "（带空格的空值）会让本地用户永远看不到面板。
func TestHeadlessEnvCIEmptyDoesNotTrigger(t *testing.T) {
	t.Setenv(headlessEnvMarker, "")
	for _, v := range []string{"", "   ", "\t"} {
		t.Setenv("CI", v)
		if headlessEnv() {
			t.Fatalf("CI=%q 为空值，不应判为无头", v)
		}
	}
}

// TestHeadlessEnvMarkerBeatsEmptyCI 钉死两源的独立性：marker 与 CI 是「任一成立」，
// 不是「必须都成立」。marker 开着而 CI 为空时，仍然必须无头。
func TestHeadlessEnvMarkerBeatsEmptyCI(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(headlessEnvMarker, "1")
	if !headlessEnv() {
		t.Fatal("WB_HEADLESS=1 单独成立时必须无头（两源是「任一」不是「都」）")
	}
}

// TestHeadlessEnvBothOffIsFalse 钉死默认：两源都关时**允许**弹面板。
// 这是用户双击快捷方式的正常路径 —— 若这里判 true，快捷方式永远打不开界面。
func TestHeadlessEnvBothOffIsFalse(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv(headlessEnvMarker, "")
	if headlessEnv() {
		t.Fatal("两源都关时必须允许弹面板，否则用户点快捷方式「点了打不开」")
	}
}

// TestSelfCheckWordIsHonest 钉死自检措辞：true => 全绿 / false => 有问题。
// 这条看着琐碎，但它是总验收报告里唯一把 bool 翻成人话的地方；
// 翻错的后果是「有问题」被显示成「全绿」—— 报告虚报的直接入口。
func TestSelfCheckWordIsHonest(t *testing.T) {
	if got := selfCheckWord(true); got != "全绿" {
		t.Fatalf("true 应翻成「全绿」，got %q", got)
	}
	if got := selfCheckWord(false); got != "有问题" {
		t.Fatalf("false 应翻成「有问题」（不得虚报为全绿），got %q", got)
	}
}

// TestKeysOfReturnsAllKeys 钉死 keysOf 完整性与空 map 安全。
// 用途：把 set 形式的判据映射转成可排序切片做报告；漏键 = 报告漏项。
func TestKeysOfReturnsAllKeys(t *testing.T) {
	m := map[string]bool{"a": true, "b": false, "c": true}
	got := keysOf(m)
	if len(got) != len(m) {
		t.Fatalf("keysOf 必须返回全部键（漏键 = 报告漏项）：got %v", got)
	}
	seen := map[string]bool{}
	for _, k := range got {
		if _, ok := m[k]; !ok {
			t.Fatalf("keysOf 返回了不存在的键 %q", k)
		}
		seen[k] = true
	}
	for k := range m {
		if !seen[k] {
			t.Fatalf("keysOf 漏了键 %q", k)
		}
	}
	// 空 map：必须返回空切片而不是 nil panic
	if out := keysOf(map[string]bool{}); out == nil {
		t.Log("keysOf(空) 返回 nil —— 可接受（len 为 0），但下游若 len() 之外还用 range 需注意")
	}
}

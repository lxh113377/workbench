package dshprofile

// profile_patch_tooling_test.go 守护「无人值守 profile 不得暴露计划模式工具」。
//
// 现场缺陷（2026-10-21，与「运行中但产出 0」同轮排查发现）：
//   dist/workbench-output/tasks/<id>/events.jsonl 里，15 分钟内 4 个任务反复出现
//   `tool_call exit_plan_mode` → `tool_result error: exit_plan_mode is only available in plan mode`
//   的成对记录（同任务内 4~8 次），两次调用之间只隔一条 thinking，
//   然后 step_start 再 step_start，始终没有最终答案 => 轮次耗尽 => bad_exit。
//
// 根因：dsh-plan-mode 刻意让 exit_plan_mode 在**非**计划模式下也保持注册
// （为请求 cache 稳定），而它的规则注入段（section）只在计划模式激活时才进 prompt。
// 于是无人值守任务看到的是一个"没有任何使用约束、却挂在工具表里"的出口工具，
// 免费车道的小模型会把它当成"提交成果/确认执行"的通用出口反复调用。
//
// 判据（本文件）：profilePatch 生成的用户层补丁必须把 plan-mode 整块禁掉，
// 且必须同时保留 tools.only: [] —— 后者是"一步直接出文本"的底座，不能因为改这里被带跑。

import (
	"strings"
	"testing"
)

// TestProfilePatchDisablesPlanMode 断言 profile 补丁禁用 plan-mode。
func TestProfilePatchDisablesPlanMode(t *testing.T) {
	p := profilePatch()
	if !strings.Contains(p, "- id: plan-mode") {
		t.Fatal("profile 补丁必须禁用 plan-mode（否则 exit_plan_mode 会留在工具表里被小模型乱调）")
	}
	// 必须紧跟 disabled: true，而不是被写成别的 config。
	idx := strings.Index(p, "- id: plan-mode")
	tail := p[idx:]
	if end := strings.Index(tail, "- id: "); end > 0 {
		tail = tail[:end]
	}
	if !strings.Contains(tail, "disabled: true") {
		t.Fatalf("plan-mode 必须被 disabled: true 关掉，实际块内容：%q", tail)
	}
}

// TestProfilePatchKeepsNativeToolIsolation 断言"一步出文本"的底座没被带跑。
func TestProfilePatchKeepsNativeToolIsolation(t *testing.T) {
	p := profilePatch()
	if !strings.Contains(p, "- id: tools") || !strings.Contains(p, "only: []") {
		t.Fatal("tools.only: [] 必须保留（无人值守靠'一步直接输出成果文本'稳定产出）")
	}
	// 文件/命令类工具必须继续禁用：它们会让模型在 step 之间等待，而 sealed 车道第二步会卡死。
	for _, must := range []string{"tool-fs", "tool-pwsh", "tool-bash", "tool-web"} {
		if !strings.Contains(p, "- id: "+must) {
			t.Fatalf("%s 必须继续禁用", must)
		}
	}
	if !strings.Contains(p, "policy: never") {
		t.Fatal("无人值守必须保持 approval.policy = never")
	}
}

// TestProfilePatchHasNoDuplicateIDs 断言补丁里没有重复 id。
//
// 为什么重要：cordis patch 按 id 逐条应用，同一 id 出现两次时后者覆盖前者 ——
// 若一个写 disabled: true、另一个写 config，最终语义就取决于顺序，极难排查。
func TestProfilePatchHasNoDuplicateIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, line := range strings.Split(profilePatch(), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- id: ") {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(trimmed, "- id: "))
		if seen[id] {
			t.Fatalf("补丁里 id %q 重复出现（后者会静默覆盖前者）", id)
		}
		seen[id] = true
	}
	if len(seen) < 10 {
		t.Fatalf("补丁条目过少（%d），疑似模板被截断", len(seen))
	}
}

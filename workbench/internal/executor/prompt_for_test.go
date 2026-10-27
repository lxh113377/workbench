package executor

import (
	"strings"
	"testing"

	"workbench/internal/artifact"
	"workbench/internal/model"
)

// TestPromptForSkipsProtocolInPlanPhase 钉住「方案阶段不得拼产物协议」（round 43 · T24）。
//
// 由来（独立审计的铁证）：协议被**无条件**拼进方案调用，而方案提示词第一句是「只输出一个 JSON 对象」——
// 实测方案 session 里两句同框，33 条方案尝试 30 条 degraded。
func TestPromptForSkipsProtocolInPlanPhase(t *testing.T) {
	plan := &model.Task{Prompt: "只输出一个 JSON 对象", Phase: model.PhasePlan}
	p, required := promptFor(plan, true)
	if required {
		t.Fatal("方案阶段不得回报「要求产物」（否则 failover 会把方案按无产物块判失败）")
	}
	if strings.Contains(p, "<<<ARTIFACTS>>>") || strings.Contains(p, artifact.ProtocolInstruction) {
		t.Fatalf("方案提示词里出现产物协议：%q", p)
	}
	if p != plan.Prompt {
		t.Fatalf("方案提示词应原样透传，实际 %q", p)
	}

	exec := &model.Task{Prompt: "写个脚本"}
	pe, re := promptFor(exec, true)
	if !re {
		t.Fatal("执行阶段应回报「要求产物」")
	}
	if !strings.Contains(pe, artifact.ProtocolInstruction) {
		t.Fatal("执行阶段提示词应含完整产物协议")
	}
	if !strings.HasPrefix(pe, exec.Prompt+"\n") {
		t.Fatal("执行阶段提示词应 = 原 prompt + 换行 + 协议")
	}

	// 开关关闭时两个阶段都不拼协议。
	if p2, r2 := promptFor(exec, false); r2 || strings.Contains(p2, "<<<ARTIFACTS>>>") {
		t.Fatalf("AppendProtocol=false 时不得拼协议：required=%v prompt=%q", r2, p2)
	}
	// nil 任务安全（引擎在极端路径上可能传 nil）。
	if _, r3 := promptFor(nil, true); r3 {
		t.Fatal("nil 任务不得回报要求产物")
	}
}

package engine

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestCompletionVerdict 钉住「无产物块不得记完成品」（round 46 · T28）。
//
// 由来（审计实锤）：`materializeArtifacts` 只在回答里**有产物块**时才返回报告，
// 于是 `verifyRep == nil` = 「模型压根没按协议交东西」；修前这种情况直接落 done（verified=false），
// 造成「0 个 artifacts-index / 0 个 workspace，却有 done 记录」的虚报完成。
func TestCompletionVerdict(t *testing.T) {
	st, note := completionVerdict(nil)
	if st != model.StateDegraded {
		t.Fatalf("无产物块必须落 degraded，实际 %s", st)
	}
	if !strings.Contains(note, "无产物块") {
		t.Fatalf("理由必须写明「无产物块」以便与「交了没过」区分，实际 %q", note)
	}

	if st, _ := completionVerdict(&model.VerifyReport{Pass: false, FailReason: "产出过短"}); st != model.StateDegraded {
		t.Fatalf("验收未过应落 degraded，实际 %s", st)
	}
	if st, note := completionVerdict(&model.VerifyReport{Pass: true}); st != model.StateDone || note == "" {
		t.Fatalf("验收通过应落 done 且有说明，实际 %s/%q", st, note)
	}
}

// TestShouldRepairAllowsMissingArtifact 钉住「无产物块也要能返工要回产物块」（round 46 · T28 的关键路径）。
//
// 为什么这条最重要：产物块**不存在**时如果跳过返工，任务只会降级、永远不会拿到产物；
// 而返工是唯一能把它拉回「按协议重交」的路径（弱车道最常见形态就是只吐自家 tool-call 语法）。
func TestShouldRepairAllowsMissingArtifact(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Executor.RepairRounds = 3
	task := &model.Task{ID: "t1", RepairRounds: 0}
	if !shouldRepair(task, nil, cfg) {
		t.Fatal("无产物块且还有返工轮数时必须允许返工")
	}
	// 返工轮用完 ⇒ 不再返工（既有语义不得被改坏）。
	task.RepairRounds = 3
	if shouldRepair(task, nil, cfg) {
		t.Fatal("返工轮用完时不得再返工")
	}
	// 验收通过 ⇒ 不返工。
	if shouldRepair(&model.Task{RepairRounds: 0}, &model.VerifyReport{Pass: true}, cfg) {
		t.Fatal("验收通过不得返工")
	}
}

// TestRepairPromptForMissingArtifact 钉住返工提示词对「没交产物」的措辞（round 46 · T28）。
func TestRepairPromptForMissingArtifact(t *testing.T) {
	p := repairPrompt(&model.Task{ID: "t1", Prompt: "写个脚本"}, nil, nil, 1)
	for _, want := range []string{"不是验收没过", "根本没有产物块", "<<<ARTIFACTS>>>", "不要输出你自己的工具调用语法"} {
		if !strings.Contains(p, want) {
			t.Errorf("无产物块的返工提示词缺少要素 %q", want)
		}
	}
}

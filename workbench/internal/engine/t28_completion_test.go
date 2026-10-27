package engine

import (
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestArtifactRequired 钉住「哪些域要求产物块」的收窄口径（round 46 · T28）。
//
// 由来：第一版把「无产物块」一刀切成未完成 ⇒ 撞掉 D7 契约（内容域 prose 型成功必须仍跑语义后处理，
// `TestPostProcessUsesInjectedRunner` 当场判红）。收窄依据：工程/质量类域的交付物是**文件**，
// 内容/报告类域的交付物是**正文本身**。
func TestArtifactRequired(t *testing.T) {
	// 默认集（不传配置）：工程/质量类域要求产物，内容/报告类域不要求。
	for _, d := range []string{"code", "quality", "frontend"} {
		if !artifactRequired(d, nil) {
			t.Errorf("域 %s 属于默认产物集，应要求产物", d)
		}
	}
	for _, d := range []string{"content", "knowledge", "report", "solution", "data", ""} {
		if artifactRequired(d, nil) {
			t.Errorf("域 %q 不在默认产物集，不应要求产物（正文即交付）", d)
		}
	}
	// 大小写与空白容错（配置由人手写，不该因为多一个空格就失效）。
	if !artifactRequired(" Code ", nil) {
		t.Error("域匹配应大小写不敏感并容忍空白")
	}
	// 显式配置覆盖默认集：加 data、去掉 quality。
	if !artifactRequired("data", []string{"code", "data"}) {
		t.Error("显式配置里有的域应要求产物")
	}
	if artifactRequired("quality", []string{"code", "data"}) {
		t.Error("显式配置里没有的域不应要求产物（覆盖默认集）")
	}
}

// TestCompletionVerdict 钉住「无产物块不得记完成品」及其收窄边界（round 46 · T28）。
func TestCompletionVerdict(t *testing.T) {
	// 要求产物的域 + 无产物块 ⇒ degraded，且理由写明「无产物块」（与「交了没过」区分）。
	st, note := completionVerdict(nil, true)
	if st != model.StateDegraded {
		t.Fatalf("要求产物的域：无产物块必须落 degraded，实际 %s", st)
	}
	if !strings.Contains(note, "无产物块") {
		t.Fatalf("理由必须写明「无产物块」，实际 %q", note)
	}
	// 不要求产物的域（内容/报告类）+ 无产物块 ⇒ done（正文即交付，D7 契约）。
	if st, _ := completionVerdict(nil, false); st != model.StateDone {
		t.Fatalf("内容类域：无产物块应仍落 done（正文即交付），实际 %s", st)
	}
	// 有报告但要产物的域：pass=false ⇒ degraded；pass=true ⇒ done。
	if st, _ := completionVerdict(&model.VerifyReport{Pass: false, FailReason: "产出过短"}, true); st != model.StateDegraded {
		t.Fatalf("验收未过应落 degraded，实际 %s", st)
	}
	if st, note := completionVerdict(&model.VerifyReport{Pass: true}, true); st != model.StateDone || note == "" {
		t.Fatalf("验收通过应落 done 且有说明，实际 %s/%q", st, note)
	}
}

// TestShouldRepairAllowsMissingArtifact 钉住「无产物块也要能返工要回产物块」（T28 的关键路径）。
//
// 为什么这条最重要：产物块**不存在**时如果跳过返工，工程域任务只会降级、永远拿不到产物；
// 而返工是唯一能把它拉回「按协议重交」的路径（弱车道最常见形态就是只吐自家 tool-call 语法）。
func TestShouldRepairAllowsMissingArtifact(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Executor.RepairRounds = 3
	task := &model.Task{ID: "t1"}
	if !shouldRepair(task, nil, cfg) {
		t.Fatal("无产物块且还有返工轮数时必须允许返工")
	}
	task.RepairRounds = 3
	if shouldRepair(task, nil, cfg) {
		t.Fatal("返工轮用完时不得再返工")
	}
	if shouldRepair(&model.Task{}, &model.VerifyReport{Pass: true}, cfg) {
		t.Fatal("验收通过不得返工")
	}
}

// TestRepairPromptForMissingArtifact 钉住返工提示词对「没交产物」的措辞（T28）。
func TestRepairPromptForMissingArtifact(t *testing.T) {
	p := repairPrompt(&model.Task{ID: "t1", Prompt: "写个脚本"}, nil, nil, 1)
	for _, want := range []string{"不是验收没过", "根本没有产物块", "<<<ARTIFACTS>>>", "不要输出你自己的工具调用语法"} {
		if !strings.Contains(p, want) {
			t.Errorf("无产物块的返工提示词缺少要素 %q", want)
		}
	}
}

// TestTemplateArtifactExemption 钉住「按模板豁免产物块」的判定顺序与容错（round 48 · T37）。
//
// 由来：域是粗粒度代理 —— code-review / code-snippet-library / code-test-plan 与 quality 审查报告类
// 的交付物就是正文/报告本身，却被 code/quality 默认工程域强制要产物块，无产物块即判未完成、白走返工。
func TestTemplateArtifactExemption(t *testing.T) {
	// ① 豁免模板 + 工程域 ⇒ 不要求产物块 ⇒ 无产物块仍判 done。
	if st, _ := completionVerdict(nil, requiresArtifactBlock("code", "code-review", nil, []string{"code-review"})); st != model.StateDone {
		t.Fatalf("豁免模板（域 code）：无产物块应仍落 done，实际 %s", st)
	}
	// ② 同域非豁免模板 ⇒ 仍要求产物块 ⇒ 无产物块判 degraded（豁免不得外溢到同域别的模板）。
	if st, _ := completionVerdict(nil, requiresArtifactBlock("code", "code-tool", nil, []string{"code-review"})); st != model.StateDegraded {
		t.Fatalf("非豁免同域模板：无产物块应落 degraded，实际 %s", st)
	}
	// ③ 豁免列表为空 ⇒ **用默认豁免集**（round 50 · T41）：存量 config.json 没有该键（实测本机 schema 6），
	//    若把空值当「无豁免」，T37 这条修正对老安装**永不生效** ⇒ 必须回落到 `defaultArtifactExemptTemplates`。
	if requiresArtifactBlock("quality", "review-five-axis", nil, nil) {
		t.Fatal("空豁免列表必须回落默认豁免集（否则存量安装拿不到 T37 修正）")
	}
	if requiresArtifactBlock("report", "report-weekly", nil, nil) {
		t.Fatal("report 域本就不要求产物块")
	}
	// ③b 默认豁免集**只**覆盖列出的模板：同域未列出的模板仍要求产物（豁免不得外溢）。
	if !requiresArtifactBlock("code", "code-tool", nil, nil) {
		t.Fatal("未列出的模板不得被空列表默认豁免")
	}
	// ④ 大小写/空白容错（配置/模板 id 由人手写）。
	if requiresArtifactBlock(" Code ", "  Code-Review ", nil, []string{"code-review"}) {
		t.Error("模板 id 匹配应大小写不敏感并容忍空白")
	}
	// ⑤ 默认配置预置了 report 型模板：域 code/quality 都在工程集里，仍须判定为「不要求产物」。
	cfg := config.DefaultConfig()
	for _, id := range []string{"code-review", "code-snippet-library", "code-test-plan", "quality-audit", "review-five-axis"} {
		if requiresArtifactBlock("code", id, cfg.Executor.ArtifactDomains, cfg.Executor.ArtifactExemptTemplates) {
			t.Errorf("模板 %s 应在默认豁免集里（域 code 也不得强制产物块）", id)
		}
	}
}

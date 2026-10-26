package modelpool

import (
	"testing"

	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestRequiresArtifacts 钉住「要不要产物块」的判据：只看提示词里有没有协议块。
func TestRequiresArtifacts(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"执行阶段（带协议块）", "请产出文件\n<<<ARTIFACTS>>>\n```manifest", true},
		{"方案阶段（只输出 JSON）", "只输出一个 JSON 对象，不要任何解释文字", false},
		{"空提示词", "", false},
	}
	for _, c := range cases {
		if got := RequiresArtifacts(c.prompt); got != c.want {
			t.Errorf("%s: RequiresArtifacts = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestJudgeArtifactRequirement 钉住「要求产物时，无产物块不算成功」这条 failover 判据（round 21 · T18）。
//
// 实测由来：某车道 kind=ok 但最终文本是过程独白（含它自己的 <dots_function_call> 协议），
// 引擎闸门随后判未完成；修前 failover 已把它当成功返回，后面的产物门成了事后诸葛。
func TestJudgeArtifactRequirement(t *testing.T) {
	prose := &executor.Result{FinalText: "我先扫描工作目录，了解项目结构。"}
	withBlock := &executor.Result{FinalText: "说明\n<<<ARTIFACTS>>>\n```manifest", ArtifactBlock: true}

	// 执行阶段（要求产物）：无产物块 ⇒ 不算成功，kind=no_artifact
	ok, kind, detail := judge(prose, nil, true)
	if ok || kind != noArtifactKind || detail == "" {
		t.Fatalf("要求产物时无产物块应判失败，实得 ok=%v kind=%q detail=%q", ok, kind, detail)
	}
	// 带产物块 ⇒ 成功
	if ok, _, _ := judge(withBlock, nil, true); !ok {
		t.Fatal("带产物块应判成功")
	}
	// 方案阶段（不要求产物）：只吐字也算成功（否则方案阶段会被误杀）
	if ok, _, _ := judge(prose, nil, false); !ok {
		t.Fatal("不要求产物时，有最终答案应判成功")
	}
	// 只吐字但没产物、且 ArtifactBlock=false ⇒ 判 no_artifact（与实际执行器口径一致）
	if ok, kind, _ := judge(&executor.Result{FinalText: "有字没块"}, nil, true); ok || kind != noArtifactKind {
		t.Fatalf("无 ArtifactBlock 标记应判 no_artifact，实得 ok=%v kind=%q", ok, kind)
	}

	// 空最终答案仍走 bad_exit，不被协议判据覆盖
	if ok, kind, _ := judge(&executor.Result{}, nil, true); ok || kind != string(executor.KindExit) {
		t.Fatalf("空最终答案应为 bad_exit，实得 ok=%v kind=%q", ok, kind)
	}
}

// TestModelAttributableNoArtifact 钉住「协议不合规可归因到车道」。
// TestRequireArtifactsFor 钉住两条判据的组合口径（执行器上报优先，提示词自描述兜底）。
//
// 由来（round 21 实测）：协议由**执行器**拼进 prompt，`task.Prompt` 里没有 `<<<ARTIFACTS>>>`，
// 只按提示词判时 `no_artifact` 一次都没触发（count=0）—— 所以执行器上报必须是第一判据。
// TestNoArtifactKindSharesExecutorConstant 钉住「类别字符串只有一份来源」。
//
// failover 的产物判据用的 noArtifactKind 必须等于 executor 的类别常量，否则两处各写一遍必然漂移
// （round 22 的教训：字符串对不上时，台账/日志里就会出现「判据生效但查不到原因」）。
func TestNoArtifactKindSharesExecutorConstant(t *testing.T) {
	if noArtifactKind != string(executor.KindNoArtifact) {
		t.Fatalf("noArtifactKind=%q 必须等于 executor.KindNoArtifact=%q", noArtifactKind, executor.KindNoArtifact)
	}
	if noArtifactKind != "no_artifact" {
		t.Fatalf("类别字符串是对外契约，不得改名（实得 %q）", noArtifactKind)
	}
}

func TestRequireArtifactsFor(t *testing.T) {
	withFlag := &executor.Result{}
	withFlag.ArtifactsRequired.Store(true)
	if !requireArtifactsFor(withFlag, nil) {
		t.Fatal("执行器上报要求产物时应判 true")
	}
	if !requireArtifactsFor(&executor.Result{}, &model.Task{Prompt: "…<<<ARTIFACTS>>>…"}) {
		t.Fatal("提示词自描述兜底应判 true")
	}
	if requireArtifactsFor(&executor.Result{}, &model.Task{Prompt: "只输出一个 JSON 对象"}) {
		t.Fatal("两条判据都不成立时应判 false")
	}
	if requireArtifactsFor(nil, nil) {
		t.Fatal("nil 结果与 nil 任务应判 false")
	}
	// 方案阶段豁免（round 24 实测回归）：执行器对**所有**阶段都拼产物协议，因此它上报的 ArtifactsRequired
	// 在方案阶段也是 true；若不以阶段判据豁免，方案阶段返回的合法 JSON 会被判成 no_artifact。
	if requireArtifactsFor(withFlag, &model.Task{Phase: model.PhasePlan}) {
		t.Fatal("方案阶段不得要求产物块（合法 JSON 曾被误判 no_artifact）")
	}
	if requireArtifactsFor(&executor.Result{}, &model.Task{Phase: model.PhasePlan, Prompt: "…<<<ARTIFACTS>>>…"}) {
		t.Fatal("方案阶段即便提示词里出现协议文本也不得要求产物块")
	}
	// 执行阶段（Phase 为空）仍要求产物块：回归修复不得把正判据一起豁免掉。
	if !requireArtifactsFor(withFlag, &model.Task{}) {
		t.Fatal("执行阶段应要求产物块")
	}
	if !requireArtifactsFor(&executor.Result{}, &model.Task{Prompt: "…<<<ARTIFACTS>>>…"}) {
		t.Fatal("执行阶段提示词自描述兜底应判 true")
	}
}
func TestModelAttributableNoArtifact(t *testing.T) {
	if !modelAttributable(noArtifactKind) {
		t.Fatal("协议不合规应归因到车道（否则永远排在前列、任务交不出成品）")
	}
	if !modelAttributable(string(executor.KindStalled)) {
		t.Fatal("既有口径（stalled 可归因）不得被改坏")
	}
}

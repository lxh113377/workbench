package modelpool

import (
	"testing"

	"workbench/internal/executor"
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
func TestModelAttributableNoArtifact(t *testing.T) {
	if !modelAttributable(noArtifactKind) {
		t.Fatal("协议不合规应归因到车道（否则永远排在前列、任务交不出成品）")
	}
	if !modelAttributable(string(executor.KindStalled)) {
		t.Fatal("既有口径（stalled 可归因）不得被改坏")
	}
}

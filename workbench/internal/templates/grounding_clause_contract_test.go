package templates

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/model"
)

// groundingClauseKey 是反虚构条款正文的起始子串（用于源文本守卫，见文件末）。
const groundingClauseKey = "先实测再引用"

// TestMdTemplatesCarryGroundingClause 钉住「每条内置 md 模板渲染后的 prompt 都带反虚构条款」，
// 且正文与 model 里的单点逐字相同 —— 判运行时真值（Defaults() 出来的字符串），不读常量名猜。
// 沿用 TestMdTemplatesCarryPlacementClause 的方法学（round 166 已证明这套能钉住单点 + 存在性）。
//
// 为什么必须有这条判据（2026-11-01 round 184 实测）：自主任务连出三份「格式全对、内容全假」的报告：
//
//	引用不存在的 package.json / docker-compose.yml / .env.example 并据其「不存在」提整改、
//	给 Go+PowerShell 仓推荐 npm 生态的 madge/depcruise、声称「没有 tests/」而该目录实际有 77 个在册门禁；
//	用真实的 reportGateProblems 跑它们，两份 0 problems —— 现有门禁只判形状（三节/file:line/分级），
//	模型照形状编一个不存在的路径就能过。格式判据本身在鼓励虚构，本条约束的是内容来源。
func TestMdTemplatesCarryGroundingClause(t *testing.T) {
	checked := 0
	for _, tp := range Defaults() {
		if tp.OutputSpec.Format != "md" {
			continue
		}
		checked++
		if !strings.Contains(tp.PromptTemplate, model.GateEvidenceGroundingClause) {
			t.Errorf("模板 %s 的渲染 prompt 缺反虚构条款（或正文与 model 单点不一致）", tp.ID)
		}
	}
	if checked < 20 {
		t.Fatalf("只查到 %d 条内置 md 模板 ⇒ 受理面退化，本判据此刻在比空气", checked)
	}
	t.Logf("%d 条内置 md 模板的渲染 prompt 均含反虚构条款（与 model 单点逐字一致）", checked)
}

// TestGroundingClauseIsNotSlogan 查单点正文自己：必须含可判要素，不能只是一句「不要虚构」的口号。
// 要素来自本轮实测的三种真实失效形态（见上），不是随手举的例子。
//
// 为什么要素查 model 而不逐模板查：中文 UTF-8 一字符 3 字节，按字节切窗口判「条款片段含词」
// 会把词从中间截断（实测 400 字节只覆盖约 133 个汉字，够不到条款第二句的示例 ⇒ 判据假红）。
// 每个模板 Contains 的是同一个完整字符串常量，单点里有 ⇒ 每条里都有，不必逐模板开窗。
func TestGroundingClauseIsNotSlogan(t *testing.T) {
	for _, must := range []string{"技术栈", "package.json", "tests/", "先实测再引用"} {
		if !strings.Contains(model.GateEvidenceGroundingClause, must) {
			t.Errorf("反虚构条款缺要素 %q ⇒ 退化成口号，模型无从判断什么算虚构", must)
		}
	}
	t.Logf("条款含全部可判要素（%d 字节）", len(model.GateEvidenceGroundingClause))
}

// TestGroundingClauseStaysSinglePoint 防架构回退：把正文复制回 defaults.go。
// 运行时 Contains 判据挡不住两处同文（照绿），所以必须用源文本守卫钉结构 —— 与
// TestPlacementClauseStaysSinglePoint 同一手法、同一理由（在册病 T57/T61/T72/T80/T84/T86）。
func TestGroundingClauseStaysSinglePoint(t *testing.T) {
	srcBytes, err := os.ReadFile("defaults.go")
	if err != nil {
		t.Fatalf("读 defaults.go 失败：%v", err)
	}
	src := string(srcBytes)
	if n := strings.Count(src, groundingClauseKey); n > 0 {
		t.Errorf("defaults.go 里出现反虚构条款正文（%d 处）⇒ 已从单点退化为两处各写一份；正文只应存在于 model/deliverable_gate.go", n)
	}
	if !strings.Contains(src, "+ model.GateEvidenceGroundingClause +") {
		t.Fatal("defaults.go 不再引用 model.GateEvidenceGroundingClause ⇒ 单点被绕过（内联文本或换常量都会让两处再次分叉）")
	}
	if !strings.Contains(src, "7. ") {
		t.Error("第 7 条编号消失 ⇒ 条款被整条删除，模板侧不再约束证据落地")
	}
	t.Logf("templates 侧只引用单点常量、未内联正文（结构合规）")
}

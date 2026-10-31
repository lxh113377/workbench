package templates

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/model"
)

// gatePlacementKey 是落点条款正文的起始子串，用来检测「源码里是否出现第二份正文」。
const gatePlacementKey = "探查用的临时脚本与重定向输出"

// TestMdTemplatesCarryPlacementClause 钉住「每条内置 md 模板渲染后的 prompt 都带落点条款」，
// 且其正文与 model 里的单点逐字相同 —— 判的是**运行时真值**（Defaults() 出来的字符串），不是读常量名猜。
// 既有 deliverable_gate_contract_test.go 已用 need 列表逐短语查过前 5 条，本判据沿用它的方法学
// （只查词会被无关上下文蒙过，故同时查「交付物例外」这一承重句）。
//
// 为什么「交付物不在此列」必须一起钉：第 1 条要求报告写 docs/，新条款要求临时产物写临时目录；
// 没有例外句时模型可能把**报告**也写进临时目录 ⇒ 反被 T88 交付物门禁判红 —— 现场就有
// 「交付物只有 explore.ps1」而被判红的实例（1793441444590-ab90，本轮 T114 取证所引）。
func TestMdTemplatesCarryPlacementClause(t *testing.T) {
	checked := 0
	for _, tp := range Defaults() {
		if tp.OutputSpec.Format != "md" {
			continue
		}
		checked++
		if !strings.Contains(tp.PromptTemplate, model.GatePlacementClause) {
			t.Errorf("模板 %s 的渲染 prompt 缺落点条款（或正文与 model 单点不一致）", tp.ID)
			continue
		}
		i := strings.Index(tp.PromptTemplate, gatePlacementKey)
		if i < 0 {
			t.Fatalf("模板 %s 含条款却找不到正文锚点 %q ⇒ 单点正文被改过，本判据需同步", tp.ID, gatePlacementKey)
		}
		span := tp.PromptTemplate[i:]
		if len(span) > 320 {
			span = span[:320]
		}
		if !strings.Contains(span, "交付物不在此列") || !strings.Contains(span, "docs/") {
			t.Errorf("模板 %s 的落点条款未写明「交付物不在此列 + 报告仍落 docs/」⇒ 与第 1 条冲突，报告可能被写进临时目录", tp.ID)
		}
	}
	if checked < 20 {
		t.Fatalf("只查到 %d 条内置 md 模板 ⇒ 受理面退化，本判据此刻在比空气", checked)
	}
	t.Logf("%d 条内置 md 模板的渲染 prompt 均含落点条款（与 model 单点逐字一致，且写明交付物例外）", checked)
}

// TestPlacementClauseStaysSinglePoint 防的是**架构回退**：把正文复制回 defaults.go。
//
// 为什么运行时判据挡不住它：两处内联同样文本时，所有 Contains 断言照绿 —— 漂移要等到有人只改
// 其中一份才暴露，而那时已经没有判据会失败。本仓在册病 T57/T61/T72/T80/T84/T86 全是「同一契约
// 多处实现」；model 里 marker 的注释也记载条款正文历来两处各写一份。
// ⇒ 用源文本守卫钉住单点结构（与既有 deliverable_gate_contract_test.go:45 钉注入行同一手法）。
func TestPlacementClauseStaysSinglePoint(t *testing.T) {
	src, err := os.ReadFile("defaults.go")
	if err != nil {
		t.Fatalf("读 defaults.go 失败：%v", err)
	}
	s := string(src)
	if n := strings.Count(s, gatePlacementKey); n > 0 {
		t.Errorf("defaults.go 里出现落点条款正文（%d 处）⇒ 已从单点退化为两处各写一份；正文只应存在于 model/deliverable_gate.go", n)
	}
	if !strings.Contains(s, "+ model.GatePlacementClause +") {
		t.Fatal("defaults.go 不再引用 model.GatePlacementClause ⇒ 单点被绕过（内联文本或换常量都会让两处再次分叉）")
	}
	if !strings.Contains(s, "6. ") {
		t.Error("第 6 条编号消失 ⇒ 条款被整条删除，模板侧不再约束探查产物落点")
	}
	t.Logf("templates 侧只引用单点常量、未内联正文（结构合规）")
}

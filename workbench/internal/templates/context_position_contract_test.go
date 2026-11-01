package templates

import (
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestContextPlaceholderSitsAtPromptTail 钉住「模板的 {context} 占位符必须在自身指令文本的末尾」。
//
// 为什么需要（2026-11-01 round 187 运行时探针实测）：quality-verify 与 quality-hygiene 两条模板里，
// 「上下文：{context}」分别卡在 ⑧ 与 ⑤ 的位置，其后还跟着 ⑨–⑳、⑥–⑩ 共 12 / 3 个编号教训 ——
// 那是历次「往模板尾部追加教训」时把新条款接到占位符之后造成的结构性漂移。后果不是排版：
// 渲染时项目上下文（变长、可能含用户粘贴来的噪声）被夹进指令中段，而免费车道模型最容易丢的
// 就是长指令清单的中后段 ⇒ 尾部那批教训形同没写。这正是本仓在册的「格式对、实质失效」形态。
//
// 口径三条（都是本轮踩过后定下来的）：
//
//	① 判运行时真值：用 Defaults() 出来的 PromptTemplate，不解析 defaults.go 源码；
//	② 先扣除 mk() 追加的门禁条款：从 DeliverableGateMarker 处切开，前半才是模板自身文本
//	   （门禁条款按设计就在 {context} 之后，不扣会造出 9 条假阳性 —— 第一版实测如此）；
//	③ 判「占位符之后有无正文」这个语义，不比对「上下文：\n{context}」的字面形态
//	   （模板源码里写的是 Go 转义，比对字面形态会把反斜杠歧义带进判据）。
func TestContextPlaceholderSitsAtPromptTail(t *testing.T) {
	violations := 0
	checked := 0
	withCtx := 0
	noCtx := 0
	for _, tp := range Defaults() {
		if tp.OutputSpec.Format != "md" {
			continue
		}
		checked++
		core := tp.PromptTemplate
		// 门禁条款在运行时以「真实两个换行 + marker」起头（Go 源码里的 \n\n 编译后是换行），
		// 必须带这个前缀切：模板正文里也会**提到**「## 交付物门禁」这几个字（教训里引用它），
		// 只按裸 marker 切会在更早处误截，把 {context} 一起截掉（本轮实测踩到）。
		if i := strings.Index(core, "\n\n"+model.DeliverableGateMarker); i >= 0 {
			core = strings.TrimRight(core[:i], "\n")
		}
		// 只约束「用了 {context} 的模板」：29 条里有 18 条本来就不注入上下文（如 content-deep-dive
		// 只要 {topic}），那是各模板自己的取舍，不由本判据管 —— 第一版把它当成违规报出 18 条，
		// 是判据越界（把「没用某机制」判成「用错了某机制」），不是模板缺陷。
		idx := strings.LastIndex(core, "{context}")
		if idx < 0 {
			noCtx++
			continue
		}
		withCtx++
		if tail := strings.TrimSpace(core[idx+len("{context}"):]); tail != "" {
			violations++
			if len(tail) > 60 {
				tail = tail[:60]
			}
			t.Errorf("模板 %s 的上下文占位符后仍有正文（%q）⇒ 往模板尾部追加教训会把 {context} 夹进指令中段，"+
				"模型最易丢长指令的中后段", tp.ID, tail)
		}
	}
	if withCtx < 8 {
		t.Fatalf("只有 %d 条模板带 {context} ⇒ 受理面退化，本判据此刻在比空气", withCtx)
	}
	if checked < 20 {
		t.Fatalf("只查到 %d 条内置 md 模板 ⇒ 受理面退化，本判据此刻在比空气", checked)
	}
	if violations > 0 {
		t.Fatalf("%d 条模板的 {context} 不在自身指令末尾（明细见上）", violations)
	}
	t.Logf("%d/%d 条带 {context} 的模板其占位符均在指令末尾（已扣除 mk 追加的门禁条款）；其余 %d 条不注入上下文，不由本判据管", withCtx, checked, noCtx)
}

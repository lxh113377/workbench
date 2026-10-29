package templates

import (
	"os"
	"strings"
	"testing"
)

// TestBuiltinMdTemplatesCarryDeliverableGate 钉住「模板库与引擎门禁同口径」（round 70）。
//
// 事故背景（2026-10-29 live 实测）：引擎侧先后立了报告内容门禁（T66：docs/ 下三节 + 证据）、分级门禁（T67）、
// 产物最小实质（T73），而模板库 prompt 一条都没要求这些 ⇒ 模板任务的产物必然被自己的门禁判红（白跑真实执行）。
// 线上 4 条模板里 3 条缺「文件:行号/分级/docs/」。
//
// 判据（对每条**内置 md 模板**逐条查要求短语，而不是查关键词 —— 只查词会被无关上下文蒙过）：
//
//	docs/ 落盘 · `## 结论` · `## 证据` · `## 门禁建议` · 文件:行号 · 分级标记（P0 阻塞 或 高/中/低）。
func TestBuiltinMdTemplatesCarryDeliverableGate(t *testing.T) {
	need := []string{"docs/", "## 结论", "## 证据", "## 门禁建议", "文件:行号", "P0 阻塞"}
	checked := 0
	for _, tp := range Defaults() {
		if tp.OutputSpec.Format != "md" {
			continue
		}
		checked++
		for _, n := range need {
			if !strings.Contains(tp.PromptTemplate, n) {
				t.Errorf("模板 %s 的 prompt 缺交付物门禁要件 %q —— 它的产物会被引擎门禁判红", tp.ID, n)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("内置 md 模板只有 %d 条，样本太少（判据可能被空集蒙过）", checked)
	}
	// 注入必须是**条件**的（只覆盖 md）。
	//
	// ⚠️ 这里不从产物侧反推：内置模板当前 **全是 md**（format 分布实测只有 md 一族），
	// 那样写的反向断言是**空集恒真**、负控实测判据无牙 —— 空集判据不算判据（本轮第二次踩同族坑）。
	// 改为钉注入点的**条件与取值**（源文本守卫）：条件必须是 md，被追加的必须是共享条款常量。
	src, err := os.ReadFile("defaults.go")
	if err != nil {
		t.Fatalf("读 defaults.go 失败: %v", err)
	}
	body := string(src)
	if !strings.Contains(body, "if out.Format == \"md\" {\n\t\t\tprompt = prompt + deliverableGateClause") {
		t.Fatal("门禁条款必须在 mk 里**按 md 条件**注入（改成无条件或换常量都会让模板口径再次漂移）")
	}
}

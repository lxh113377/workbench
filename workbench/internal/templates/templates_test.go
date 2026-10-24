package templates

import (
	"strings"
	"testing"
)

func TestDefaultsCount(t *testing.T) {
	all := Defaults()
	if len(all) != 29 {
		t.Fatalf("内置模板数 = %d, want 29", len(all))
	}
	domains := map[string]int{}
	for _, tpl := range all {
		domains[tpl.Domain]++
	}
	// quality 领域 2026-10-24 round 88 新增（工程质量四动作：体检/判据化/验证/固化）
	want := []string{"content", "code", "knowledge", "data", "solution", "report", "quality"}
	// 各领域模板数（round 88/90 新增 quality 后：content..report 各 4，quality 5）
	wantCount := map[string]int{"content": 4, "code": 4, "knowledge": 4, "data": 4, "solution": 4, "report": 4, "quality": 5}
	for _, d := range want {
		if domains[d] != wantCount[d] {
			t.Fatalf("领域 %s 模板数 = %d, want %d", d, domains[d], wantCount[d])
		}
	}
}

func TestValidateAllDefaults(t *testing.T) {
	for _, tpl := range Defaults() {
		if err := Validate(tpl); err != nil {
			t.Fatalf("内置模板 %s 校验失败: %v", tpl.ID, err)
		}
	}
}

func TestRender(t *testing.T) {
	out := Render("请围绕「{topic}」写 {words} 字。上下文：{context}", RenderVars{Topic: "AI", Words: "500", Context: "无"})
	if out != "请围绕「AI」写 500 字。上下文：无" {
		t.Fatalf("render = %q", out)
	}
}

func TestValidateRejectsBadInput(t *testing.T) {
	bad := Defaults()[0]
	bad.ID = ""
	if err := Validate(bad); err == nil {
		t.Fatal("空 ID 应被拒绝")
	}
	bad2 := Defaults()[0]
	bad2.PromptTemplate = "没有主题占位符"
	if err := Validate(bad2); err == nil {
		t.Fatal("缺少 {topic} 应被拒绝")
	}
	bad3 := Defaults()[0]
	bad3.PromptTemplate = "请围绕「{topic}」处理 {unknown}"
	if err := Validate(bad3); err == nil {
		t.Fatal("未知占位符应被拒绝")
	}
}

func TestRegistryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	reg, err := Open(dir + "/templates.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reg.List()); got != 29 {
		t.Fatalf("registry list = %d, want 29", got)
	}
	custom := Defaults()[0]
	custom.ID = "custom-1"
	custom.Name = "自定义模板"
	if err := reg.Upsert(custom); err != nil {
		t.Fatal(err)
	}
	reg2, err := Open(dir + "/templates.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg2.Get("custom-1"); !ok {
		t.Fatal("自定义模板未持久化")
	}
}

// TestQualityVerifyPromptCarriesMutationSampleRule 钉住「变异样本必须取自真实缺陷形态」
// 这条教训确实在运行时 prompt 里（round 98 实测踩过：判据只搜 rgb( 而本仓多数写 rgba(，
// 注入 rgba 样本时 rc=0 漏检）。写进源码不等于运行时读得到 —— 故用断言实测字符串值。
func TestQualityVerifyPromptCarriesMutationSampleRule(t *testing.T) {
	for _, tpl := range Defaults() {
		if tpl.ID != "quality-verify" {
			continue
		}
		if !strings.Contains(tpl.PromptTemplate, "变异样本必须取自本仓真实缺陷形态") {
			t.Error("quality-verify 的 prompt 缺「变异样本取自真实形态」这条 —— 教训没进运行时指令")
		}
		if !strings.Contains(tpl.PromptTemplate, "rgba") {
			t.Error("quality-verify 的 prompt 未举 rgba/rgb 双形态实例 —— 抽象规则需要一个具体反例才有效")
		}
		return
	}
	t.Fatal("找不到 quality-verify 模板")
}

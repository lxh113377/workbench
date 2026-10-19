package templates

import "testing"

func TestDefaultsCount(t *testing.T) {
	all := Defaults()
	if len(all) != 24 {
		t.Fatalf("内置模板数 = %d, want 24", len(all))
	}
	domains := map[string]int{}
	for _, tpl := range all {
		domains[tpl.Domain]++
	}
	want := []string{"content", "code", "knowledge", "data", "solution", "report"}
	for _, d := range want {
		if domains[d] != 4 {
			t.Fatalf("领域 %s 模板数 = %d, want 4", d, domains[d])
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
	if got := len(reg.List()); got != 24 {
		t.Fatalf("registry list = %d, want 24", got)
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

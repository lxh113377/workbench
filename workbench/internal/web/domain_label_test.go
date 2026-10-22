package web

import (
	"regexp"
	"strings"
	"testing"
)

// TestDomainLabelsMatchFrontendTable 钉住「Go 导出元信息」与「面板表格」两张
// 领域译名表的键与值完全一致。
//
// 为什么必须是机器判据：面板表格读 app.js 的 DOMAIN_TEXT，导出（md/txt/docx）
// 读 Go 的 domainLabels。两处各写一份、又不在同一个文件里，改一处漏一处是
// 必然发生的（本轮就是先改了前端、导出仍显英文）。这条测试把「同源」变成
// 构建期判据：任一侧加/改/删领域而另一侧没跟上，立刻判红并点名差异。
func TestDomainLabelsMatchFrontendTable(t *testing.T) {
	// 从 appJS 里抠出 DOMAIN_TEXT 的键值对。
	start := strings.Index(appJS, "DOMAIN_TEXT = {")
	if start < 0 {
		t.Fatal("app.js 里找不到 DOMAIN_TEXT（前端译名表被改名或删除，导出与面板必然漂移）")
	}
	end := strings.Index(appJS[start:], "}")
	if end < 0 {
		t.Fatal("DOMAIN_TEXT 没有闭合花括号")
	}
	body := appJS[start : start+end]
	re := regexp.MustCompile(`([a-z]+):\s*'([^']+)'`)
	js := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		js[m[1]] = m[2]
	}
	if len(js) == 0 {
		t.Fatal("DOMAIN_TEXT 解析出 0 条（正则失配，判据本身失效）")
	}

	// 键集合必须完全相等（双向差集都点名，禁只报计数）。
	for k, v := range js {
		got, ok := domainLabels[k]
		if !ok {
			t.Errorf("Go domainLabels 缺键 %q（前端有「%s」，导出会漏译成英文）", k, v)
			continue
		}
		if got != v {
			t.Errorf("键 %q 译名漂移：Go=%q 前端=%q", k, got, v)
		}
	}
	for k := range domainLabels {
		if _, ok := js[k]; !ok {
			t.Errorf("前端 DOMAIN_TEXT 缺键 %q（Go 有，面板会显示裸枚举）", k)
		}
	}
}

// TestDomainLabelBehavior 钉住映射函数本身：命中翻译、空值给占位、
// 未知枚举原样返回（新增领域时「看得见没翻译」优于「被吞成空」）。
func TestDomainLabelBehavior(t *testing.T) {
	if got := domainLabel("code"); got != "代码生成" {
		t.Fatalf("code 应译成「代码生成」，实得 %q", got)
	}
	if got := domainLabel(""); got != "-" {
		t.Fatalf("空领域应给占位 - ，实得 %q", got)
	}
	if got := domainLabel("brand-new-domain"); got != "brand-new-domain" {
		t.Fatalf("未知枚举必须原样返回以便发现缺项，实得 %q", got)
	}
}

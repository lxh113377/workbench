package web

// domainLabels 是任务领域枚举的中文显示名。
//
// 为什么 Go 侧也要一份（2026-10-22）：面板表格走 app.js 的 DOMAIN_TEXT，
// 而批量导出（md / txt / docx 三条路径）在 Go 侧拼元信息行。只改前端会让
// 同一份数据在面板显示「内容创作」、导出的文档里仍是「content」——
// 用户看到的标签必须同源。domain_label_test.go 钉住两张表的键值一致。
var domainLabels = map[string]string{
	"content":   "内容创作",
	"code":      "代码生成",
	"knowledge": "知识整理",
	"data":      "数据处理",
	"solution":  "方案撰写",
	"report":    "报告生成",
	"quality":   "工程质量",
}

// domainLabel 取领域的中文显示名；未知枚举原样返回（不吞数据，便于发现新增值）。
func domainLabel(d string) string {
	if name, ok := domainLabels[d]; ok {
		return name
	}
	if d == "" {
		return "-"
	}
	return d
}

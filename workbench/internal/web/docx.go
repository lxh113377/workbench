package web

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeDocx 用 archive/zip 手写最小但合规的 OOXML 文档（零第三方依赖）。
func writeDocx(path string, docs []exportDoc) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		f.Close()
		if cleanup {
			os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(f)
	add := func(name, body string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(body))
		return err
	}
	if err := add("[Content_Types].xml", contentTypesXML); err != nil {
		return err
	}
	if err := add("_rels/.rels", rootRelsXML); err != nil {
		return err
	}
	if err := add("word/_rels/document.xml.rels", docRelsXML); err != nil {
		return err
	}
	if err := add("word/styles.xml", stylesXML); err != nil {
		return err
	}
	if err := add("word/document.xml", buildDocumentXML(docs)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func buildDocumentXML(docs []exportDoc) string {
	var doc strings.Builder
	doc.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	doc.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	doc.WriteString(docxPara("Title", "个人 AI 工作台 · 产出合集"))
	doc.WriteString(docxPara("Normal", fmt.Sprintf("导出时间：%s ｜ 共 %d 篇", time.Now().Format("2006-01-02 15:04:05"), len(docs))))
	for i, d := range docs {
		doc.WriteString(docxPara("Heading1", fmt.Sprintf("%d. %s", i+1, d.Task.Title)))
		meta := fmt.Sprintf("领域：%s ｜ 模型：%s ｜ 耗时：%.1f 分钟 ｜ 自评：%d/10", domainLabel(d.Task.Domain), d.Task.Model, float64(d.Task.ElapsedMs)/60000, qualityOf(d.Task))
		doc.WriteString(docxPara("Normal", meta))
		doc.WriteString(markdownToParas(d.Content))
	}
	doc.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1200" w:bottom="1440" w:left="1200"/></w:sectPr></w:body></w:document>`)
	return doc.String()
}

func docxPara(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
}

// markdownToParas 把 Markdown 转成 Word 段落（支持 #/##/### 与列表）。
func markdownToParas(md string) string {
	var b strings.Builder
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			b.WriteString("<w:p/>")
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "### "):
			b.WriteString(docxPara("Heading2", strings.TrimPrefix(trimmed, "### ")))
		case strings.HasPrefix(trimmed, "## "):
			b.WriteString(docxPara("Heading2", strings.TrimPrefix(trimmed, "## ")))
		case strings.HasPrefix(trimmed, "# "):
			b.WriteString(docxPara("Heading1", strings.TrimPrefix(trimmed, "# ")))
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "):
			b.WriteString(docxPara("Normal", "• "+strings.TrimSpace(trimmed[2:])))
		default:
			b.WriteString(docxPara("Normal", stripMarkdown(trimmed)))
		}
	}
	return b.String()
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 38:
			b.WriteString("&amp;")
		case 60:
			b.WriteString("&lt;")
		case 62:
			b.WriteString("&gt;")
		case 34:
			b.WriteString("&quot;")
		case 39:
			b.WriteString("&apos;")
		default:
			if r < 0x20 && r != 9 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
</Types>`

const rootRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

const docRelsXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
</Relationships>`

const stylesXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr>
<w:rFonts w:ascii="Segoe UI" w:eastAsia="Microsoft YaHei" w:hAnsi="Segoe UI"/>
<w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:pPr><w:spacing w:before="240" w:after="120"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:pPr><w:spacing w:before="240" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="30"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:pPr><w:spacing w:before="200" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="26"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Normal"><w:name w:val="Normal"/><w:pPr><w:spacing w:after="80" w:line="300" w:lineRule="auto"/></w:pPr></w:style>
</w:styles>`

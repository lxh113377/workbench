package web

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// handleExport 批量导出产出：md 合集 / txt 合集 / docx。
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Format string   `json:"format"`
		Scope  string   `json:"scope"`
		IDs    []string `json:"ids"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	if body.Format == "" {
		body.Format = "md"
	}
	ids := body.IDs
	if body.Scope != "selected" || len(ids) == 0 {
		ids = nil
		for _, t := range s.engine.Store().List() {
			if t.State == model.StateDone {
				ids = append(ids, t.ID)
			}
		}
	}
	if len(ids) == 0 {
		bad(w, "没有可导出的已完成任务")
		return
	}
	docs := make([]exportDoc, 0, len(ids))
	for _, id := range ids {
		t, found := s.engine.Store().Get(id)
		if !found || t.State != model.StateDone {
			continue
		}
		data, err := s.engine.Store().ReadTaskArtifact(id, "final.md")
		if err != nil {
			continue
		}
		docs = append(docs, exportDoc{Task: t, Content: string(data)})
	}
	if len(docs) == 0 {
		bad(w, "选中的任务没有可导出的产出")
		return
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Task.CreatedAt < docs[j].Task.CreatedAt })
	stamp := time.Now().Format("20060102-150405")
	if err := os.MkdirAll(s.layout.ExportsDir, 0o755); err != nil {
		bad(w, err.Error())
		return
	}
	switch body.Format {
	case "md", "txt":
		name := fmt.Sprintf("workbench-export-%s.%s", stamp, body.Format)
		path := filepath.Join(s.layout.ExportsDir, name)
		data := buildCollection(docs, body.Format == "md")
		if err := config.WriteFileAtomic(path, data); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"path": path, "name": name, "count": len(docs), "bytes": len(data)})
	case "docx":
		name := fmt.Sprintf("workbench-export-%s.docx", stamp)
		path := filepath.Join(s.layout.ExportsDir, name)
		if err := writeDocx(path, docs); err != nil {
			bad(w, "生成 DOCX 失败: "+err.Error())
			return
		}
		var size int64
		if st, err := os.Stat(path); err == nil {
			size = st.Size()
		}
		ok(w, map[string]any{"path": path, "name": name, "count": len(docs), "bytes": size})
	default:
		bad(w, "不支持的导出格式: "+body.Format)
	}
}

type exportDoc struct {
	Task    *model.Task
	Content string
}

// buildCollection 生成 Markdown / 纯文本合集。
func buildCollection(docs []exportDoc, markdown bool) []byte {
	var b bytes.Buffer
	if markdown {
		b.WriteString("# 个人 AI 工作台 · 产出合集\n\n")
		b.WriteString(fmt.Sprintf("导出时间：%s\n\n", time.Now().Format("2006-01-02 15:04:05")))
		b.WriteString(fmt.Sprintf("共 %d 篇\n\n---\n\n", len(docs)))
		for i, d := range docs {
			b.WriteString(fmt.Sprintf("## %d. %s\n\n", i+1, d.Task.Title))
			b.WriteString(fmt.Sprintf("- 领域：%s\n- 模型：%s\n- 完成时间：%s\n- 耗时：%.1f 分钟\n- 自评：%d/10\n\n",
				d.Task.Domain, d.Task.Model, time.UnixMilli(d.Task.FinishedAt).Format("2006-01-02 15:04"), float64(d.Task.ElapsedMs)/60000, qualityOf(d.Task)))
			b.WriteString(d.Content)
			b.WriteString("\n\n---\n\n")
		}
		return b.Bytes()
	}
	b.WriteString("个人 AI 工作台 · 产出合集\n")
	b.WriteString(fmt.Sprintf("导出时间：%s\n共 %d 篇\n\n", time.Now().Format("2006-01-02 15:04:05"), len(docs)))
	for i, d := range docs {
		b.WriteString(fmt.Sprintf("===== %d. %s =====\n", i+1, d.Task.Title))
		b.WriteString(fmt.Sprintf("领域：%s | 耗时：%.1f 分钟 | 自评：%d/10\n\n", d.Task.Domain, float64(d.Task.ElapsedMs)/60000, qualityOf(d.Task)))
		b.WriteString(stripMarkdown(d.Content))
		b.WriteString("\n\n")
	}
	return b.Bytes()
}

func qualityOf(t *model.Task) int {
	if t.Critique != nil {
		return t.Critique.Quality
	}
	return 0
}

// stripMarkdown 把 Markdown 粗转为纯文本（标题降级、去强调符）。
func stripMarkdown(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			continue
		}
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		line = strings.ReplaceAll(line, "`", "")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// handleAutostart 读取/设置开机自启（HKCU Run）。
func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		enabled, err := autostartEnabled()
		ok(w, map[string]any{"enabled": enabled && err == nil, "error": errString(err)})
	case http.MethodPost, http.MethodPut:
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := readBody(r, &body); err != nil {
			bad(w, err.Error())
			return
		}
		if err := setAutostart(body.Enabled); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"enabled": body.Enabled})
	default:
		bad(w, "不支持的方法")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
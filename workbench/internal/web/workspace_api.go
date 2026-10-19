package web

import (
	"net/http"
	"strings"

	"workbench/internal/workspace"
)

// readWorkspaceAudit 读取审计文件最近的 n 条（新→旧）。
func readWorkspaceAudit(path string, n int) ([]workspace.Record, error) {
	recs, err := workspace.ReadAudit(path)
	if err != nil {
		return nil, err
	}
	if n <= 0 {
		n = 50
	}
	if len(recs) > n {
		recs = recs[len(recs)-n:]
	}
	out := make([]workspace.Record, 0, len(recs))
	for i := len(recs) - 1; i >= 0; i-- {
		out = append(out, recs[i])
	}
	return out, nil
}

// ── 工作目录 API（AI 开发功能扩展：可选外部目录模式） ──────────────
//
// 路由：
//   GET  /api/workspace               读取当前模式/根/校验/文件树/审计摘要
//   POST /api/workspace/validate      只校验一个目录（不落配置，选择器"先验"用）
//   POST /api/workspace/select        校验并切换到外部目录（唯一写入口）
//   POST /api/workspace/reset         切回项目模式
//   GET  /api/workspace/audit         最近的路径裁决审计（允许/拒绝）

func (s *Server) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, s.engine.WorkspaceView())
	default:
		bad(w, "不支持的方法")
	}
}

func (s *Server) handleWorkspaceValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, "请求体无效: "+err.Error())
		return
	}
	v, err := s.engine.ValidateWorkspaceDir(strings.TrimSpace(body.Path))
	if err != nil {
		// 校验失败也返回完整结果：面板要展示"为什么不行"与建议，而不是只有一个错误串。
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "validation": v, "error": err.Error()})
		return
	}
	ok(w, map[string]any{"ok": true, "validation": v})
}

func (s *Server) handleWorkspaceSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, "请求体无效: "+err.Error())
		return
	}
	v, err := s.engine.SelectWorkspaceDir(strings.TrimSpace(body.Path))
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "validation": v, "error": err.Error()})
		return
	}
	ok(w, map[string]any{"ok": true, "validation": v, "view": s.engine.WorkspaceView()})
}

func (s *Server) handleWorkspaceReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	cfg := s.engine.ResetWorkspaceProject()
	ok(w, map[string]any{"ok": true, "config": cfg, "view": s.engine.WorkspaceView()})
}

func (s *Server) handleWorkspaceBrowse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		bad(w, "仅支持 GET")
		return
	}
	path := r.URL.Query().Get("path")
	showHidden := r.URL.Query().Get("hidden") == "1"
	res, err := workspace.Browse(path, showHidden)
	if err != nil {
		// 浏览失败也要把父目录/盘符带回去，面板才能"退回去重选"。
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "browse": res, "error": err.Error()})
		return
	}
	ok(w, map[string]any{"ok": true, "browse": res})
}

func (s *Server) handleWorkspaceAudit(w http.ResponseWriter, r *http.Request) {
	n := atoiDefault(r.URL.Query().Get("tail"), 50)
	snap := s.engine.Workspace().Snapshot()
	var recs []workspace.Record
	if p := snap.AuditPath; p != "" {
		// 复用 workspace 包的读取器，坏行自动跳过。
		if list, err := readWorkspaceAudit(p, n); err == nil {
			recs = list
		}
	}
	ok(w, map[string]any{
		"lines":     recs,
		"summary":   snap.Audit,
		"auditPath": snap.AuditPath,
	})
}

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doJSON 用给定方法/请求体调用一个 handler，并解析 JSON 响应。
func doJSON(t *testing.T, fn http.HandlerFunc, srv *Server, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = strings.NewReader(string(buf))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("X-WB-Token", srv.Token())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	fn(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// TestWorkspaceAPIProjectMode 覆盖默认行为：项目模式下接口可用、模式与根正确、
// 所有既有接口不受影响（这是"新增功能不破坏原逻辑"的可执行判据）。
func TestWorkspaceAPIProjectMode(t *testing.T) {
	srv, eng, _ := newTestServer(t)
	code, out := doJSON(t, srv.handleWorkspace, srv, http.MethodGet, "/api/workspace", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/workspace 状态码 = %d", code)
	}
	if out["mode"] != "project" {
		t.Fatalf("默认应为项目模式: %+v", out)
	}
	root, _ := out["root"].(string)
	if root == "" || !strings.Contains(root, "workbench-output") {
		t.Fatalf("项目模式根应指向数据根: %q", root)
	}
	// 既有接口仍在
	if code, _ := doJSON(t, srv.handleMetrics, srv, http.MethodGet, "/api/metrics", nil); code != http.StatusOK {
		t.Fatalf("metrics 接口应仍可用")
	}
	_ = eng
}

// TestWorkspaceAPISelectAndReset 覆盖外部目录模式的完整 HTTP 链路：
// 校验 → 切换 → 状态反映 → 审计留痕 → 切回项目模式。
func TestWorkspaceAPISelectAndReset(t *testing.T) {
	srv, eng, _ := newTestServer(t)
	target := t.TempDir()
	readme := filepath.Join(target, "README.md")
	if err := os.WriteFile(readme, []byte("# demo"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1) 校验
	code, out := doJSON(t, srv.handleWorkspaceValidate, srv, http.MethodPost, "/api/workspace/validate", map[string]any{"path": target})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("校验应通过: %d %+v", code, out)
	}
	// 2) 切换
	code, out = doJSON(t, srv.handleWorkspaceSelect, srv, http.MethodPost, "/api/workspace/select", map[string]any{"path": target})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("切换应成功: %d %+v", code, out)
	}
	// 3) 状态
	_, view := doJSON(t, srv.handleWorkspace, srv, http.MethodGet, "/api/workspace", nil)
	if view["mode"] != "external" {
		t.Fatalf("切换后应为 external: %+v", view)
	}
	if got, _ := view["root"].(string); !strings.EqualFold(got, target) {
		t.Fatalf("根应为目标目录: %q", got)
	}
	// 引擎侧工作目录确实指向用户目录
	if wd := eng.WorkDirFor("task-any"); !strings.EqualFold(wd, target) {
		t.Fatalf("引擎任务工作目录 = %q", wd)
	}
	// 4) 非法目录不改变模式（失败不污染状态）
	code, out = doJSON(t, srv.handleWorkspaceSelect, srv, http.MethodPost, "/api/workspace/select", map[string]any{"path": "relative"})
	if code != http.StatusOK || out["ok"] != false {
		t.Fatalf("非法目录应返回 ok=false: %d %+v", code, out)
	}
	_, view = doJSON(t, srv.handleWorkspace, srv, http.MethodGet, "/api/workspace", nil)
	if view["mode"] != "external" {
		t.Fatalf("失败的选择不应改变模式: %+v", view)
	}
	// 5) 审计
	_, audit := doJSON(t, srv.handleWorkspaceAudit, srv, http.MethodGet, "/api/workspace/audit?tail=20", nil)
	sum, _ := audit["summary"].(map[string]any)
	if sum == nil || sum["total"].(float64) < 1 {
		t.Fatalf("审计应有记录: %+v", audit)
	}
	if sum["denied"].(float64) < 1 {
		t.Fatalf("应记录至少一次拒绝: %+v", sum)
	}
	// 6) 切回项目模式
	code, out = doJSON(t, srv.handleWorkspaceReset, srv, http.MethodPost, "/api/workspace/reset", map[string]any{})
	if code != http.StatusOK || out["ok"] != true {
		t.Fatalf("切回失败: %d %+v", code, out)
	}
	_, view = doJSON(t, srv.handleWorkspace, srv, http.MethodGet, "/api/workspace", nil)
	if view["mode"] != "project" {
		t.Fatalf("切回后应为项目模式: %+v", view)
	}
}

// TestWorkspaceAPIBrowse 覆盖目录选择器：空路径列盘符、给定路径列子目录、文件不入列。
func TestWorkspaceAPIBrowse(t *testing.T) {
	srv, _, _ := newTestServer(t)
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "sub-a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspace/browse?path="+base, nil)
	req.Header.Set("X-WB-Token", srv.Token())
	rec := httptest.NewRecorder()
	srv.handleWorkspaceBrowse(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON: %v", err)
	}
	if out["ok"] != true {
		t.Fatalf("浏览应成功: %+v", out)
	}
	b, _ := out["browse"].(map[string]any)
	entries, _ := b["entries"].([]any)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "sub-a") {
		t.Fatalf("应列出子目录: %v", names)
	}
	if strings.Contains(joined, ".hidden") {
		t.Fatalf("默认不列隐藏目录: %v", names)
	}
	if strings.Contains(joined, "readme.txt") {
		t.Fatalf("选择器只列目录: %v", names)
	}
	// 空路径 = 盘符列表（Windows）
	req2 := httptest.NewRequest(http.MethodGet, "/api/workspace/browse", nil)
	req2.Header.Set("X-WB-Token", srv.Token())
	rec2 := httptest.NewRecorder()
	srv.handleWorkspaceBrowse(rec2, req2)
	var out2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &out2)
	b2, _ := out2["browse"].(map[string]any)
	drives, _ := b2["drives"].([]any)
	if len(drives) == 0 {
		t.Fatalf("空路径应返回盘符列表: %+v", out2)
	}
}

// TestWorkspaceMethodsRejected 覆盖方法约束：写接口只接受 POST。
func TestWorkspaceMethodsRejected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, fn := range []http.HandlerFunc{srv.handleWorkspaceSelect, srv.handleWorkspaceReset, srv.handleWorkspaceValidate} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/workspace/select", nil)
		req.Header.Set("X-WB-Token", srv.Token())
		fn(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("GET 写接口应 400，实际 %d", rec.Code)
		}
	}
}

// TestAdminPageHasWorkspaceTab 覆盖 UI 可发现性：管理面板必须出现「工作目录」页签。
func TestAdminPageHasWorkspaceTab(t *testing.T) {
	srv, _, _ := newTestServer(t)
	html := adminHTML(srv.Token())
	if !strings.Contains(html, "data-tab=\"workspace\"") || !strings.Contains(html, "tab-workspace") {
		t.Fatalf("管理面板缺少工作目录页签")
	}
	if !strings.Contains(html, "data-tab=\"settings\"") {
		t.Fatalf("既有页签不应被破坏")
	}
}

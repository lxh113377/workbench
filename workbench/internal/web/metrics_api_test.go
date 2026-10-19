package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
	"workbench/internal/engine"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/templates"
)

func newTestServer(t *testing.T) (*Server, *engine.Engine, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := templates.Open(filepath.Join(layout.DataRoot, "templates.json"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	// WAL 文件是长期打开的句柄：测试结束必须关闭，否则 t.TempDir 的 RemoveAll
	// 在 Windows 上会因文件被占用而失败（与 engine 包同源的假红）。
	t.Cleanup(func() { _ = st.Close() })
	hub := NewHub(16)
	eng, err := engine.New(layout, cstore, st, reg, hub)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(layout, cstore, eng, hub), eng, st
}

func doGet(t *testing.T, srv *Server, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-WB-Token", srv.Token())
	rec := httptest.NewRecorder()
	switch path {
	case "/api/metrics":
		srv.handleMetrics(rec, req)
	case "/api/failures":
		srv.handleFailures(rec, req)
	default:
		t.Fatalf("未知路径: %s", path)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("%s 状态码 = %d, body=%s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s 响应不是合法 JSON: %v; body=%s", path, err, rec.Body.String())
	}
	return out
}

func TestFailuresEmpty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	out := doGet(t, srv, "/api/failures")
	if out["empty"] != true {
		t.Fatalf("无失败任务时 empty 应为 true: %+v", out)
	}
	if out["total"].(float64) != 0 {
		t.Fatalf("total = %v, want 0", out["total"])
	}
	buckets, _ := out["buckets"].([]any)
	if len(buckets) != 0 {
		t.Fatalf("buckets 应为空数组而非 null: %+v", out["buckets"])
	}
}

func TestFailuresAggregatesByErrorKind(t *testing.T) {
	srv, _, st := newTestServer(t)
	seedFailed(t, st, "t1", "timeout")
	seedFailed(t, st, "t2", "timeout")
	seedFailed(t, st, "t3", "missing_credential")
	seedFailed(t, st, "t4", "") // 空类别归入 unknown

	out := doGet(t, srv, "/api/failures")
	if out["empty"] != false {
		t.Fatalf("有失败任务时 empty 应为 false: %+v", out)
	}
	if out["total"].(float64) != 4 {
		t.Fatalf("total = %v, want 4", out["total"])
	}
	buckets, _ := out["buckets"].([]any)
	if len(buckets) != 3 {
		t.Fatalf("buckets = %d, want 3 (timeout/missing_credential/unknown)", len(buckets))
	}
	first := buckets[0].(map[string]any)
	if first["kind"] != "timeout" || first["count"].(float64) != 2 {
		t.Fatalf("首个桶应按数量降序且为 timeout×2: %+v", first)
	}
	samples, _ := first["samples"].([]any)
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2", len(samples))
	}
}

func TestMetricsShape(t *testing.T) {
	srv, _, _ := newTestServer(t)
	out := doGet(t, srv, "/api/metrics")
	for _, key := range []string{"at", "tasks", "done", "failed", "successRate", "avgSeconds", "attempts", "memLimitMB", "handleLimit", "heapMB", "goroutine"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("/api/metrics 缺少字段 %q: %+v", key, out)
		}
	}
	if out["memLimitMB"].(float64) <= 0 {
		t.Fatalf("memLimitMB 应来自配置且大于 0: %v", out["memLimitMB"])
	}
}

func seedFailed(t *testing.T, st *store.Store, id, kind string) {
	t.Helper()
	task := &model.Task{ID: id, Title: id, State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, to := range []model.TaskState{model.StateEvaluating, model.StateApproved, model.StateQueued, model.StateRunning, model.StateFailed} {
		if _, err := st.Transition(id, to, "test", ""); err != nil {
			t.Fatalf("%s -> %s: %v", id, to, err)
		}
	}
	if _, err := st.Update(id, func(x *model.Task) error {
		x.ErrorKind = kind
		x.FinishedAt = model.Now()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = os.Stdout
}

package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/engine"
	"workbench/internal/modelpool"
	"workbench/internal/store"
	"workbench/internal/templates"
)

// newTestPoolServer 构造一个带模型池的最小 Server（不启动监听）。
func newTestPoolServer(t *testing.T) (*Server, *modelpool.Service) {
	t.Helper()
	root := t.TempDir()
	layout, err := config.BuildLayout(root, "")
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
	reg, err := templates.Open(layout.DataRoot + "/templates.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(layout, cstore, st, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(10)
	srv := NewServer(layout, cstore, eng, hub)
	pool := modelpool.NewService(modelpool.NewStore(layout.DataRoot), nil, modelpool.ServiceConfig{
		Enabled: true, ProbeSeconds: 3600, SyncSeconds: 3600, Strategy: modelpool.StrategyPriority,
		DSHHome: layout.DSHHome,
	})
	srv.SetModelPool(pool)
	return srv, pool
}

// call 发起一次带 token 的请求。
func call(t *testing.T, srv *Server, method, path, body string) (int, map[string]any) {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "127.0.0.1:12345" // 中间件只允许回环（有意安全边界）
	req.Header.Set("X-WB-Token", srv.Token())
	rec := httptest.NewRecorder()
	srv.withMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/pool/models/"):
			srv.handlePoolModelItem(w, r)
		case r.URL.Path == "/api/pool/models":
			srv.handlePoolModels(w, r)
		case r.URL.Path == "/api/pool":
			srv.handlePoolStatus(w, r)
		case r.URL.Path == "/api/pool/strategy":
			srv.handlePoolStrategy(w, r)
		case r.URL.Path == "/api/pool/reorder":
			srv.handlePoolReorder(w, r)
		case r.URL.Path == "/api/pool/log":
			srv.handlePoolLog(w, r)
		default:
			http.NotFound(w, r)
		}
	})).ServeHTTP(rec, req)
	out := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
	}
	return rec.Code, out
}

func TestPoolAPIStatusAndCRUD(t *testing.T) {
	srv, _ := newTestPoolServer(t)

	code, body := call(t, srv, http.MethodGet, "/api/pool", "")
	if code != http.StatusOK {
		t.Fatalf("status code=%d body=%v", code, body)
	}
	if _, okf := body["specs"]; !okf {
		t.Fatal("响应应包含 specs")
	}

	// 新增带斜杠 id 的模型（OFM 真实 id 形态）
	payload := `{"id":"vendor/model-x","label":"测试模型","model":"model-x","baseUrl":"http://127.0.0.1:1/v1","apiKey":"sk-secret-1234","format":"openai-chat","lane":"custom","tier":"fast","priority":1,"weight":10,"enabled":true,"routable":true}`
	code, body = call(t, srv, http.MethodPost, "/api/pool/models", payload)
	if code != http.StatusOK {
		t.Fatalf("新增失败 code=%d body=%v", code, body)
	}
	model, _ := body["model"].(map[string]any)
	if model["id"] != "vendor/model-x" {
		t.Fatalf("id 应保留斜杠，实际 %v", model["id"])
	}
	if model["hasKey"] != true || model["keyHint"] != "…1234" {
		t.Fatalf("密钥应脱敏成指纹，实际 hasKey=%v hint=%v", model["hasKey"], model["keyHint"])
	}
	if _, leaked := model["apiKey"]; leaked {
		t.Fatal("API Key 明文绝不能被回显")
	}

	// 删除同一 id（带斜杠）
	code, body = call(t, srv, http.MethodDelete, "/api/pool/models/vendor/model-x", "")
	if code != http.StatusOK {
		t.Fatalf("删除失败 code=%d body=%v", code, body)
	}
	code, _ = call(t, srv, http.MethodGet, "/api/pool", "")
	if code != http.StatusOK {
		t.Fatalf("删除后状态查询失败 code=%d", code)
	}
}

func TestPoolAPIStrategyAndReorder(t *testing.T) {
	srv, pool := newTestPoolServer(t)
	code, body := call(t, srv, http.MethodPost, "/api/pool/strategy", `{"strategy":"round-robin"}`)
	if code != http.StatusOK || body["strategy"] != "round-robin" {
		t.Fatalf("策略切换失败 code=%d body=%v", code, body)
	}
	if got := pool.Scheduler().Strategy(); got != modelpool.StrategyRoundRobin {
		t.Fatalf("调度器策略未生效: %s", got)
	}
	// 非法策略应 400
	code, _ = call(t, srv, http.MethodPost, "/api/pool/strategy", `{"strategy":"bogus"}`)
	if code != http.StatusBadRequest {
		t.Fatalf("非法策略应 400，实际 %d", code)
	}
	// 调优先级
	ids := []string{"z-ai/glm-5.3", "openai/gpt-oss-20b"}
	raw, _ := json.Marshal(map[string]any{"ids": ids})
	code, _ = call(t, srv, http.MethodPost, "/api/pool/reorder", string(raw))
	if code != http.StatusOK {
		t.Fatalf("reorder 失败 code=%d", code)
	}
	specs := pool.Manager().Specs()
	if len(specs) == 0 || specs[0].ID != "z-ai/glm-5.3" {
		t.Fatalf("reorder 后第一条应为 z-ai/glm-5.3，实际 %+v", specs[:min(1, len(specs))])
	}
}

func TestPoolAPILogAndAuth(t *testing.T) {
	srv, _ := newTestPoolServer(t)
	code, body := call(t, srv, http.MethodGet, "/api/pool/log?tail=5", "")
	if code != http.StatusOK {
		t.Fatalf("日志接口失败 code=%d", code)
	}
	if _, okf := body["lines"]; !okf {
		t.Fatal("日志响应应包含 lines")
	}
	// 无 token 必须 401
	req := httptest.NewRequest(http.MethodGet, "/api/pool", nil)
	rec := httptest.NewRecorder()
	srv.guard(srv.handlePoolStatus)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，实际 %d", rec.Code)
	}
}

func TestPoolPageRenders(t *testing.T) {
	srv, _ := newTestPoolServer(t)
	rec := httptest.NewRecorder()
	srv.handleModelPoolPage(rec, httptest.NewRequest(http.MethodGet, "/modelpool", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("页面状态码 %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"模型池", "/assets/pool.js", "同步网关清单", "立即探测"} {
		if !strings.Contains(body, want) {
			t.Fatalf("页面缺少 %q", want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

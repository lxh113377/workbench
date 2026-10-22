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
		case r.URL.Path == "/api/pool/lane":
			srv.handlePoolLane(w, r)
		case r.URL.Path == "/api/pool/purge":
			srv.handlePoolPurge(w, r)
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

// TestPoolSlashIDActions 覆盖「id 带斜杠 + 尾部动作词」的三条路由：
// 历史缺陷是把路径按 / 切第一段，vendor/model-x 的 disable/enable/restore
// 被截成 vendor ⇒ 全部 400。删除路径已有回归用例，这里补齐另外三条动作。
func TestPoolSlashIDActions(t *testing.T) {
	srv, pool := newTestPoolServer(t)

	// 种一个带斜杠 id 的模型
	payload := `{"id":"vendor/act-x","label":"动作路由","model":"act-x","baseUrl":"http://127.0.0.1:1/v1","format":"openai-chat","lane":"custom","priority":1,"weight":10,"enabled":true,"routable":true}`
	code, body := call(t, srv, http.MethodPost, "/api/pool/models", payload)
	if code != http.StatusOK {
		t.Fatalf("新增失败 code=%d body=%v", code, body)
	}

	// disable：id 带斜杠时必须 200 且 enabled 变 false
	code, body = call(t, srv, http.MethodPost, "/api/pool/models/vendor/act-x/disable", "")
	if code != http.StatusOK {
		t.Fatalf("disable 失败 code=%d body=%v（斜杠 id 被截断的回归）", code, body)
	}
	if sp, _, okf := pool.Manager().Get("vendor/act-x"); !okf || sp.Enabled {
		t.Fatalf("disable 后 enabled 应为 false，实际 %+v", sp.Enabled)
	}

	// enable：恢复为 true
	code, body = call(t, srv, http.MethodPost, "/api/pool/models/vendor/act-x/enable", "")
	if code != http.StatusOK {
		t.Fatalf("enable 失败 code=%d body=%v", code, body)
	}
	if sp, _, _ := pool.Manager().Get("vendor/act-x"); !sp.Enabled {
		t.Fatal("enable 后 enabled 应为 true")
	}

	// restore：清除自动停用标记（不应报路由错误）
	code, body = call(t, srv, http.MethodPost, "/api/pool/models/vendor/act-x/restore", "")
	if code != http.StatusOK {
		t.Fatalf("restore 失败 code=%d body=%v", code, body)
	}

	// 未知动作必须 400（防误吞 id 末尾段）
	code, _ = call(t, srv, http.MethodPost, "/api/pool/models/vendor/act-x/frobnicate", "")
	if code != http.StatusBadRequest {
		t.Fatalf("未知动作应 400，实际 %d", code)
	}
}

// TestPoolLaneBatchToggle 钉住「各个免费池的一键启用/一键停用」（老大 2026-10-22 令）。
func TestPoolLaneBatchToggle(t *testing.T) {
	srv, pool := newTestPoolServer(t)
	pre := `{"id":"kilo-auto/e2e-toggle","model":"kilo-auto/e2e-toggle","format":"openai-chat","lane":"kilo","group":"Kilo 免费池","tier":"fast","priority":5,"weight":10,"enabled":true,"routable":true}`
	if code, body := call(t, srv, http.MethodPost, "/api/pool/models", pre); code != http.StatusOK {
		t.Fatalf("预置 kilo 模型失败 code=%d body=%v", code, body)
	}
	// 一键停用（按 lane）
	code, body := call(t, srv, http.MethodPost, "/api/pool/lane", `{"lane":"kilo","enabled":false}`)
	if code != http.StatusOK {
		t.Fatalf("一键停用失败 code=%d body=%v", code, body)
	}
	if sp, _, okf := pool.Manager().Get("kilo-auto/e2e-toggle"); !okf || sp.Enabled {
		t.Fatalf("一键停用后 kilo 池模型应 enabled=false，实际 %+v", sp)
	}
	// 一键启用（按 group —— 面板分组卡走的就是 group）
	code, body = call(t, srv, http.MethodPost, "/api/pool/lane", `{"group":"Kilo 免费池","enabled":true}`)
	if code != http.StatusOK {
		t.Fatalf("一键启用失败 code=%d body=%v", code, body)
	}
	if sp, _, _ := pool.Manager().Get("kilo-auto/e2e-toggle"); !sp.Enabled {
		t.Fatal("一键启用后应 enabled=true")
	}
	// 缺 enabled / 池未指定必须 400（不许静默成功）
	if code, _ = call(t, srv, http.MethodPost, "/api/pool/lane", `{"lane":"kilo"}`); code != http.StatusBadRequest {
		t.Fatalf("缺 enabled 应 400，实际 %d", code)
	}
	if code, _ = call(t, srv, http.MethodPost, "/api/pool/lane", `{"enabled":true}`); code != http.StatusBadRequest {
		t.Fatalf("缺 lane/group 应 400，实际 %d", code)
	}
	if code, _ = call(t, srv, http.MethodPost, "/api/pool/lane", `{"lane":"bogus","enabled":true}`); code != http.StatusBadRequest {
		t.Fatalf("未知车道应 400，实际 %d", code)
	}
}

// TestPoolPurgeGatewayLanes 钉住「网关专属车道和网关其它车道去掉」：
// POST /api/pool/purge 清掉 provider=gateway 的存量条目，用户自填端点不误删。
func TestPoolPurgeGatewayLanes(t *testing.T) {
	srv, pool := newTestPoolServer(t)
	mk := func(body string) {
		if code, b := call(t, srv, http.MethodPost, "/api/pool/models", body); code != http.StatusOK {
			t.Fatalf("预置失败 code=%d body=%v", code, b)
		}
	}
	mk(`{"id":"deepseek-official/legacy-a","model":"a","format":"openai-chat","lane":"aggregate","provider":"gateway","group":"网关专属车道","tier":"fast","priority":5,"weight":10,"enabled":true,"routable":false}`)
	mk(`{"id":"codearts/legacy-b","model":"b","format":"openai-chat","lane":"aggregate","provider":"gateway","group":"网关其他车道","tier":"fast","priority":5,"weight":10,"enabled":true,"routable":true}`)
	mk(`{"id":"open_router/user-own","model":"u","baseUrl":"http://127.0.0.1:1/v1","format":"openai-chat","lane":"custom","tier":"fast","priority":9,"weight":10,"enabled":true,"routable":true}`)

	code, body := call(t, srv, http.MethodPost, "/api/pool/purge", `{}`)
	if code != http.StatusOK {
		t.Fatalf("purge 失败 code=%d body=%v", code, body)
	}
	for _, gone := range []string{"deepseek-official/legacy-a", "codearts/legacy-b"} {
		if _, _, found := pool.Manager().Get(gone); found {
			t.Fatalf("%s 应被清理（网关自有车道存量）", gone)
		}
	}
	if _, _, found := pool.Manager().Get("open_router/user-own"); !found {
		t.Fatal("用户自填端点不得被 purge 误删")
	}
	// 幂等：第二次无存量
	if _, body = call(t, srv, http.MethodPost, "/api/pool/purge", `{}`); true {
		if purged, okf := body["purged"].(map[string]any); !okf {
			t.Fatalf("purge 响应应含 purged 对象，实际 %v", body)
		} else if removed, _ := purged["removed"].([]any); len(removed) != 0 {
			t.Fatalf("第二次 purge 应为空，实际 %v", removed)
		}
	}
	// /api/pool 的 specs 视图不得再出现网关自有车道
	code, body = call(t, srv, http.MethodGet, "/api/pool", "")
	if code != http.StatusOK {
		t.Fatalf("状态查询失败 code=%d", code)
	}
	sbs, _ := body["specs"].([]any)
	for _, raw := range sbs {
		sp, _ := raw.(map[string]any)
		g, _ := sp["group"].(string)
		if g == "网关专属车道" || g == "网关其他车道" || g == "网关其它车道" {
			t.Fatalf("purge 后仍残留网关自有车道分组：%v", sp["id"])
		}
		if p, _ := sp["provider"].(string); p == "gateway" {
			t.Fatalf("purge 后仍残留 provider=gateway 条目：%v", sp["id"])
		}
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
	// 回归守护（2026-10-19 现场事故）：injectFmt 只许替换 %s。
	// 响应式 CSS 里的 width:100% 曾被误当占位符，把 token 注进样式表，
	// 前端 __WB_TOKEN__ 变空串 → 全部 /api/pool 请求 401。
	if strings.Contains(body, "100;") {
		t.Fatal("CSS 的 width:100% 被占位符替换破坏（token 泄漏进样式表）")
	}
	if !strings.Contains(body, "width: 100%;") {
		t.Fatal("响应式 CSS 的 width: 100%; 应原样保留")
	}
	if strings.Contains(body, "%s") {
		t.Fatal("渲染后残留未替换的占位符（percent-s 形态）")
	}
	if !strings.Contains(body, srv.Token()) {
		t.Fatal("token 未注入页面（__WB_TOKEN__ 为空 ⇒ 前端请求全 401）")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

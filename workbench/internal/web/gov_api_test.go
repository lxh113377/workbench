package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/gov"
)

// newGovTestServer 构造一个只挂治理面的测试服务（engine 为 nil，治理接口不依赖主引擎）。
func newGovTestServer(t *testing.T) (*Server, *gov.Engine) {
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
	gst, err := gov.Open(layout.GovDir)
	if err != nil {
		t.Fatal(err)
	}
	geng := gov.NewEngine(layout, gst, gov.SilentSource{}, nil, func() config.GovConfig { return cstore.Get().Gov })
	s := NewServer(layout, cstore, nil, NewHub(10))
	s.SetGovernance(geng)
	return s, geng
}

func doReq(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	s.routes(mux)
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-WB-Token", s.Token())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGovAPIsRequireToken(t *testing.T) {
	s, _ := newGovTestServer(t)
	mux := http.NewServeMux()
	s.routes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/gov/summary", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401，实得 %d", rec.Code)
	}
}

func TestGovSummaryAndSample(t *testing.T) {
	s, _ := newGovTestServer(t)
	rec := doReq(t, s, http.MethodGet, "/api/gov/summary", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary 状态 %d: %s", rec.Code, rec.Body.String())
	}
	var sum gov.Summary
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatalf("summary 解析失败: %v", err)
	}
	// 新装环境：无采样 ⇒ 总等级必须 unverified，且带数据缺口说明
	if sum.Level != gov.LevelUnknown {
		t.Fatalf("首启无采样总等级应为 unverified，实得 %s", sum.Level)
	}
	if len(sum.DataGaps) == 0 {
		t.Fatal("首启必须登记数据缺口")
	}
	// 采样一次后应有三域指标
	rec2 := doReq(t, s, http.MethodPost, "/api/gov/sample", "{}")
	if rec2.Code != http.StatusOK {
		t.Fatalf("sample 状态 %d: %s", rec2.Code, rec2.Body.String())
	}
	var sum2 gov.Summary
	if err := json.Unmarshal(rec2.Body.Bytes(), &sum2); err != nil {
		t.Fatalf("sample 解析失败: %v", err)
	}
	if len(sum2.Domains) != 3 || len(sum2.Evals) != 13 {
		t.Fatalf("采样后应有 3 域 13 指标，实得 domains=%d evals=%d", len(sum2.Domains), len(sum2.Evals))
	}
}

func TestGovContractCRUDAndTransition(t *testing.T) {
	s, _ := newGovTestServer(t)
	body := `{"id":"C-API-1","title":"接口测试契约","counterpart":"相对方","stage":"draft",
	 "owner":"o","drafter":"d","reviewer":"r","signer":"s","perfMonitor":"p","archivist":"a",
	 "amount":100000,"expireAt":"2027-12-31","deliverables":["交付物"],
	 "clauses":[{"id":"c1","title":"付款","riskLevel":1,"signed":true}]}`
	rec := doReq(t, s, http.MethodPost, "/api/gov/contracts", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("新增契约失败 %d: %s", rec.Code, rec.Body.String())
	}
	// 列表
	rec = doReq(t, s, http.MethodGet, "/api/gov/contracts", "")
	var list struct {
		Contracts []gov.Contract `json:"contracts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Contracts) != 1 {
		t.Fatalf("列表异常: %v %s", err, rec.Body.String())
	}
	// 非法迁移（draft → archived 跨阶段）必须 400
	rec = doReq(t, s, http.MethodPost, "/api/gov/contracts/C-API-1/transition", `{"to":"archived","actor":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法跨阶段迁移应 400，实得 %d: %s", rec.Code, rec.Body.String())
	}
	// 合法迁移
	for _, stage := range []string{"review", "signed"} {
		rec = doReq(t, s, http.MethodPost, "/api/gov/contracts/C-API-1/transition",
			`{"to":"`+stage+`","actor":"tester"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("迁移到 %s 失败 %d: %s", stage, rec.Code, rec.Body.String())
		}
	}
	// 单份详情含合规分与缺失项
	rec = doReq(t, s, http.MethodGet, "/api/gov/contracts/C-API-1", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "compliance") {
		t.Fatalf("详情缺少合规分: %d %s", rec.Code, rec.Body.String())
	}
	// 删除
	rec = doReq(t, s, http.MethodDelete, "/api/gov/contracts/C-API-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("删除失败 %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGovBudgetRoundTrip(t *testing.T) {
	s, geng := newGovTestServer(t)
	rec := doReq(t, s, http.MethodGet, "/api/gov/budget", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("读取阈值失败 %d", rec.Code)
	}
	rec = doReq(t, s, http.MethodPost, "/api/gov/budget", `{"memLimitMB":123,"handleLimit":456,"contractAmountMax":1000,
	 "taskDailyMax":10,"activeContractMax":5,"outputBytesMax":1048576,"goroutineLimit":100,"concurrencyMax":2,
	 "eventsBytesMax":1048576,"backlogMax":10,"throughputDailyMin":1,"avgCycleSecondsMax":100,"failureRateMax":0.2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("写入阈值失败 %d: %s", rec.Code, rec.Body.String())
	}
	if got := geng.Store().Budget().MemLimitMB; got != 123 {
		t.Fatalf("阈值未生效，MemLimitMB=%d", got)
	}
	// 非法值必须被 Normalize 回默认，而不是原样生效
	rec = doReq(t, s, http.MethodPost, "/api/gov/budget", `{"memLimitMB":-5,"failureRateMax":9}`)
	var resp struct {
		Budget gov.Budget `json:"budget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Budget.MemLimitMB <= 0 || resp.Budget.FailureRateMax > 1 {
		t.Fatalf("非法阈值未归一化: %+v", resp.Budget)
	}
}

func TestGovernancePageAndAssets(t *testing.T) {
	s, _ := newGovTestServer(t)
	mux := http.NewServeMux()
	s.routes(mux)
	for _, path := range []string{"/governance", "/assets/gov.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s 状态 %d", path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "undefined") {
			t.Fatalf("%s 含 undefined 字面量", path)
		}
	}
	// 治理页必须注入 token 与脚本
	req := httptest.NewRequest(http.MethodGet, "/governance", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	html := rec.Body.String()
	for _, want := range []string{"__WB_TOKEN__", "/assets/gov.js", "治理控制台"} {
		if !strings.Contains(html, want) {
			t.Fatalf("治理页缺少 %q", want)
		}
	}
}

func TestGovRecomputeReturnsAllSections(t *testing.T) {
	s, geng := newGovTestServer(t)
	// 造一份高风险 + 逾期契约，重算后必须有预警
	c := gov.Contract{ID: "R1", Title: "高风险", Stage: gov.StagePerforming, Amount: 1,
		ExpireAt: "2020-01-01", Owner: "o", Drafter: "d", Reviewer: "r", Signer: "s", PerfMonitor: "p",
		Deliverables: []string{"x"}, Disputes: 2,
		Clauses: []gov.Clause{{ID: "c", Title: "c", RiskLevel: 3, Signed: false}}}
	if _, err := geng.Store().UpsertContract(c); err != nil {
		t.Fatal(err)
	}
	_ = geng.Store().AddSample(gov.Sample{At: time.Now().UnixMilli(), MemMB: 10, Handles: 10, Goroutines: 10, ThroughputDaily: 1})
	rec := doReq(t, s, http.MethodPost, "/api/gov/recompute", "{}")
	if rec.Code != http.StatusOK {
		t.Fatalf("重算失败 %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Alerts      []gov.Alert      `json:"alerts"`
		Adjustments []gov.Adjustment `json:"adjustments"`
		Linkage     gov.LinkageReport `json:"linkage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Alerts) == 0 {
		t.Fatal("高风险/逾期契约必须产生预警")
	}
	if out.Linkage.GeneratedAt == "" {
		t.Fatal("联动报告缺少生成时间")
	}
}

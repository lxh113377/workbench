package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"workbench/internal/logging"
)

// recSink 实现 logging.Handler：把每条日志收进内存（不碰磁盘、不依赖日志文件路径）。
// 之所以这样捕获：logging.Open 在 Dir 为空时 writers 落到 io.Discard（logging.go 的 Open 内 writers 组装处），
// 而 emit 里的 handler.Handle 与 writers 无关（emit 内 handler.Handle 分支）⇒ Options{Handler: sink} 是纯内存通路。
type recSink struct {
	mu   sync.Mutex
	recs []logging.Record
}

func (c *recSink) Handle(r logging.Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, r)
}

func (c *recSink) find(msg string) []logging.Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []logging.Record{}
	for _, r := range c.recs {
		if r.Message == msg {
			out = append(out, r)
		}
	}
	return out
}

// cr/lf/tab 用 rune 构造：让本文件不含任何转义序列与多字节字面量（沿用 helper_identity_contract_test.go
// 的定规），也避免我在这里再踩一次引号/转义类语法坑。
var (
	crLit  = string(rune(13))
	lfLit  = string(rune(10))
	tabLit = string(rune(9))
)

const visitMsg = "面板页面首次被访问"

func newVisitServer(t *testing.T) (*Server, *recSink) {
	t.Helper()
	srv, _, _ := newTestServer(t)
	sink := &recSink{}
	lg, err := logging.Open(logging.Options{Handler: sink})
	if err != nil {
		t.Fatalf("构造内存日志器失败：%v", err)
	}
	t.Cleanup(func() { _ = lg.Close() })
	srv.SetLogger(lg)
	return srv, sink
}

func getAs(path, ua string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	return req
}

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36"

// TestPanelVisitCountsRealBrowser 是 T113 的主判据。
//
// 为什么要这条（一手取证）：本会话连着两轮想回答「面板这两个页面真被真人打开过吗」都取不到证据 ——
//
//	audit.jsonl 165,211 行的 actor 只有 engine/autonomy/system/user，无 panel 取值；176 条 actor=user
//	全是 cancel+transition（今日 6 条实为我部署 --quit 的副作用）；全仓 grep accessLog/logRequest/访问计数=0。
//
// ⇒ 界面使用情况此前完全不可知，C3/C4 这类改动永远只能停在「机制成立、伤害率未取证」。
func TestPanelVisitCountsRealBrowser(t *testing.T) {
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	srv.handleAdmin(rec, getAs("/admin?token=x", chromeUA))
	got := sink.find(visitMsg)
	// 进门断言（元发现 19/23 同族：任何"0 条"读数先证明样本进了被测面）
	if len(got) == 0 {
		t.Fatalf("一条 %s 都没捕获 ⇒ 计数根本没接上（后续断言全是空气）。全部捕获=%d 条", visitMsg, len(sink.recs))
	}
	if len(got) != 1 {
		t.Fatalf("首次访问应有且只有 1 条，实得 %d", len(got))
	}
	f := got[0].Fields
	if p, _ := f["path"].(string); p != "/admin" {
		t.Errorf("path 应为 /admin，实得 %q（只记 path、不记 query）", p)
	}
	if h, _ := f["human"].(bool); !h {
		t.Errorf("Chrome UA 应判 human=true，实得 %v ⇒ 名单把真人也挡了", h)
	}
	if c, _ := f["count"].(int); c != 1 {
		t.Errorf("首次 count 应为 1，实得 %v", f["count"])
	}
	if ua, _ := f["ua"].(string); !strings.Contains(ua, "Chrome") {
		t.Errorf("ua 字段应保留可辨认内容，实得 %q", ua)
	}
	// 关键安全断言：/admin 的 URL 带 ?token=，日志与快照都不得泄漏它
	tok := srv.Token()
	if len(tok) > 4 {
		blob, _ := json.Marshal(f)
		if strings.Contains(string(blob), tok) {
			t.Error("访问日志里出现面板 token ⇒ 落盘即泄漏凭据（本条判据存在的理由之一）")
		}
		snap, _ := json.Marshal(srv.visits.snapshot())
		if strings.Contains(string(snap), tok) {
			t.Error("/api/status 的 panelVisits 里出现 token ⇒ 凭据经接口外泄")
		}
	} else {
		t.Fatalf("token 长度异常(%d)，泄漏断言等于没测", len(tok))
	}
}

// TestPanelVisitOneShotPerPage 钉住"每页每进程只落一条"。
// 这不是洁癖：engine.go:116 记过「running 迁移、0 产出，audit.jsonl 一夜涨到 2.3MB」，
// 而 withMiddleware 在每次资源请求都会跑 —— 逐请求打日志就是把那个事故重演一遍。
func TestPanelVisitOneShotPerPage(t *testing.T) {
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	for i := 0; i < 5; i++ {
		srv.handleAdmin(rec, getAs("/admin", chromeUA))
	}
	srv.handleModelPoolPage(rec, getAs("/modelpool", chromeUA))
	srv.handleGovernancePage(rec, getAs("/governance", chromeUA))
	if n := len(sink.find(visitMsg)); n != 3 {
		t.Errorf("三个不同页应各落 1 条（共 3 条），实得 %d ⇒ 要么逐请求刷屏、要么去重失效", n)
	}
	if tot := srv.visits.total(); tot != 7 {
		t.Errorf("累计次数应为 7（admin×5 + pool×1 + gov×1），实得 %d ⇒ 计数与日志是两件事，不能靠日志条数推断", tot)
	}
	snap := srv.visits.snapshot()
	for _, p := range []string{"/admin", "/modelpool", "/governance"} {
		if _, ok := snap[p]; !ok {
			t.Errorf("快照缺页 %q（四个页里三个必须到这里，/index 见下一条）", p)
		}
	}
}

// TestPanelVisitIgnoresFourOhFour 钉住"扫描器不算使用"：handleIndex 的 404 早退在计数之前。
func TestPanelVisitIgnoresFourOhFour(t *testing.T) {
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	for _, p := range []string{"/nope", "/../../etc/passwd", "/admin/../x"} {
		srv.handleIndex(rec, getAs(p, chromeUA))
	}
	if n := len(sink.find(visitMsg)); n != 0 {
		t.Errorf("404 不应计入面板使用（扫描器打错路径就能刷高证据），实得 %d 条", n)
	}
	if tot := srv.visits.total(); tot != 0 {
		t.Errorf("404 后计数应仍为 0，实得 %d", tot)
	}
	// 正向对照：同一 server 打一次真首页必须计数（证明上面那个 0 不是"根本没接上"）
	srv.handleIndex(rec, getAs("/", chromeUA))
	if tot := srv.visits.total(); tot != 1 {
		t.Fatalf("正向对照失败：真访问 / 后 total 应为 1，实得 %d ⇒ 上一条的 0 是空气不是结论", tot)
	}
}

// TestPanelVisitClassifiesAutomatedUA 钉住"观测者效应必须显式处理"：
// 本仓自己的 CDP 探针（cmd/workbench/ui_probe.go）与 liveness 脚本都会访问面板，
// 不过滤就会把「我自己跑过探针」报成「老大打开过面板」。
func TestPanelVisitClassifiesAutomatedUA(t *testing.T) {
	auto := []string{
		"HeadlessChrome/120.0 Safari/537.36",
		"go-http-client/1.1",
		"Mozilla/5.0 (Windows) PowerShell/7.4.0",
		"curl/8.4.0",
		"python-requests/2.31",
		"Puppeteer/23.0",
		"", // 空 UA：真人浏览器必带 ⇒ 按自动处理，宁少记不误记
	}
	for _, ua := range auto {
		if !isAutomatedUA(ua) {
			t.Errorf("UA %q 应判为自动件，却判成真人 ⇒ 探针会污染使用证据", ua)
		}
	}
	human := []string{chromeUA, "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Safari/605.1.15", "Mozilla/5.0 (X11; Linux x86_64) Firefox/121.0"}
	for _, ua := range human {
		if isAutomatedUA(ua) {
			t.Errorf("UA %q 是真人浏览器却被判成自动 ⇒ 使用证据会被系统性低估", ua)
		}
	}
	// 端到端一次：自动件访问要留下记录但标 human=false（"来过但不是人"同样是有用的证据）
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	srv.handleAdmin(rec, getAs("/admin", "go-http-client/1.1"))
	got := sink.find(visitMsg)
	if len(got) != 1 {
		t.Fatalf("自动件也应记一条（区分 来过 与 人来过），实得 %d", len(got))
	}
	if h, _ := got[0].Fields["human"].(bool); h {
		t.Error("go-http-client 被记成 human=true")
	}
}

// TestCleanUAForLogBlocksLogInjection 钉住 UA 是攻击者可控字符串：
// 不清洗就能用一条请求在日志里伪造出后续整行（包括伪造"有人用浏览器打开过面板"这条结论本身）。
func TestCleanUAForLogBlocksLogInjection(t *testing.T) {
	evil := "Mozilla/5.0" + crLit + lfLit + "{\"msg\":\"面板页面首次被访问\",\"fields\":{\"human\":true}}" + lfLit
	out := cleanUAForLog(evil)
	if strings.ContainsAny(out, crLit+lfLit) {
		t.Errorf("清洗后仍含 CR/LF ⇒ 一行日志可被拆成多行伪造：%q", out)
	}
	if len(out) > 80 {
		t.Errorf("清洗后长度 %d 超过 80 字节上限", len(out))
	}
	// 长度上限的另一半：超长 UA 必须被截断而不是原样落盘
	long := strings.Repeat("A", 300) + chromeUA[len(chromeUA)-1:]
	if got := cleanUAForLog(long); len(got) != 80 {
		t.Errorf("300+ 字节 UA 应截到 80，实得 %d", len(got))
	}
	// 端到端：注入型 UA 落到日志后必须是单行、且不产生第二条"首次访问"
	srv, sink := newVisitServer(t)
	rec := httptest.NewRecorder()
	srv.handleAdmin(rec, getAs("/admin", evil))
	if n := len(sink.find(visitMsg)); n != 1 {
		t.Errorf("注入型 UA 应只留 1 条真实记录，实得 %d ⇒ 伪造成功", n)
	}
	body, _ := json.Marshal(sink.find(visitMsg)[0].Fields)
	if strings.ContainsAny(string(body), crLit+lfLit) {
		t.Errorf("落盘字段里仍有换行：%s", body)
	}
}

// TestPanelVisitsExposedInStatus 钉住"证据要能被看到"：计数若只存在内存里、接口不暴露，
// 等于又要靠翻日志才能回答，而面板本该自己说清（与本仓「停摆必须自证」同一原则）。
func TestPanelVisitsExposedInStatus(t *testing.T) {
	srv, _ := newVisitServer(t)
	rec := httptest.NewRecorder()
	srv.handleAdmin(rec, getAs("/admin", chromeUA))
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-WB-Token", srv.Token())
	sr := httptest.NewRecorder()
	srv.handleStatus(sr, req)
	var payload map[string]any
	if err := json.Unmarshal(sr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("/api/status 返回不是 JSON：%v（体 %s）", err, sr.Body.String()[:120])
	}
	raw, ok := payload["panelVisits"]
	if !ok {
		t.Fatal("/api/status 不含 panelVisits ⇒ 证据对使用者不可见")
	}
	blob, _ := json.Marshal(raw)
	if !strings.Contains(string(blob), "/admin") {
		t.Errorf("panelVisits 里看不到 /admin：%s", blob)
	}
	if strings.Contains(string(blob), srv.Token()) {
		t.Error("panelVisits 泄漏 token")
	}
}

// TestPanelVisitNilLoggerIsSafeButSilent 钉住未注入日志器时的行为边界：
// emit 对 nil 安全（emit 开头的 nil 检查）⇒ 不 panic；但"不 panic"不等于"有证据"，
// 所以计数仍要在（内存里），而日志一条也没有 —— 这条把 main 必须 SetLogger 的理由钉死。
func TestPanelVisitNilLoggerIsSafeButSilent(t *testing.T) {
	srv, _, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.handleAdmin(rec, getAs("/admin", chromeUA))
	if tot := srv.visits.total(); tot != 1 {
		t.Errorf("nil 日志器下计数应仍工作，实得 %d（panic 已被吸收则应为 1）", tot)
	}
}

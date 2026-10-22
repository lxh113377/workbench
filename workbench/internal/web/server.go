package web

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"workbench/internal/config"
	"workbench/internal/engine"
	"workbench/internal/evaluate"
	"workbench/internal/gov"
	"workbench/internal/model"
	"workbench/internal/modelpool"
	"workbench/internal/store"
	"workbench/internal/supervisor"
)

// Server 是工作台 HTTP 服务。
type Server struct {
	layout config.Layout
	cstore *config.Store
	engine *engine.Engine
	gov    *gov.Engine
	// pool 是模型池服务（模型清单/探测/调度策略的配置面；未注入时 /api/pool* 返回 503）。
	pool *modelpool.Service
	// supervisor 用于读取最近资源采样（metrics 面板）。
	supervisor *supervisor.Manager
	hub        *Hub
	token      string
	srv        *http.Server
	ln         net.Listener
	port       int

	mu       sync.Mutex
	starting bool

	// onQuit 是「整体退出」钩子，由 main 注入（做 eng.Stop + srv.Close + 退出）。
	// 放在 web 层只做触发、不自己决定怎么退，避免 web 与 main 的退出语义分叉。
	onQuit func()
}

// NewServer 构造服务（token 每次进程启动随机生成）。
func NewServer(layout config.Layout, cstore *config.Store, eng *engine.Engine, hub *Hub) *Server {
	return &Server{layout: layout, cstore: cstore, engine: eng, hub: hub, token: randomToken()}
}

// SetGovernance 注入治理引擎（治理为可选子系统，未注入时治理接口返回 503）。
func (s *Server) SetGovernance(g *gov.Engine) { s.gov = g }

// SetModelPool 注入模型池服务（模型清单/探测/调度策略的配置面）。
func (s *Server) SetModelPool(p *modelpool.Service) { s.pool = p }

// SetQuitHook 注入「整体退出」钩子（不注入时 /api/engine/quit 返回 503）。
func (s *Server) SetQuitHook(f func()) { s.onQuit = f }

// SetSupervisor 注入资源治理器（/api/metrics 读取其最近采样）。
func (s *Server) SetSupervisor(m *supervisor.Manager) { s.supervisor = m }

// Token 返回本次进程的访问令牌（SPA 由服务端注入）。
func (s *Server) Token() string { return s.token }

// Port 返回实际监听端口。
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// URL 返回面板地址。
func (s *Server) URL() string {
	return fmt.Sprintf("http://%s:%d/", s.cfgHost(), s.Port())
}

func (s *Server) cfgHost() string {
	host := s.cstore.Get().Server.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return host
}

// Listen 启动监听；端口被占用时自动 +1 重试。
//
// 幂等保护（2026-10-19 现场缺陷修复）：
//
// 现场：Server 声明了 mu + starting 两个字段，本意是防「并发/重复启动」，
// 但 starting **从未被读或写**（全库只出现在结构体声明处）——
// 于是重复调用会 net.Listen 成功、覆盖 s.ln/s.port、再起一个 Serve 循环：
// 实测端口从 17892 漂到 17894，且两个监听并存。
//
// 危害：端口漂移会让 panel.url 指向错误实例（现场「重试」事故的同源风险）；
// 双 Serve 让面板响应时有时无。main.go 与 ui_probe.go 都会调用 Listen()，
// 路径不止一条，所以这道保护必须真的生效。
//
// 实现：用 starting 做「已启动」标记（持 mu 读写），已启动则直接返回 nil。
func (s *Server) Listen() error {
	// 先判是否已启动：幂等 —— 重复调用不得换端口、不得起第二个 Serve。
	s.mu.Lock()
	if s.starting {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	cfg := s.cstore.Get()
	host := s.cfgHost()
	base := cfg.Server.Port
	var lastErr error
	for i := 0; i < 10; i++ {
		addr := net.JoinHostPort(host, strconv.Itoa(base+i))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			lastErr = err
			continue
		}
		s.mu.Lock()
		s.ln = ln
		s.port = base + i
		// 置已启动标记：后续重复调用在入口即返回，不再换端口、不再起 Serve。
		s.starting = true
		s.mu.Unlock()
		mux := http.NewServeMux()
		s.routes(mux)
		s.srv = &http.Server{
			Handler:           s.withMiddleware(mux),
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
				_ = err
			}
		}()
		// 写入面板地址与端口，供二次启动/CLI 定位。
		// 注：这里曾把 panel.url 连写两遍（复制粘贴残留），已清理。
		_ = os.WriteFile(filepath.Join(s.layout.RuntimeDir, "panel.url"), []byte(s.URL()), 0o644)
		_ = os.WriteFile(filepath.Join(s.layout.RuntimeDir, "panel.port"), []byte(strconv.Itoa(s.Port())), 0o644)
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("无法分配端口")
	}
	return fmt.Errorf("启动面板服务失败: %w", lastErr)
}

// Close 关闭服务。
func (s *Server) Close() {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(ctx)
	}
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 仅允许回环访问
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// 仅允许同源 Origin
		if origin := r.Header.Get("Origin"); origin != "" {
			if !strings.HasPrefix(origin, "http://"+s.cfgHost()+":") && !strings.HasPrefix(origin, "http://127.0.0.1:") && !strings.HasPrefix(origin, "http://localhost:") {
				http.Error(w, "bad origin", http.StatusForbidden)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// routes 注册全部路由。
func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/admin", s.handleAdmin)
	mux.HandleFunc("/assets/app.css", s.handleCSS)
	mux.HandleFunc("/assets/app.js", s.handleJS)
	mux.HandleFunc("/assets/gov.js", s.handleGovJS)
	// 图标 + 旧浏览器兜底（favicon.go）。2026-10-19 实测回归：两个处理器早已实现，但 routes() 从未注册，页面声明的 /assets/favicon.svg 落到 handleIndex 的 404；
	// 既有 TestPagesDeclareFavicon 只断言 HTML 里有这行 link，所以一直是绿的。
	// 交叉校验见 asset_route_contract_test.go。
	mux.HandleFunc("/assets/favicon.svg", s.handleFavicon)
	mux.HandleFunc("/favicon.ico", s.handleFaviconICO)
	mux.HandleFunc("/api/status", s.guard(s.handleStatus))
	mux.HandleFunc("/api/engine/start", s.guard(s.handleEngineStart))
	mux.HandleFunc("/api/engine/pause", s.guard(s.handleEnginePause))
	mux.HandleFunc("/api/engine/resume", s.guard(s.handleEngineResume))
	mux.HandleFunc("/api/engine/stop", s.guard(s.handleEngineStop))
	// /api/engine/quit 是**整体退出**入口（2026-10-19 现场补齐）：
	// 产品此前没有程序化退出路径，运维换二进制只能强杀。见 handleEngineQuit。
	mux.HandleFunc("/api/engine/quit", s.guard(s.handleEngineQuit))
	mux.HandleFunc("/api/queue", s.guard(s.handleQueue))
	mux.HandleFunc("/api/tasks", s.guard(s.handleTasks))
	mux.HandleFunc("/api/tasks/", s.guard(s.handleTaskDetail))
	mux.HandleFunc("/api/tasks/manual", s.guard(s.handleManualTask))
	mux.HandleFunc("/api/templates", s.guard(s.handleTemplates))
	mux.HandleFunc("/api/templates/", s.guard(s.handleTemplateItem))
	mux.HandleFunc("/api/eval/config", s.guard(s.handleEvalConfig))
	mux.HandleFunc("/api/eval/preview", s.guard(s.handleEvalPreview))
	mux.HandleFunc("/api/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/api/models", s.guard(s.handleModels))
	mux.HandleFunc("/api/export", s.guard(s.handleExport))
	mux.HandleFunc("/api/diagnose", s.guard(s.handleDiagnose))
	mux.HandleFunc("/api/logs", s.guard(s.handleLogs))
	mux.HandleFunc("/api/metrics", s.guard(s.handleMetrics))
	mux.HandleFunc("/api/failures", s.guard(s.handleFailures))
	// P1-3：Prometheus 文本格式导出（外部监控系统直抓），与 /api/metrics 同源同口径。
	mux.HandleFunc("/metrics", s.guard(s.handleMetricsProm))
	mux.HandleFunc("/api/autonomy", s.guard(s.handleAutonomySummary))
	mux.HandleFunc("/api/autonomy/decisions", s.guard(s.handleAutonomyDecisions))
	mux.HandleFunc("/api/autonomy/goal", s.guard(s.handleAutonomyGoalSet))
	mux.HandleFunc("/api/autonomy/resume", s.guard(s.handleAutonomyResume))
	mux.HandleFunc("/api/autostart", s.guard(s.handleAutostart))
	// 工作目录（AI 开发功能扩展）：可选外部目录模式的读写入口。
	mux.HandleFunc("/api/workspace", s.guard(s.handleWorkspace))
	mux.HandleFunc("/api/workspace/validate", s.guard(s.handleWorkspaceValidate))
	mux.HandleFunc("/api/workspace/select", s.guard(s.handleWorkspaceSelect))
	mux.HandleFunc("/api/workspace/reset", s.guard(s.handleWorkspaceReset))
	mux.HandleFunc("/api/workspace/audit", s.guard(s.handleWorkspaceAudit))
	mux.HandleFunc("/api/workspace/browse", s.guard(s.handleWorkspaceBrowse))
	mux.HandleFunc("/modelpool", s.handleModelPoolPage)
	mux.HandleFunc("/assets/pool.js", s.handlePoolJS)
	mux.HandleFunc("/api/pool", s.guard(s.handlePoolStatus))
	mux.HandleFunc("/api/pool/sync", s.guard(s.handlePoolSync))
	mux.HandleFunc("/api/pool/probe", s.guard(s.handlePoolProbe))
	mux.HandleFunc("/api/pool/models", s.guard(s.handlePoolModels))
	mux.HandleFunc("/api/pool/models/", s.guard(s.handlePoolModelItem))
	mux.HandleFunc("/api/pool/lane", s.guard(s.handlePoolLane))
	mux.HandleFunc("/api/pool/purge", s.guard(s.handlePoolPurge))
	mux.HandleFunc("/api/pool/reorder", s.guard(s.handlePoolReorder))
	mux.HandleFunc("/api/pool/strategy", s.guard(s.handlePoolStrategy))
	mux.HandleFunc("/api/pool/log", s.guard(s.handlePoolLog))
	mux.HandleFunc("/governance", s.handleGovernancePage)
	mux.HandleFunc("/api/gov/summary", s.guard(s.handleGovSummary))
	mux.HandleFunc("/api/gov/contracts", s.guard(s.handleGovContracts))
	mux.HandleFunc("/api/gov/contracts/", s.guard(s.handleGovContractItem))
	mux.HandleFunc("/api/gov/budget", s.guard(s.handleGovBudget))
	mux.HandleFunc("/api/gov/sample", s.guard(s.handleGovSample))
	mux.HandleFunc("/api/gov/recompute", s.guard(s.handleGovRecompute))
	mux.HandleFunc("/events", s.handleSSE)
}

// guard 校验 token（写操作与读操作统一要求，SPA 由服务端注入）。
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("X-WB-Token")
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok != s.token {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func ok(w http.ResponseWriter, v any) { writeJSON(w, http.StatusOK, v) }

func bad(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": msg})
}

func readBody(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("请求体为空")
	}
	return json.Unmarshal(body, v)
}

func randomToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

// ── 页面 ─────────────────────────────────────────────────────────

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, indexHTML(s.token, s.engine.Phase()))
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, adminHTML(s.token))
}

func (s *Server) handleCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = io.WriteString(w, appCSS)
}

func (s *Server) handleJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(w, appJS)
}

func (s *Server) handleGovJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(w, govJS)
}

// ── API ─────────────────────────────────────────────────────────

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := s.cstore.Get()
	st := s.engine.Stats()
	ok(w, map[string]any{
		"phase":     s.engine.Phase(),
		"stats":     st,
		"queueLen":  len(s.engine.QueueSnapshot()),
		"config":    map[string]any{"concurrency": cfg.Executor.Concurrency, "port": s.Port()},
		"lastError": s.engine.LastError(),
		"now":       model.Now(),
		"models":    s.engine.RouterHealth(),
		"startedAt": s.engine.Stats().TotalTasks,
		// 限流状态自证：上游 429 时工作台会"运行中但 0 产出"，
		// 必须让面板能说清"我为什么没在干活"（否则 24/7 系统会静默停摆）。
		"rateLimit": s.engine.RateLimitStatus(),
		// 停摆原因自证（2026-10-21 现场）：用户报告的正是"运行中但今日产出 0"。
		// 只报 running + 0 而不说为什么，等于把排查成本全推给人；
		// stallReason 把"为什么现在没有产出"归纳成稳定原因码 + 一句人话。
		"stallReason": s.engine.StallReason(),
	})
}

func (s *Server) handleEngineStart(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Start(); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"phase": s.engine.Phase()})
}

func (s *Server) handleEnginePause(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Pause(); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"phase": s.engine.Phase()})
}

func (s *Server) handleEngineResume(w http.ResponseWriter, r *http.Request) {
	if err := s.engine.Resume(); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"phase": s.engine.Phase()})
}

func (s *Server) handleEngineStop(w http.ResponseWriter, r *http.Request) {
	go func() { _ = s.engine.Stop() }()
	ok(w, map[string]any{"phase": "stopping"})
}

// handleEngineQuit 触发**整体退出**（进程结束），而不只是停引擎。
//
// 语义分工：
//
//	/api/engine/stop —— 停引擎，进程与服务继续（用户可再点「开始」）；
//	/api/engine/quit —— 整体退出，交给 main 注入的钩子走优雅路径。
//
// 为什么不在 web 层直接 os.Exit：退出要落盘相位、关服务、等 drain，
// 那是 main 的职责；web 只负责「被请求了」这件事。
func (s *Server) handleEngineQuit(w http.ResponseWriter, r *http.Request) {
	if s.onQuit == nil {
		http.Error(w, "quit hook not installed", http.StatusServiceUnavailable)
		return
	}
	// 先落「用户主动退出」标记（2026-10-19 现场）：
	// 外部守护者（tests/keepalive.ps1）原本把 stopped/idle 一律拉回 running，
	// 区分不出「意外停机」与「用户主动退出」——于是 --quit 后 60 秒又被拉起。
	// 产品侧先表达出来，外部才有据可依；Start() 时会清掉它。
	if err := s.engine.MarkUserQuit(); err != nil {
		http.Error(w, "mark user quit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ok(w, map[string]any{"quitting": true})
	go s.onQuit()
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"tasks": s.engine.QueueSnapshot()})
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	domain := q.Get("domain")
	limit := atoiDefault(q.Get("limit"), 500)
	var out []*model.Task
	for _, t := range s.engine.Store().List() {
		if state != "" && string(t.State) != state {
			continue
		}
		if domain != "" && t.Domain != domain {
			continue
		}
		out = append(out, t)
	}
	// 倒序：最新在前
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	total := len(out)
	if len(out) > limit {
		out = out[:limit]
	}
	ok(w, map[string]any{"tasks": out, "total": total, "truncated": total > len(out)})
}

func (s *Server) handleTaskDetail(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if rest == "" {
		bad(w, "缺少任务 ID")
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	if len(parts) > 1 {
		s.handleTaskAction(w, r, id, strings.Join(parts[1:], "/"))
		return
	}
	t, found := s.engine.Store().Get(id)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "任务不存在"})
		return
	}
	payload := map[string]any{"task": t}
	// 主产出按三级回落解析（OutputFile 绝对路径 → 目录 final.md → 目录最大 .md/.txt）。
	// 此前只读 final.md，而产出名由 choosePrimary 按格式与体积决定（如 article.md），
	// 于是绝大多数已完成任务在面板显示「（暂无产出）」——实测 5935B 的任务只显 6 字符。
	if content, from, ok := readTaskArtifact(s.engine.Store(), t); ok {
		payload["content"] = content
		payload["contentFrom"] = from
	}
	if events, err := os.ReadFile(s.layout.TaskFile(id, "events.jsonl")); err == nil {
		payload["events"] = tailLines(string(events), 200)
	}
	// P3 验收证据：面板必须能看到"它到底真的跑通过了没有"。
	// 历史任务没有这些文件时字段缺省，前端按"未验收"展示，而不是假装通过。
	if data, err := os.ReadFile(s.layout.TaskFile(id, "artifacts-index.json")); err == nil {
		payload["artifacts"] = json.RawMessage(data)
	}
	if data, err := os.ReadFile(s.layout.TaskFile(id, "verify.json")); err == nil {
		payload["verify"] = json.RawMessage(data)
	}
	payload["verified"] = t.Verified
	payload["repairRounds"] = t.RepairRounds
	// 双角色闭环证据：面板必须能看出"这一轮是思考模型出的方案，还是退化直执行"。
	//
	// 为什么这不能只靠日志：'思考模型只出方案' 这条要求若没有可观测面，
	// 退化直执行会安静地变成常态（方案模型限流 8 秒无输出 -> 每天都直执行），
	// 而面板仍然显示一切正常 —— 这正是"无法闭环"的典型形态。
	payload["planRounds"] = t.PlanRounds
	payload["planDegraded"] = t.PlanDegraded
	payload["planSummary"] = t.PlanSummary
	payload["planModel"] = t.PlanModel
	if plans := listPlanEvidence(s.layout.TaskDir(id)); len(plans) > 0 {
		payload["planEvidence"] = plans
	}
	if rounds := listRepairEvidence(s.layout.TaskDir(id)); len(rounds) > 0 {
		payload["repairEvidence"] = rounds
	}
	ok(w, payload)
}

// listPlanEvidence 列出任务目录 plans/ 下的方案证据文件（每轮方案与提示词）。
//
// 只回文件名与体积，面板按需再取全文（与 listRepairEvidence 同一口径）。
func listPlanEvidence(taskDir string) []map[string]any {
	dir := filepath.Join(taskDir, "plans")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		out = append(out, map[string]any{"name": e.Name(), "bytes": info.Size()})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}

// listRepairEvidence 列出任务目录里的返工证据文件（每轮验收与返工 prompt）。
// 只回文件名与体积，面板按需再取全文，避免详情接口过大。
func listRepairEvidence(taskDir string) []map[string]any {
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "verify-round-") && !strings.HasPrefix(name, "prompt.repair-") {
			continue
		}
		if info, ierr := e.Info(); ierr == nil {
			out = append(out, map[string]any{"name": name, "bytes": info.Size()})
		}
	}
	return out
}

func (s *Server) handleTaskAction(w http.ResponseWriter, r *http.Request, id, action string) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Priority int  `json:"priority"`
		Pinned   bool `json:"pinned"`
		Rating   int  `json:"rating"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	switch action {
	case "cancel":
		if err := s.engine.CancelTask(id); err != nil {
			bad(w, err.Error())
			return
		}
	case "retry":
		if err := s.engine.RetryTask(id); err != nil {
			bad(w, err.Error())
			return
		}
	case "priority":
		if err := s.engine.SetPriority(id, body.Priority, body.Pinned); err != nil {
			bad(w, err.Error())
			return
		}
	case "rate":
		if err := s.engine.Rate(id, body.Rating); err != nil {
			bad(w, err.Error())
			return
		}
	default:
		bad(w, "未知操作: "+action)
		return
	}
	ok(w, map[string]any{"ok": true})
}

func (s *Server) handleManualTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Title    string `json:"title"`
		Prompt   string `json:"prompt"`
		Domain   string `json:"domain"`
		Priority int    `json:"priority"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Prompt) == "" {
		bad(w, "标题与任务描述不能为空")
		return
	}
	t, err := s.engine.EnqueueManual(body.Title, body.Prompt, body.Domain, body.Priority)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"task": t, "warning": err.Error()})
		return
	}
	ok(w, map[string]any{"task": t})
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, map[string]any{"templates": s.engine.Templates().List()})
	case http.MethodPost:
		var t model.Template
		if err := readBody(r, &t); err != nil {
			bad(w, err.Error())
			return
		}
		if err := s.engine.Templates().Upsert(t); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"ok": true})
	default:
		bad(w, "不支持的方法")
	}
}

func (s *Server) handleTemplateItem(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/templates/")
	if id == "" {
		bad(w, "缺少模板 ID")
		return
	}
	if id == "reset" && r.Method == http.MethodPost {
		if err := s.engine.Templates().ResetDefaults(); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"ok": true})
		return
	}
	if id == "export" && r.Method == http.MethodGet {
		data, err := s.engine.Templates().ExportJSON()
		if err != nil {
			bad(w, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=templates.json")
		_, _ = w.Write(data)
		return
	}
	switch r.Method {
	case http.MethodGet:
		t, found := s.engine.Templates().Get(id)
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "模板不存在"})
			return
		}
		ok(w, map[string]any{"template": t})
	case http.MethodDelete:
		if err := s.engine.Templates().Delete(id); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"ok": true})
	default:
		bad(w, "不支持的方法")
	}
}

func (s *Server) handleEvalConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, map[string]any{"eval": s.cstore.Get().Eval})
	case http.MethodPut, http.MethodPost:
		var body config.EvalConfig
		if err := readBody(r, &body); err != nil {
			bad(w, err.Error())
			return
		}
		if _, err := s.cstore.Update(func(c *config.Config) { c.Eval = body }); err != nil {
			bad(w, err.Error())
			return
		}
		s.engine.ReloadConfig()
		ok(w, map[string]any{"eval": s.cstore.Get().Eval})
	default:
		bad(w, "不支持的方法")
	}
}

func (s *Server) handleEvalPreview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
		Domain string `json:"domain"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	tmp := &model.Task{ID: "preview", Title: body.Title, Prompt: body.Prompt, Domain: body.Domain, CreatedAt: model.Now()}
	scores := s.engine.Evaluator().Evaluate(evaluateInput(tmp, s.engine))
	ok(w, map[string]any{"scores": scores})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ok(w, map[string]any{"config": s.cstore.Get(), "port": s.Port()})
	case http.MethodPut, http.MethodPost:
		var body config.Config
		if err := readBody(r, &body); err != nil {
			bad(w, err.Error())
			return
		}
		cfg, err := s.cstore.Replace(body)
		if err != nil {
			bad(w, err.Error())
			return
		}
		s.engine.ReloadConfig()
		ok(w, map[string]any{"config": cfg})
	default:
		bad(w, "不支持的方法")
	}
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ok(w, map[string]any{"health": s.engine.RouterHealth(), "routes": s.cstore.Get().Models})
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request) {
	cfg := s.cstore.Get()
	res := map[string]any{
		"layout":  s.layout,
		"phase":   s.engine.Phase(),
		"config":  cfg,
		"models":  s.engine.RouterHealth(),
		"stats":   s.engine.Stats(),
		"port":    s.Port(),
		"clients": s.hub.Clients(),
		// 请求速率闸门状态（2026-10-19 事故后新增）：
		// 让「限速是否真的在工作」成为可读读数，而不是靠日志猜。
		"rateLimiter": s.engine.RateLimiterSnapshot(),
		// 执行器配置里的限速参数（与上面 snapshot 对照，确认配置已生效）。
		"requestsPerMinute": cfg.Executor.RequestsPerMinute,
		"requestBurst":      cfg.Executor.RequestBurst,
	}
	if _, err := os.Stat(s.layout.DSHHome); err != nil {
		res["dshHomeError"] = err.Error()
	}
	ok(w, res)
}

// MetricsSnapshot 是 /api/metrics 的响应（P1-3：metrics.csv 之外给面板实时读数）。
type MetricsSnapshot struct {
	At        int64   `json:"at"`
	HeapMB    float64 `json:"heapMB"`
	AllocMB   float64 `json:"allocMB"`
	Goroutine int     `json:"goroutine"`
	Handles   int     `json:"handles"`
	NumGC     uint32  `json:"numGC"`
	// 任务侧聚合
	Tasks       int     `json:"tasks"`
	Done        int     `json:"done"`
	Failed      int     `json:"failed"`
	SuccessRate float64 `json:"successRate"`
	AvgSeconds  float64 `json:"avgSeconds"`
	// 台账摘要
	Attempts []store.AttemptRecord `json:"attempts"`
	// 资源上限（用于面板画阈值线）
	MemLimitMB  int `json:"memLimitMB"`
	HandleLimit int `json:"handleLimit"`
}

// handleMetrics 返回实时资源与任务聚合指标（含最近尝试台账）。
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	cfg := s.cstore.Get()
	st := s.engine.Stats()
	snap := MetricsSnapshot{
		At:          model.Now(),
		Tasks:       st.TotalTasks,
		Done:        st.DoneTasks,
		Failed:      st.FailedTasks,
		SuccessRate: st.SuccessRate,
		AvgSeconds:  st.AvgSeconds,
		Attempts:    s.engine.AttemptSummary(30),
		MemLimitMB:  cfg.Supervisor.MemLimitMB,
		HandleLimit: cfg.Supervisor.HandleLimit,
	}
	// 资源读数来自 supervisor 的最近一次采样（未启动采样时为零值，面板按"暂无采样"展示）
	if s.supervisor != nil {
		samples := s.supervisor.Samples()
		if len(samples) > 0 {
			last := samples[len(samples)-1]
			snap.HeapMB, snap.AllocMB = last.HeapMB, last.AllocMB
			snap.Goroutine, snap.Handles, snap.NumGC = last.Goroutine, last.Handles, last.NumGC
			snap.At = last.At
		}
	}
	ok(w, snap)
}

// handleFailures 返回失败原因分布（P1-4：让 P0-1 修复的 ErrorKind 真正可见）。
func (s *Server) handleFailures(w http.ResponseWriter, r *http.Request) {
	buckets := s.engine.FailureBreakdown()
	total := 0
	for _, b := range buckets {
		total += b.Count
	}
	ok(w, map[string]any{
		"buckets": buckets,
		"total":   total,
		"empty":   total == 0,
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	tail := atoiDefault(r.URL.Query().Get("tail"), 200)
	name := r.URL.Query().Get("file")
	if name == "" {
		name = "audit.jsonl"
	}
	if strings.ContainsAny(name, "\\/:*?\"<>|") {
		bad(w, "非法文件名")
		return
	}
	data, err := os.ReadFile(s.layout.LogFile(name))
	if err != nil {
		ok(w, map[string]any{"lines": []string{}})
		return
	}
	ok(w, map[string]any{"lines": tailLines(string(data), tail)})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("token") != s.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	flusher, okf := w.(http.Flusher)
	if !okf {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.hub.Subscribe()
	defer cancel()
	enc := json.NewEncoder(w)
	send := func(ev model.Event) bool {
		if _, err := io.WriteString(w, "data: "); err != nil {
			return false
		}
		if err := enc.Encode(ev); err != nil {
			return false
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	// 握手：先推一条 ready
	if !send(model.Event{Type: "ready", At: model.Now(), Msg: "connected"}) {
		return
	}
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			if !send(ev) {
				return
			}
		case <-ping.C:
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func tailLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// evaluateInput 供预览使用。
func evaluateInput(t *model.Task, eng *engine.Engine) evaluate.Input {
	var tpl *model.Template
	if t.TemplateID != "" {
		if x, found := eng.Templates().Get(t.TemplateID); found {
			tpl = &x
		}
	}
	return evaluate.Input{Task: t, Template: tpl, History: eng.Store().List(), Now: model.Now(), DailyCount: 0}
}

var _ = filepath.Join
var _ = store.AuditRecord{}

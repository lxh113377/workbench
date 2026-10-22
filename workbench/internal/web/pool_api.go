package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"workbench/internal/modelpool"
)

// ── 模型池 API ──────────────────────────────────────────────────
//
// 与 /api/models（只读展示 modelroute 健康度）互补：这里是模型池的「配置面」，
// 支持增删模型、调优先级、切调度策略、立即同步/探测，以及查看状态日志。
// 密钥一律不回显明文，只给「是否已配置」与后四位指纹。

// ensurePool 统一处理「模型池未注入」的情况。
func (s *Server) ensurePool(w http.ResponseWriter) bool {
	if s.pool == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "模型池未启用"})
		return false
	}
	return true
}

// handlePoolStatus 返回模型池全量视图。
func (s *Server) handlePoolStatus(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodGet {
		bad(w, "仅支持 GET")
		return
	}
	st := s.pool.Status()
	// 密钥脱敏：只暴露指纹。
	if specs, okf := st["specs"].([]modelpool.Spec); okf {
		safe := make([]map[string]any, 0, len(specs))
		for _, sp := range specs {
			safe = append(safe, redactSpec(sp))
		}
		st["specs"] = safe
	}
	st["chain"] = s.pool.TopAvailable(10)
	ok(w, st)
}

// redactSpec 把 Spec 转成脱敏后的 map（密钥明文不出本进程）。
func redactSpec(sp modelpool.Spec) map[string]any {
	return map[string]any{
		"id": sp.ID, "label": sp.Label, "provider": sp.Provider, "lane": sp.Lane,
		"baseUrl": sp.BaseURL, "model": sp.Model, "format": sp.Format, "tier": sp.Tier,
		"group": sp.Group, "priority": sp.Priority, "weight": sp.Weight,
		"enabled": sp.Enabled, "vision": sp.Vision, "contextWindow": sp.ContextWindow,
		"maxOutput": sp.MaxOutput, "maxFail": sp.MaxFail, "cooldownSec": sp.CooldownSec,
		"source": sp.Source, "note": sp.Note, "routable": sp.Routable,
		// autoDisabled 要回显：界面必须区分「用户手动停用」与
		// 「上游判死自动停用（网关重新广播后会自动恢复）」，
		// 否则两种状态在面板上长得一样，用户不知道该不该手动启用。
		"autoDisabled": sp.AutoDisabled,
		"hasKey":       sp.APIKey != "", "keyHint": keyHint(sp.APIKey),
		"updatedAt": sp.UpdatedAt,
		"state":     sp.HealthState(), "cooldownTo": sp.HealthCooldown(),
	}
}

func keyHint(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 4 {
		return "…" + k
	}
	return "…" + k[len(k)-4:]
}

// handlePoolSync 立即从 OFM 渠道网关同步模型清单。
func (s *Server) handlePoolSync(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	res := s.pool.SyncNow("panel")
	s.pool.Manager().NotifyRoutes()
	ok(w, map[string]any{"sync": res, "status": s.pool.Status()["gateway"]})
}

// handlePoolProbe 立即执行一轮可用性探测。
func (s *Server) handlePoolProbe(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	sum := s.pool.ProbeNow()
	ok(w, map[string]any{"probe": sum})
}

// handlePoolModels 模型清单的增（POST）/查（GET）。
func (s *Server) handlePoolModels(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		ok(w, map[string]any{"models": s.pool.Status()["specs"]})
	case http.MethodPost:
		var body modelpool.Spec
		if err := readBody(r, &body); err != nil {
			bad(w, err.Error())
			return
		}
		sp, err := s.pool.Manager().Upsert(body, "panel")
		if err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"model": redactSpec(sp)})
	default:
		bad(w, "不支持的方法")
	}
}

// handlePoolModelItem 单模型操作：PUT 编辑 / DELETE 删除 / POST <action>。
// 路径形态：/api/pool/models/<id>[/enable|disable|restore]
func (s *Server) handlePoolModelItem(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/pool/models/")
	if rest == "" {
		bad(w, "缺少模型 ID")
		return
	}
	// 模型 id 本身含斜杠（OFM 真实形态如 vendor/model-x），不能按 / 切分取 parts[0]
	// —— 实测会把 vendor/model-x 截成 vendor，删除报"模型不存在"。
	//
	// 约定：末尾若是已知动作词，则它是 action；否则整段都是 id。
	// 动作词必须列全 —— 只认 edit 会让 /<id>/disable 之类把 "disable" 留在 id 里，
	// 落到 default 分支报「未知操作」（容器 e2e 实测：停用/启用/恢复三条全 400）。
	rest = strings.TrimSuffix(rest, "/")
	idPart := rest
	action := ""
	knownActions := map[string]bool{"enable": true, "disable": true, "restore": true, "probe": true, "edit": true}
	if i := strings.LastIndex(rest, "/"); i > 0 {
		if tail := rest[i+1:]; knownActions[tail] {
			action = tail
			idPart = rest[:i]
		}
	}
	id, err := url.PathUnescape(idPart)
	if err != nil {
		bad(w, "模型 ID 非法")
		return
	}
	mgr := s.pool.Manager()
	switch {
	case r.Method == http.MethodDelete:
		if err := mgr.Delete(id, "panel"); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"ok": true, "deleted": id})
	case r.Method == http.MethodPut || (r.Method == http.MethodPost && action == "edit"):
		var body modelpool.Spec
		if err := readBody(r, &body); err != nil {
			bad(w, err.Error())
			return
		}
		body.ID = id
		sp, err := mgr.Upsert(body, "panel")
		if err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"model": redactSpec(sp)})
	case r.Method == http.MethodPost:
		switch action {
		case "enable":
			err = mgr.SetEnabled(id, true, "panel")
		case "disable":
			err = mgr.SetEnabled(id, false, "panel")
		case "restore":
			err = mgr.Restore(id, "panel")
		case "probe":
			s.pool.ProbeNow()
		default:
			bad(w, "未知操作: "+action+"（可用 enable/disable/restore/probe）")
			return
		}
		if err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"ok": true, "action": action, "id": id})
	default:
		bad(w, "不支持的方法")
	}
}

// handlePoolLane 免费池的一键启用/一键停用。
// body {"lane":"free|kilo|eac|aggregate|custom","group":"OFM 免费","enabled":true}
// lane 与 group 至少给一个（面板按分组卡发 group，CLI/脚本可发 lane）。
func (s *Server) handlePoolLane(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		Lane    string `json:"lane"`
		Group   string `json:"group"`
		Enabled *bool  `json:"enabled"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	if body.Enabled == nil {
		bad(w, "缺少 enabled（true=一键启用 / false=一键停用）")
		return
	}
	if strings.TrimSpace(body.Lane) == "" && strings.TrimSpace(body.Group) == "" {
		bad(w, "缺少 lane 或 group（要操作哪个池）")
		return
	}
	res, err := s.pool.Manager().SetPoolEnabled(body.Lane, body.Group, *body.Enabled, "panel")
	if err != nil {
		bad(w, err.Error())
		return
	}
	s.pool.Manager().NotifyRoutes()
	ok(w, map[string]any{"toggle": res})
}

// handlePoolPurge 清理网关专属/网关其它车道的存量条目（老大 2026-10-22 令：
// 模型池只留 eac/kilo/ofm 等免费车道）。同步时会自动做，这里给人工/脚本兜底。
func (s *Server) handlePoolPurge(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	res := s.pool.Manager().PurgeGatewayLanes("panel")
	s.pool.Manager().NotifyRoutes()
	ok(w, map[string]any{"purged": res})
}

// handlePoolReorder 调整优先级：body {"ids":["a","b",...]}（最高优先级在前）。
func (s *Server) handlePoolReorder(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 POST")
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	if len(body.IDs) == 0 {
		bad(w, "ids 不能为空")
		return
	}
	if err := s.pool.Manager().Reorder(body.IDs, "panel"); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"ok": true, "count": len(body.IDs)})
}

// handlePoolStrategy 切换调度策略：body {"strategy":"priority|round-robin|weighted|least-loaded"}。
func (s *Server) handlePoolStrategy(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	var body struct {
		Strategy modelpool.Strategy `json:"strategy"`
	}
	if r.Method == http.MethodGet {
		ok(w, map[string]any{"strategy": s.pool.Scheduler().Strategy()})
		return
	}
	if r.Method != http.MethodPost {
		bad(w, "仅支持 GET/POST")
		return
	}
	if err := readBody(r, &body); err != nil {
		bad(w, err.Error())
		return
	}
	if err := s.pool.SetStrategy(body.Strategy); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"strategy": s.pool.Scheduler().Strategy()})
}

// handlePoolLog 返回状态日志（默认 200 行）。
func (s *Server) handlePoolLog(w http.ResponseWriter, r *http.Request) {
	if !s.ensurePool(w) {
		return
	}
	tail := atoiDefault(r.URL.Query().Get("tail"), 200)
	ok(w, map[string]any{"lines": s.pool.LogTail(tail)})
}

// poolJSON 是调试用的 JSON 序列化（保持与服务端一致）。
func poolJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

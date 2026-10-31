package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"workbench/internal/gov"
)

// governanceAvailable 判断治理子系统是否可用（未注入时返回 503，禁止静默空成功）。
func (s *Server) governanceAvailable(w http.ResponseWriter) bool {
	if s.gov == nil || s.gov.Store() == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "治理子系统未启用"})
		return false
	}
	return true
}

func (s *Server) handleGovernancePage(w http.ResponseWriter, r *http.Request) {
	// 治理页（govJS 那页）
	s.recordVisit(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(governanceHTML(s.token)))
}

// handleGovSummary 返回治理总览（契约 + 体量 + 预警 + 调整 + 联动 + 考核）。
func (s *Server) handleGovSummary(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	ok(w, s.gov.Store().Summary(time.Now()))
}

// handleGovContracts 列表 / 新增。
func (s *Server) handleGovContracts(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		ok(w, map[string]any{"contracts": gov.SortedByStage(s.gov.Store().Contracts())})
	case http.MethodPost:
		var c gov.Contract
		if err := readBody(r, &c); err != nil {
			bad(w, err.Error())
			return
		}
		saved, err := s.gov.Store().UpsertContract(c)
		if err != nil {
			bad(w, err.Error())
			return
		}
		// 新增契约后立刻重算，保证面板与预警同一时刻口径
		s.gov.Store().Recompute(time.Now())
		ok(w, saved)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

// handleGovContractItem 处理 /api/gov/contracts/<id> 与 /<id>/transition。
func (s *Server) handleGovContractItem(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/gov/contracts/")
	if strings.TrimSpace(rest) == "" {
		bad(w, "缺少契约 ID")
		return
	}
	parts := strings.Split(rest, "/")
	id := parts[0]
	if len(parts) > 1 && parts[1] == "transition" {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		var req struct {
			To    string `json:"to"`
			Actor string `json:"actor"`
			Note  string `json:"note"`
			At    string `json:"at"`
		}
		if err := readBody(r, &req); err != nil {
			bad(w, err.Error())
			return
		}
		c, err := s.gov.Store().TransitionContract(id, gov.ContractStage(req.To), req.Actor, req.Note, req.At)
		if err != nil {
			bad(w, err.Error())
			return
		}
		s.gov.Store().Recompute(time.Now())
		ok(w, c)
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, found := s.gov.Store().Contract(id)
		if !found {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "契约不存在"})
			return
		}
		ok(w, map[string]any{"contract": c, "compliance": gov.ComplianceScore(c), "missing": gov.MissingDuties(c)})
	case http.MethodDelete:
		if err := s.gov.Store().DeleteContract(id); err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"deleted": id})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

// handleGovBudget 读取 / 更新体量阈值（更新必须回显生效值）。
func (s *Server) handleGovBudget(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		b := s.gov.Store().Budget()
		ok(w, map[string]any{"budget": b, "defaults": gov.DefaultBudget()})
	case http.MethodPost:
		var b gov.Budget
		if err := readBody(r, &b); err != nil {
			bad(w, err.Error())
			return
		}
		got, err := s.gov.Store().SetBudget(b)
		if err != nil {
			bad(w, err.Error())
			return
		}
		ok(w, map[string]any{"budget": got, "effective": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
	}
}

// handleGovSample 立即采样一次（面板按钮与 e2e 用）。
func (s *Server) handleGovSample(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	sum := s.gov.RunOnce(time.Now())
	ok(w, sum)
}

// handleGovRecompute 重算预警 / 调整 / 联动（不新增采样）。
func (s *Server) handleGovRecompute(w http.ResponseWriter, r *http.Request) {
	if !s.governanceAvailable(w) {
		return
	}
	alerts, adjust, lr := s.gov.Store().Recompute(time.Now())
	ok(w, map[string]any{"alerts": alerts, "adjustments": adjust, "linkage": lr})
}

// 保证 strconv 在后续扩展里可用（避免再次出现未用导入）。
var _ = strconv.Itoa

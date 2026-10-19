package web

// 本文件是「自主运行」子系统的 HTTP 出口：面板「自主」页签与外部集成共用同一组 API。
//
// 只读接口（GET）与写接口（POST）分开，写接口必须显式传参并留决策台账，
// 与治理子系统的「自动调整不自动改阈值」思路一致：人可以给方向，但动作要留痕。

import (
	"net/http"
	"strings"

	"workbench/internal/autonomy"
)

// handleAutonomySummary 返回自主总览：当前目标、进度、阶段、最近决策。
func (s *Server) handleAutonomySummary(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("decisions"), 30)
	sum := s.engine.AutonomySummary(limit)
	ok(w, map[string]any{
		"enabled":    sum.Enabled,
		"active":     sum.Active,
		"activeId":   sum.ActiveID,
		"stage":      sum.Stage,
		"progress":   sum.Progress,
		"goals":      sum.Goals,
		"decisions":  sum.Decisions,
		"statusLine": s.engine.AutonomyStatusLine(),
	})
}

// handleAutonomyDecisions 返回决策台账（可按目标过滤）。
func (s *Server) handleAutonomyDecisions(w http.ResponseWriter, r *http.Request) {
	limit := atoiDefault(r.URL.Query().Get("limit"), 100)
	goalID := strings.TrimSpace(r.URL.Query().Get("goalId"))
	sum := s.engine.AutonomySummary(limit)
	decs := sum.Decisions
	if goalID != "" {
		filtered := make([]autonomy.Decision, 0, len(decs))
		for _, d := range decs {
			if d.GoalID == goalID {
				filtered = append(filtered, d)
			}
		}
		decs = filtered
	}
	ok(w, map[string]any{"decisions": decs, "total": len(decs)})
}

// handleAutonomyGoalSet 人工设置/切换项目目标。
//
// 请求体：{"title":"...","intent":"...","domain":"content"}
func (s *Server) handleAutonomyGoalSet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title  string `json:"title"`
		Intent string `json:"intent"`
		Domain string `json:"domain"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, "请求体解析失败: "+err.Error())
		return
	}
	g, err := s.engine.SetAutonomyGoal(strings.TrimSpace(body.Title), strings.TrimSpace(body.Intent), strings.TrimSpace(body.Domain))
	if err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"goal": g})
}

// handleAutonomyResume 人工解锁一个 blocked 目标。
func (s *Server) handleAutonomyResume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GoalID string `json:"goalId"`
	}
	if err := readBody(r, &body); err != nil {
		bad(w, "请求体解析失败: "+err.Error())
		return
	}
	if strings.TrimSpace(body.GoalID) == "" {
		bad(w, "缺少 goalId")
		return
	}
	if err := s.engine.ResumeAutonomyGoal(strings.TrimSpace(body.GoalID)); err != nil {
		bad(w, err.Error())
		return
	}
	ok(w, map[string]any{"ok": true})
}

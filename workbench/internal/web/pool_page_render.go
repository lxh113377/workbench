package web

import (
	"io"
	"net/http"
)

// handleModelPoolPage 渲染模型池控制台。
func (s *Server) handleModelPoolPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, modelPoolPageHTML(s.token))
}

// modelPoolPageHTML 注入 token 并生成页面。
func modelPoolPageHTML(token string) string {
	return injectFmt(modelPoolPage, token, token, tokenScript(token))
}

// injectFmt 是带 token 转义的简单模板替换。
//
// 2026-10-19 现场修复：这里原来按「第一个 %」无条件替换，而模型池页面新加的响应式 CSS
// 里含 `width: 100%;` —— 第一个 % 后面跟的是分号不是 s，于是 token 被塞进 CSS，
// 页面上的 `width: 100;<token>` 直接把样式表和 token 一起泄进 DOM，
// 结果前端拿到的 __WB_TOKEN__ 是空串、所有 /api/pool 请求 401。
// 硬性约定：只替换 `%s`，其它 % 一律跳过（CSS 百分比是合法内容，不得被当成占位符）。
func injectFmt(tpl string, pairs ...string) string {
	out := tpl
	for _, v := range pairs {
		idx := indexPlaceholder(out)
		if idx < 0 {
			break
		}
		out = out[:idx] + v + out[idx+2:]
	}
	return out
}

// indexPlaceholder 返回下一个 %s 的下标；找不到返回 -1。
func indexPlaceholder(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '%' && s[i+1] == 's' {
			return i
		}
	}
	return -1
}

// handlePoolJS 输出模型池前端脚本。
func (s *Server) handlePoolJS(w http.ResponseWriter, r *http.Request) {
	serveAsset(w, r, "application/javascript; charset=utf-8", poolJS)
}

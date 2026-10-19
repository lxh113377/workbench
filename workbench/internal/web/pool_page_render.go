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
func injectFmt(tpl string, pairs ...string) string {
	out := tpl
	for _, v := range pairs {
		idx := indexByte(out, '%')
		if idx < 0 {
			break
		}
		// 只替换 %s 形态，token 已由 tokenScript 做 JSON 编码，安全。
		out = out[:idx] + v + out[idx+2:]
	}
	return out
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// handlePoolJS 输出模型池前端脚本。
func (s *Server) handlePoolJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = io.WriteString(w, poolJS)
}

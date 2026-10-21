package web

import (
	"io"
	"net/http"
)

// faviconSVG 是内嵌的站点图标（零外部请求、零额外文件：随 EXE 一起发布）。
//
// 为什么必须内嵌：面板是本地单文件 EXE，不能依赖外部文件系统路径；
// 同时浏览器默认会请求 /favicon.ico，缺失会在 DevTools 里留下 404 噪声，
// 把「页面加载是否干净」的读数污染成假红。
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64">
<rect width="64" height="64" rx="14" fill="#0b0d10"/>
<rect x="10" y="10" width="44" height="44" rx="10" fill="#3b74e0"/>
<circle cx="32" cy="32" r="9" fill="#e8edf3"/>
</svg>`

// handleFavicon 输出内嵌图标（长缓存，见 serveAsset）。
func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	serveAsset(w, r, "image/svg+xml", faviconSVG)
}

// handleFaviconICO 兜底：旧浏览器会直接请求 /favicon.ico，回 204 而不是 404，
// 避免把「没有图标」记成一次资源加载失败。
func (s *Server) handleFaviconICO(w http.ResponseWriter, r *http.Request) {
	// 与 serveAsset 同一套策略：可重校验，避免升级后仍吃旧响应。
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.WriteHeader(http.StatusNoContent)
	_, _ = io.WriteString(w, "")
}

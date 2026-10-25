package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestDeclaredAssetsAreRoutable 把「页面声明的静态资源」与「routes() 实际注册的路由」
// 做交叉校验。
//
// 为什么要这条（2026-10-19 实测回归）：
//
//	favicon.go 内嵌了图标并实现了 handleFavicon / handleFaviconICO，四个页面也都
//	声明了 <link rel="icon" href="/assets/favicon.svg">，但 routes() 里没有任何一处
//	注册它们。结果是浏览器请求落到 "/" 的 handleIndex（非 "/" 一律 404），图标 404、
//	DevTools 留下加载失败噪声 —— 而 TestPagesDeclareFavicon 一直是绿的，因为它断言的是
//	「HTML 里有这行 link」，不是「这个地址真的有人接」。断言声明 != 断言可达。
func TestDeclaredAssetsAreRoutable(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.routes(mux)

	pages := map[string]string{
		"index":      indexHTML("tok", "idle"),
		"admin":      adminHTML("tok"),
		"governance": governanceHTML("tok"),
		"pool":       modelPoolPageHTML("tok"),
	}
	re := regexp.MustCompile(`(?:href|src)="(/assets/[^"]+)"`)
	seen := map[string]bool{}
	for _, name := range []string{"index", "admin", "governance", "pool"} {
		page := pages[name]
		for _, m := range re.FindAllStringSubmatch(page, -1) {
			path := m[1]
			if seen[path] {
				continue
			}
			seen[path] = true
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("%s 页声明的资源 %s 不可达（status=%d）：routes() 未注册该路径，请求被 handleIndex 当 404 处理", name, path, rr.Code)
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("未从页面中提取到任何 /assets 引用（页面结构或正则变了，本测试已失效）")
	}
}

// TestFaviconEndpoints 图标端点必须是「可被浏览器取到的 SVG」而不是 404；
// /favicon.ico 是旧浏览器的兜底，按设计回 204（避免把"没图标"记成资源加载失败）。
func TestFaviconEndpoints(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.routes(mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/favicon.svg", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("/assets/favicon.svg 期望 200，实际 %d（favicon 处理器未注册到路由）", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Fatalf("/assets/favicon.svg Content-Type 期望 image/svg+xml，实际 %q", ct)
	}
	if !strings.Contains(rr.Body.String(), "<svg") {
		t.Fatalf("/assets/favicon.svg 正文不是 SVG：%q", rr.Body.String())
	}

	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if rr2.Code != http.StatusNoContent {
		t.Fatalf("/favicon.ico 期望 204（兜底不报错），实际 %d", rr2.Code)
	}
}

// TestFaviconRevalidate 图标与其它静态资源同一套缓存策略：带 ETag，命中即 304，
// 保证「升级 EXE 后浏览器一定拿到新图标」。
func TestFaviconRevalidate(t *testing.T) {
	srv := &Server{}
	mux := http.NewServeMux()
	srv.routes(mux)

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/assets/favicon.svg", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("favicon 响应缺少 ETag")
	}
	req := httptest.NewRequest(http.MethodGet, "/assets/favicon.svg", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	mux.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Fatalf("带 If-None-Match 的重复请求期望 304，实际 %d", second.Code)
	}
}

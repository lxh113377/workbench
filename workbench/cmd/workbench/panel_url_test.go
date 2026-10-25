package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
)

// TestPanelAliveRejectsDeadPort 守护 2026-10-19 现场缺陷：
// 「端口在监听」不能等同「面板可用」；死端口/非 200 必须判为不可达，
// 否则浏览器会被指向一个拿不到数据的地址，首页自动响应连续失败 → 按钮落进 error 态（「重试」）。
func TestPanelAliveRejectsDeadPort(t *testing.T) {
	// 1) 一个真实可用的面板
	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer okSrv.Close()
	if !panelAlive(okSrv.URL + "/") {
		t.Fatal("可响应面板必须判为可达")
	}
	// 2) 关掉后同一地址必须判为不可达
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadSrv.URL + "/"
	deadSrv.Close()
	if panelAlive(deadURL) {
		t.Fatal("已关闭的端口必须判为不可达（否则浏览器会指向死端口）")
	}
	// 3) 端口在监听但返回错误码，也不得判为可达
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()
	if panelAlive(badSrv.URL + "/") {
		t.Fatal("非 200 响应不得判为可达（「在监听」≠「面板可用」）")
	}
}

// TestResolveRunningPanelURLPrefersRecordedURL 守护修复后的优先级：
// 必须优先采用实例自写的 panel.url，而不是「相邻端口有东西在听」。
func TestResolveRunningPanelURLPrefersRecordedURL(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, filepath.Join(dir, "workbench-output"))
	if err != nil {
		t.Fatalf("BuildLayout: %v", err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	// 真实面板（模拟运行实例），并把它写进 panel.url
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(layout.RuntimeDir, "panel.url"), []byte(srv.URL+"/"), 0o644); err != nil {
		t.Fatalf("写 panel.url: %v", err)
	}
	got := resolveRunningPanelURL(layout)
	if got != srv.URL+"/" {
		t.Fatalf("必须优先采用 panel.url 记录的地址，期望 %q 实得 %q", srv.URL+"/", got)
	}
}

// TestResolveRunningPanelURLRejectsStaleRecord 守护：panel.url 记录的是死地址时，
// 绝不能把它交给浏览器（这正是现场「重试」的直接来源）。
func TestResolveRunningPanelURLRejectsStaleRecord(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, filepath.Join(dir, "workbench-output"))
	if err != nil {
		t.Fatalf("BuildLayout: %v", err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	// 起一个服务再关掉，拿到一个确定死的端口号
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	staleURL := dead.URL + "/"
	dead.Close()
	if err := os.WriteFile(filepath.Join(layout.RuntimeDir, "panel.url"), []byte(staleURL), 0o644); err != nil {
		t.Fatalf("写 panel.url: %v", err)
	}
	if got := resolveRunningPanelURL(layout); got == staleURL {
		t.Fatalf("不得把死地址交给浏览器，实得 %q", got)
	}
}

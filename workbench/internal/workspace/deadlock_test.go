package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSetConfigExternalDoesNotDeadlock 复现并锁定真缺陷（2026-10-19 docker 轮实测）：
// SetConfig 在持写锁（m.mu.Lock）期间调用 m.Mode()/validateNoAudit，而这条路径会
// 再取一次读锁 → sync.RWMutex 不可重入 → 进程永久挂起。
//
// 用户可感后果：配置为 external 模式时，Linux/容器下工作台**启动即挂死**
// （panel.url 永不落盘、无任何日志输出）。Windows 本机因为默认 project 模式而躲过。
func TestSetConfigExternalDoesNotDeadlock(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "wb")
	data := filepath.Join(project, "workbench-output")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(data, project, DefaultConfig())

	done := make(chan error, 1)
	go func() {
		cfg := DefaultConfig()
		cfg.Mode = ModeExternal
		cfg.Root = target
		done <- m.SetConfig(cfg)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetConfig 应成功: %v", err)
		}
		if !m.IsExternal() {
			t.Fatalf("应切到 external: %s", m.Mode())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SetConfig 在 external 模式下死锁（持写锁内再取读锁）—— 这正是容器里启动挂死的根因")
	}
}

// TestSelectExternalThenReloadRoundTrip 覆盖真实启动路径：
// config 落盘为 external 后，engine 启动时 ReloadConfig → SetConfig 必须能正常返回。
func TestSelectExternalThenReloadRoundTrip(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "wb")
	data := filepath.Join(project, "workbench-output")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "proj")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	m := New(data, project, DefaultConfig())
	if _, err := m.SelectExternal(target); err != nil {
		t.Fatal(err)
	}
	// 模拟 engine.ReloadConfig：拿配置副本再 SetConfig 回去
	cfg := m.Config()
	done := make(chan error, 1)
	go func() { done <- m.SetConfig(cfg) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReloadConfig 往返死锁 —— 启动即挂死")
	}
}

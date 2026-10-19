package modelroute

import (
	"path/filepath"
	"testing"
	"time"

	"workbench/internal/config"
)

// TestHealthPersistsAcrossRestart 覆盖 P1 关键修复：
// 健康度必须跨进程重启保留，否则每次重启都会重新踩一遍已知死模型
// （现场实测 glm-5.3 重启后仍被优先选中并再次 stall）。
func TestHealthPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models-health.json")
	routes := []config.ModelRoute{
		{Model: "dead", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 1, CooldownM: 30},
		{Model: "ok", Tier: "balanced", Enabled: true, Weight: 50, MaxFail: 3, CooldownM: 30},
	}
	r1 := NewWithPersistence(routes, path)
	r1.RecordFailure("dead", "model_unavailable", "EOL")
	r1.RecordSuccess("ok", 1234)

	// 模拟进程重启：同一文件重新构造路由器。
	r2 := NewWithPersistence(routes, path)
	health := map[string]Health{}
	for _, h := range r2.Health() {
		health[h.Model] = h
	}
	if got := health["dead"]; got.Failure != 1 || got.CooldownTo == 0 {
		t.Fatalf("dead 健康度未持久化: failure=%d cooldown=%d", got.Failure, got.CooldownTo)
	}
	if got := health["ok"]; got.Success != 1 || got.AvgMs <= 0 {
		t.Fatalf("ok 健康度未持久化: success=%d avgMs=%.1f", got.Success, got.AvgMs)
	}
	// 重启后熔断依然生效：dead 仍在冷却，只能选 ok。
	got, err := r2.Pick("balanced", nil)
	if err != nil {
		t.Fatalf("重启后应仍可选出 ok: %v", err)
	}
	if got != "ok" {
		t.Fatalf("重启后选中了 %s，期望 ok（dead 应仍在冷却）", got)
	}
}

// TestEarliestAvailable 覆盖池等待闸门的时间来源。
func TestEarliestAvailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	routes := []config.ModelRoute{
		{Model: "a", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 1, CooldownM: 1},
		{Model: "b", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 1, CooldownM: 1},
	}
	r := NewWithPersistence(routes, path)
	if got := r.EarliestAvailable(); got != 0 {
		t.Fatalf("全可用时应返回 0，得到 %d", got)
	}
	r.RecordFailure("a", "upstream", "x")
	r.RecordFailure("b", "upstream", "x")
	got := r.EarliestAvailable()
	if got <= time.Now().UnixMilli() {
		t.Fatalf("全冷却时应返回未来时刻，得到 %d", got)
	}
}

// TestPersistenceSkipsUnknownModel 保证配置文件里删掉的模型不会因历史健康度复活。
func TestPersistenceSkipsUnknownModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	r1 := NewWithPersistence([]config.ModelRoute{
		{Model: "a", Tier: "balanced", Enabled: true, Weight: 100},
		{Model: "gone", Tier: "balanced", Enabled: true, Weight: 100},
	}, path)
	r1.RecordSuccess("gone", 100)

	r2 := NewWithPersistence([]config.ModelRoute{
		{Model: "a", Tier: "balanced", Enabled: true, Weight: 100},
	}, path)
	for _, h := range r2.Health() {
		if h.Model == "gone" {
			t.Fatal("已从配置删除的模型不应被健康度文件复活")
		}
	}
}

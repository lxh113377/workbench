package executor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// makeWideTree 造一棵「够大但无白名单产出」的目录树（vita：不触发出产命中的提前退出）。
//
// 为什么用 .bin 扩展名：白名单只认 .md/.txt/.json/... ，于是探针不会对任何文件做 Info()，
// 遍历代价**全部**来自「每个目录项」——正是本轮要治的那部分放大成本。
func makeWideTree(t *testing.T, dirs, perDir int) string {
	t.Helper()
	root := t.TempDir()
	old := time.Now().Add(-3 * time.Hour)
	for i := 0; i < dirs; i++ {
		sub := filepath.Join(root, "d"+strconv.Itoa(i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < perDir; j++ {
			p := filepath.Join(sub, "f"+strconv.Itoa(j)+".bin")
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

// TestIsStalledSkipsDiskScanWhenEventFresh 钉住「事件信号仍新鲜 ⇒ 不读盘」这条快速路径。
//
// 判据用**分配量**而不是墙钟：走一遍树要为每个目录项分配对象，免扫则接近零分配；
// 墙钟在多任务机器上会抖，分配量可复现（本仓 CI 与本机一致）。
//
// 这条同时是本次优化的负控：把 IsStalled 里的快速路径删掉，第一个断言立刻判红。
func TestIsStalledSkipsDiskScanWhenEventFresh(t *testing.T) {
	dir := makeWideTree(t, 40, 100) // 4000 个非白名单文件
	now := time.Now().UnixMilli()

	// ① 事件新鲜（now-1s，远在 420s 窗口内）⇒ 结论已由事件信号定死 ⇒ 不得读盘。
	fast := testing.AllocsPerRun(5, func() {
		if IsStalled(now-1_000, dir, 420, false, now) {
			t.Error("事件新鲜时不得判卡死")
		}
	})
	// ② 事件过期（now-480s）⇒ 必须回落到产出信号 ⇒ 必然扫树 ⇒ 分配量显著上升。
	slow := testing.AllocsPerRun(5, func() {
		if !IsStalled(now-480_000, dir, 420, false, now) {
			t.Error("双信号都过期时必须判卡死")
		}
	})

	if fast > 200 {
		t.Fatalf("事件新鲜路径分配量 = %.0f，应≈0（说明仍在扫盘）", fast)
	}
	if slow < 1000 {
		t.Fatalf("双信号过期路径分配量 = %.0f，未显著高于免扫路径 %.0f（负控失效：树太小或探针没走）", slow, fast)
	}
}

// walkOldRef 是改造前的实现（filepath.Walk + 全树取值），只用于基准对比的参照物。
func walkOldRef(dir string) int64 {
	var latest int64
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if !outputExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if ms := info.ModTime().UnixMilli(); ms > latest {
			latest = ms
		}
		return nil
	})
	return latest
}

// BenchmarkOutputProbe 对比「改造前 Walk 全树」与「现实现」在同一棵树上的每轮开销。
// 复跑：go test ./internal/executor/ -run TestIsStalledSkipsDiskScanWhenEventFresh -bench BenchmarkOutputProbe -benchtime 20x
func BenchmarkOutputProbe(b *testing.B) {
	dir := b.TempDir()
	old := time.Now().Add(-3 * time.Hour)
	for i := 0; i < 60; i++ {
		sub := filepath.Join(dir, "d"+strconv.Itoa(i))
		_ = os.MkdirAll(sub, 0o755)
		for j := 0; j < 100; j++ {
			p := filepath.Join(sub, "f"+strconv.Itoa(j)+".bin")
			_ = os.WriteFile(p, []byte("x"), 0o644)
			_ = os.Chtimes(p, old, old)
		}
	}
	watermark := time.Now().UnixMilli() - 420_000
	b.Run("old_Walk_full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = walkOldRef(dir)
		}
	})
	b.Run("new_WalkDir_watermark", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = latestOutputMtime(dir, watermark)
		}
	})
}

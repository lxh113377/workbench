package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
)

// appendSample 直接往 Manager 的采样环里塞一条（单测不启动后台循环）。
func appendSample(m *Manager, at int64, heap float64, handles int) {
	m.mu.Lock()
	m.samples = append(m.samples, Sample{At: at, HeapMB: heap, AllocMB: heap / 2, Goroutine: 10, Handles: handles})
	m.mu.Unlock()
}

// TestWriteCSVIsIncremental 守护「metrics.csv 必须增量追加」。
//
// 现场缺陷（2026-10-19）：改造前 WriteCSV 每次整文件重写内存里的最近 720 条采样。
// 24h 压测（20s 采样 ≈ 4320 条）会被截断成最近 4 小时 —— A7「跑满 24h」的判据
// 永远拿不到完整跨度。这不是"没跑够"，是"取证被自己写丢了"。
//
// 判据：
//  1. 首次落盘写表头 + 全部样本；
//  2. 再次落盘只追加新样本，历史一行不丢；
//  3. 内存环形缓冲被裁掉旧样本后，文件里的历史仍然在（这是关键）；
//  4. 没有新样本时不碰文件。
func TestWriteCSVIsIncremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs", "metrics.csv")
	m := New(config.Layout{}, func() config.SupervisorConfig { return config.DefaultConfig().Supervisor })

	// 1) 首批 3 条
	appendSample(m, 1000, 1, 100)
	appendSample(m, 2000, 2, 101)
	appendSample(m, 3000, 3, 102)
	if err := m.WriteCSV(path); err != nil {
		t.Fatalf("首次落盘失败: %v", err)
	}
	first := readFile(t, path)
	if !strings.HasPrefix(first, "at,heap_mb") {
		t.Fatalf("首次落盘必须写表头，实际开头: %q", first[:min(len(first), 40)])
	}
	if n := strings.Count(first, "\n") - 1; n != 3 {
		t.Fatalf("首次应有 3 条数据行，实际 %d", n)
	}

	// 2) 再加 2 条：只应追加，不重写
	appendSample(m, 4000, 4, 103)
	appendSample(m, 5000, 5, 104)
	if err := m.WriteCSV(path); err != nil {
		t.Fatalf("第二次落盘失败: %v", err)
	}
	second := readFile(t, path)
	if n := strings.Count(second, "\n") - 1; n != 5 {
		t.Fatalf("第二次应有 5 条数据行（3 旧 + 2 新），实际 %d", n)
	}
	if strings.Count(second, "at,heap_mb") != 1 {
		t.Fatal("表头不得重复写入")
	}
	if !strings.Contains(second, "1000,") || !strings.Contains(second, "5000,") {
		t.Fatal("历史行不得丢失")
	}

	// 3) 关键用例：模拟内存环形缓冲把旧样本裁掉（24h 压测真实会发生的场景）
	m.mu.Lock()
	m.samples = m.samples[len(m.samples)-2:] // 只留最后两条
	m.mu.Unlock()
	appendSample(m, 6000, 6, 105)
	if err := m.WriteCSV(path); err != nil {
		t.Fatalf("第三次落盘失败: %v", err)
	}
	third := readFile(t, path)
	if n := strings.Count(third, "\n") - 1; n != 6 {
		t.Fatalf("内存裁剪后文件仍应保留全部 6 行历史，实际 %d（增量落盘失效）", n)
	}
	if !strings.Contains(third, "1000,") {
		t.Fatal("最早的行被写丢了 —— 增量落盘的语义就是历史不可丢")
	}

	// 4) 没有新样本：不碰文件（mtime 不变）
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if err := m.WriteCSV(path); err != nil {
		t.Fatalf("空写失败: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("没有新样本时不应重写文件")
	}
}

// TestReadMetricsCSVAfterIncremental 保证增量落盘仍能被 --soak 的读取器解析。
func TestReadMetricsCSVAfterIncremental(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.csv")
	m := New(config.Layout{}, func() config.SupervisorConfig { return config.SupervisorConfig{SampleSeconds: 60} })
	for i := 0; i < 5; i++ {
		appendSample(m, int64(1000*(i+1)), float64(i+1), 100+i)
	}
	if err := m.WriteCSV(path); err != nil {
		t.Fatal(err)
	}
	pts, skipped, err := ReadMetricsCSV(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("不应有跳过行，实际 %d", skipped)
	}
	if len(pts) != 5 {
		t.Fatalf("应解析出 5 个点，实际 %d", len(pts))
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
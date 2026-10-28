package store

// 基准测试：量化「每个调度 tick 都要付的持久化/快照成本」。
// 背景（2026-10-28 性能定位）：现场 queue.json 5073 条 / 4.7MB，
// 而 tick 每 3 秒一次、每次调 store.List() 至少 3~5 处（tasksToday、
// supplyCandidates、rejectionPressure、lastCompletedTask），
// 每处都是「全量拷贝 + 排序」。本基准把这几条路径的单价测出来。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// benchStore 构造一个含 n 条任务的 Store（落盘在临时目录）。
func benchStore(b *testing.B, n int) (*Store, string) {
	b.Helper()
	dir := filepath.Join(b.TempDir(), "root")
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(layout.DataRoot, 0o755); err != nil {
		b.Fatal(err)
	}
	s, err := Open(layout)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	// 关闭 WAL 阈值检查以外的干扰：直接构造内存记录
	for i := 0; i < n; i++ {
		t := &model.Task{
			ID:         fmt.Sprintf("task-%05d", i),
			Title:      fmt.Sprintf("基准任务 %05d 主题描述用于模拟真实标题长度", i),
			Prompt:     fmt.Sprintf("提示词正文，长度模拟真实模板拼装后的规模 %05d %s", i, "内容内容内容内容内容内容"),
			TemplateID: fmt.Sprintf("tpl-%03d", i%24),
			State:      model.StateRejected,
			CreatedAt:  int64(1_700_000_000_000 + i*1000),
			UpdatedAt:  int64(1_700_000_000_000 + i*1000),
			Tags:       []string{"基准"},
		}
		s.tasks[t.ID] = t
	}
	return s, dir
}

func BenchmarkStoreList5k(b *testing.B) {
	s, _ := benchStore(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.List()
	}
}

func BenchmarkStorePersist5k(b *testing.B) {
	s, _ := benchStore(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.persist(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkWALAppend(b *testing.B) {
	s, _ := benchStore(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cp := *s.tasks["task-00000"]
		if err := s.record(walRecord{Op: OpUpdate, Task: &cp}); err != nil {
			b.Fatal(err)
		}
		if i == 1900 {
			b.StopTimer() // 越过压缩阈值前停下，避免把 persist 的成本混进 append
			b.StartTimer()
			break
		}
	}
}

// BenchmarkTickWorkload 模拟一个真实 tick 的重复计算量：
// tasksToday(1 次 List) + supplyCandidates(1 次 List) + rejectionPressure(1 次 List)。
func BenchmarkTickWorkload(b *testing.B) {
	s, _ := benchStore(b, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		all := s.List()
		start := int64(1_700_000_000_000)
		n := 0
		for _, t := range all {
			if t.CreatedAt >= start && t.State != model.StateRejected {
				n++
			}
		}
		all2 := s.List()
		for _, t := range all2 {
			_ = t.ID
		}
		all3 := s.List()
		for i2 := len(all3) - 1; i2 >= 0; i2-- {
			if all3[i2].Source == "autonomy" {
				break
			}
		}
		_ = n
	}
}

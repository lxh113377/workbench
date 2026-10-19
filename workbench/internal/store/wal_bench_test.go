package store

import (
	"fmt"
	"testing"

	"workbench/internal/model"
)

// TestWALWriteAmplification 用数据证明 P2-3 的效果：
// 同一次 Create 在旧实现下要序列化全部 N 个任务，新实现只追加一条记录。
func TestWALWriteAmplification(t *testing.T) {
	s, layout := newWALStore(t)
	defer s.Close()

	const n = 100
	for i := 0; i < n; i++ {
		task := &model.Task{
			ID:     fmt.Sprintf("task-amp-%03d", i),
			Title:  "写放大测试任务",
			State:  model.StateCandidate,
			Prompt: "这段 prompt 模拟真实任务的长度，用于让全量快照的体积接近真实规模。aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}
		if err := s.Create(task); err != nil {
			t.Fatal(err)
		}
	}

	if got := countLines(layout.WALFile); got != n {
		t.Fatalf("WAL 行数 = %d, want %d（每次迁移恰好一条追加）", got, n)
	}
	walSize, err := fileSize(layout.WALFile)
	if err != nil {
		t.Fatal(err)
	}
	if walSize == 0 {
		t.Fatal("WAL 文件为空")
	}

	if err := s.Savepoint(); err != nil {
		t.Fatal(err)
	}
	snapSize, err := fileSize(layout.QueueFile)
	if err != nil {
		t.Fatal(err)
	}
	if snapSize <= walSize/n {
		t.Fatalf("快照 %d 字节应明显大于单条记录 %d 字节", snapSize, walSize/int64(n))
	}
	t.Logf("WAL 总 %d 字节（%d 条，均 %d 字节/条）；压缩后快照 %d 字节 ⇒ 旧实现每次迁移都要写 %d 字节",
		walSize, n, walSize/int64(n), snapSize, snapSize)
}

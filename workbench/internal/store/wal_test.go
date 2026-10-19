package store

import (
	"os"
	"path/filepath"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func newWALStore(t *testing.T) (*Store, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	return s, layout
}

func TestWALAppendsOnEveryMutation(t *testing.T) {
	s, layout := newWALStore(t)
	defer s.Close()

	task := &model.Task{ID: "task-wal", Title: "WAL", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition("task-wal", model.StateRejected, "test", ""); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(layout.WALFile)
	if err != nil {
		t.Fatalf("WAL 文件应已创建: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("WAL 为空：Create/Transition 应各追加一条")
	}
	if n := countLines(layout.WALFile); n != 2 {
		t.Fatalf("WAL 行数 = %d, want 2", n)
	}
}

func TestWALReplayRebuildsState(t *testing.T) {
	s, layout := newWALStore(t)
	task := &model.Task{ID: "task-replay", Title: "回放", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition("task-replay", model.StateRejected, "test", "拒"); err != nil {
		t.Fatal(err)
	}
	// 不调用 Savepoint：模拟"崩溃前只写了 WAL，没来得及压缩快照"
	_ = s.Close()

	// 快照仍停在 Create 之前的空状态（queue.json 未写），必须靠 WAL 回放补齐
	reopened, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, ok := reopened.Get("task-replay")
	if !ok {
		t.Fatal("WAL 回放后应能读到任务")
	}
	if got.State != model.StateRejected {
		t.Fatalf("state = %s, want rejected（WAL 里的最后一次迁移）", got.State)
	}
}

func TestWALReplayIgnoresPartialLine(t *testing.T) {
	s, layout := newWALStore(t)
	task := &model.Task{ID: "task-partial", Title: "半行", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	// 追加一条被截断的 JSON 行（模拟写一半崩溃）
	f, err := os.OpenFile(layout.WALFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, werr := f.WriteString("{\"at\":123,\"op\":\"update\",\"task\":{\"id\":\"task-par"); werr != nil {
		t.Fatal(werr)
	}
	_ = f.Close()

	reopened, err := Open(layout)
	if err != nil {
		t.Fatalf("半行不应导致打开失败: %v", err)
	}
	defer reopened.Close()
	if _, ok := reopened.Get("task-partial"); !ok {
		t.Fatal("半行之前的记录应正常回放")
	}
}

func TestSavepointCompactsWAL(t *testing.T) {
	s, layout := newWALStore(t)
	for i := 0; i < 5; i++ {
		task := &model.Task{ID: "task-c" + string(rune('a'+i)), Title: "压缩", State: model.StateCandidate}
		if err := s.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if countLines(layout.WALFile) == 0 {
		t.Fatal("压缩前 WAL 应有内容")
	}
	if err := s.Savepoint(); err != nil {
		t.Fatal(err)
	}
	if n := countLines(layout.WALFile); n != 0 {
		t.Fatalf("Savepoint 后 WAL 应被清空，实际 %d 行", n)
	}
	// 快照必须包含全部任务
	var snap Snapshot
	if err := config.ReadJSON(layout.QueueFile, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Tasks) != 5 {
		t.Fatalf("快照任务数 = %d, want 5", len(snap.Tasks))
	}
	_ = s.Close()
}

func TestCompactionAtThreshold(t *testing.T) {
	s, layout := newWALStore(t)
	defer s.Close()
	// 把阈值压到 3 便于观察自动压缩
	s.wal.mu.Lock()
	s.wal.threshold = 3
	s.wal.mu.Unlock()

	for i := 0; i < 4; i++ {
		task := &model.Task{ID: "task-t" + string(rune('a'+i)), Title: "阈值", State: model.StateCandidate}
		if err := s.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	// 第 3 条触发压缩 → WAL 被清空，后续再追加
	if n := countLines(layout.WALFile); n >= 3 {
		t.Fatalf("达到阈值后应自动压缩，WAL 仍 %d 行", n)
	}
	var snap Snapshot
	if err := config.ReadJSON(layout.QueueFile, &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Tasks) < 3 {
		t.Fatalf("压缩后快照应至少 3 个任务，实际 %d", len(snap.Tasks))
	}
}

func TestReplayHandlesMissingWAL(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "queue.wal.jsonl")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	s, err := Open(layout)
	if err != nil {
		t.Fatalf("WAL 不存在时不应报错: %v", err)
	}
	defer s.Close()
	if n := len(s.List()); n != 0 {
		t.Fatalf("空目录任务数 = %d, want 0", n)
	}
}

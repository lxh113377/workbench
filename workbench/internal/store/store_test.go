package store

import (
	"os"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

func newTestStore(t *testing.T) (*Store, config.Layout) {
	t.Helper()
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	set, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	// WAL 持文件句柄：测试结束前必须先关闭，否则 Windows 下 TempDir 清理会因句柄占用失败。
	t.Cleanup(func() { _ = set.Close() })
	return set, layout
}

func TestCreateAndTransition(t *testing.T) {
	s, _ := newTestStore(t)
	task := &model.Task{ID: "task-1", Title: "测试任务", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition("task-1", model.StateRejected, "test", ""); err != nil {
		t.Fatalf("candidate -> rejected should be allowed: %v", err)
	}
	if _, err := s.Transition("task-1", model.StateRunning, "test", ""); err == nil {
		t.Fatal("rejected -> running must be rejected")
	}
}

func TestPersistAndReload(t *testing.T) {
	s, layout := newTestStore(t)
	for i := 0; i < 3; i++ {
		task := &model.Task{ID: "task-" + string(rune('a'+i)), Title: "任务", State: model.StateDone, OutputBytes: int64(i * 100)}
		if err := s.Create(task); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Savepoint(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := len(reopened.List()); got != 3 {
		t.Fatalf("reload got %d tasks, want 3", got)
	}
}

func TestReconcileInterrupted(t *testing.T) {
	s, layout := newTestStore(t)
	task := &model.Task{ID: "task-x", Title: "运行中任务", State: model.StateQueued}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition("task-x", model.StateRunning, "engine", ""); err != nil {
		t.Fatal(err)
	}
	n, err := s.Reconcile()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("reconcile count = %d, want 1", n)
	}
	got, _ := s.Get("task-x")
	if got.State != model.StateInterrupted {
		t.Fatalf("state = %s, want interrupted", got.State)
	}
	_ = layout
}

func TestAtomicWriteNoPartialFile(t *testing.T) {
	s, layout := newTestStore(t)
	task := &model.Task{ID: "task-y", Title: "原子写", State: model.StateQueued}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	// P2-3 起写路径走 WAL：普通迁移只追加 WAL，queue.json 由 Savepoint/压缩产生。
	if n := countLines(layout.WALFile); n != 1 {
		t.Fatalf("WAL 行数 = %d, want 1（Create 追加一条）", n)
	}
	// Savepoint 后 queue.json 必须是完整合法 JSON（末尾换行 = 原子写结果）
	if err := s.Savepoint(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(layout.QueueFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != 10 {
		t.Fatal("queue.json 末尾缺少换行，可能不是原子写结果")
	}
	var snap Snapshot
	if err := config.ReadJSON(layout.QueueFile, &snap); err != nil {
		t.Fatalf("queue.json 不是合法 JSON: %v", err)
	}
	if len(snap.Tasks) != 1 {
		t.Fatalf("快照任务数 = %d, want 1", len(snap.Tasks))
	}
}

func TestAppendAndListAttempts(t *testing.T) {
	s, layout := newTestStore(t)
	task := &model.Task{ID: "task-attempt", Title: "尝试台账", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	s.AppendAttempt(AttemptRecord{TaskID: "task-attempt", Attempt: 0, Model: "m1", Kind: "timeout", Action: "retry", DelayMs: 5000, ElapsedMs: 1200})
	s.AppendAttempt(AttemptRecord{TaskID: "task-attempt", Attempt: 1, Model: "m2", Kind: "done", Action: "done", ElapsedMs: 900})

	recs, err := s.ListAttempts("task-attempt")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("attempts = %d, want 2", len(recs))
	}
	if recs[0].Kind != "timeout" || recs[0].Action != "retry" {
		t.Fatalf("first attempt = %+v", recs[0])
	}
	if recs[1].Model != "m2" || recs[1].Kind != "done" {
		t.Fatalf("second attempt = %+v", recs[1])
	}
	data, err := os.ReadFile(layout.TaskFile("task-attempt", "attempts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != 10 {
		t.Fatal("attempts.jsonl 末尾缺少换行，可能是覆写而非追加")
	}
}

func TestListAttemptsMissingFile(t *testing.T) {
	s, _ := newTestStore(t)
	recs, err := s.ListAttempts("no-such-task")
	if err != nil {
		t.Fatalf("missing attempts file should not error: %v", err)
	}
	if len(recs) != 0 {
		t.Fatalf("attempts = %d, want 0", len(recs))
	}
}

func TestAppendTaskEventNeverTruncates(t *testing.T) {
	s, layout := newTestStore(t)
	task := &model.Task{ID: "task-events", Title: "事件流", State: model.StateCandidate}
	if err := s.Create(task); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		s.AppendTaskEvent("task-events", model.Event{Type: "tick", At: model.Now(), TaskID: "task-events"})
	}
	data, err := os.ReadFile(layout.TaskFile("task-events", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("events.jsonl 被截断为 0 字节（回归：completeTask 覆盖事件流）")
	}
	lines := 0
	for _, b := range data {
		if b == 10 {
			lines++
		}
	}
	if lines != 3 {
		t.Fatalf("events.jsonl lines = %d, want 3", lines)
	}
}

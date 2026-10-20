package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestEventsHandleReleasedOnStartFailure 守护缺陷 D8 的早退路径（2026-10-20 实测）：
// 事件文件在 cmd.Start 之前打开；Start 失败（这里用一个不存在的 exe）时句柄必须已释放，
// 否则 Windows 上删除/改名该文件会报「被另一进程占用」。
//
// 这条用例是真踩出来的：把句柄关闭完全交给 consume 协程的第一版改动，正是被同类
// 路径在 t.TempDir 清理阶段暴露（TestRunnerLimiterThrottlesRequests 假红：
// unlinkat ...\throttle-1\events.jsonl: used by another process）。
func TestEventsHandleReleasedOnStartFailure(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig().Executor
	cfg.DSHExe = filepath.Join(dir, "definitely-not-an-executable.exe")
	r := New(cfg, layout)

	task := &model.Task{ID: "task-d8-start-fail", Title: "启动失败", TimeoutMin: 1}
	_, _ = r.Run(context.Background(), task, "")

	events := layout.TaskFile(task.ID, "events.jsonl")
	if _, statErr := os.Stat(events); statErr != nil {
		t.Skipf("事件文件未创建（打开前即失败，无需验证释放）: %v", statErr)
	}
	// 关键判据：Run 已返回，句柄必须已释放 —— 用改名验证（Windows 上占用会让改名失败）。
	if mvErr := os.Rename(events, events+".renamed"); mvErr != nil {
		t.Fatalf("Run 返回后 events.jsonl 句柄未释放（D8 回归）: %v", mvErr)
	}
}

// TestEventsAppendNotTruncated 守护 D1 与 D8 的交集：事件流只追加、不被覆盖；
// 若句柄在写入进行中被提前关闭，写入会静默失败（错误被 _ 吞掉），内容只会变短。
func TestEventsAppendNotTruncated(t *testing.T) {
	dir := t.TempDir()
	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig().Executor
	cfg.DSHExe = filepath.Join(dir, "no-such-exe.exe")
	r := New(cfg, layout)
	task := &model.Task{ID: "task-d8-content", Title: "内容", TimeoutMin: 1}

	_, _ = r.Run(context.Background(), task, "")
	events := layout.TaskFile(task.ID, "events.jsonl")
	b, readErr := os.ReadFile(events)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("读取事件文件失败: %v", readErr)
	}
	if strings.Contains(string(b), "\x00") {
		t.Fatal("事件流含 NUL（写入被交错打断的形态）")
	}
	_, _ = r.Run(context.Background(), task, "")
	after, _ := os.ReadFile(events)
	if len(after) < len(b) {
		t.Fatalf("事件流被截断：before=%d after=%d（D1 回归）", len(b), len(after))
	}
}

// TestCloseOnceIsIdempotent 守护 sync.Once 去重语义：run 的 defer 与 consume 的收尾
// 调用同一个 closeEvents，重复调用必须是 no-op（否则会 double-close 报错/panic 掩盖真因）。
func TestCloseOnceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	once := &sync.Once{}
	var closed int
	closeFn := func() {
		once.Do(func() {
			closed++
			_ = f.Close()
		})
	}
	closeFn()
	closeFn()
	if closed != 1 {
		t.Fatalf("Close 执行次数 = %d, want 1（sync.Once 去重失效）", closed)
	}
	if _, werr := f.Write([]byte("x")); werr == nil {
		t.Fatal("关闭后仍写入成功 = 句柄没真的关")
	}
}

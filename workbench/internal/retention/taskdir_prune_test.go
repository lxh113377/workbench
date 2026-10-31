package retention

// taskdir_prune_test.go 守护「任务目录本身也要按 TaskDirDays 回收」（2026-10-19 现场缺陷）。

// 现场：Policy.TaskDirDays 被定义（默认 90 天）、被 Normalize 补齐、被写进注释
// （「这些是审计与成果资产，保留最久」），但**没有任何代码使用它** ——
// PruneTaskWorkspaces 只回收 <task>/workspace/，任务目录本身（events.jsonl /
// final.md / attempts.jsonl / verify.json）永远不回收。对一个 7x24 长跑系统，
// 这是只涨不落的一半：workspace 有闸门，任务目录没有。
//
// 本测试先钉住正确语义，再让实现跟上。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestPruneReclaimsExpiredTaskDir 断言：超过 TaskDirDays 的任务目录应被整体回收，
// 且**宽松**区分 —— 未到期的任务目录必须原样保留。
func TestPruneReclaimsExpiredTaskDir(t *testing.T) {
	root := t.TempDir()
	// 旧任务目录：所有文件 mtime 都推到 200 天前（> TaskDirDays=90）
	old := filepath.Join(root, "task-old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"final.md", "events.jsonl", "attempts.jsonl", "verify.json"} {
		if err := os.WriteFile(filepath.Join(old, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 新任务目录：刚创建
	newer := filepath.Join(root, "task-new")
	if err := os.MkdirAll(newer, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newer, "final.md"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().AddDate(0, 0, -200)
	_ = filepath.Walk(old, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		_ = os.Chtimes(p, oldTime, oldTime)
		return nil
	})

	// 注意：任务目录回收走**独立函数** PruneTaskDir ——
	// PruneTaskWorkspaces 刻意不动任务目录（它只清 workspace 明细、保留成果资产）。
	res := PruneTaskDir(root, DefaultPolicy(), false)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("超过 TaskDirDays 的任务目录应被回收，实际仍在（reclaimed=%d skipped=%d errors=%v）",
			res.ReclaimedDirs, res.SkippedDirs, res.Errors)
	}
	if _, err := os.Stat(filepath.Join(newer, "final.md")); err != nil {
		t.Fatal("未到期的任务目录不得被回收")
	}
}

// TestPruneTaskDirRespectsTaskDirDays 断言阈值真的生效：
// 同一个目录在 TaskDirDays=1 下应被回收，在 TaskDirDays=3650 下应保留。
// 这直接证明「TaskDirDays 参与判定」而不是被忽略。
func TestPruneTaskDirRespectsTaskDirDays(t *testing.T) {
	build := func() string {
		root := t.TempDir()
		d := filepath.Join(root, "task-x")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "final.md"), []byte("z"), 0o644); err != nil {
			t.Fatal(err)
		}
		aged := time.Now().AddDate(0, 0, -30)
		_ = filepath.Walk(d, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi == nil {
				return nil
			}
			_ = os.Chtimes(p, aged, aged)
			return nil
		})
		return root
	}

	// 阈值 1 天：30 天前的目录必须回收
	r1 := build()
	p1 := DefaultPolicy()
	p1.TaskDirDays = 1
	p1.WorkspaceDays = 3650 // 不让 workspace 规则干扰
	PruneTaskDir(r1, p1, false)
	if _, err := os.Stat(filepath.Join(r1, "task-x")); !os.IsNotExist(err) {
		t.Fatal("TaskDirDays=1 时 30 天前的目录应被回收")
	}

	// 阈值 3650 天：同一批目录必须保留
	r2 := build()
	p2 := DefaultPolicy()
	p2.TaskDirDays = 3650
	PruneTaskDir(r2, p2, false)
	if _, err := os.Stat(filepath.Join(r2, "task-x", "final.md")); err != nil {
		t.Fatal("TaskDirDays=3650 时不得回收")
	}
}

// TestPruneTaskDirDryRunKeepsEverything 断言 dry-run 绝不删任何任务目录。
func TestPruneTaskDirDryRunKeepsEverything(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "task-dry")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "final.md"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	aged := time.Now().AddDate(0, 0, -500)
	_ = filepath.Walk(d, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi == nil {
			return nil
		}
		_ = os.Chtimes(p, aged, aged)
		return nil
	})
	_ = PruneTaskDir(root, DefaultPolicy(), true)
	if _, err := os.Stat(filepath.Join(d, "final.md")); err != nil {
		t.Fatal("dry-run 不得删除任何东西")
	}
}

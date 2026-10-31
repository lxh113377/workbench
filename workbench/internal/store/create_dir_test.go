package store

// create_dir_test.go 守护「登记任务不得创建磁盘目录」（2026-10-19 现场）。
//
// 现场（真实运行数据）：tasks/ 下有 **1314 个空目录**，恰好等于
// audit.jsonl 里 candidate->rejected 的 1314 次 —— 完全相等。
//
// 机理：Store.Create 无条件 os.MkdirAll(TaskDir(t.ID))，
// 而调用方（engine_autonomy.go）是**先 Create、后 Evaluate**：
//   Create 建目录 -> Evaluate 命中硬门禁 -> 拒 -> 目录留下、永远空着。
// 空转热循环期间（每 3 秒被拒一次）就每 3 秒落一个空目录。
//
// 为什么 Create 不该建目录：
//   - 目录是**执行期**资源；Create 只是「登记」，两者耦合导致「被拒也占地」；
//   - executor.go 已经有懒建逻辑（真正要跑时才建 workspace），
//     store 这行是冗余的、且是有害的那一半；
//   - 与 executor 已修的同类现场（external 模式留空沙箱）方向一致：
//     **不该建的时候不要建**。
//
// 判据：Create 一个任务后，TaskDir 不应存在；真到执行期由 executor 建。

import (
	"os"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestCreateDoesNotCreateTaskDir 断言：登记任务不落磁盘目录。
func TestCreateDoesNotCreateTaskDir(t *testing.T) {
	// 用产品自己的布局解析，避免手拼字段漏项（EnsureDirs 会校验所有目录）。
	layout, err := config.ResolveLayout(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	// TasksDir 用于承载任务目录，本测试正是要断言「不建」，
	// 所以这里必须**预先存在**（否则无法区分「没建」与「建失败」）。
	if err := os.MkdirAll(layout.TasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	st, err := Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	task := &model.Task{ID: "task-nodir-1", Title: "登记即止", State: model.StateCandidate}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}

	dir := layout.TaskDir(task.ID)
	if _, serr := os.Stat(dir); serr == nil {
		t.Fatalf("Create 不应创建任务目录 %s（被拒任务会留空目录；真实现场累积 1314 个）", dir)
	}
	// Create 的登记契约仍必须成立
	if _, ok := st.Get(task.ID); !ok {
		t.Fatal("Create 仍必须登记任务")
	}
}

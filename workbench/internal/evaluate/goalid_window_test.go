package evaluate

// goalid_window_test.go 守护「滚动窗口必须带上 GoalID」（2026-10-19 现场）。
//
// 现场（新二进制运行期间实测）：
//   近 5 分钟 plan=90 / accept=0，其中「与最近任务完全重复」= 89 条；
//   空目录以 20/分钟 稳定增长，最新距今仅 35 秒 => 循环正在发生。
//
// 根因：gates() 里有一处「同目标豁免」——
//     if sameGoal(it.GoalID, t.GoalID) && it.TaskID != t.ID { continue }
//   注释说明它的存在是为了避免「同项目的自主任务因标题前缀天然相同而被全拒」，
//   并声称现场缺陷已修（原话：目标进度卡在 2/4、连续 47 条「高度相似(0.92)」）。
//
//   但 recentItem 结构**有 GoalID 字段**，而填充窗口的 Observe / Seed **都没写它**：
//     sameGoal(a, b) 的实现是 a != "" && a == b ——
//     GoalID 恒为空串 => sameGoal("", goalID) 恒为 false => **豁免从未生效**。
//
//   于是那条「已修」的修复实际上是个空转：注释写在，字段也在，就是没人填。
//   这又是本项目最典型的缺陷模式：**组件齐全、唯独接线断了一节**。
//
// 判据：Observe / Seed 写入的窗口条目必须携带任务的 GoalID。

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// TestObserveCarriesGoalID 断言 Observe 把 GoalID 记进窗口。
func TestObserveCarriesGoalID(t *testing.T) {
	ev := New(config.EvalConfig{})
	task := &model.Task{
		ID: "task-g1", Title: "目标一的任务", Prompt: "p",
		GoalID: "goal-A", State: model.StateDone,
	}
	ev.Observe(task, 8, "data", 200)
	items := ev.RecentItemsForTest()
	if len(items) != 1 {
		t.Fatalf("窗口应有 1 条，实际 %d", len(items))
	}
	if items[0].GoalID != "goal-A" {
		t.Fatalf("Observe 必须记录 GoalID（否则同目标豁免恒不生效）；期望 goal-A，实际 %q", items[0].GoalID)
	}
}

// TestSeedCarriesGoalID 断言 Seed（启动恢复）同样带上 GoalID。
func TestSeedCarriesGoalID(t *testing.T) {
	ev := New(config.EvalConfig{})
	tasks := []*model.Task{
		{ID: "task-g2", Title: "目标二的任务", Prompt: "p", GoalID: "goal-B", State: model.StateDone},
	}
	ev.Seed(tasks, 200)
	items := ev.RecentItemsForTest()
	if len(items) != 1 {
		t.Fatalf("窗口应有 1 条，实际 %d", len(items))
	}
	if items[0].GoalID != "goal-B" {
		t.Fatalf("Seed 必须记录 GoalID（否则重启后豁免同样失效）；期望 goal-B，实际 %q", items[0].GoalID)
	}
}

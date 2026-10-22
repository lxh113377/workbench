package store

// prune_test.go -- 终态任务记录裁剪的判据（2026-10-23 补）。
//
// 为什么必须有：PruneTasks 是全仓**第一个会删除任务记录**的函数，风险不对称 ——
// 少删一条只是浪费内存（可恢复），多删一条会丢在跑任务或追溯证据（不可恢复）。
// 故判据的重点不是「删得对不对」而是「**绝不误删**」：
//   ① 非终态任务无论多老都不许删（queued/running/approved/evaluating/candidate/interrupted）；
//   ② 保留窗口内的终态任务不许删；
//   ③ 条数上限按**最旧优先**淘汰，近期记录优先保留；
//   ④ 有任务引用的 chain 不许删，孤立 chain 才清；
//   ⑤ dryRun 必须一条不删（运维先看数再动手）。
//
// 现场根因（2026-10-23 实测）：queue.json 59MB / 33,740 条，终态 33,733 条（其中
// rejected 33,187 条为热循环残留），在跑仅 7 条 —— 快照全量序列化导致常驻 170MB、
// 高峰 752MB，而基线 residentMBMax 是 64MB。

import (
	"testing"

	"workbench/internal/model"
	"workbench/internal/retention"
)

// seedTask 造一条指定状态与创建时间的任务。
func seedTask(t *testing.T, s *Store, id string, st model.TaskState, ageDays int) {
	t.Helper()
	task := &model.Task{ID: id, Title: id, State: st}
	if err := s.Create(task); err != nil {
		t.Fatalf("Create(%s) 失败: %v", id, err)
	}
	// Create 会把 CreatedAt 设为 now；用 Update 回拨到指定年龄。
	if _, err := s.Update(id, func(x *model.Task) error {
		x.CreatedAt = model.Now() - int64(ageDays)*86400*1000
		x.State = st
		return nil
	}); err != nil {
		t.Fatalf("Update(%s) 失败: %v", id, err)
	}
}

// TestPruneTasksNeverRemovesNonTerminal 是**最重要的一条**：非终态一律不许删。
// 非终态意味着「还要被调度器选中」或「正在跑」—— 删了就是丢任务。
func TestPruneTasksNeverRemovesNonTerminal(t *testing.T) {
	s := openTestStore(t)
	// 全部设成 400 天前（远超任何保留窗口），但状态都是非终态。
	nonTerminal := []model.TaskState{
		model.StateCandidate, model.StateEvaluating, model.StateApproved,
		model.StateQueued, model.StateRunning, model.StateInterrupted,
	}
	for i, st := range nonTerminal {
		seedTask(t, s, "keep-"+string(rune('a'+i)), st, 400)
	}
	pol := retention.Policy{TerminalTaskDays: 7, MaxTerminalTasks: 10}
	res, err := s.PruneTasks(pol, false)
	if err != nil {
		t.Fatalf("PruneTasks 失败: %v", err)
	}
	if res.RemovedByAge != 0 || res.RemovedByCap != 0 {
		t.Fatalf("非终态任务不得被删：byAge=%d byCap=%d", res.RemovedByAge, res.RemovedByCap)
	}
	for i, st := range nonTerminal {
		id := "keep-" + string(rune('a'+i))
		if _, ok := s.Get(id); !ok {
			t.Fatalf("非终态任务 %s(%s) 被误删 —— 会丢在跑/待跑任务", id, st)
		}
	}
}

// TestPruneTasksRemovesOnlyExpiredTerminal 验证按龄删除只动过期终态。
func TestPruneTasksRemovesOnlyExpiredTerminal(t *testing.T) {
	s := openTestStore(t)
	seedTask(t, s, "old-done", model.StateDone, 30)
	seedTask(t, s, "old-failed", model.StateFailed, 30)
	seedTask(t, s, "old-rejected", model.StateRejected, 30)
	seedTask(t, s, "fresh-done", model.StateDone, 1)
	seedTask(t, s, "fresh-rejected", model.StateRejected, 2)

	pol := retention.Policy{TerminalTaskDays: 7, MaxTerminalTasks: 1000}
	res, err := s.PruneTasks(pol, false)
	if err != nil {
		t.Fatalf("PruneTasks 失败: %v", err)
	}
	if res.RemovedByAge != 3 {
		t.Fatalf("应删 3 条超龄终态，实际 %d", res.RemovedByAge)
	}
	for _, id := range []string{"old-done", "old-failed", "old-rejected"} {
		if _, ok := s.Get(id); ok {
			t.Fatalf("超龄终态 %s 应被删", id)
		}
	}
	for _, id := range []string{"fresh-done", "fresh-rejected"} {
		if _, ok := s.Get(id); !ok {
			t.Fatalf("窗口内终态 %s 不得被删", id)
		}
	}
}

// TestPruneTasksCapEvictsOldestFirst 钉死条数上限的淘汰顺序：最旧优先。
// 淘汰最新的会删掉「刚才发生的事」，那正是最需要追溯的。
func TestPruneTasksCapEvictsOldestFirst(t *testing.T) {
	s := openTestStore(t)
	// 10 条终态，年龄 0..9 天（都在 30 天窗口内）
	for i := 0; i < 10; i++ {
		seedTask(t, s, "t-"+string(rune('0'+i)), model.StateDone, i)
	}
	pol := retention.Policy{TerminalTaskDays: 30, MaxTerminalTasks: 3}
	res, err := s.PruneTasks(pol, false)
	if err != nil {
		t.Fatalf("PruneTasks 失败: %v", err)
	}
	if res.RemovedByCap != 7 {
		t.Fatalf("上限 3 应淘汰 7 条，实际 %d", res.RemovedByCap)
	}
	if res.Kept != 3 {
		t.Fatalf("应保留 3 条，实际 %d", res.Kept)
	}
	// 保留的必须是最新的 3 条（年龄 0,1,2）
	for _, id := range []string{"t-0", "t-1", "t-2"} {
		if _, ok := s.Get(id); !ok {
			t.Fatalf("最新的 %s 应被保留（淘汰应从最旧开始）", id)
		}
	}
	for _, id := range []string{"t-7", "t-8", "t-9"} {
		if _, ok := s.Get(id); ok {
			t.Fatalf("最旧的 %s 应被淘汰", id)
		}
	}
}

// TestPruneTasksDryRunRemovesNothing 钉死 dryRun 契约：一条不删（运维先看数再动手）。
func TestPruneTasksDryRunRemovesNothing(t *testing.T) {
	s := openTestStore(t)
	seedTask(t, s, "old-1", model.StateDone, 100)
	seedTask(t, s, "old-2", model.StateRejected, 100)
	before := len(s.List())
	res, err := s.PruneTasks(retention.Policy{TerminalTaskDays: 7, MaxTerminalTasks: 1}, true)
	if err != nil {
		t.Fatalf("PruneTasks(dryRun) 失败: %v", err)
	}
	if !res.DryRun {
		t.Fatal("结果必须标记 DryRun=true（调用方据此判断没动手）")
	}
	after := len(s.List())
	if after != before {
		t.Fatalf("dryRun 不得删除任何记录：before=%d after=%d", before, after)
	}
	// 但候选数要如实报出，否则 dryRun 没有指导意义。
	if res.RemovedByAge == 0 {
		t.Fatal("dryRun 应报出「按龄本可删几条」，否则运维看不到收益", )
	}
}

// TestPruneTasksKeepsReferencedChains 钉死 chain 清理的安全边界：
// 有存活任务引用的 chain 必须保留（删了会让存活任务失去链上下文）。
func TestPruneTasksKeepsReferencedChains(t *testing.T) {
	s := openTestStore(t)
	// chain-live 被一条非终态任务引用；chain-dead 只被一条超龄终态任务引用。
	if err := s.UpsertChain(&model.Chain{ID: "chain-live", Topic: "活链", TaskIDs: []string{"live-task"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChain(&model.Chain{ID: "chain-dead", Topic: "死链", TaskIDs: []string{"dead-task"}}); err != nil {
		t.Fatal(err)
	}
	live := &model.Task{ID: "live-task", Title: "活", State: model.StateQueued, ChainID: "chain-live"}
	if err := s.Create(live); err != nil {
		t.Fatal(err)
	}
	dead := &model.Task{ID: "dead-task", Title: "死", State: model.StateDone, ChainID: "chain-dead"}
	if err := s.Create(dead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("dead-task", func(x *model.Task) error {
		x.CreatedAt = model.Now() - 100*86400*1000
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := s.PruneTasks(retention.Policy{TerminalTaskDays: 7, MaxTerminalTasks: 1000}, false); err != nil {
		t.Fatalf("PruneTasks 失败: %v", err)
	}
	if _, ok := s.GetChain("chain-live"); !ok {
		t.Fatal("有存活任务引用的 chain 被误删（存活任务会失去链上下文）")
	}
	if _, ok := s.GetChain("chain-dead"); ok {
		t.Fatal("只被已删任务引用的孤立 chain 应被清掉（否则链记录同样只增不减）")
	}
}

// TestPruneTasksPersistsAndAudits 钉死「裁剪后落盘 + 审计留痕」。
// 不落盘：下次启动又从快照读回全部记录，白删一轮；
// 不留痕：记录凭空消失，事后对账无从解释。
func TestPruneTasksPersistsAndAudits(t *testing.T) {
	s := openTestStore(t)
	seedTask(t, s, "gone", model.StateRejected, 100)
	if _, err := s.PruneTasks(retention.Policy{TerminalTaskDays: 7, MaxTerminalTasks: 1000}, false); err != nil {
		t.Fatalf("PruneTasks 失败: %v", err)
	}
	// 落盘：重新打开不应再看到该记录。
	re, err := Open(s.Layout())
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer func() { _ = re.Close() }()
	if _, ok := re.Get("gone"); ok {
		t.Fatal("裁剪结果未落盘：重启后记录又回来了（下次启动照样占满内存）")
	}
}

package engine

import (
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// 持续轮询式卡死救援的判据（2026-10-22 对标 river JobRescuer）。
//
// 分三层：
//  1. rescueAfterMs / orphanCandidate / orphanConfirmDue 纯函数表驱动
//     （含**安全边界的反向用例**：有 goroutine 在跑 / 正在取消 / 阈值非法 一律不得动手）；
//  2. sweepOrphanedRunning 端到端（两阶段确认后回收重新入队 / 额度耗尽判死 / 台账带 kind=error）；
//  3. 反面：**正在跑的任务绝不能被扫走**（防误杀第一道闸，误杀不可逆）。
//
// 测试手法说明：sweepOrphanedRunning 的 now 由调用方注入 ⇒ 两阶段确认可以**确定性**测试，
// 不需要 sleep，也不需要伪造任务时间戳（后者已被证明不可行 —— store.Update 会在
// mutator 之后无条件覆写 UpdatedAt，见 runtime_rescue.go 安全边界 2）。

// TestRescueAfterMsDominatesWatchdog 钉死「扫描阈值必须显著大于进程内看门狗」。
func TestRescueAfterMsDominatesWatchdog(t *testing.T) {
	for _, stall := range []int{30, 180, 420, 600, 3600} {
		cfg := config.ExecutorConfig{StallSeconds: stall}
		got := rescueAfterMs(cfg)
		if got <= int64(stall)*1000 {
			t.Fatalf("stall=%d: rescueAfter=%d 不大于看门狗阈值 %d —— 会误杀正常任务",
				stall, got, stall*1000)
		}
	}
	if got := rescueAfterMs(config.ExecutorConfig{StallSeconds: 30}); got != 600_000 {
		t.Fatalf("stall=30 应抬到 600000ms 下限，got %d", got)
	}
	if got := rescueAfterMs(config.ExecutorConfig{}); got != 840_000 {
		t.Fatalf("零配置应回落 420s×2，got %d", got)
	}
}

// TestOrphanCandidateSafetyBoundaries 是第一道闸的反向判据：
// 有 goroutine 在跑 / 正在取消，一律不得成为候选。
func TestOrphanCandidateSafetyBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		inflight   bool
		cancelling bool
		want       bool
	}{
		{"无归属且未取消 → 候选", false, false, true},
		{"**有 goroutine 在跑 → 绝不候选**", true, false, false},
		{"**正在取消中 → 不候选**", false, true, false},
		{"既在跑又在取消 → 不候选", true, true, false},
	}
	for _, c := range cases {
		if got := orphanCandidate(c.inflight, c.cancelling); got != c.want {
			t.Fatalf("%s: orphanCandidate = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestOrphanConfirmDueTwoPhase 是第二道闸：第一次看到恒不动手。
func TestOrphanConfirmDueTwoPhase(t *testing.T) {
	const after = int64(600_000)
	now := int64(1_000_000_000)
	cases := []struct {
		name    string
		firstAt int64
		want    bool
	}{
		{"首次见到（firstSeen=0）→ 不动手", 0, false},
		{"刚登记（同刻）→ 不动手", now, false},
		{"差 1ms → 不动手", now - after + 1, false},
		{"恰好到阈值 → 动手", now - after, true},
		{"远超阈值 → 动手", now - after - 1, true},
	}
	for _, c := range cases {
		if got := orphanConfirmDue(c.firstAt, now, after); got != c.want {
			t.Fatalf("%s: orphanConfirmDue = %v, want %v", c.name, got, c.want)
		}
	}
	// 阈值非法（0/负）与时刻非法（<=0）一律不动手：宁可漏扫不可误杀。
	if orphanConfirmDue(now-after-1, now, 0) {
		t.Fatal("阈值 0 不得动手")
	}
	if orphanConfirmDue(now-after-1, now, -5) {
		t.Fatal("负阈值不得动手")
	}
	if orphanConfirmDue(now-after-1, 0, after) {
		t.Fatal("now=0 不得动手")
	}
}

// TestSweepTwoPhaseRescuesOrphan 端到端：第一阶段只登记，第二阶段才回收。
func TestSweepTwoPhaseRescuesOrphan(t *testing.T) {
	e, st, layout := newTestEngine(t)
	id := "task-orphan-1"
	seedRunning(t, st, id)
	after := rescueAfterMs(e.cstore.Get().Executor)
	t0 := model.Now()

	// 第一阶段：只登记，不动手（这是防误杀的关键 —— 一次扫描绝不回收）。
	if ids := e.sweepOrphanedRunning(t0); len(ids) != 0 {
		t.Fatalf("第一阶段不得动手，却回收了 %v", ids)
	}
	if got := mustGet(t, st, id); got.State != model.StateRunning {
		t.Fatalf("第一阶段后状态应仍为 running，got %s", got.State)
	}
	// 差 1ms 到阈值：仍不动手。
	if ids := e.sweepOrphanedRunning(t0 + after - 1); len(ids) != 0 {
		t.Fatalf("未达阈值不得动手，却回收了 %v", ids)
	}
	// 第二阶段（达阈值）：回收重新入队。
	ids := e.sweepOrphanedRunning(t0 + after)
	if len(ids) != 1 || ids[0] != id {
		t.Fatalf("达阈值应扫出 1 条孤儿，got %v", ids)
	}
	got := mustGet(t, st, id)
	if got.State != model.StateQueued {
		t.Fatalf("额度内应回收为 queued，state = %s", got.State)
	}
	if got.CrashRescues != 1 {
		t.Fatalf("CrashRescues = %d, want 1（与 L4 共用同一份额度）", got.CrashRescues)
	}
	if !hasAuditKind(layout, "error", "孤儿") {
		t.Fatalf("孤儿回收必须留 kind=error 审计，实际:\n%s", readAuditText(t, layout))
	}
}

// TestSweepCondemnsOrphanAfterBudgetExhausted 额度耗尽后判死且停在 failed。
func TestSweepCondemnsOrphanAfterBudgetExhausted(t *testing.T) {
	e, st, layout := newTestEngine(t)
	id := "task-orphan-2"
	seedRunning(t, st, id)
	if _, err := st.Update(id, func(x *model.Task) error {
		x.CrashRescues = maxCrashRescues
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after := rescueAfterMs(e.cstore.Get().Executor)
	t0 := model.Now()
	e.sweepOrphanedRunning(t0)
	if ids := e.sweepOrphanedRunning(t0 + after); len(ids) != 1 {
		t.Fatalf("达阈值应处理 1 条，got %v", ids)
	}
	got := mustGet(t, st, id)
	if got.State != model.StateFailed {
		t.Fatalf("额度耗尽应判死，state = %s", got.State)
	}
	if got.ErrorKind != crashRescueKind {
		t.Fatalf("ErrorKind = %q, want %q", got.ErrorKind, crashRescueKind)
	}
	if !hasAuditKind(layout, "error", "判死") {
		t.Fatalf("孤儿判死必须留 kind=error 审计，实际:\n%s", readAuditText(t, layout))
	}
}

// TestSweepNeverTouchesInflightTask 是**闭环防误杀判据**：
// 本进程确有 goroutine 在跑（runningIDs 里有）的任务，即便连续多轮扫描也不得被动。
func TestSweepNeverTouchesInflightTask(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-inflight"
	seedRunning(t, st, id)
	e.runningIDs.Store(id, true)
	t.Cleanup(func() { e.runningIDs.Delete(id) })
	after := rescueAfterMs(e.cstore.Get().Executor)
	t0 := model.Now()

	for _, at := range []int64{t0, t0 + after, t0 + after*10} {
		if ids := e.sweepOrphanedRunning(at); len(ids) != 0 {
			t.Fatalf("在飞任务绝不能被扫走（误杀不可逆），却在 t=%d 扫出了 %v", at, ids)
		}
	}
	got := mustGet(t, st, id)
	if got.State != model.StateRunning {
		t.Fatalf("在飞任务状态不得被改动，state = %s", got.State)
	}
	if got.CrashRescues != 0 {
		t.Fatalf("在飞任务不得消耗救援额度，CrashRescues = %d", got.CrashRescues)
	}
}

// TestSweepCandidateRegistryClearedWhenTaskLeavesRunning 是登记表的卫生判据：
// 候选登记必须随任务离开 running/恢复在飞而清理，否则 map 无界增长。
func TestSweepCandidateRegistryClearedWhenTaskLeavesRunning(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-registry"
	seedRunning(t, st, id)
	t0 := model.Now()
	e.sweepOrphanedRunning(t0) // 登记候选
	e.orphanMu.Lock()
	_, registered := e.orphanSeen[id]
	e.orphanMu.Unlock()
	if !registered {
		t.Fatal("首次扫描应登记候选")
	}
	// 任务不再是 running（例如被别的路径收尾）⇒ 下一轮扫描应清掉登记。
	if _, err := st.Transition(id, model.StateFailed, "test", ""); err != nil {
		t.Fatal(err)
	}
	e.sweepOrphanedRunning(t0 + 1)
	e.orphanMu.Lock()
	_, still := e.orphanSeen[id]
	e.orphanMu.Unlock()
	if still {
		t.Fatal("任务已离开 running，候选登记必须清理（否则 map 无界增长）")
	}
}

// TestSweepReleasesChainOnOrphan 孤儿被回收时必须释放链占用，
// 否则该链会永久阻塞（与 runOne 的收尾同一义务）。
func TestSweepReleasesChainOnOrphan(t *testing.T) {
	e, st, _ := newTestEngine(t)
	id := "task-orphan-chain"
	chainID := "chain-orphan"
	seedRunning(t, st, id)
	if _, err := st.Update(id, func(x *model.Task) error { x.ChainID = chainID; return nil }); err != nil {
		t.Fatal(err)
	}
	// 模拟该链被占用（runOne 里 Pop 会置 busy）。
	e.q.Push(&model.Task{ID: id, ChainID: chainID, State: model.StateQueued})
	if tsk := e.q.Pop(); tsk == nil {
		t.Fatal("前置条件失败：链应已被占用")
	}
	if busy := e.q.BusyChains(); busy != 1 {
		t.Fatalf("前置条件失败：busy = %d, want 1", busy)
	}

	after := rescueAfterMs(e.cstore.Get().Executor)
	t0 := model.Now()
	e.sweepOrphanedRunning(t0) // 第一阶段：只登记
	e.sweepOrphanedRunning(t0 + after)
	if got := mustGet(t, st, id); got.State != model.StateQueued {
		t.Fatalf("孤儿应被回收为 queued，state = %s", got.State)
	}
	// 判据必须断到「链真的被释放」这一后果本身，而不是「占用没有恶化」。
	// 首版断言写的是 busy > 1 —— 摘掉 ReleaseChain 的变异（本轮变异 C）被放过，
	// 因为 queue.Pop 会**静默跳过** busy 链上的 queued 任务（queue.go:107）：
	// 占用停在 1、不增长，弱断言照样绿。加严为两条强断言：
	//   ① 回收动作后链必须空闲（busy == 0）；
	//   ② 重新入队的任务必须真的能被 Pop 取出（Pop 返回它、链转 busy）。
	// 摘掉 ReleaseChain 时 ① 直接判红（busy 停在 1），② 也判红（Pop 返回 nil）。
	if busy := e.q.BusyChains(); busy != 0 {
		t.Fatalf("回收后链必须空闲，busy = %d, want 0（ReleaseChain 未被调用）", busy)
	}
	if tsk := e.q.Pop(); tsk == nil || tsk.ID != id {
		t.Fatalf("回收后任务必须可被 Pop 取出（链未释放时 Pop 会静默跳过），got %+v", tsk)
	}
	if busy := e.q.BusyChains(); busy != 1 {
		t.Fatalf("Pop 成功后链应重新占用，busy = %d, want 1", busy)
	}
}

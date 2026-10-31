package engine

// restart_count_test.go 守护「重启次数必须被真实累计」（2026-10-19 现场）。
//
// 现场：EngineState.RestartCount 在整个代码库里**只出现在结构体定义处**，
// 从未被任何代码写入 —— 于是 state.json 永远是 restartCount: 0。
//
// 危害（7x24 视角）：重启次数是判断「系统是否在反复崩溃」的核心健康指标。
// 恒为 0 会掩盖自愈频繁重启这种严重问题：运维看面板以为一直稳，
// 实际可能每次内存超阈值都在重启（只是「壳活着」）。
//
// 这与前几次同源：字段定义了、落盘了、展示链路也在，唯独**没人写它**。
//   TaskDirDays / 风险旋钮 / NormalizeRetention / recentItem.GoalID / transitionRecord.TaskID
//
// 判据：每次「重启后接续」（即 self-heal 停机 + 下次启动接续）必须把计数 +1。

import (
	"testing"

	"workbench/internal/model"
)

// TestSelfHealStopIncrementsRestartCount 断言：自愈停机时重启计数 +1。
//
// 语义：AbortForSelfHeal 表示「被动中断，重启后应接续」——
// 这正是「一次重启」的发生点，计数应在此累加。
func TestSelfHealStopIncrementsRestartCount(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	before := st.State().RestartCount

	if err := e.AbortForSelfHeal("测试：内存超阈值"); err != nil {
		t.Fatal(err)
	}
	after := st.State().RestartCount
	if after != before+1 {
		t.Fatalf("自愈停机应把重启计数 +1（否则面板永远显示 0，掩盖频繁重启）；before=%d after=%d", before, after)
	}
}

// TestUserStopDoesNotIncrementRestartCount 断言：用户主动停机**不**计数。
//
// 语义区分：用户主动 Stop 是「不想跑了」，不是「重启」。
// 若也计数，运维会把「用户停过几次」误读成「系统崩过几次」。
func TestUserStopDoesNotIncrementRestartCount(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	before := st.State().RestartCount
	if err := e.Stop(); err != nil {
		t.Fatal(err)
	}
	after := st.State().RestartCount
	if after != before {
		t.Fatalf("用户主动停机不应计入重启次数；before=%d after=%d", before, after)
	}
	_ = model.PhaseStopped
}

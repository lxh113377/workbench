package engine

// state_persist_test.go 守护「内存相位与磁盘 state.json 必须一致」（2026-10-19 现场）。
//
// 现场：老大要 7x24，我为了换二进制用了 --stop（CLI 跨进程盲写）+ 面板 POST /api/engine/start。
// 结果磁盘 state.json 停在 stopped，而引擎实际 running，且**再也纠正不过来**：
//
//   runStopCLI（另一个进程）：自己开 store，把 stopped 写盘 —— 不知道内存里引擎仍在跑；
//   Engine.Start()：如果内存已是 running，第 300 行直接 return nil，**不落盘**；
//   两者叠加 => 磁盘 stopped / 内存 running 永久分歧。
//
// 为什么对 7x24 致命：重启后的接续判据 shouldAutoStart(cfg, phase, resumeMarked) 读的是
// **磁盘 state.json**。磁盘说 stopped => 重启后不自启 => 无人值守链路断在重启这一步。
//
// 判据：Start() 即使内存已是 running，也必须把磁盘状态对齐（自愈式落盘），
// 因为磁盘可能被别的进程（CLI）写坏。

import (
	"testing"

	"workbench/internal/model"
)

// TestStartRepairsStaleDiskState 断言：磁盘被外部写成 stopped 后，
// 再调 Start() 必须把磁盘纠正回 running（而不是 early-return 放任分歧）。
func TestStartRepairsStaleDiskState(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)

	// 1) 正常启动一次，让磁盘 = running。
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Stop() }()
	if got := st.State().Phase; got != model.PhaseRunning {
		t.Fatalf("启动后磁盘相位应为 running，实际 %s", got)
	}

	// 2) 模拟「另一个进程（--stop CLI）把磁盘写成 stopped」，但内存引擎仍在 running。
	//    这正是现场：CLI 盲写 + 引擎没停。
	s := st.State()
	s.Phase = model.PhaseStopped
	s.StoppedAt = model.Now()
	if err := st.SaveState(s); err != nil {
		t.Fatal(err)
	}
	if got := st.State().Phase; got != model.PhaseStopped {
		t.Fatalf("前置条件失败：磁盘应被写成 stopped，实际 %s", got)
	}

	// 3) 关键断言：再调一次 Start()（内存已是 running => 走 early-return 分支），
	//    磁盘必须被纠正回 running。
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	if got := st.State().Phase; got != model.PhaseRunning {
		t.Fatalf("Start() 必须把被外部写坏的磁盘状态纠正为 running，实际 %s —— 重启接续判据会因此误判", got)
	}
}

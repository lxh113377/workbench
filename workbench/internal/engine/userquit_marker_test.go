package engine

// userquit_marker_test.go 守护「用户主动退出必须可被外部守护者识别」（2026-10-19 现场）。
//
// 现场：`--quit` 走优雅路径退出、state.json 正确落 stopped（尊重用户意愿），
// 但外部守护者（tests/keepalive.ps1）的分支是：
//     keepalive.ps1:161  # stopped = 优雅停机后；idle = 从未启动。两者都拉回 running。
//     keepalive.ps1:162  if ($proc -and ($state -eq "stopped" -or $state -le "idle")) { POST start }
// 于是用户 --quit 后 **60 秒内又被拉起来**，--quit 等于不生效。
//
// 为什么要在产品侧解决：keepalive 是外部脚本（他会话产物），产品必须先把
// 「这是用户主动退出」这件事**表达出来**，外部才有据可依。
// 否则任何守护者都只能在「停机」与「用户停机」之间瞎猜。
//
// 表达方式（二选一都提供，便于不同消费者）：
//   ① state.json 增加 UserQuit 标记（结构化消费者）；
//   ② runtime/user-quit.flag 空文件（shell/PS 一行即可判断）。
//
// 判据：注记后两者都存在；用户点「开始」（Start）后两者都被清除。

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUserQuitMarkerSetAndCleared 断言：标记既会被置上，也会被用户「开始」清掉。
func TestUserQuitMarkerSetAndCleared(t *testing.T) {
	e, st, layout := newAutonomyTestEngine(t)
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Stop() }()

	// 用户主动退出：置标记
	if err := e.MarkUserQuit(); err != nil {
		t.Fatalf("MarkUserQuit 应成功: %v", err)
	}
	if !st.State().UserQuit {
		t.Fatal("state.json 必须落 UserQuit 标记，否则外部守护者无从判断用户意愿")
	}
	flag := filepath.Join(layout.RuntimeDir, "user-quit.flag")
	if _, err := os.Stat(flag); err != nil {
		t.Fatalf("必须写 user-quit.flag（shell/PS 一行即可判断），实际: %v", err)
	}

	// 用户点「开始」：清标记（恢复正常守护语义）
	if err := e.Start(); err != nil {
		t.Fatal(err)
	}
	if st.State().UserQuit {
		t.Fatal("用户点「开始」后必须清掉 UserQuit 标记")
	}
	if _, err := os.Stat(flag); err == nil {
		t.Fatal("用户点「开始」后必须删掉 flag 文件")
	}
}

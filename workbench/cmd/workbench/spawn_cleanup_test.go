package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CI 连续假红的根因判据（2026-10-22 新增）。
//
// 实测形态（远端 CI 连续 8 次 red，其中一次还是纯文档提交）：
//
//	--- FAIL: TestSilentDoesNotOpenPanel
//	    testing.go:1619: TempDir RemoveAll cleanup: unlinkat ...\\TestSilentDoesNotOpenPanel*\\002:
//	    The process cannot access the file because it is being used by another process.
//	--- FAIL: TestNonSilentDeclaresPanel   （同型）
//
// 结论：契约判据本身全绿，红来自**收尾方式** —— 测试临时根目录里有进程还活着、
// 或还有未释放的句柄，RemoveAll 就删不净；而 t.TempDir() 把「删不净」记成测试 FAIL。
// 下面两条判据在**可在本机复现**的最小同型场景上钉死处置（不依赖真实 workbench 实例）。
//
// 复现口径（本机实测 2026-10-22）：
//   - 子进程把目录当 CWD 并不阻止删除（本机实测 OK）；
//   - **未共享删除权的文件句柄**才阻止删除（实测 FAIL: 'held.txt' is being used by another process）。
// 故判据用「子进程持有文件句柄」构造占用，而不是用 CWD 构造。

// TestKillTreeReapsGrandchild —— killTree 必须连孙进程一起收。
//
// 场景：cmd.exe -> powershell.exe 两层；孙进程在 dir 下持有 child.lock（无共享删除权）并睡 60s。
// 只杀 cmd（旧实现 os.Process.Kill）时孙进程仍活着占住句柄，RemoveAll 必失败 ——
// 这正是 CI 判红的形态。用 taskkill /T 收整棵树后目录可删。
//
// 命令里刻意不出现双引号：Go 经 CreateProcess 传参时会把内层双引号转义成 \"，
// 而 cmd.exe 不认这种转义 —— 实测首版「cmd /c powershell ... -Command "..."」
// 的孙进程根本没起来（判据空跑）。这里只用单引号与管道无关的语句。
func TestKillTreeReapsGrandchild(t *testing.T) {
	dir := newSpawnDir(t)
	lock := filepath.Join(dir, "child.lock")
	script := "$f=[IO.File]::Open('child.lock','Create','ReadWrite','None'); Start-Sleep -Seconds 60"
	parent := hiddenTestCmd("cmd.exe", "/c", "powershell -NoProfile -NonInteractive -Command "+script)
	parent.Dir = dir
	if err := parent.Start(); err != nil {
		t.Fatalf("起「父->孙」场景失败: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(lock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			killTree(parent.Process)
			t.Skip("孙进程未在 20s 内落地（环境限制），判据跳过而非假绿")
		}
		sleepMs(100)
	}
	killTree(parent.Process)
	sleepMs(500) // 留给 Windows 释放句柄
	if !removeSpawnDirWithin(t, dir, 3*time.Second) {
		t.Fatalf("killTree 后目录仍删不净：孙进程未被回收（这正是 CI TempDir 清理判红的形态）")
	}
}

// TestRemoveSpawnDirDoesNotFailOnLockedDir —— 目录删不净是环境残留，不得判红。
//
// 本测试自己持有 dir 内文件句柄（Go 打开文件不带 FILE_SHARE_DELETE），
// 然后直接调用清理函数：若清理函数改用 t.Fatalf/t.Errorf，本测试自身立刻 red（负向对照）。
// 判据不成立时**跳过**（Skip），不把「没测成」当成「已通过」。
func TestRemoveSpawnDirDoesNotFailOnLockedDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "wb-spawn-locked-")
	if err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "locked.txt"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("建占位句柄失败: %v", err)
	}
	if removed := removeSpawnDirWithin(t, dir, time.Second); removed {
		_ = f.Close()
		t.Skip("本机句柄未阻止删除（环境限制），判据不成立")
	}
	// 走到这里即通过：清理函数选择了「留证不判红」。
	_ = f.Close()
	if !removeSpawnDirWithin(t, dir, 2*time.Second) {
		t.Logf("收尾仍未删净（环境残留，按设计不判红）: %s", dir)
	}
}

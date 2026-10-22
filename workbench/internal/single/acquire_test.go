package single

// acquire_test.go -- 单实例锁的跨进程正确性判据（2026-10-22 补）。
//
// 为什么必须有：Acquire/Release 是本包唯一的**互斥正确性**代码，也是
// 「后台实例爆炸」事故（2026-10-20，实测 24 个 workbench.exe 并存）的防线。
// 此前该包只有 ProbeExactPort 的端口判据（16.2% 覆盖，贴着 16% 门禁线），
// 锁本身零测试 —— 而它失效的后果是**多实例并存**：多个 scheduler 同时抢同一份
// queue.json，任务被重复执行、账本互相覆盖。这类缺陷不会编译失败、不会测试失败，
// 只会在现场表现为「怎么后台显示有这么多个人AI工作台在运行」。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func lockPathIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "runtime", "instance.lock")
}

// TestAcquireWritesPIDPayload 钉死锁文件内容契约：持有者必须写入 pid + at。
// 为什么重要：故障排查时（现场「多个实例」）第一件事就是读锁文件判断「谁持锁」。
// 若 payload 缺失或不是合法 JSON，这一手证据就没了。
// 实现注记（2026-10-22 实测）：锁文件被 LockFile **独占**后，同进程也读不了它 ——
// 报 'The process cannot access the file because another process has locked a portion'。
// 这正是独占锁的语义（不是缺陷）。故 payload 只能在 Release 之前用**已打开的句柄**读；
// 但 Release 会清空内容，所以这里改为：先读自身句柄副本。
func TestAcquireWritesPIDPayload(t *testing.T) {
	p := lockPathIn(t)
	inst, running, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	if running {
		t.Fatal("首次获取不得报告 alreadyRunning")
	}
	defer inst.Release()

	// 用自己的句柄读（句柄持有独占锁，同进程经句柄读取是允许的）。
	buf := make([]byte, 512)
	n, rerr := inst.f.ReadAt(buf, 0)
	if rerr != nil && n == 0 {
		t.Fatalf("经自身句柄读锁文件失败: %v", rerr)
	}
	raw := buf[:n]
	var payload map[string]any
	if jerr := json.Unmarshal(raw, &payload); jerr != nil {
		t.Fatalf("锁文件必须是合法 JSON（取证依赖），got %q: %v", string(raw), jerr)
	}
	if pidv, ok := payload["pid"]; !ok || pidv == nil {
		t.Fatalf("锁文件缺 pid 字段: %s", string(raw))
	}
	if _, ok := payload["at"]; !ok {
		t.Fatalf("锁文件缺 at 字段: %s", string(raw))
	}
}

// TestSecondAcquireIsRefused 是**核心互斥判据**：同进程内第二次 Acquire 必须
// 报告 alreadyRunning=true，且不得返回可用实例。
//
// 注意：同进程内第二次打开同一文件并 LockFile，Windows 会因「已持有独占锁」而失败 ——
// 这正是锁的语义。判据落在这里而不是起两个进程，是为了能在 CI 上确定性执行。
func TestSecondAcquireIsRefused(t *testing.T) {
	p := lockPathIn(t)
	first, running, err := Acquire(p)
	if err != nil || running {
		t.Fatalf("首次 Acquire 应成功且非 alreadyRunning: err=%v running=%v", err, running)
	}
	defer first.Release()

	second, running2, err2 := Acquire(p)
	if err2 != nil {
		t.Fatalf("被拒绝时不得返回错误（alreadyRunning 是正常结果，不是错误）: %v", err2)
	}
	if !running2 {
		t.Fatal("第二次 Acquire 必须报告 alreadyRunning=true —— 否则就是「多实例并存」事故的入口")
	}
	if second != nil {
		t.Fatal("已被占用时不得返回可用实例（调用方会据此认为自己是唯一实例）")
	}
}

// TestReleaseAllowsReacquire 钉死释放语义：Release 后必须能再次获取。
// 若释放不彻底（未解锁/未关闭句柄），重启链会永久锁死 —— 现场表现为「进程退出后
// 再也起不来」，只能手工删锁文件。
func TestReleaseAllowsReacquire(t *testing.T) {
	p := lockPathIn(t)
	first, _, err := Acquire(p)
	if err != nil {
		t.Fatalf("首次 Acquire 失败: %v", err)
	}
	first.Release()

	again, running, err := Acquire(p)
	if err != nil {
		t.Fatalf("Release 后再次 Acquire 失败: %v", err)
	}
	if running {
		t.Fatal("Release 后不应再报告 alreadyRunning（锁未被真正释放）")
	}
	again.Release()
}

// TestReleaseUnlocksBeforeClosing 是**解锁语义**的加严判据（2026-10-22 实测教训）：
// 第一版只写 TestReleaseAllowsReacquire，把 Release 里的 unlockFile() 整行删掉，
// 它**照样通过** —— 因为 Close() 也会释放文件锁，判据根本没碰到 unlock。
// 这正是本仓在册的「判据没碰到被测代码」失效形态（与看门狗判据第一版同源）。
//
// 加严方式：Release 之后**不再重新 Acquire**（那会重新加锁，掩盖问题），
// 而是直接尝试用第二个句柄对该文件加锁 —— 若 unlockFile 缺失，文件仍被旧句柄独占，
// 二句柄加锁会失败。注意 Release 已 Close，故这里真正验证的是「Close 前是否先解锁」：
// 若 unlock 缺失，Close 仍会释放，所以必须在 **Release 内部**制造可观测差异。
// 结论：单独验证 unlockFile 需要用「不 Close 的路径」或反射，成本高于价值；
// 改为**语义等价判据**——锁定期间同进程经新句柄也拿不到锁（这是独占性的正向证明），
// 并显式记录「unlock+Close 双释放」是刻意冗余（谁先谁后都能放开，安全性优先）。
func TestLockIsExclusiveAgainstNewHandle(t *testing.T) {
	p := lockPathIn(t)
	inst, running, err := Acquire(p)
	if err != nil || running {
		t.Fatalf("Acquire 失败: err=%v running=%v", err, running)
	}
	defer inst.Release()

	// 独占性正向证明：新开一个句柄尝试加锁，必须失败。
	// 这是「锁真的排他」的直接证据 —— 比「Release 后能重取」更强（后者可被 Close 掩盖）。
	f2, oerr := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0o600)
	if oerr != nil {
		t.Fatalf("二次打开失败: %v", oerr)
	}
	defer func() { _ = f2.Close() }()
	if lerr := lockFileExclusive(f2); lerr == nil {
		t.Fatal("锁不排他：第二个句柄竟然加锁成功 —— 两个实例会同时认为自己是唯一实例")
	}
}

// TestReleaseKeepsFilePresent 钉死「Release 只清空内容、不删文件」。
// 为什么：锁文件被删除会引入 TOCTOU —— 另一个进程可能对刚被删的 inode 加锁，
// 而第三个进程对新建的同名文件加锁，两者都持锁。保留文件是正确做法。
func TestReleaseKeepsFilePresent(t *testing.T) {
	p := lockPathIn(t)
	inst, _, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	inst.Release()
	if _, serr := os.Stat(p); serr != nil {
		t.Fatalf("Release 不得删除锁文件（删了会引入 TOCTOU 双持锁）: %v", serr)
	}
}

// TestReleaseClearsPayload 钉死「释放后锁文件内容被清空」。
// 为什么：锁文件残留旧 pid 会让事后取证误判（以为那个 pid 还持锁）。
func TestReleaseClearsPayload(t *testing.T) {
	p := lockPathIn(t)
	inst, _, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	inst.Release()

	raw, rerr := os.ReadFile(p)
	if rerr != nil {
		t.Fatalf("释放后锁文件应仍存在（只清空内容）: %v", rerr)
	}
	if len(raw) != 0 {
		t.Fatalf("Release 后锁文件内容必须清空，got %q（残留旧 pid 会误导取证）", string(raw))
	}
}

// TestReleaseIsIdempotentAndNilSafe 钉死防御性契约：
// Release 会被 defer 调用，可能重复触发或作用于 nil —— 不得 panic。
func TestReleaseIsIdempotentAndNilSafe(t *testing.T) {
	var nilInst *Instance
	nilInst.Release() // 不得 panic

	p := lockPathIn(t)
	inst, _, err := Acquire(p)
	if err != nil {
		t.Fatalf("Acquire 失败: %v", err)
	}
	inst.Release()
	inst.Release() // 第二次不得 panic
}

// TestAcquireCreatesParentDir 钉死目录自建：锁路径常位于 <root>/runtime/，
// 首次启动时该目录可能不存在。Acquire 必须自建父目录而不是报错退出
// （那会让首次安装永远起不来）。
func TestAcquireCreatesParentDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a", "b", "c", "instance.lock")
	inst, running, err := Acquire(p)
	if err != nil {
		t.Fatalf("父目录不存在时 Acquire 应自建: %v", err)
	}
	if running {
		t.Fatal("首次获取不得报告 alreadyRunning")
	}
	defer inst.Release()
	if _, serr := os.Stat(p); serr != nil {
		t.Fatalf("锁文件未创建: %v", serr)
	}
}

// TestConcurrentAcquireExactlyOneWins 是并发正确性判据：N 个 goroutine 同时抢同一把锁，
// **恰好一个**成功，其余全部 alreadyRunning。
//
// 为什么必须有：单实例锁失效的直接后果就是并发实例抢同一份数据。
// 用 -race 跑本用例能同时验证锁路径上没有数据竞争。
func TestConcurrentAcquireExactlyOneWins(t *testing.T) {
	p := lockPathIn(t)
	const n = 8
	var wins int64
	var refused int64
	var wg sync.WaitGroup
	insts := make([]*Instance, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			inst, running, err := Acquire(p)
			if err != nil {
				return // 错误不计入任何一方，由最终断言抓（wins+refused 不足 n）
			}
			if running {
				atomic.AddInt64(&refused, 1)
				return
			}
			atomic.AddInt64(&wins, 1)
			insts[i] = inst
		}()
	}
	close(start)
	wg.Wait()
	defer func() {
		for _, in := range insts {
			if in != nil {
				in.Release()
			}
		}
	}()

	if wins != 1 {
		t.Fatalf("并发抢锁必须恰好 1 个成功，实际 %d（refused=%d）—— 多于 1 即多实例并存", wins, refused)
	}
	if refused != n-1 {
		t.Fatalf("其余 %d 个应全部报告 alreadyRunning，实际 %d（wins=%d）", n-1, refused, wins)
	}
}

package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 看门狗活跃度双信号（2026-10-22 对标 river watchStuck 后新增的覆盖）。
//
// 现场形态：artifact 产出协议下模型"先静默写文件、最后才吐 final 事件"，
// 长文写作类任务的静默写盘期可达数分钟，期间 stdout 一条事件都没有。
// 只认事件的旧判据会在 StallSeconds 到期时把任务判成卡死并杀进程树，
// **已写出的产物一并丢失** —— 这是不可逆的产出损失，故本文件用判据钉住。
//
// 判据强度说明：本文件全部直接测 **IsStalled**（看门狗 goroutine 实际调用的纯函数），
// 而不是在测试里重写一遍判据逻辑。重写版看似通过，变异测试一回退就照样绿（本轮实测踩到），
// 直接测真实判据才能让"改回单信号"这类回归立刻判红。

// TestLatestOutputMtimeMsFindsNewestFile 验证扩展名白名单与"取最新"语义。
func TestLatestOutputMtimeMsFindsNewestFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.md")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	// 非白名单扩展名：即使更新也不得被当成产出信号。
	ignored := filepath.Join(dir, "run.log")
	if err := os.WriteFile(ignored, []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	newTime := time.Now()
	if err := os.Chtimes(ignored, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "article.md")
	if err := os.WriteFile(fresh, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	midTime := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(fresh, midTime, midTime); err != nil {
		t.Fatal(err)
	}

	got := LatestOutputMtimeMs(dir)
	want := midTime.UnixMilli()
	if got != want {
		t.Fatalf("LatestOutputMtimeMs = %d, want %d（应取最新产出文件，忽略 .log）", got, want)
	}
}

// TestLatestOutputMtimeMsEmptyAndMissing 验证无信号时的退化行为。
func TestLatestOutputMtimeMsEmptyAndMissing(t *testing.T) {
	dir := t.TempDir()
	if got := LatestOutputMtimeMs(dir); got != 0 {
		t.Fatalf("空目录应返回 0，got %d", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LatestOutputMtimeMs(dir); got != 0 {
		t.Fatalf("只有非白名单文件应返回 0，got %d", got)
	}
	if got := LatestOutputMtimeMs(filepath.Join(dir, "no-such-dir")); got != 0 {
		t.Fatalf("不存在目录应返回 0，got %d", got)
	}
	if got := LatestOutputMtimeMs("   "); got != 0 {
		t.Fatalf("空路径应返回 0，got %d", got)
	}
}

// TestStallLimitForThinking 钉死阈值放大语义（原内联在看门狗 goroutine 里，现抽成纯函数）。
func TestStallLimitForThinking(t *testing.T) {
	if got := stallLimitFor(420, false); got != 420*time.Second {
		t.Fatalf("无 thinking 帧时应为基础阈值，got %v", got)
	}
	// thinking 帧出现 => 2 倍且不低于 600 秒。
	if got := stallLimitFor(420, true); got != 840*time.Second {
		t.Fatalf("420 基础 + thinking 应为 840s，got %v", got)
	}
	if got := stallLimitFor(100, true); got != 600*time.Second {
		t.Fatalf("小基础值 + thinking 应抬到 600s 下限，got %v", got)
	}
	// 零值/负值回落 420 秒默认。
	if got := stallLimitFor(0, false); got != 420*time.Second {
		t.Fatalf("零配置应回落 420s，got %v", got)
	}
	if got := stallLimitFor(-5, false); got != 420*time.Second {
		t.Fatalf("负配置应回落 420s，got %v", got)
	}
}

// TestIsStalledDualSignal 是本轮的核心契约：**任一信号在前进就不判卡死**。
// 直接调用 IsStalled（看门狗真实判据），三种时序逐条断言。
func TestIsStalledDualSignal(t *testing.T) {
	const base = 420
	now := time.Now().UnixMilli()
	limitMs := int64(420 * 1000)

	// ① 事件过期但文件刚写（静默写盘期）=> 不得判卡死。
	dir := t.TempDir()
	f := filepath.Join(dir, "article.md")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsStalled(now-limitMs-60_000, dir, base, false, now) {
		t.Fatal("写盘期（事件旧、文件新）不得判卡死 —— 这正是本轮要修的误杀")
	}
	// ② 文件过期但事件刚来（正在长思考/流式输出）=> 不得判卡死。
	if IsStalled(now-10_000, dir, base, false, now) {
		t.Fatal("流式输出期（事件新）不得判卡死")
	}
	// ③ 两个信号都过期 => 必须判卡死（否则看门狗失效）。
	// 必须先把文件时间也推到过去，否则"文件刚写"会让双信号判据放行。
	stale := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(f, stale, stale); err != nil {
		t.Fatal(err)
	}
	if !IsStalled(now-limitMs-60_000, dir, base, false, now) {
		t.Fatal("两个信号都过期时必须判卡死，否则看门狗形同虚设")
	}
	// ④ 无目录（external 模式下 workDir 可能是空串）时只认事件。
	if IsStalled(now-10_000, "", base, false, now) {
		t.Fatal("无目录时事件信号仍有效")
	}
	if !IsStalled(now-limitMs-60_000, "", base, false, now) {
		t.Fatal("无目录且事件过期必须判卡死")
	}
	// ⑤ 两个信号都没有（0 与空目录）=> 启动初期不判卡死。
	if IsStalled(0, "", base, false, now) {
		t.Fatal("无任何进展证据时不得判卡死（启动初期）")
	}
	// ⑥ thinking 帧放大阈值：事件只过期 500s 时，420 基础会判卡死但 840 不判。
	if IsStalled(now-500_000, "", base, false, now) != true {
		t.Fatal("无 thinking 帧时 500s 过期应判卡死")
	}
	if IsStalled(now-500_000, "", base, true, now) {
		t.Fatal("有 thinking 帧时阈值放大到 840s，500s 过期不应判卡死")
	}
}

// TestIsStalledUsesFileSignal 用**真实文件 mtime** 驱动判据，
// 证明 IsStalled 真的去读盘了（而不是只吃传入的事件时间）。
// 这条同时是变异测试的抓手：把 IsStalled 里的 LatestOutputMtimeMs 调用删掉，
// 本用例立刻 FAIL。
func TestIsStalledUsesFileSignal(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "draft.md")
	if err := os.WriteFile(f, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	// 事件停在 30 分钟前（早该判卡死），但文件刚写。
	if IsStalled(now-30*60_000, dir, 420, false, now) {
		t.Fatal("IsStalled 必须采信产出文件 mtime：文件刚写时不得判卡死")
	}
	// 把文件时间改到 30 分钟前，此时两个信号都过期，必须判卡死。
	old := time.Now().Add(-30 * time.Minute)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatal(err)
	}
	if !IsStalled(now-30*60_000, dir, 420, false, now) {
		t.Fatal("文件也过期后必须判卡死（证明判定确实由文件 mtime 驱动）")
	}
}

package engine

// 「重复快照次数下降」的实测判据（2026-10-28 性能修复配套；只在 -bench 时跑）。
//
// 收益口径 = 单价 x 次数，两条读数分开取，不口算：
//   单价 = internal/store 的 BenchmarkStoreList5k（全量 List 一次 1.97-2.75ms / 2.9MB）
//   次数 = 本文件两个基准的耗时比。改造前 StallReason() 要算 3 份全量快照
//         （StallReason 自身 + upstreamFailingStall + queuedWaitingForLane 各一次），
//         改造后 StallReasonWith(e.Stats()) 只算 1 份（就是调用方那一份）⇒ 理论比值 3.0。
//
// 阈值取 2.5（留 17% 余量给负载抖动）；若参数根本没被消费，比值会退化成 1.0 判红 ——
// 这就是本判据的牙（同一形态的行为级负控另见 stall_reason_with_test.go 的注入对照）。
//
// 为什么不复用 newTestEngine：它绑死 t.TempDir()/t.Cleanup()，benchmark 传不进 *testing.T。
// 这里自建同构引擎，并**必须**按 smoke 那套收尾顺序排空在途 + 关 store（WAL 长句柄），
// 否则 Windows 下 b.TempDir 的 RemoveAll 会 "used by another process"（本仓在册坑）。
//
// 跑法（可复现）：
//   go test ./internal/engine -run XXX -bench StallReasonCalls -benchtime=200x -benchmem

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/templates"
)

func newBenchEngine(b *testing.B) (*Engine, *store.Store) {
	b.Helper()
	layout, err := config.BuildLayout(b.TempDir(), "")
	if err != nil {
		b.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		b.Fatal(err)
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		b.Fatal(err)
	}
	reg, err := templates.Open(layout.DataRoot + string(os.PathSeparator) + "templates.json")
	if err != nil {
		b.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		b.Fatal(err)
	}
	e, err := New(layout, cstore, st, reg, nil)
	if err != nil {
		b.Fatal(err)
	}
	e.SetRunner(&noopTestRunner{})
	e.ReloadConfig()
	// 收尾：先停机排空，再关 store（WAL 句柄），最后才轮到 b.TempDir 删目录。
	b.Cleanup(func() {
		_ = e.Stop()
		_ = st.Close()
	})
	return e, st
}

// fillStallShape 把引擎填成与生产同构的形态：3000 条终态大快照 + 一条排队 + 闸门按住，
// 使 StallReason 必然命中 QueuedNoLane 分支（两个入口走同一条路径，比值才有意义）。
// 构造分开（b.TempDir 与 t.TempDir 不是同一套 TB），填充逻辑只此一份。
func fillStallShape(tb testing.TB, e *Engine, st *store.Store, prefix string) {
	tb.Helper()
	e.mu.Lock()
	e.phase = model.PhaseRunning
	e.mu.Unlock()
	for i := 0; i < 3000; i++ {
		id := prefix + strconv.Itoa(i)
		if err := st.Create(&model.Task{
			ID: id, Title: "基准任务 " + id, Prompt: "p",
			CreatedAt: model.Now(), UpdatedAt: model.Now(),
		}); err != nil {
			tb.Fatal(err)
		}
		if _, err := st.Transition(id, model.StateRejected, "bench", "基准"); err != nil {
			tb.Fatal(err)
		}
	}
	qid := prefix + "queued"
	if err := st.Create(&model.Task{
		ID: qid, Title: "排队基准任务", Prompt: "p",
		CreatedAt: model.Now(), UpdatedAt: model.Now(),
	}); err != nil {
		tb.Fatal(err)
	}
	if _, err := st.Transition(qid, model.StateApproved, "bench", ""); err != nil {
		tb.Fatal(err)
	}
	if _, err := st.Transition(qid, model.StateQueued, "bench", ""); err != nil {
		tb.Fatal(err)
	}
	// 闸门按住 + 限流第 3 档 ⇒ queuedWaitingForLane 必然命中。
	e.poolWaitUntil.Store(model.Now() + 20*60*1000)
	e.rateLimitMu.Lock()
	e.rateLimit.consecutive = 3
	e.rateLimit.lastAt = model.Now() - 60_000
	e.rateLimitMu.Unlock()

	if got := e.StallReason().Code; got != StallQueuedNoLane {
		tb.Fatalf("前置未成立：应命中 %s，实得 %q（读数将空转）", StallQueuedNoLane, got)
	}
}

func benchStallEngine(b *testing.B) *Engine {
	b.Helper()
	e, st := newBenchEngine(b)
	fillStallShape(b, e, st, "bench-")
	return e
}

func BenchmarkStallReasonCallsLegacy(b *testing.B) {
	e := benchStallEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.StallReason()
	}
}

func BenchmarkStallReasonCallsWith(b *testing.B) {
	e := benchStallEngine(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.StallReasonWith(e.Stats())
	}
}

// TestNoInternalStatsInStallPath 本项优化的防回归判据（源码级结构断言，不靠计时）。
//
// 为什么不用计时比当判据（本轮一手实踩，写死在此以免下一个人重踩）：
// 第一版我写的是「StallReason() 与 StallReasonWith(e.Stats()) 耗时比 >= 2.5」，
// 实测 0.69 判红 —— 因为改完之后 StallReason() 只是 StallReasonWith(e.Stats()) 的薄封装，
// 两边快照次数**已经相同**。那条计时比测的是「封装有没有额外开销」，不是「新旧差多少」。
// 拿它当收益证据 = 判据测错了对象（假红，与假绿同级）。
// ⇒ 「快照次数下降」是**结构事实**，就用结构判据钉：判据路径上不允许再出现 e.Stats()。
// 负向对照（本轮已实测）：把任一 e.Stats() 注回 StallReasonWith 体内，本判据立刻判红。
func TestNoInternalStatsInStallPath(t *testing.T) {
	src, err := os.ReadFile("stall_reason.go")
	if err != nil {
		t.Fatalf("读不到被测源文件: %v", err)
	}
	text := string(src)
	bodies := map[string]string{
		"StallReasonWith":      sliceFuncBody(t, text, "func (e *Engine) StallReasonWith("),
		"upstreamFailingStall": sliceFuncBody(t, text, "func (e *Engine) upstreamFailingStall("),
		"queuedWaitingForLane": sliceFuncBody(t, text, "func (e *Engine) queuedWaitingForLane("),
	}
	for name, body := range bodies {
		if !strings.Contains(body, "stats") {
			t.Fatalf("%s 体内已不引用 stats 形参 ⇒ 判据会空转（签名还在但不再用它）", name)
		}
		if strings.Contains(body, "e.Stats()") {
			t.Fatalf("%s 体内又出现 e.Stats() ⇒ 重复快照回归，本轮省下的 List() 被吐回去了", name)
		}
	}
	// 兼容入口必须仍然自算一份：否则「不传就报错」会改变对外语义（目标要求 1）。
	entry := sliceFuncBody(t, text, "func (e *Engine) StallReason() StallReason {")
	if !strings.Contains(entry, "StallReasonWith(e.Stats())") {
		t.Fatal("StallReason() 兼容入口必须委托 StallReasonWith(e.Stats())，否则对外语义变了")
	}
}

// sliceFuncBody 取某个顶层 func 的函数体（到下一个顶层 func 或文件尾为止）。
// 换行一律走 nl 常量而不写字面转义：本文件的判据文本经多层工具传输时反斜杠会被吃掉
// （本仓在册教训「多层字符串改写」，本轮我自己刚撞过一次）。
func sliceFuncBody(t *testing.T, text, sig string) string {
	t.Helper()
	const nl = "\n"
	i := strings.Index(text, sig)
	if i < 0 {
		t.Fatalf("源文件里找不到 %q（判据将空转）", sig)
	}
	rest := text[i:]
	k := strings.Index(rest, nl)
	if k < 0 {
		t.Fatalf("%q 后面没有内容", sig)
	}
	rest = rest[k+1:]
	if j := strings.Index(rest, nl+"func "); j >= 0 {
		return rest[:j]
	}
	return rest
}

func benchStallEngineForTest(t *testing.T) *Engine {
	t.Helper()
	layout, err := config.BuildLayout(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := templates.Open(layout.DataRoot + string(os.PathSeparator) + "templates.json")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout)
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(layout, cstore, st, reg, nil)
	if err != nil {
		t.Fatal(err)
	}
	e.SetRunner(&noopTestRunner{})
	e.ReloadConfig()
	// 收尾顺序同 smoke：先停引擎排空在途，再关 store 放掉 WAL 句柄，最后 t.TempDir 删目录。
	t.Cleanup(func() {
		_ = e.Stop()
		_ = st.Close()
	})
	fillStallShape(t, e, st, "tbench-")
	return e
}

// 注：这里不留 timeIt —— 计时判据已被上面的结构判据取代（见 TestNoInternalStatsInStallPath
// 的注释），留一个没人调用的未导出函数会被「死代码」门禁判红。

package modelpool

// failover_kind_test.go 守护「失败类别必须能传递到上层」（2026-10-19 现场）。
//
// 现场（真实运行日志）：34 条 kind=boot_failed，其中 elapsedMs 有 378478ms（6 分钟）的，
// 与「启动失败」语义自相矛盾。attempts.jsonl 里写着：
//   {"kind":"boot_failed","elapsedMs":106371,"error":"任务无最终答案"}
//
// 缺陷链：
//   judge() 正确判出 bad_exit（空产出）；
//   但 FailoverRunner 失败时只上抛 lastErr，**不把 kind 写回 res.ErrorKind**；
//   engine 的兜底分支（runErr 非 nil 且无 ErrorKind）一律记成 boot_failed。
// => 用户看到「启动失败」，实际是「模型产不出终稿」，排查方向被误导。
//
// 判据：Run 失败返回时，res 必须带上非空的 ErrorKind，
// 且该 kind 必须等于 judge 判定的类别（不能丢失）。

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// TestEmptyResultFailoverCarriesKind 断言：整链失败返回时带上正确的 ErrorKind。
//
// 用「空产出且无 ErrorKind」的 fake runner，模拟现场最典型的场景：
// 模型跑了很久（elapsed 大），但产不出最终答案。
func TestEmptyResultFailoverCarriesKind(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown), spec("b", 20, 50, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	// 两个模型都返回「空产出、无 ErrorKind、跑了一些时间」
	empty := &emptyOutputRunner{}
	fr := NewFailoverRunner(empty, m, sch)

	res, err := fr.Run(context.Background(), &model.Task{ID: "t-empty"}, "a")
	if err == nil {
		t.Fatal("空产出应算失败（err 非 nil）")
	}
	if res == nil {
		t.Fatal("失败时也应返回 res（否则上层拿不到 ErrorKind，只能兜底成 boot_failed）")
	}
	kind := strings.TrimSpace(res.ErrorKind)
	if kind == "" {
		t.Fatal("res.ErrorKind 不得为空：空值会让 engine 兜底成 boot_failed（启动失败）")
	}
	if kind == string(executor.KindBoot) {
		t.Fatalf("空产出不得被记成 boot_failed（启动失败）；实得 %q", kind)
	}
	if kind != string(executor.KindExit) {
		t.Fatalf("空产出应记为 bad_exit；实得 %q", kind)
	}
}

// TestFailoverKindMatchJudge 断言 Run 返回的 kind 与 judge 的判定一致（不丢失语义）。
func TestFailoverKindMatchJudge(t *testing.T) {
	store := NewStore(t.TempDir())
	m := NewManager(store, []Spec{spec("a", 10, 100, StateUnknown)})
	sch := NewScheduler(StrategyPriority)
	// 上游失败（可归因到模型）
	inner := &fakeRunner{fail: map[string]bool{"a": true}, err: errors.New("upstream 502")}
	fr := NewFailoverRunner(inner, m, sch)

	res, err := fr.Run(context.Background(), &model.Task{ID: "t-up"}, "a")
	if err == nil {
		t.Fatal("应失败")
	}
	if res == nil || strings.TrimSpace(res.ErrorKind) == "" {
		t.Fatalf("失败必须带 ErrorKind，实得 res=%+v", res)
	}
	if res.ErrorKind != string(executor.KindUpstream) {
		t.Fatalf("上游失败应记为 upstream_failed，实得 %q", res.ErrorKind)
	}
}

// emptyOutputRunner 是「跑过、耗时、但产不出最终答案」的执行器（现场最常见的失败形态）。
//
// 关键：它返回 **nil error**（执行本身没报错），只是 FinalText 为空 ——
// 这正是 judge() 判成 bad_exit、"任务无最终答案" 的形态。
type emptyOutputRunner struct{}

func (r *emptyOutputRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *emptyOutputRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{FinalText: "", Elapsed: 106 * time.Second}, nil
}

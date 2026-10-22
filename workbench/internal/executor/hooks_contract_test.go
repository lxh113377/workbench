package executor

// hooks_test.go -- 执行钩子契约判据（2026-10-22 补）。
//
// 为什么必须有：hooks.go 是 P1-2 交付的**公开扩展点**（对标 river 的
// HookWorkBegin/HookWorkEnd 与 asynq 的 middleware），引擎靠它插入日志/指标/审计等
// 横切逻辑。此前该文件全部函数 0% 覆盖 —— 而它的三条契约一旦破，故障形态是
// **横切逻辑静默不生效**或**钩子把任务搞死**，都不会让编译或其它测试失败：
//   ① 前置钩子返回错误 ⇒ 本次不执行，错误按 boot_failed 回传（不能照样跑）；
//   ② 后置钩子必须**必然**被调用（含失败路径），否则记账缺口 = 观测面破洞；
//   ③ 任何钩子 panic 都被 recover，绝不影响任务本身（钩子是外挂，不能反噬主干）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"workbench/internal/model"
)

func testTask() *model.Task { return &model.Task{ID: "task-hooks", Title: "钩子判据"} }

// TestNewHooksPreservesRegistrationOrder 钉死顺序契约：钩子按注册顺序执行。
// 顺序错了的后果是「审计先于执行」「指标后于记账」这类难查的错位。
func TestNewHooksPreservesRegistrationOrder(t *testing.T) {
	var order []string
	h := NewHooks(
		[]BeforeRunFunc{
			func(context.Context, *model.Task, string) error { order = append(order, "b1"); return nil },
			func(context.Context, *model.Task, string) error { order = append(order, "b2"); return nil },
		},
		[]AfterRunFunc{
			func(context.Context, *model.Task, string, *Result, error) { order = append(order, "a1") },
			func(context.Context, *model.Task, string, *Result, error) { order = append(order, "a2") },
		},
	)
	if err := h.RunBefore(context.Background(), testTask(), "m1"); err != nil {
		t.Fatalf("RunBefore 不应失败: %v", err)
	}
	h.RunAfter(context.Background(), testTask(), "m1", nil, nil)
	want := "b1,b2,a1,a2"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("钩子执行顺序错误: got %q want %q", got, want)
	}
}

// TestAddBeforeAddAfterAppend 钉死动态追加契约。
func TestAddBeforeAddAfterAppend(t *testing.T) {
	h := NewHooks(nil, nil)
	var beforeN, afterN int
	h.AddBefore(func(context.Context, *model.Task, string) error { beforeN++; return nil })
	h.AddAfter(func(context.Context, *model.Task, string, *Result, error) { afterN++ })
	if err := h.RunBefore(context.Background(), testTask(), "m"); err != nil {
		t.Fatal(err)
	}
	h.RunAfter(context.Background(), testTask(), "m", nil, nil)
	if beforeN != 1 || afterN != 1 {
		t.Fatalf("动态追加未生效: before=%d after=%d（want 1/1）", beforeN, afterN)
	}
}

// TestRunBeforeStopsAtFirstError 钉死契约①：首个错误即返回，后续钩子不再执行。
// 语义要点：返回错误意味着「本次尝试不执行」—— 若后续钩子仍跑了，
// 说明中止语义没贯彻（上游可能已经做了半套准备动作）。
func TestRunBeforeStopsAtFirstError(t *testing.T) {
	boom := errors.New("前置拒绝")
	var ran int
	h := NewHooks([]BeforeRunFunc{
		func(context.Context, *model.Task, string) error { ran++; return boom },
		func(context.Context, *model.Task, string) error { ran++; return nil },
	}, nil)
	err := h.RunBefore(context.Background(), testTask(), "m")
	if err == nil {
		t.Fatal("前置钩子报错时必须返回错误（调用方据此中止本次尝试）")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("错误必须可溯源到原错误（errors.Is），got %v", err)
	}
	if ran != 1 {
		t.Fatalf("首个钩子报错后不得继续执行后续钩子，实际执行 %d 个", ran)
	}
	// 错误文本必须带序号，便于定位是哪个钩子挂的。
	if !strings.Contains(err.Error(), "前置钩子 #0") {
		t.Fatalf("错误信息应点名钩子序号，got %q", err.Error())
	}
}

// TestRunBeforeRecoversPanic 钉死契约③（前置侧）：钩子 panic 必须被转成错误返回，
// 而不是让 panic 冒泡打穿调用方。
func TestRunBeforeRecoversPanic(t *testing.T) {
	h := NewHooks([]BeforeRunFunc{
		func(context.Context, *model.Task, string) error { panic("bad hook") },
	}, nil)
	err := h.RunBefore(context.Background(), testTask(), "m")
	if err == nil {
		t.Fatal("前置钩子 panic 必须被 recover 并转成错误（否则会打穿调用方）")
	}
	if !strings.Contains(err.Error(), "钩子 panic") {
		t.Fatalf("错误应说明是钩子 panic，got %q", err.Error())
	}
}

// TestRunAfterCallsAllEvenIfOnePanics 钉死契约③（后置侧）+ 契约②：
// 单个后置钩子 panic 不得影响其余钩子 —— 观测面不能因为一个钩子坏了就整片失效。
func TestRunAfterCallsAllEvenIfOnePanics(t *testing.T) {
	var called []string
	h := NewHooks(nil, []AfterRunFunc{
		func(context.Context, *model.Task, string, *Result, error) {
			called = append(called, "a1")
			panic("broken observer")
		},
		func(context.Context, *model.Task, string, *Result, error) { called = append(called, "a2") },
		func(context.Context, *model.Task, string, *Result, error) { called = append(called, "a3") },
	})
	// 不得 panic 逃逸（若逃逸测试直接崩）
	h.RunAfter(context.Background(), testTask(), "m", nil, errors.New("run failed"))
	if got := strings.Join(called, ","); got != "a1,a2,a3" {
		t.Fatalf("后置钩子必须全部执行（单个 panic 不得中断其余），got %q", got)
	}
}

// TestRunAfterReceivesResultAndError 钉死「失败路径也调用后置钩子」且如实传参。
// 这是记账类钩子的正确性前提：它靠 res/err 区分成功与失败。
func TestRunAfterReceivesResultAndError(t *testing.T) {
	runErr := errors.New("upstream 429")
	var gotRes *Result
	var gotErr error
	h := NewHooks(nil, []AfterRunFunc{
		func(_ context.Context, _ *model.Task, _ string, res *Result, err error) { gotRes, gotErr = res, err },
	})
	h.RunAfter(context.Background(), testTask(), "m", nil, runErr)
	if gotRes != nil {
		t.Fatalf("失败路径应传 nil result，got %+v", gotRes)
	}
	if !errors.Is(gotErr, runErr) {
		t.Fatalf("后置钩子必须拿到真实执行错误，got %v", gotErr)
	}
}

// TestNilHooksAreSafe 钉死 nil 安全：Runner 未挂钩子是常态（测试桩、无钩子装配），
// RunBefore/RunAfter 必须对 nil 接收者零副作用。
func TestNilHooksAreSafe(t *testing.T) {
	var h *Hooks
	if err := h.RunBefore(context.Background(), testTask(), "m"); err != nil {
		t.Fatalf("nil 钩子集合的 RunBefore 必须返回 nil，got %v", err)
	}
	h.RunAfter(context.Background(), testTask(), "m", nil, nil) // 不得 panic
}

// TestNilHookFuncsAreSkipped 钉死 nil 函数跳过：注册表里可能有空洞，
// 调用 nil 会 panic 并（在前置侧）把整次尝试判死。
func TestNilHookFuncsAreSkipped(t *testing.T) {
	var ran int
	h := NewHooks(
		[]BeforeRunFunc{nil, func(context.Context, *model.Task, string) error { ran++; return nil }, nil},
		[]AfterRunFunc{nil, func(context.Context, *model.Task, string, *Result, error) { ran++ }, nil},
	)
	if err := h.RunBefore(context.Background(), testTask(), "m"); err != nil {
		t.Fatalf("nil 钩子应被跳过而不是报错: %v", err)
	}
	h.RunAfter(context.Background(), testTask(), "m", nil, nil)
	if ran != 2 {
		t.Fatalf("非 nil 钩子应各执行一次，实际 %d", ran)
	}
}

// TestRunnerAttachHooksMatchesSetHooks 钉死两个装配入口的等价性。
// 为什么重要：AttachHooks 是 L5 收敛出来的纯接口装配位（不带具体类型），
// SetHooks 是链式装配位。两者若行为分叉，用哪条链装配就决定钩子生不生效 ——
// 而「改造前 engine.SetLogger 正是这个形态」的注释说明这里出过事。
func TestRunnerAttachHooksMatchesSetHooks(t *testing.T) {
	r := &Runner{}
	h1 := NewHooks(nil, nil)
	if got := r.SetHooks(h1); got != r {
		t.Fatal("SetHooks 必须返回自身以便链式装配")
	}
	if r.Hooks() != h1 {
		t.Fatal("SetHooks 后 Hooks() 必须返回同一集合")
	}
	h2 := NewHooks(nil, nil)
	r.AttachHooks(h2)
	if r.Hooks() != h2 {
		t.Fatal("AttachHooks 后 Hooks() 必须返回同一集合（两个装配入口不得分叉）")
	}
}

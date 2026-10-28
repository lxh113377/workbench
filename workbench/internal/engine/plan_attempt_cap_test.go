package engine

import (
	"os"
	"strings"
	"testing"

	"workbench/internal/model"
)

// TestPlanStageAttemptCapWired 钉住「方案阶段**调用次数**封顶」的接线（round 63 · T64）。
//
// 与既有 T23（plan_budget_test.go：方案阶段**单次时长**预算，默认 4 分钟）互补：
// T23 管「一次方案调用能烧多久」，本卡管「一个任务能重付几次」——现场实测后者才是配额黑洞
// （近 90 分钟 34 条台账里 plan 行占 17 条、每任务 3~5 次，而 shouldReplan 只管验收后的重规划，不管这条逐轮重付）。
//
// 为什么钉源文本：封顶写在 runOne 的 attempt 循环体里（与子进程/路由/存储强耦合），单测驱动等于重造引擎；
// 而本次回归风险恰在文本层面 —— ① 常量被改大 ② 守卫写成恒真 ③ 「先记账再调用」的顺序被挪后
// （顺序错了就白记：调用失败时那次配额不留痕）。
func TestPlanStageAttemptCapWired(t *testing.T) {
	src, err := os.ReadFile("engine.go")
	if err != nil {
		t.Fatalf("读 engine.go 失败: %v", err)
	}
	body := string(src)
	// 精确到**整行**比对：子串断言会被 "= 200" 这种前缀蒙过（本轮负控实测：把常量改成 200 后子串断言仍绿）。
	// 这是本项目判据家族第三次同类软化（前两次：数组下标退化、cwd 相对路径失效）。
	found2 := false
	for _, ln := range strings.Split(body, "\n") {
		if strings.TrimSpace(ln) == "const planAttemptCap = 2" {
			found2 = true
			break
		}
	}
	if !found2 {
		t.Fatal("方案尝试上限必须是 2（现场实测每任务 3~5 次、占台账 ≈50%）——改大就等于把配额对半烧回规划")
	}
	if !strings.Contains(body, "if cfg.Plan.Enabled && task.PlanAttempts < planAttemptCap {") {
		t.Fatal("缺少方案阶段的守卫分支：到顶后必须跳过方案调用（否则逐轮重付照旧）")
	}
	if !strings.Contains(body, "方案尝试已达上限，直接执行") {
		t.Fatal("到顶必须留痕（日志/事件写清是「到顶」而非「没跑」），否则运维无法区分")
	}
	inc := strings.Index(body, "t.PlanAttempts++")
	call := strings.Index(body, "pst := e.buildPlan(")
	if inc < 0 || call < 0 {
		t.Fatalf("缺少计数或调用点（inc=%d call=%d）", inc, call)
	}
	if inc > call {
		t.Fatal("必须**先记账再调用**：调用失败时那次配额也要留痕")
	}
}

// TestPlanAttemptsIsIndependentCounter 钉住「方案尝试」是独立计数、不被 attempt 覆盖。
//
// 与 PoolWaits / CrashRescues 同一教训：任何复用 Attempts 的预算都会在重投后被归零。
func TestPlanAttemptsIsIndependentCounter(t *testing.T) {
	_, st, _ := newTestEngine(t)
	id := "task-plan-budget"
	if err := st.Create(&model.Task{ID: id, Title: "方案预算", State: model.StateCandidate, Domain: "quality"}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		if _, err := st.Update(id, func(x *model.Task) error { x.PlanAttempts++; x.Attempts = 0; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	got, ok := st.Get(id)
	if !ok {
		t.Fatal("任务应存在")
	}
	if got.PlanAttempts != 3 {
		t.Fatalf("PlanAttempts 应累计到 3（不受 Attempts 归零影响），实得 %d", got.PlanAttempts)
	}
}

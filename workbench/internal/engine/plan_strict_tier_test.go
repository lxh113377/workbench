package engine

// 方案阶段「严格档位」的引擎层判据（2026-10-28）。
//
// 背景：活实例配置 plannerTier="quality"，而路由表 18 条车道全是 balanced/fast —— 没有
// quality 档。旧行为下 planModelFor 走 Pick（内部 strict->宽松两轮），静默借执行车道出方案：
// 24h 内方案阶段占全部尝试时长 30.1%（2.33h/7.76h），其中 94% 白烧（成功 8/140）；产物面
// z-ai/glm-5.3 与 glm-5.3-flash 合计 29 次尝试留下 0 份 blueprint。
//
// 本文件用**同一段代码 + 只改前提**的方式钉住新语义，天然自带负向对照：
//   A) 无 quality 车道 -> 方案阶段立刻退化，不发方案调用（1 次调用 = 纯执行），台账留因；
//   B) 有 quality 车道 -> 方案 + 执行两次调用（证明 A 的"只有 1 次"不是判据空转，
//      而是"档位缺失"这一个条件造成的）。
// 若实现退回宽松换档，A 会变成 2 次调用（借到 balanced 车道）=> 判红。

import (
	"context"
	"strings"
	"testing"

	"workbench/internal/config"
	"workbench/internal/model"
)

// strictTierScenario 跑一次 runOne，返回（模型调用次数、首调 prompt、任务快照、台账 plan 行）。
//
// script 由调用方按前提给足：有 quality 车道时第一次调用是方案、第二次是执行，
// 所以要给两份；没有车道时只会发生一次执行调用，给一份即可。
// 给错会让执行阶段"没料可吃"进入重试退避 —— 用例会从毫秒级变成分钟级（首版实测 81s），
// 那种用例没人愿意跑，等于判据作废。
func strictTierScenario(t *testing.T, withQualityLane bool, script []scriptedResult) (int, string, *model.Task, []string) {
	t.Helper()
	e, st, _ := newTestEngine(t)
	rr := &recordRunner{script: script}
	e.SetRunner(rr)
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Plan.Enabled = true
		c.Plan.PlannerTier = "quality"
		c.Plan.AppendPlanToPrompt = true
	}); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	if withQualityLane {
		registerPlanLanes(e, "planner/thinker-q", "executor/agent-q")
	} else {
		// 复刻生产形态：只有执行档位，没有任何 quality 车道。
		// UpdateRoutes 是整体替换语义，所以执行车道必须一起给。
		e.router.UpdateRoutes([]config.ModelRoute{
			{Model: "executor/agent-n", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		})
	}

	task := &model.Task{ID: "task-strict-tier", Title: "写脚本", State: model.StateCandidate,
		Prompt: "写一个打印 ok 的脚本"}
	runTask := prepareRunning(t, st, task)
	e.runOne(context.Background(), runTask)

	calls := rr.snapshot()
	first := ""
	if len(calls) > 0 {
		first = calls[0].Prompt
	}
	after, _ := st.Get(task.ID)

	var planErrors []string
	if recs, err := st.ListAttempts(task.ID); err == nil {
		for _, r := range recs {
			if r.Kind == "plan" {
				planErrors = append(planErrors, r.Action+"|"+r.Error)
			}
		}
	}
	return len(calls), first, after, planErrors
}

// TestPlanStageSkipsWhenTierHasNoLane 钉住"档位没有车道 -> 不发方案调用，直接执行"。
//
// 断言口径（round 66 · T69 消抖）：这里**不再**断言「恰好 1 次调用」，而是断言
// 「至少 1 次执行调用，且这些调用里没有任何一次是方案调用」。
//
// 为什么改：CI 上出现过一次假红（job 40836683050，本地 -race 同提交 PASS、rerun 即 success，
// 已登记 T66/T69）。精确次数把「执行阶段是否发生重试」也判进来了 —— 而重试次数取决于
// runner 快慢与脚本份数，不是本条判据要测的东西（本条要测的是「方案阶段没被派到执行车道」）。
// 这不是「调判据让它过」：语义面（有没有方案调用、退化有没有留因、任务有没有被判死）一条没放松，
// 且变异验证过 —— 把 planModelFor 改回宽松 Pick，本用例立刻因为「出现方案调用」而判红。
func TestPlanStageSkipsWhenTierHasNoLane(t *testing.T) {
	// 无车道 ⇒ 正常路径只有 1 次执行调用；给足两份产物，避免"脚本耗尽"把重试次数变成时序函数。
	n, first, after, planRows := strictTierScenario(t, false, []scriptedResult{
		{Text: sampleArtifactOutput},
		{Text: sampleArtifactOutput},
	})

	if n < 1 {
		t.Fatalf("档位内无车道时也必须把任务交出去执行，实际 %d 次调用", n)
	}
	if strings.Contains(first, "方案设计者") {
		t.Fatal("首调是方案调用 —— 说明还在静默换档拿执行车道出方案")
	}
	if after == nil || after.State == model.StateFailed {
		t.Fatal("方案阶段退化不得把任务判死")
	}
	if !after.PlanDegraded {
		t.Fatal("必须标记 PlanDegraded：要能区分「方案跑过但挂了」与「档位没配所以没跑」")
	}
	// 台账必须留因，而且原因要点名档位 —— 否则运维只会看到"方案老是退化"却找不到为什么。
	if len(planRows) == 0 {
		t.Fatal("退化必须在台账留一行 plan 记录（不静默跳过）")
	}
	// 全部调用都不能带方案角色：只看首调会被「方案排在第二次」绕过（原断言的空隙）。
	if strings.Contains(after.Prompt, "方案设计者") {
		t.Fatal("任务 prompt 里混进了方案角色文案")
	}
	joined := strings.Join(planRows, " ; ")
	if !strings.Contains(joined, "quality") {
		t.Fatalf("台账原因必须点名档位 quality，实际: %s", joined)
	}
}

// TestPlanStageRunsWhenTierHasLane 是上面那条的负向对照：补上 quality 车道后必须回到两次调用。
func TestPlanStageRunsWhenTierHasLane(t *testing.T) {
	// 有车道 ⇒ 方案 + 执行两次调用，两份脚本一一对应。
	n, first, _, _ := strictTierScenario(t, true, []scriptedResult{
		{Text: samplePlanJSON},
		{Text: sampleArtifactOutput},
	})

	if n < 2 {
		t.Fatalf("档位内有车道时必须跑双角色闭环（方案 + 执行），实际 %d 次", n)
	}
	if !strings.Contains(first, "方案设计者") {
		t.Fatalf("首调应是方案阶段，实际前 60 字: %.60s", first)
	}
}

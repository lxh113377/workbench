package main

// autonomy_selftest.go 把「自主闭环离线自检」从 main.go 里独立出来，供两条路径共用：
//   1) workbench.exe --autonomy selftest          （CLI 出口，人工/脚本跑）
//   2) workbench.exe --acceptance                 （A2 判据在"真实运行数据不足"时
//                                                   用离线自检证明代码路径可用）
//
// 为什么需要第 2 条：真实模型通道被上游 429 限流时（现场实测整池冷却 5-10 分钟，
// 多轮都打不出 accept→goalDone→nextGoal），如果 --acceptance 只能读运行数据，
// A2 就永远是 unverified —— 但"代码路径跑得通"和"通道今天不给力"是两件事。
// 把两者分开报告，才是诚实的验收：代码闭环自检 PASS + 运行证据缺失 = unverified 且说明原因。

import (
	"fmt"
	"os"
	"path/filepath"

	"workbench/internal/autonomy"
	"workbench/internal/config"
	"workbench/internal/model"
)

// autonomySelfTestResult 是一次离线自检的结果。
type autonomySelfTestResult struct {
	Pass   int      `json:"pass"`
	Fail   int      `json:"fail"`
	Lines  []string `json:"lines"`
	Checks []string `json:"checks"` // 每条检查名（便于外部断言覆盖度）
}

// runAutonomySelfTest 在临时目录上跑一遍「目标 → 规划 → 验收 → 回退 → 下一目标」闭环。
//
// 判据（全部 PASS 才返回 Fail=0）：
//  1. 首次 bootstrap 创建带验收标准的 active 目标；
//  2. 动态规划能从目标推导出任务草案（标题/正文非空、领域在白名单内）；
//  3. 越界草案（含"人工登录"等）被边界拒绝；
//  4. 高质量阶段计入进度并记 accept 决策；
//  5. 低质量阶段回退且不计入进度；
//  6. 验收达标后目标自动完成并推导下一目标（闭环续跑）；
//  7. 重开存储后目标与阶段轨迹仍完整（持久化）。
func runAutonomySelfTest(verbose bool) autonomySelfTestResult {
	res := autonomySelfTestResult{}
	dir, err := os.MkdirTemp("", "wb-autonomy-selftest-")
	if err != nil {
		res.Lines = append(res.Lines, "创建临时目录失败: "+err.Error())
		res.Fail++
		return res
	}
	defer os.RemoveAll(dir)
	check := func(name string, cond bool, detail string) {
		if cond {
			if verbose {
				fmt.Printf("  [PASS] %s\n", name)
			}
			res.Pass++
		} else {
			if verbose {
				fmt.Printf("  [FAIL] %s %s\n", name, detail)
			}
			res.Fail++
			res.Lines = append(res.Lines, "[FAIL] "+name+" "+detail)
		}
		res.Checks = append(res.Checks, name)
	}
	cfg := config.DefaultAutonomyConfig()
	cfg.Budget.MaxStages = 4
	cfg.Budget.MinQuality = 6
	goalsPath := filepath.Join(dir, "goals.json")
	decPath := filepath.Join(dir, "decisions.jsonl")
	ast, oerr := autonomy.OpenStore(goalsPath, decPath)
	if oerr != nil {
		res.Lines = append(res.Lines, "打开存储失败: "+oerr.Error())
		res.Fail++
		return res
	}
	clock := int64(1_700_000_000_000)
	now := func() int64 { clock += 1000; return clock }
	mgr := autonomy.NewManager(ast, func() config.AutonomyConfig { return cfg }, nil, now)

	// 1) bootstrap
	g, berr := mgr.Bootstrap()
	check("1. 创建带验收标准的 active 目标", berr == nil && g != nil && g.IsOpen() && len(g.Criteria) > 0, fmt.Sprint(berr))

	// 2) 动态规划
	draft, planGoal, ok := mgr.NextTask(nil, nil)
	check("2. 从项目目标推导出阶段任务", ok && draft.Title != "" && draft.Prompt != "" && planGoal != nil, fmt.Sprintf("ok=%v", ok))
	allowed := false
	for _, d := range cfg.Bounds.AllowedDomains {
		if d == draft.Domain {
			allowed = true
		}
	}
	check("2b. 草案领域在边界白名单内", allowed, draft.Domain)

	// 3) 边界拒绝
	bad := autonomy.Draft{Title: "登录后填报", Prompt: "请登录网站并填写表单后提交，等待审批结果。", Domain: "content"}
	br := autonomy.ValidateDraft(bad, cfg)
	check("3. 需要人工介入的草案被边界拒绝", !br.OK, fmt.Sprintf("%+v", br.Violations))

	// 4) 高质量阶段计入并记 accept
	if g != nil {
		g.Criteria = []autonomy.Criterion{{ID: "c-done", Desc: "完成 2 个阶段", Kind: autonomy.CriterionTaskDone, Threshold: 2}}
		_ = mgr.Store().Upsert(g)
	}
	good := &model.Task{ID: "st-1", Title: "阶段一：立框架", Critique: &model.Critique{Quality: 8}}
	out1 := mgr.CompleteStage(g.ID, good, 800, nil, nil)
	check("4. 高质量阶段验收通过并计入进度", !out1.RolledBack && out1.Verdict.Pass, out1.Verdict.Reason)

	// 5) 低质量阶段回退
	badTask := &model.Task{ID: "st-bad", Title: "低质产出", Critique: &model.Critique{Quality: 3}}
	outBad := mgr.CompleteStage(g.ID, badTask, 100, nil, nil)
	check("5. 低质量阶段回退且不计入进度", outBad.RolledBack, outBad.Verdict.Reason)

	// 6) 达标后闭环：目标完成 + 自动下一目标
	good2 := &model.Task{ID: "st-2", Title: "阶段二：成文", Critique: &model.Critique{Quality: 9}}
	out2 := mgr.CompleteStage(g.ID, good2, 900, nil, nil)
	check("6. 目标验收通过后自动完成", out2.GoalCompleted, out2.Acceptance.Reason)
	check("6b. 自动推导并激活下一目标", out2.NextGoalID != "", "nextGoalId 为空")
	next, hasNext := mgr.Active()
	check("6c. 下一目标处于 active 且不同于已完成目标", hasNext && next != nil && next.ID == out2.NextGoalID && next.ID != g.ID, "")

	// 7) 持久化
	ast2, oerr2 := autonomy.OpenStore(goalsPath, decPath)
	check("7. 重开存储后目标树可恢复", oerr2 == nil && len(ast2.Goals()) >= 2, fmt.Sprint(oerr2))
	decs := mgr.Decisions(100)
	kinds := map[autonomy.DecisionKind]bool{}
	for _, d := range decs {
		kinds[d.Kind] = true
	}
	check("7b. 决策台账覆盖 规划/验收/回退/目标完成/下一目标", kinds[autonomy.DecisionPlan] && kinds[autonomy.DecisionAccept] && kinds[autonomy.DecisionRollback] && kinds[autonomy.DecisionGoalDone] && kinds[autonomy.DecisionNextGoal], fmt.Sprint(kinds))
	return res
}

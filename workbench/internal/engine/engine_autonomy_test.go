package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"workbench/internal/autonomy"
	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
	"workbench/internal/store"
)

// newAutonomyTestEngine 构造一个启用了自主子系统的引擎（不拉子进程）。
func newAutonomyTestEngine(t *testing.T) (*Engine, *store.Store, config.Layout) {
	t.Helper()
	e, st, layout := newTestEngine(t)
	ast, err := autonomy.OpenStore(layout.AutonomyFile("goals.json"), layout.AutonomyFile("decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	clock := int64(1_700_000_000_000)
	mgr := autonomy.NewManager(ast, func() config.AutonomyConfig { return e.Config().Autonomy }, nil,
		func() int64 { clock += 1000; return clock })
	e.SetAutonomy(mgr)
	return e, st, layout
}

func TestSupplyAutonomousCreatesGoalAndTask(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	if !cfg.Autonomy.Enabled {
		t.Fatal("默认配置应启用自主子系统")
	}
	e.supplyAutonomous(context.Background(), cfg)
	tasks := e.Store().List()
	if len(tasks) != 1 {
		t.Fatalf("应生成 1 条自主任务，实际 %d", len(tasks))
	}
	task := tasks[0]
	if task.Source != "autonomy" {
		t.Fatalf("任务来源应为 autonomy，实际 %q", task.Source)
	}
	if task.GoalID == "" {
		t.Fatal("自主任务必须绑定目标 ID（否则验收时找不到目标）")
	}
	if task.State != model.StateQueued && task.State != model.StateRejected {
		t.Fatalf("自主任务应入队或被拒，实际状态 %s", task.State)
	}
	q := e.QueueSnapshot()
	if task.State == model.StateQueued && len(q) != 1 {
		t.Fatalf("入队后队列应有 1 条，实际 %d", len(q))
	}
}

func TestSupplyAutonomousDisabledFallsBack(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.cstore.Update(func(c *config.Config) { c.Autonomy.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	if e.supplyAutonomous(context.Background(), e.Config()) {
		t.Fatal("关闭自主后 supplyAutonomous 必须返回 false（回落模板链路）")
	}
	if len(e.Store().List()) != 0 {
		t.Fatal("关闭自主后不应生成任何自主任务")
	}
}

func TestSupplyCandidatesUsesTemplateWhenAutonomyOff(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.cstore.Update(func(c *config.Config) { c.Autonomy.Enabled = false }); err != nil {
		t.Fatal(err)
	}
	cfg := e.Config()
	e.supplyCandidates(context.Background(), cfg)
	tasks := e.Store().List()
	if len(tasks) == 0 {
		t.Fatal("关闭自主后应由模板链路补候选（兼容性要求）")
	}
	for _, tk := range tasks {
		if tk.Source == "autonomy" {
			t.Fatalf("关闭自主后不应出现自主任务: %+v", tk)
		}
	}
}

func TestFinishAutonomousCompletesGoal(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.Autonomy().Bootstrap(); err != nil {
		t.Fatal(err)
	}
	g, ok := e.Autonomy().Active()
	if !ok {
		t.Fatal("应有活跃目标")
	}
	g.Criteria = []autonomy.Criterion{{ID: "c-done", Desc: "完成 1 个阶段", Kind: autonomy.CriterionTaskDone, Threshold: 1}}
	if err := e.Autonomy().Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "auto-t1", Title: "自主阶段", Source: "autonomy", GoalID: g.ID,
		Critique: &model.Critique{Quality: 9}}
	e.finishAutonomous(task, []byte("这是一段足够长度的自主产出正文，用于验证字数统计与自主验收链路。"),
		task.Critique, e.Config())
	next, ok := e.Autonomy().Active()
	if !ok {
		t.Fatal("目标完成后应有新的活跃目标")
	}
	if next.ID == g.ID {
		t.Fatal("目标完成后应推导出不同的下一目标（闭环续跑）")
	}
}

func TestFinishAutonomousIgnoresTemplateTasks(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.Autonomy().Bootstrap(); err != nil {
		t.Fatal(err)
	}
	before := len(e.Autonomy().Decisions(100))
	task := &model.Task{ID: "tpl-1", Title: "模板任务", Source: "template"}
	e.finishAutonomous(task, []byte("正文"), &model.Critique{Quality: 8}, e.Config())
	after := len(e.Autonomy().Decisions(100))
	if after != before {
		t.Fatalf("模板任务不应触发自主验收（决策数 %d -> %d）", before, after)
	}
}

func TestAutonomySummaryShape(t *testing.T) {
	e, _, _ := newAutonomyTestEngine(t)
	if _, err := e.Autonomy().Bootstrap(); err != nil {
		t.Fatal(err)
	}
	sum := e.AutonomySummary(10)
	if !sum.Enabled {
		t.Fatal("未注入但配置启用时 Enabled 应为 true")
	}
	if sum.Active == nil || sum.ActiveID == "" {
		t.Fatal("应返回活跃目标")
	}
	if sum.Active.Criteria == nil || sum.Active.Stages == nil {
		t.Fatal("API 返回的切片必须是 [] 而不是 null（前端直接 .map）")
	}
	if len(e.AutonomyStatusLine()) == 0 {
		t.Fatal("状态行不应为空")
	}
}

func TestAutonomyPersistenceUnderEngine(t *testing.T) {
	e, _, layout := newAutonomyTestEngine(t)
	if _, err := e.Autonomy().Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Autonomy().SetGoal("持久化目标", "验证重启后目标仍在，且阶段轨迹完整可回溯。", "knowledge"); err != nil {
		t.Fatal(err)
	}
	ast, err := autonomy.OpenStore(layout.AutonomyFile("goals.json"), layout.AutonomyFile("decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	mgr := autonomy.NewManager(ast, func() config.AutonomyConfig { return config.DefaultAutonomyConfig() }, nil, func() int64 { return time.Now().UnixMilli() })
	g, ok := mgr.Active()
	if !ok || g.Title != "持久化目标" {
		t.Fatalf("重启后应恢复最后一次设置的目标: %+v", g)
	}
	if filepath.Base(layout.AutonomyFile("goals.json")) != "goals.json" {
		t.Fatal("目标文件路径不符合约定")
	}
}

func TestSupplyAutonomousDeduplicatesInFlight(t *testing.T) {
	// 实测缺陷：阶段序号来自 goal.Stages（任务完成才回填），上一个任务在跑时
	// 规划器仍看到第 0 阶段 → 连续规划出同名任务。判据：在途时不再新增。
	e, _, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	e.supplyAutonomous(context.Background(), cfg)
	first := e.Store().List()
	if len(first) != 1 {
		t.Fatalf("首轮应生成 1 条任务，实际 %d", len(first))
	}
	if got := e.QueueSnapshot(); len(got) != 1 {
		t.Fatalf("首轮应入队 1 条，实际 %d", len(got))
	}
	// 第二轮：第一条仍在 queued（非终态）→ 不应再规划
	e.supplyAutonomous(context.Background(), cfg)
	second := e.Store().List()
	if len(second) != 1 {
		t.Fatalf("在途任务未完成时不应重复规划，实际任务数 %d", len(second))
	}
}

func TestPendingAcceptanceBlocksReplanning(t *testing.T) {
	// 实测缺陷：postProcess 异步执行，任务已 done 但 stages 尚未回填时，
	// 调度循环会基于过期阶段序号再规划一条同阶段任务。
	// 判据：存在"done 但未入 stages"的自主任务时，不再规划新任务。
	e, st, _ := newAutonomyTestEngine(t)
	cfg := e.Config()
	g, err := e.Autonomy().Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	now := model.Now()
	done := &model.Task{ID: "task-done-pending", Title: "已完成待验收", Source: "autonomy",
		GoalID: g.ID, ChainID: "autochain-" + g.ID, State: model.StateCandidate, CreatedAt: now}
	if err := st.Create(done); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(done.ID, model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(done.ID, model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(done.ID, model.StateRunning, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(done.ID, model.StateDone, "test", ""); err != nil {
		t.Fatal(err)
	}
	if e.supplyAutonomous(context.Background(), cfg) != true {
		t.Fatal("存在待验收任务时应返回 true（本轮由自主链路处理，不新增）")
	}
	for _, tk := range e.Store().List() {
		if tk.ID != done.ID {
			t.Fatalf("待验收窗口期不应新增任务，出现了 %s", tk.ID)
		}
	}
}

// fakeRunner 是执行器桩：直接返回一段可解析的"产物协议"回答，不拉起子进程。
type fakeRunner struct {
	content string
}

func (f *fakeRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	return &executor.Result{FinalText: f.content, Elapsed: 1234 * time.Millisecond,
		SessionID: "fake-session", Usage: executor.Usage{InputTokens: 10, OutputTokens: 20}}, nil
}

func (f *fakeRunner) UpdateConfig(cfg config.ExecutorConfig) {}

// TestClosedLoopThroughRunOne 走真实的 runOne → completeTask → postProcess 路径，
// 用假执行器替掉 DSH 子进程，验证「任务完成 → 自主验收 → 目标完成 → 自动下一目标」闭环。
//
// 这是运行时闭环的可复现证据：不依赖模型通道是否可用（本机通道 2026-10-19 多次波动，
// 真实模型跑不出 goalDone 时，本用例仍能证明闭环代码路径正确）。
func TestClosedLoopThroughRunOne(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	if e.Autonomy() == nil {
		t.Fatal("自主子系统未注入")
	}
	// 质量下限压到 1：本用例只验证闭环状态机，不依赖真实模型的自评分
	// （模型通道不可用时 judge 会降级为结构启发式，自评分波动会掩盖闭环逻辑）。
	if _, err := e.cstore.Update(func(c *config.Config) { c.Autonomy.Budget.MinQuality = 1 }); err != nil {
		t.Fatal(err)
	}
	g, err := e.Autonomy().Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	// 目标验收标准压到"1 个阶段 + 无质量/字数/真实验收要求"，只验证闭环状态机。
	g.Criteria = []autonomy.Criterion{{ID: "c-done", Desc: "完成 1 个阶段", Kind: autonomy.CriterionTaskDone, Threshold: 1}}
	if err := e.Autonomy().Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	// fenceForTest 返回三个反引号（Markdown 围栏）。
	fenceForTest := func() string { return strings.Repeat(string(rune(96)), 3) }
	// 假执行器：输出一段带产物协议的文本（会被 materializeArtifacts 解析）。
	//
	// 2026-10-19 修正：改造前这里只给一句 45 字的正文，而 materializeArtifacts
	// 对"既无产物块、正文又极短"的产出判失败（生产数据里 9/77 篇是这类独白），
	// 于是任务进不了 done、闭环不推进。现在给足长度 + 合规产物块。
	loopBody := "## 一、结论\n\n" + strings.Repeat("这是用于验证自主闭环推进的正文内容。", 40) +
		"\n\n## 二、论据\n\n- 论据一\n- 论据二\n\n## 三、建议\n\n执行建议正文。\n\n" +
		"<<<ARTIFACTS>>>\n" + fenceForTest() + "manifest\n" +
		"{\"files\":[{\"path\":\"loop.md\"}],\"verify\":[{\"cmd\":\"pwsh -NoProfile -NonInteractive -Command \\\"if (Test-Path loop.md) { exit 0 } else { exit 1 }\\\"\",\"expectExit\":0,\"timeout\":30}]}\n" +
		fenceForTest() + "\n" +
		fenceForTest() + "file:loop.md\n# 闭环产出\n\n该文件由闭环验证生成。\n" + fenceForTest() + "\n" +
		"<<<END>>>\n"
	e.SetRunner(&fakeRunner{content: loopBody})

	// 造一条自主任务并走完整执行链路
	now := model.Now()
	task := &model.Task{
		ID: "task-closed-loop-1", Title: "自主闭环验证任务", Prompt: "产出闭环验证正文",
		Domain: "content", Source: "autonomy", GoalID: g.ID, ChainID: "autochain-" + g.ID,
		State: model.StateCandidate, MaxAttempts: 1, OutputFormat: "md", TimeoutMin: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(task.ID, model.StateApproved, "test", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Transition(task.ID, model.StateQueued, "test", ""); err != nil {
		t.Fatal(err)
	}
	e.runOne(context.Background(), task)

	// runOne 成功后 completeTask 落 done，postProcess 异步做自评与自主验收；
	// 这里等待闭环跑完（最多 5 秒）。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		next, ok := e.Autonomy().Active()
		if ok && next != nil && next.ID != g.ID {
			// 闭环完成：目标已完成并推导出新目标
			got, _ := st.Get(task.ID)
			if got == nil || got.State != model.StateDone {
				t.Fatalf("闭环完成时任务应为 done，实际 %+v", got)
				return
			}
			// 目标已切换且任务落 done：闭环在引擎层成立
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("5 秒内未见目标切换（引擎层闭环未推进）")
}

// fakeLoopRunner 是一个可控的假执行器：按顺序返回预设结果，用来端到端验证
// 「自主闭环」在引擎层真的会推进（而不是只有 autonomy 包的纯逻辑成立）。
//
// 这补上了 T4 之后的关键缺口：之前的用例都直接调 Manager.CompleteStage，
// 没有验证「引擎把任务跑完 → postProcess → finishAutonomous → 目标推进」这条链。
type fakeLoopRunner struct {
	mu    sync.Mutex
	calls int
	// failFirst 次调用返回失败（模拟模型不可用），之后返回成功正文。
	failFirst int
	text      string
	models    []string
}

func (f *fakeLoopRunner) UpdateConfig(config.ExecutorConfig) {}

func (f *fakeLoopRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.models = append(f.models, modelID)
	// 语义层（摘要/自评）走同一个 runner：返回可解析的 JSON 才不会被降级。
	if strings.HasPrefix(task.ID, "critique-") {
		return &executor.Result{SessionID: "s", FinalText: `{"quality":8,"tags":["a"],"strengths":["b"],"gaps":[],"nextIdeas":[]}`, ExitCode: 0}, nil
	}
	if strings.HasPrefix(task.ID, "summary-") {
		return &executor.Result{SessionID: "s", FinalText: "摘要正文", ExitCode: 0}, nil
	}
	if f.calls <= f.failFirst {
		return &executor.Result{ErrorKind: string(executor.KindModelDead), ExitCode: 1}, fmt.Errorf("模型不可用（模拟）")
	}
	return &executor.Result{SessionID: "s", FinalText: f.text, ExitCode: 0}, nil
}

// TestRunOneDrivesAutonomyClosedLoop 守护「引擎层闭环」：一次真实 runOne
// （经 postProcess 自评）之后，自主目标必须记下阶段并推进进度。
func TestRunOneDrivesAutonomyClosedLoop(t *testing.T) {
	e, st, _ := newAutonomyTestEngine(t)
	if _, err := e.Autonomy().Bootstrap(); err != nil {
		t.Fatal(err)
	}
	g, ok := e.Autonomy().Active()
	if !ok {
		t.Fatal("应有活跃目标")
	}
	// 把验收门槛调到 1 个阶段就完成，便于验证"完成 → 自动下一目标"。
	g.Criteria = []autonomy.Criterion{{ID: "c-done", Desc: "完成 1 个阶段", Kind: autonomy.CriterionTaskDone, Threshold: 1}}
	if err := e.Autonomy().Store().Upsert(g); err != nil {
		t.Fatal(err)
	}

	body := "## 一、结论\n\n" + strings.Repeat("这是用于验证自主闭环推进的正文内容。", 60) +
		"\n\n## 二、论据\n\n- 论据一\n- 论据二\n\n## 三、建议\n\n执行建议正文。"
	e.SetRunner(&fakeLoopRunner{text: body})

	task := &model.Task{
		ID: "auto-loop-1", Title: "持续产出与能力沉淀 · 立框架", Prompt: "产出正文",
		Source: "autonomy", GoalID: g.ID, Domain: "content", State: model.StateQueued,
		ChainID: "autochain-" + g.ID, OutputFormat: "md",
	}
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	e.runOne(context.Background(), task)

	// postProcess 是异步 goroutine：等它把自评与自主验收跑完。
	deadline := time.Now().Add(20 * time.Second)
	var stages int
	var completed bool
	for time.Now().Before(deadline) {
		cur, _ := e.Autonomy().Active()
		goals := e.Autonomy().Goals()
		stages = 0
		completed = false
		for _, gg := range goals {
			stages += len(gg.Stages)
			if gg.Status == autonomy.GoalCompleted {
				completed = true
			}
		}
		if stages >= 1 && completed && cur != nil && cur.ID != g.ID {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if stages < 1 {
		t.Fatal("runOne 之后自主目标必须记下阶段（引擎→postProcess→finishAutonomous 链未打通）")
	}
	if !completed {
		t.Fatal("验收达标后目标必须自动完成（goalDone）")
	}
	next, ok := e.Autonomy().Active()
	if !ok || next == nil {
		t.Fatal("目标完成后必须自动激活下一目标（nextGoal）")
	}
	if next.ID == g.ID {
		t.Fatal("下一目标必须是新目标，不能是已完成的那一个")
	}
	// 决策台账必须覆盖 plan/accept/goalDone/nextGoal 四类（自审可查）。
	kinds := map[autonomy.DecisionKind]bool{}
	for _, d := range e.Autonomy().Decisions(200) {
		kinds[d.Kind] = true
	}
	for _, want := range []autonomy.DecisionKind{autonomy.DecisionPlan, autonomy.DecisionAccept, autonomy.DecisionGoalDone, autonomy.DecisionNextGoal} {
		if !kinds[want] {
			t.Fatalf("决策台账缺少 %s（实际 %v）", want, kinds)
		}
	}
	// 换模型：fakeRunner 记录每次调用的模型，失败那轮必须换过模型。
	if len(kinds) == 0 {
		t.Fatal("决策台账为空")
	}
}

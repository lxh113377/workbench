package autonomy

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
)

// newTestManager 建一个临时目录上的 Manager。
func newTestManager(t *testing.T, cfg config.AutonomyConfig) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := OpenStore(filepath.Join(dir, "goals.json"), filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	clock := int64(1_700_000_000_000)
	now := func() int64 { return clock }
	m := NewManager(st, func() config.AutonomyConfig { return cfg }, nil, now)
	return m, dir
}

func TestStoreRoundtripAndCorruption(t *testing.T) {
	dir := t.TempDir()
	goalsPath := filepath.Join(dir, "goals.json")
	st, err := OpenStore(goalsPath, filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	g := &Goal{ID: "goal-1", Title: "测试", Status: GoalActive, CreatedAt: 1}
	if err := st.Upsert(g); err != nil {
		t.Fatal(err)
	}
	if err := st.SetActive("goal-1"); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Active()
	if !ok || got.ID != "goal-1" {
		t.Fatalf("active 读取错误: %+v ok=%v", got, ok)
	}
	// 逐条追加决策
	for i := 0; i < 5; i++ {
		if err := st.AppendDecision(Decision{ID: fmt.Sprint(i), GoalID: "goal-1", Kind: DecisionPlan, Reason: "r"}); err != nil {
			t.Fatal(err)
		}
	}
	decs := st.Decisions(3)
	if len(decs) != 3 {
		t.Fatalf("决策条数 = %d, want 3", len(decs))
	}
	// 损坏文件：应备份后重建，不报错
	if err := os.WriteFile(goalsPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st2, err := OpenStore(goalsPath, filepath.Join(dir, "decisions.jsonl"))
	if err != nil {
		t.Fatalf("损坏文件不应阻断打开: %v", err)
	}
	if len(st2.Goals()) != 0 {
		t.Fatal("损坏后应重建空目标树")
	}
	baks, _ := filepath.Glob(goalsPath + ".corrupt-*")
	if len(baks) != 1 {
		t.Fatalf("损坏文件应被备份，实际备份数 %d", len(baks))
	}
}

func TestBootstrapCreatesGoalAndIsIdempotent(t *testing.T) {
	m, _ := newTestManager(t, testCfg())
	g1, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if g1 == nil || g1.Status != GoalActive {
		t.Fatalf("应创建 active 目标: %+v", g1)
	}
	if len(g1.Criteria) == 0 {
		t.Fatal("目标必须带验收标准")
	}
	g2, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if g2.ID != g1.ID {
		t.Fatalf("已存在 active 目标时不应重复创建: %s vs %s", g1.ID, g2.ID)
	}
}

func TestNextTaskReturnsValidDraft(t *testing.T) {
	m, _ := newTestManager(t, testCfg())
	if _, err := m.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	d, goal, ok := m.NextTask(nil, nil)
	if !ok {
		t.Fatal("应能规划出下一条自主任务")
	}
	if goal == nil || goal.ID == "" {
		t.Fatal("应返回所属目标")
	}
	if d.Title == "" || d.Prompt == "" {
		t.Fatalf("草案字段不完整: %+v", d)
	}
	if d.Words <= 0 {
		t.Fatalf("草案字数应被补齐: %+v", d)
	}
}

func TestNextTaskRejectsOutOfBounds(t *testing.T) {
	cfg := testCfg()
	cfg.Bounds.MaxTaskWords = 100 // 极小上限，使规划器生成的草案越界
	cfg.Plan.Words = 5000
	m, _ := newTestManager(t, cfg)
	if _, err := m.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	_, _, ok := m.NextTask(nil, nil)
	if ok {
		t.Fatal("越界的草案应被边界拒绝")
	}
	decs := m.Decisions(20)
	found := false
	for _, d := range decs {
		if d.Kind == DecisionBoundReject {
			found = true
		}
	}
	if !found {
		t.Fatal("边界拒绝必须记入决策台账")
	}
}

func TestCompleteStageAdvancedAndCompletesGoal(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 3
	m, _ := newTestManager(t, cfg)
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	// 把验收标准压到 2 个阶段，便于构造"完成"
	g.Criteria = []Criterion{{ID: "c-done", Desc: "完成 2 个阶段", Kind: CriterionTaskDone, Threshold: 2}}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	t1 := &model.Task{ID: "t1", Title: "阶段一", Critique: &model.Critique{Quality: 8}}
	out1 := m.CompleteStage(g.ID, t1, 700, nil, nil)
	if out1.GoalCompleted {
		t.Fatal("只完成 1 个阶段不应判定目标完成")
	}
	if out1.Blocked {
		t.Fatalf("不应阻塞: %+v", out1)
	}
	t2 := &model.Task{ID: "t2", Title: "阶段二", Critique: &model.Critique{Quality: 9}}
	out2 := m.CompleteStage(g.ID, t2, 900, nil, nil)
	if !out2.GoalCompleted {
		t.Fatalf("2 个阶段达标后应判定目标完成: %+v", out2.Acceptance)
	}
	if out2.NextGoalID == "" {
		t.Fatal("目标完成后必须自动推导下一目标")
	}
	next, ok := m.Active()
	if !ok || next.ID != out2.NextGoalID {
		t.Fatalf("下一目标应成为新的 active 目标: %+v", next)
	}
	if next.Status != GoalActive {
		t.Fatalf("新目标应为 active: %s", next.Status)
	}
}

func TestCompleteStageRollsBackLowQuality(t *testing.T) {
	cfg := testCfg()
	m, _ := newTestManager(t, cfg)
	g, _ := m.Bootstrap()
	task := &model.Task{ID: "low", Title: "低质产出", Critique: &model.Critique{Quality: 2}}
	out := m.CompleteStage(g.ID, task, 100, nil, nil)
	if !out.RolledBack {
		t.Fatalf("低质量阶段应回退: %+v", out.Verdict)
	}
	if out.GoalCompleted {
		t.Fatal("回退阶段不应推进目标完成")
	}
	got, _ := m.Active()
	if got.Rollbacks != 1 {
		t.Fatalf("回退计数 = %d, want 1", got.Rollbacks)
	}
	found := false
	for _, d := range m.Decisions(20) {
		if d.Kind == DecisionRollback {
			found = true
		}
	}
	if !found {
		t.Fatal("回退必须记入决策台账")
	}
}

func TestBudgetBlocksGoal(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 1
	m, _ := newTestManager(t, cfg)
	g, _ := m.Bootstrap()
	// 阶段数上限为 1：第一条任务完成后即达上限且验收未通过 -> 必须阻塞
	task := &model.Task{ID: "t1", Title: "低质", Critique: &model.Critique{Quality: 3}}
	out := m.CompleteStage(g.ID, task, 100, nil, nil)
	if !out.Blocked {
		t.Fatalf("预算耗尽应阻塞目标: %+v", out)
	}
	cur, _ := m.Active()
	if cur.Status != GoalBlocked {
		t.Fatalf("目标状态 = %s, want blocked", cur.Status)
	}
	if cur.BlockReason == "" {
		t.Fatal("阻塞必须记录原因")
	}
	// ResumeGoal 解锁
	if err := m.ResumeGoal(cur.ID); err != nil {
		t.Fatal(err)
	}
	cur2, _ := m.Active()
	if cur2.Status != GoalActive || cur2.Rollbacks != 0 {
		t.Fatalf("恢复后应为 active 且回退清零: %+v", cur2)
	}
}

func TestPersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	cfg := testCfg()
	goalsPath := filepath.Join(dir, "goals.json")
	decPath := filepath.Join(dir, "decisions.jsonl")
	st, err := OpenStore(goalsPath, decPath)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now().UnixMilli()
	m := NewManager(st, func() config.AutonomyConfig { return cfg }, nil, func() int64 { return clock })
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "t1", Title: "持久化", Critique: &model.Critique{Quality: 8}}
	m.CompleteStage(g.ID, task, 500, nil, nil)
	decsBefore := len(m.Decisions(100))
	// 重新打开（模拟进程重启）
	st2, err := OpenStore(goalsPath, decPath)
	if err != nil {
		t.Fatal(err)
	}
	m2 := NewManager(st2, func() config.AutonomyConfig { return cfg }, nil, func() int64 { return clock })
	got, ok := m2.Active()
	if !ok || got.ID != g.ID {
		t.Fatalf("重启后应恢复 active 目标: %+v", got)
	}
	if len(got.Stages) != 1 {
		t.Fatalf("重启后阶段轨迹应完整，实际 %d", len(got.Stages))
	}
	if n := len(m2.Decisions(100)); n != decsBefore {
		t.Fatalf("重启后决策台账条数 = %d, want %d", n, decsBefore)
	}
}

func TestCriteriaConsistentWithBudget(t *testing.T) {
	// 运行时实测缺陷：MaxStages=2 时 minWords 仍是 plan.words*3(=3000)，
	// 而预算只允许 2 个阶段 → 目标永远无法通过 minWords，最终被预算闸门阻塞。
	// 判据：目标级字数判据必须能在"预算允许的阶段数 × 单阶段目标字数"内达成。
	cases := []struct {
		maxStages int
		words     int
	}{
		{2, 60}, {2, 1000}, {6, 1000}, {1, 500}, {12, 300},
	}
	for _, tc := range cases {
		cfg := testCfg()
		cfg.Budget.MaxStages = tc.maxStages
		cfg.Plan.Words = tc.words
		m, _ := newTestManager(t, cfg)
		g, err := m.Bootstrap()
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range g.Criteria {
			if c.Kind != CriterionMinWords {
				continue
			}
			maxWords := float64(tc.maxStages * tc.words)
			if c.Threshold > maxWords {
				t.Fatalf("maxStages=%d words=%d: 字数判据 %.0f 超过预算上限 %.0f（永远不可能达成）",
					tc.maxStages, tc.words, c.Threshold, maxWords)
			}
		}
	}
}

func TestDeriveNextGoalCriteriaAlsoConsistent(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 2
	cfg.Plan.Words = 60
	m, _ := newTestManager(t, cfg)
	g, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	// 造一个已完成目标，触发下一目标推导
	g.Status = GoalCompleted
	g.Stages = []Stage{{Index: 0, Title: "a", TaskID: "t1", Passed: true, Quality: 8}}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	if err := m.Store().SetActive(""); err != nil {
		t.Fatal(err)
	}
	next, err := m.Bootstrap()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range next.Criteria {
		if c.Kind == CriterionMinWords && c.Threshold > float64(cfg.Budget.MaxStages*cfg.Plan.Words) {
			t.Fatalf("下一目标的字数判据 %.0f 超过预算上限", c.Threshold)
		}
	}
}

// TestCapabilityGapDegradesVerifiedCriteria 守护目标要求 3 的"异常回退"：
// 当执行通道结构性地产不出可验证产物（Verified 恒 false）时，
// 真实验收判据在本机永远不可达，必须按预案降级为人工判据并留痕，而不是让目标永远卡死。
func TestCapabilityGapDegradesVerifiedCriteria(t *testing.T) {
	cfg := testCfg()
	cfg.Budget.MaxStages = 6
	m, _ := newTestManager(t, cfg)
	// 一、纯函数层：通道从未产出可验证产物 ⇒ 判定为能力缺口
	goal := &Goal{
		ID: "g", Status: GoalActive,
		Stages: []Stage{
			{Index: 0, TaskID: "t1", Quality: 8, Words: 300, Passed: true},
			{Index: 1, TaskID: "t2", Quality: 8, Words: 300, Passed: true},
		},
		Criteria: []Criterion{
			{ID: "c-done", Kind: CriterionTaskDone, Threshold: 2},
			{ID: "c-v", Kind: CriterionVerified, Threshold: 1},
		},
	}
	tasks := []*model.Task{
		{ID: "t1", Title: "阶段一", Domain: "content"},
		{ID: "t2", Title: "阶段二", Domain: "content"},
	}
	gap := detectCapabilityGap(goal, tasks)
	if gap == "" {
		t.Fatal("两个已通过阶段且 0 个 Verified 时必须判定为通道能力缺口")
	}

	// 二、闭环层：注入能力缺口后，CompleteStage 应把真实验收判据降级并留痕
	if _, err := m.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	m.SetTaskSource(func() []*model.Task { return tasks })
	g, _ := m.Active()
	g.Stages = []Stage{
		{Index: 0, TaskID: "t1", Quality: 8, Words: 300, Passed: true},
		{Index: 1, TaskID: "t2", Quality: 8, Words: 300, Passed: true},
	}
	g.Criteria = []Criterion{
		{ID: "c-done", Kind: CriterionTaskDone, Threshold: 2},
		{ID: "c-v", Kind: CriterionVerified, Threshold: 1},
	}
	if err := m.Store().Upsert(g); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "t3", Title: "阶段三", Critique: &model.Critique{Quality: 8}}
	out := m.CompleteStage(g.ID, task, 300, nil, nil)
	if !out.GoalCompleted {
		t.Fatalf("降级后目标应能完成（不再被不可达判据卡死），实际 reason=%q", out.Reason)
	}
	cur, ok := m.Store().Get(g.ID)
	if !ok || cur.Status != GoalCompleted {
		t.Fatalf("原目标应 completed 且可回读: %+v", cur)
	}
	found := false
	for _, d := range m.Decisions(40) {
		if d.Kind == DecisionDegrade {
			found = true
		}
	}
	if !found {
		t.Fatal("判据降级必须留痕（决策台账出现 degrade）")
	}
}

// TestCapabilityGapNotTriggeredWhenVerifiedExists 反向断言：通道有过成功验收时不降级。
func TestCapabilityGapNotTriggeredWhenVerifiedExists(t *testing.T) {
	goal := &Goal{ID: "g", Stages: []Stage{{Index: 0, TaskID: "a", Passed: true}, {Index: 1, TaskID: "b", Passed: true}}}
	tasks := []*model.Task{{ID: "a", Verified: true}, {ID: "b"}}
	if gap := detectCapabilityGap(goal, tasks); gap != "" {
		t.Fatalf("通道已产出过可验证产物时不应判能力缺口: %s", gap)
	}
	// 阶段不足 2 个时也不判定（样本不足）
	if gap := detectCapabilityGap(&Goal{Stages: []Stage{{TaskID: "a", Passed: true}}}, tasks); gap != "" {
		t.Fatal("样本不足时不应判定能力缺口")
	}
}

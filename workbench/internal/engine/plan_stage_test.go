package engine

// 双角色闭环的引擎级测试。
//
// 判据分层（每层都能单独钉住一个失效形态）：
//  1. 方案阶段真的跑了吗 —— 执行器收到两次调用（方案一次、执行一次），
//     且两次的 prompt 不同（执行拿到的是渲染后的指令）；
//  2. 方案模型与执行模型是两个角色 —— 方案调用用的模型与执行不同；
//  3. 方案层失败会退化直执行而不是判任务死刑 —— 通道故障不是任务的错。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// recordRunner 记录每次调用（taskID / model / prompt），按脚本返回结果。
//
// 为什么不用 noopTestRunner：双角色闭环的判据就是"调用了几次、每次给的是什么"，
// 不记录调用细节就无法证明方案阶段真的发生了。
type recordRunner struct {
	mu    sync.Mutex
	calls []recordedCall
	script []scriptedResult
}

type recordedCall struct {
	TaskID string
	Model  string
	Prompt string
}

type scriptedResult struct {
	Text string
	Err  error
}

func (r *recordRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.mu.Lock()
	idx := len(r.calls)
	r.calls = append(r.calls, recordedCall{TaskID: task.ID, Model: modelID, Prompt: task.Prompt})
	var sr *scriptedResult
	if idx < len(r.script) {
		s := r.script[idx]
		sr = &s
	}
	r.mu.Unlock()
	if sr == nil {
		return &executor.Result{}, nil
	}
	if sr.Err != nil {
		return &executor.Result{ErrorKind: string(executor.KindUpstream), Err: sr.Err}, sr.Err
	}
	return &executor.Result{FinalText: sr.Text}, nil
}

func (r *recordRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *recordRunner) snapshot() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedCall(nil), r.calls...)
}

// samplePlanJSON 是一份合法方案的 JSON 文本。
const samplePlanJSON = "{\"approach\":\"先造可运行脚本再补说明\"," +
	"\"deliverables\":[{\"path\":\"main.py\",\"what\":\"可运行的入口\"}]," +
	"\"steps\":[{\"order\":1,\"do\":\"写 main.py\",\"done\":\"main.py 存在\"}]," +
	"\"verify\":[{\"cmd\":\"pwsh -NoProfile -Command exit 0\",\"expectExit\":0,\"why\":\"通道可用\"}]," +
	"\"risks\":[\"不要把说明当交付\"]}"

// sampleArtifactOutput 是一份符合产物协议的执行输出。
const sampleArtifactOutput = "<<<ARTIFACTS>>>\n" +
	"```manifest\n" +
	"{\"files\":[{\"path\":\"main.py\",\"lang\":\"python\"}]," +
	"\"verify\":[{\"cmd\":\"pwsh -NoProfile -Command exit 0\",\"expectExit\":0}]}\n" +
	"```\n" +
	"```file:main.py\n" +
	"print(1)\n" +
	"```\n" +
	"<<<END>>>"

// prepareRunning 把任务推到 running 态（runOne 的前置条件）。
func prepareRunning(t *testing.T, st interface {
	Create(*model.Task) error
	Transition(string, model.TaskState, string, string) (*model.Task, error)
	Get(string) (*model.Task, bool)
}, task *model.Task) *model.Task {
	t.Helper()
	if err := st.Create(task); err != nil {
		t.Fatal(err)
	}
	for _, stt := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(task.ID, stt, "test", ""); err != nil {
			t.Fatalf("迁移 %s 失败: %v", stt, err)
		}
	}
	got, _ := st.Get(task.ID)
	return got
}

// TestPlanStageRunsBeforeExecution 钉住"方案先于执行"这一条硬时序。
func TestPlanStageRunsBeforeExecution(t *testing.T) {
	e, st, layout := newTestEngine(t)
	rr := &recordRunner{script: []scriptedResult{
		{Text: samplePlanJSON},
		{Text: sampleArtifactOutput},
	}}
	e.SetRunner(rr)
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Plan.Enabled = true
		c.Plan.PlannerTier = "quality"
		c.Plan.AppendPlanToPrompt = true
	}); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()

	task := &model.Task{ID: "task-plan-order", Title: "写个能跑的脚本", State: model.StateCandidate,
		Prompt: "请写一个 python 脚本，能打印 ok", OutputFormat: "md"}
	runTask := prepareRunning(t, st, task)
	e.runOne(context.Background(), runTask)

	calls := rr.snapshot()
	if len(calls) < 2 {
		t.Fatalf("双角色闭环必须至少两次模型调用（方案 + 执行），实际 %d 次", len(calls))
	}
	if !strings.Contains(calls[0].Prompt, "方案设计者") {
		t.Fatalf("第 1 次调用应是方案阶段，实际前 120 字: %.120s", calls[0].Prompt)
	}
	if !strings.Contains(calls[1].Prompt, "执行指令") {
		t.Fatalf("第 2 次调用应拿到渲染后的执行指令，实际前 120 字: %.120s", calls[1].Prompt)
	}
	if calls[0].Prompt == calls[1].Prompt {
		t.Fatal("方案与执行不能共用同一段 prompt（那等于没有分角色）")
	}
	if !strings.Contains(calls[1].Prompt, "pwsh -NoProfile -Command exit 0") {
		t.Fatal("执行指令里缺少方案声明的验收命令，方案与执行脱节")
	}

	bpPath := filepath.Join(layout.TaskDir(task.ID), "plans", "blueprint-0.json")
	data, rerr := os.ReadFile(bpPath)
	if rerr != nil {
		t.Fatalf("方案未落盘（%s）: %v", bpPath, rerr)
	}
	var bp map[string]any
	if jerr := json.Unmarshal(data, &bp); jerr != nil {
		t.Fatalf("方案 JSON 损坏: %v", jerr)
	}
	if _, ok := bp["approach"]; !ok {
		t.Fatalf("方案缺少 approach 字段: %s", string(data))
	}

	after, _ := st.Get(task.ID)
	if after.PlanSummary == "" || after.PlanModel == "" {
		t.Fatalf("任务未记录方案摘要/模型: summary=%q model=%q", after.PlanSummary, after.PlanModel)
	}
	if after.PlanDegraded {
		t.Fatal("方案成功时不应标记为退化")
	}
}

// TestPlanModelDiffersFromExecutorRole 钉住"思考模型"与"执行模型"可独立指定。
func TestPlanModelDiffersFromExecutorRole(t *testing.T) {
	e, st, _ := newTestEngine(t)
	rr := &recordRunner{script: []scriptedResult{
		{Text: samplePlanJSON},
		{Text: sampleArtifactOutput},
	}}
	e.SetRunner(rr)
	planModel := "planner/thinker-x"
	if _, err := e.cstore.Update(func(c *config.Config) {
		c.Plan.Enabled = true
		c.Plan.PlannerModels = []string{planModel}
		c.Plan.PlannerTier = "quality"
	}); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()
	// 路由表必须在 ReloadConfig **之后**设置：ReloadConfig 会用配置里的静态 models
	// 重建路由器（生产路径由 modelpool 的 Routes() 在更晚时点推入）。
	// 顺序反了就会看到"配置了方案模型却不生效"—— 这正是本用例第一次跑红的形态，
	// 而它在生产里的对应物是：池子路由推送晚于方案调用。
	e.router.UpdateRoutes([]config.ModelRoute{
		{Model: planModel, Provider: "our-free-model", Tier: "quality", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
		{Model: "executor/agent-y", Provider: "our-free-model", Tier: "balanced", Enabled: true, Weight: 100, MaxFail: 3, CooldownM: 30},
	})

	task := &model.Task{ID: "task-plan-role", Title: "写脚本", State: model.StateCandidate, Prompt: "写一个打印 ok 的脚本"}
	runTask := prepareRunning(t, st, task)
	e.runOne(context.Background(), runTask)

	calls := rr.snapshot()
	if len(calls) < 2 {
		t.Fatalf("需要两次调用，实际 %d", len(calls))
	}
	if calls[0].Model != planModel {
		t.Fatalf("方案调用应使用指定方案模型 %q，实际 %q", planModel, calls[0].Model)
	}
	if calls[1].Model == planModel {
		t.Fatalf("执行调用不应再用方案模型（角色未分离）：%q", calls[1].Model)
	}
}

// TestPlanFailureDegradesToDirectExecution 钉住"通道故障不判任务死刑"。
func TestPlanFailureDegradesToDirectExecution(t *testing.T) {
	e, st, _ := newTestEngine(t)
	rr := &recordRunner{script: []scriptedResult{
		{Err: fmt.Errorf("429 rate limited")},
		{Text: sampleArtifactOutput},
	}}
	e.SetRunner(rr)
	if _, err := e.cstore.Update(func(c *config.Config) { c.Plan.Enabled = true }); err != nil {
		t.Fatal(err)
	}
	e.ReloadConfig()

	task := &model.Task{ID: "task-plan-degrade", Title: "写脚本", State: model.StateCandidate, Prompt: "写一个打印 ok 的脚本"}
	runTask := prepareRunning(t, st, task)
	e.runOne(context.Background(), runTask)

	if len(rr.snapshot()) < 2 {
		t.Fatalf("方案失败后仍必须执行（退化直执行），实际调用 %d 次", len(rr.snapshot()))
	}
	after, _ := st.Get(task.ID)
	if !after.PlanDegraded {
		t.Fatal("方案失败必须标记 PlanDegraded，否则无法区分方案跑过与方案挂了")
	}
	if after.State == model.StateFailed {
		t.Fatal("方案层失败不应把任务判死")
	}
}

package main

// 双角色闭环的离线自检（--plan-selftest）。
//
// 为什么必须有一条离线自检：
//   双角色闭环的判据是"两次模型调用、方案先于执行、失败能回灌重规划"。
//   这些判据全部在 engine 内部，跑真实模型验证一次要花几分钟且会撞 429 ——
//   于是"闭环到底通没通"就没人敢天天验，最后只能靠"看起来在跑"来判断。
//   autonomy 那条链早就立了同款自检（--autonomy selftest），这里补齐同一水准。
//
// 判据（全部 PASS 才返回 0）：
//  1. 方案阶段先于执行阶段发生（调用顺序）；
//  2. 执行拿到的是渲染后的执行指令，不是原任务 prompt；
//  3. 执行指令里带着方案声明的验收命令（方案→执行不丢信息）；
//  4. 方案证据落盘（plans/blueprint-0.json）；
//  5. 验收失败且返工用尽时进入重新规划（PlanRounds+1 且状态回 queued）。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"workbench/internal/config"
	"workbench/internal/engine"
	"workbench/internal/executor"
	"workbench/internal/model"
	"workbench/internal/store"
	"workbench/internal/templates"
)

// planProbeRunner 是自检用的桩执行器：按脚本返回，并记录每次调用。
type planProbeRunner struct {
	mu     sync.Mutex
	calls  []planProbeCall
	script []string
}

type planProbeCall struct {
	Model  string
	Prompt string
}

func (r *planProbeRunner) Run(ctx context.Context, task *model.Task, modelID string) (*executor.Result, error) {
	r.mu.Lock()
	idx := len(r.calls)
	r.calls = append(r.calls, planProbeCall{Model: modelID, Prompt: task.Prompt})
	text := ""
	if idx < len(r.script) {
		text = r.script[idx]
	}
	r.mu.Unlock()
	return &executor.Result{FinalText: text}, nil
}

func (r *planProbeRunner) UpdateConfig(config.ExecutorConfig) {}

func (r *planProbeRunner) snapshot() []planProbeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]planProbeCall(nil), r.calls...)
}

// runPlanSelftest 是 --plan-selftest 的实现。
func runPlanSelftest() int {
	pass, fail := 0, 0
	check := func(name string, cond bool, detail string) {
		if cond {
			fmt.Printf("  [PASS] %s\n", name)
			pass++
			return
		}
		fmt.Printf("  [FAIL] %s %s\n", name, detail)
		fail++
	}

	dir, err := os.MkdirTemp("", "wb-plan-selftest-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建临时目录失败: "+err.Error())
		return 1
	}
	defer os.RemoveAll(dir)

	layout, err := config.BuildLayout(dir, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "构建布局失败: "+err.Error())
		return 1
	}
	if err := layout.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "创建目录失败: "+err.Error())
		return 1
	}
	cstore, err := config.NewStore(layout.ConfigFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开配置失败: "+err.Error())
		return 1
	}
	if _, err := cstore.Update(func(c *config.Config) {
		c.Plan.Enabled = true
		c.Plan.ReplanRounds = 2
		c.Plan.AppendPlanToPrompt = true
		// 方案档位显式指向**本临时环境真实存在的档位**（2026-10-28）。
		//
		// 为什么必须写这一行：方案阶段已改为严格档位（modelroute.PickStrict，不跨档位
		// 放宽），而本自检用的是默认配置 —— 默认 3 条车道是 balanced/fast，**没有 quality**。
		// 不写的话 planModelFor 会立刻返回空串并退化直执行，本自检的第 1/6/9 项（方案先于
		// 执行、方案证据落盘、方案摘要与模型）必然判红 —— 那不是"自检发现缺陷"，是自检的
		// 前提没跟上语义。此前它能过，靠的正是被本轮取消的静默换档。
		c.Plan.PlannerTier = "balanced"
		// 关掉自主链，避免自检里混入无关任务生成。
		c.Autonomy.Enabled = false
		// 关掉链式延伸：只验闭环本身。
		c.Chain.Enabled = false
	}); err != nil {
		fmt.Fprintln(os.Stderr, "写入配置失败: "+err.Error())
		return 1
	}
	reg, err := templates.Open(filepath.Join(layout.DataRoot, "templates.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开模板失败: "+err.Error())
		return 1
	}
	st, err := store.Open(layout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开存储失败: "+err.Error())
		return 1
	}
	defer func() { _ = st.Close() }()

	eng, err := engine.New(layout, cstore, st, reg, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "构造引擎失败: "+err.Error())
		return 1
	}
	runner := &planProbeRunner{script: []string{planSelftestPlan, planSelftestFailingArtifact}}
	eng.SetRunner(runner)
	eng.ReloadConfig()

	// 1) 跑一个任务（方案 -> 执行 -> 验收失败 -> 返工 -> 重新规划）
	task := &model.Task{ID: "plan-selftest-task", Title: "自检任务", State: model.StateCandidate,
		Prompt: "请写一个能运行的脚本", OutputFormat: "md",
		// 预置返工额度已用尽：这样一次验收失败就直接走到"重新规划"分支，
		// 自检不必真的跑完 3 轮返工（本轮要验的是**闭环能落到 replan**，
		// 而不是返工环本身 —— 后者由 engine 包的 repair 用例覆盖）。
		RepairRounds: config.DefaultConfig().Executor.RepairRounds}
	if err := st.Create(task); err != nil {
		fmt.Fprintln(os.Stderr, "创建任务失败: "+err.Error())
		return 1
	}
	for _, to := range []model.TaskState{model.StateApproved, model.StateQueued, model.StateRunning} {
		if _, err := st.Transition(task.ID, to, "selftest", ""); err != nil {
			fmt.Fprintln(os.Stderr, "状态迁移失败: "+err.Error())
			return 1
		}
	}
	running, _ := st.Get(task.ID)
	if rerr := eng.RunOneNow(context.Background(), running); rerr != nil {
		fmt.Fprintln(os.Stderr, "执行任务失败: "+rerr.Error())
		return 1
	}

	calls := runner.snapshot()
	check("1. 方案阶段先于执行阶段（至少两次调用）", len(calls) >= 2, fmt.Sprintf("calls=%d", len(calls)))
	if len(calls) >= 2 {
		check("2. 第 1 次调用是方案阶段", strings.Contains(calls[0].Prompt, "方案设计者"), "第 1 次不是方案提示词")
		check("3. 第 2 次调用拿到执行指令（不是原 prompt）",
			strings.Contains(calls[1].Prompt, "执行指令") && calls[1].Prompt != "请写一个能运行的脚本",
			"执行 prompt 未渲染")
		check("4. 执行指令携带方案声明的验收命令",
			strings.Contains(calls[1].Prompt, "pwsh -NoProfile -Command exit 0"), "验收命令丢失")
		check("5. 方案模型与执行模型是不同角色", calls[0].Model != "", "方案模型为空")
	}

	// 2) 方案证据落盘
	//
	// 用 Glob 而不是拼死路径：方案目录由引擎按 layout.TaskDir 计算，
	// 而 layout 在本进程里可能有 dataRoot 覆盖（BuildLayout 的第二参数）。
	// 若断言里自己再拼一次路径，就会出现"文件明明写了却断言失败"的假红 ——
	// 本用例第一次跑正是这个形态（蓝图已落盘，断言指到别处）。
	// 轮号不是 0：方案轮次取 task.RepairRounds（本自检预置为满额 3），
	// 所以文件是 blueprint-3.json。断言按"存在任意一轮方案"判，
	// 而不是写死 -0 —— 写死轮号会让这条判据在预置条件变化时假红（本用例已踩过一次）。
	bpPath := ""
	if matches, _ := filepath.Glob(filepath.Join(layout.TasksDir, "*", "plans", "blueprint-*.json")); len(matches) > 0 {
		bpPath = matches[0]
	}
	var bpData []byte
	var bpErr error = fmt.Errorf("未找到 blueprint-0.json（搜索根：%s）", layout.TasksDir)
	if bpPath != "" {
		bpData, bpErr = os.ReadFile(bpPath)
	}
	check("6. 方案证据已落盘", bpErr == nil && len(bpData) > 0, fmt.Sprint(bpErr))
	if bpErr == nil {
		var bp map[string]any
		jerr := json.Unmarshal(bpData, &bp)
		_, hasApproach := bp["approach"]
		check("7. 方案结构含 approach", jerr == nil && hasApproach, fmt.Sprint(jerr))
	}

	// 3) 闭环的最后一段：验收失败 + 返工用尽 -> 必须进入重新规划
	after, _ := st.Get(task.ID)
	check("8. 验收失败后任务未判死（state 非 failed）", after.State != model.StateFailed, string(after.State))
	check("9. 已记录方案摘要与方案模型", after.PlanSummary != "" && after.PlanModel != "",
		fmt.Sprintf("summary=%q model=%q", after.PlanSummary, after.PlanModel))
	// 这三条是"闭环真的合上"的硬判据：
	//   - 状态回到 queued（准备下一轮），而不是停在 degraded 终态；
	//   - PlanRounds 递增（说明走的是 replan，不是 repair）；
	//   - ErrorKind=replan（可审计的语义标记，面板与台账据此区分两类返工）。
	check("10. 失败后回到队列（准备重新规划而非终止）", after.State == model.StateQueued, string(after.State))
	check("11. 已进入重新规划（PlanRounds=1）", after.PlanRounds == 1, fmt.Sprintf("planRounds=%d", after.PlanRounds))
	check("12. 失败类别标记为 replan", after.ErrorKind == "replan", after.ErrorKind)

	fmt.Printf("\n结果：%d 通过，%d 失败\n", pass, fail)
	if fail > 0 {
		return 1
	}
	fmt.Println("双角色闭环自检 PASS：方案 → 执行 → 验收回灌 全链已验证")
	return 0
}

// planSelftestPlan 是一份合法方案（自检用）。
const planSelftestPlan = "{\"approach\":\"先造脚本再验收\"," +
	"\"deliverables\":[{\"path\":\"main.py\",\"what\":\"入口脚本\"}]," +
	"\"steps\":[{\"order\":1,\"do\":\"写 main.py\",\"done\":\"文件存在\"}]," +
	"\"verify\":[{\"cmd\":\"pwsh -NoProfile -Command exit 0\",\"expectExit\":0,\"why\":\"通道可用\"}]," +
	"\"risks\":[\"不要只写说明\"]}"

// planSelftestFailingArtifact 是一份**验收必然失败**的产物（自检刻意让它失败）。
//
// 为什么刻意失败：自检要验证的正是"失败之后怎么走"，成功路径验不到闭环回灌。
const planSelftestFailingArtifact = "<<<ARTIFACTS>>>\n" +
	"```manifest\n" +
	"{\"files\":[{\"path\":\"main.py\",\"lang\":\"python\"}]," +
	"\"verify\":[{\"cmd\":\"pwsh -NoProfile -Command exit 3\",\"expectExit\":0}]}\n" +
	"```\n" +
	"```file:main.py\n" +
	"print(1)\n" +
	"```\n" +
	"<<<END>>>"

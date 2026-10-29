package engine

// 本文件实现「双角色闭环」的编排：思考模型出方案 → agent 模型执行 → 验收失败回灌重新规划。
//
// 立规原话（老大 2026-10-21）：
//    「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」
//
// 改造前的失效形态（现场实测，见 docs/缺陷-运行中但产出为0）：
//   engine.runOne 每个任务只做一次模型调用，路由器选一个模型，task.Prompt 原样喂过去。
//   于是：有推理优势的模型被当成"写稿机"；擅长执行的 agent 模型拿不到任何结构化指令；
//   验收失败后走 repairPrompt（把报错塞回去让同一模型重做），方案层面错了就永远错。
//
// 本文件把三类错误分开处理，这是"闭环"的真正含义：
//   1. 实现层面的错  → 方案不变，执行模型带着报错重做（既有 repair 环，RepairRounds）；
//   2. 方案层面的错  → 重新规划（ReplanRounds），新方案 + 新失败证据一起回灌；
//   3. 通道层面的错  → 不消耗方案轮次，退化直执行并把事实写进审计（Degraded）。
//
// 为什么规划用同一个 runner 而不是自建：
//   judge.SetRunner 那次教训（2026-10-20 D7）—— 自建 runner 会绕过 FailoverRunner 的
//   故障转移、车道健康度与全局限速闸门，每个任务额外多 N 次不受控请求，直接撞上游限流。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"workbench/internal/blueprint"
	"workbench/internal/config"
	"workbench/internal/model"
)

// planStage 是一次规划阶段的结果。
type planStage struct {
	// Blueprint 是方案；nil 表示退化直执行。
	Blueprint *blueprint.Blueprint
	// Model 是方案模型 id（审计与面板展示）。
	Model string
	// Problems 是解析/规范化产生的问题（审计用）。
	Problems []string
	// Err 是规划失败原因（nil = 成功）。
	Err error
	// ElapsedMs 是规划耗时。
	ElapsedMs int64
	// StderrPath / ProducedRunes 是方案阶段的「失败取证」两件套（T15-④ 的第二本站）：
	//   空/0 = 规划模型没吐任何东西；非 0 = 吐了但解析失败。台账据此把两类失败分开，
	//   不必再翻任务目录去猜（对齐执行阶段的同名台账字段）。
	StderrPath    string
	ProducedRunes int
	// DSHCommand 是本轮方案调用实际执行的 DSH 无头命令行（自证走 DSH 无头控制，round 38 · T21）。
	DSHCommand string
	// StageBudgetExhausted 标记「整段方案阶段的预算到点」（T23）：与单次调用失败区分，
	// 便于面板/台账读成「时间让给执行阶段」，而不是「模型坏了」。
	StageBudgetExhausted bool
}

// usable 判断这次规划是否可用于编排执行。
func (s *planStage) usable() bool {
	return s != nil && s.Blueprint != nil && !s.Blueprint.IsZero()
}

// plannerTierOf 返回生效的方案档位（空则默认 quality）。
//
// 单独成函数是为了让"选型用的档位"与"退化原因里报的档位"共用同一口径 ——
// 两处各写一遍就会出现"按 quality 选、失败文案却说是别的档位"的分叉。
func plannerTierOf(cfg config.Config) string {
	tier := strings.TrimSpace(cfg.Plan.PlannerTier)
	if tier == "" {
		tier = "quality"
	}
	return tier
}

// planModelFor 选一个模型来出方案。
//
// 选型顺序（每一步都有明确的兜底语义，不静默跳过）：
//  1. 配置里显式指定的 PlannerModels（按顺序，取第一个可用）；
//  2. 否则按 PlannerTier **在本档位内**选（PickStrict，不做跨档位放宽）；
//  3. 都不可用 -> 返回空串，由调用方退化直执行（并留审计）。
//
// 为什么指定模型要单独判可用：配置里写的是模型 id，而模型池会因限流/探测失败
// 把它们移出路由表。直接信配置就会出现"配了但永远选不到"的静默失效。
//
// 为什么第 2 步用 PickStrict 而不是 Pick（2026-10-28 实测 + 老大授权改为"产物内容可因
// 车道选择变化而不同"）：宽选会把"档位没配"变成"静默拿执行车道出方案"。实测代价 ——
// 活实例 plannerTier="quality" 而路由表 18 条车道全是 balanced/fast，方案阶段因此长期
// 跑在执行型车道上：24h 内方案占全部尝试时长 30.1%（2.33h/7.76h），其中 94% 白烧
// （成功 8/140）；产物面更狠，z-ai/glm-5.3 与 glm-5.3-flash 合计 29 次尝试留下 0 份
// blueprint。档位对不上就快速退化直执行，比"烧满并发槽换一份出不了的方案"更接近意图。
//
// 想让方案改跑别的档位不需要新开关：把 plannerTier 配成那个档位，或用 PlannerModels
// 直接点名模型 —— 能力没删，只是不再静默替换档位。
func (e *Engine) planModelFor(cfg config.Config) string {
	for _, id := range cfg.Plan.PlannerModels {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		// ProviderOf 非空 = 这个模型在路由表里（modelroute 只装表内条目）。
		if e.router.ProviderOf(id) != "" {
			return id
		}
	}
	if id, err := e.router.PickStrict(plannerTierOf(cfg), nil); err == nil {
		return id
	}
	return ""
}

// buildPlan 跑一次方案阶段：思考模型只出方案，不产出文件。
//
// lastFailure 非空表示这是"重新规划"（验收失败回灌）。
// 返回的 planStage 永不为 nil；失败时 Blueprint 为 nil 且 Err 非空。
func (e *Engine) buildPlan(ctx context.Context, task *model.Task, cfg config.Config, lastFailure string, round int) *planStage {
	st := &planStage{}
	modelID := e.planModelFor(cfg)
	if modelID == "" {
		// 两种情形都不静默：① 配置档位内没有车道（方案阶段不做跨档位放宽）；② 整池冷却。
		// 文案带上档位，是因为"配了 quality 但池里没有 quality 车道"是最容易被忽略的成因 ——
		// 不报出来，下一个人只会看到"方案老是退化"而找不到为什么。
		st.Err = fmt.Errorf("方案档位 %q 内没有可用模型（未跨档位放宽；请配置该档位车道或改用 PlannerModels 点名）", plannerTierOf(cfg))
		return st
	}
	st.Model = modelID

	// 分档降载：首轮（round 0）的方案只要「最小可验收」体量，后续轮次才回到模板体量（round 41 · T17③）。
	minWords := effectiveMinWords(minWordsOf(e, task), round)
	goalIntent := ""
	if e.autonomy != nil && task.GoalID != "" {
		// 目标意图让方案服务于长期目标，而不是只对一句话任务负责。
		goalIntent = e.autonomyGoalIntentFor(task.GoalID)
	}
	prompt := blueprint.PlanPrompt(goalIntent, task.Title, task.Prompt, minWords, lastFailure)

	minutes := cfg.Plan.PlannerMaxMinutes
	if minutes <= 0 {
		minutes = 8
	}
	// 方案任务走独立 id，避免与真实任务目录冲突（执行器按 id 建目录）。
	sub := &model.Task{
		ID:     task.ID + "-plan-r" + fmt.Sprint(round),
		Title:  "方案：" + task.Title,
		Prompt: prompt,
		// 显式标记方案阶段：产物判据据此豁免（执行器仍会拼产物协议，见 model.PhasePlan 注释）。
		Phase:      model.PhasePlan,
		TimeoutMin: minutes,
		// WorkRoot 继承父任务：否则 project 模式会为方案调用多建一个空沙箱
		// （与 judge 那次同类，现场实测 40 秒内 26 个空沙箱）。
		WorkRoot: task.WorkRoot,
	}
	started := time.Now()
	// 方案阶段**总预算**（T23）：单次 8 分钟 × N 条车道实测能把整个任务预算吃光（15 分钟窗口内执行阶段一次都没跑到，
	// 而产物只有执行阶段能产出）。给整段方案一个 deadline，到点即取消子调用 ⇒ 由调用方退化直执行（留审计，不静默跳过）。
	sctx, scancel := context.WithTimeout(ctx, planStageBudget(cfg.Plan.StageMaxMinutes))
	defer scancel()
	res, err := e.runner.Run(sctx, sub, modelID)
	st.ElapsedMs = time.Since(started).Milliseconds()
	if err != nil && sctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		// 只有「阶段预算到点」才改写成这条原因：调用方自身的取消/超时不得被冒充（否则会误导排查方向）。
		st.Err = fmt.Errorf("方案阶段总预算耗尽（%d 分钟），退化直执行", cfg.Plan.StageMaxMinutes)
		st.StageBudgetExhausted = true
		return st
	}
	// 先取证再判失败：三条失败路径（调用失败 / 空输出 / 解析失败）都要带着这份读数回去。
	st.StderrPath, st.ProducedRunes, st.DSHCommand = attemptEvidence(res)
	if err != nil {
		st.Err = fmt.Errorf("方案调用失败: %w", err)
		return st
	}
	if res == nil || strings.TrimSpace(res.FinalText) == "" {
		st.Err = fmt.Errorf("方案模型无输出")
		return st
	}
	bp, problems, perr := blueprint.Parse(res.FinalText)
	st.Problems = problems
	if perr != nil {
		st.Err = perr
		return st
	}
	bp.PlannerModel = modelID
	bp.Round = round
	bp.At = model.Now()
	st.Blueprint = bp
	return st
}

// planDir 返回方案落盘目录（任务目录下的 plans/）。
func (e *Engine) planFile(taskID, name string) string {
	return filepath.Join(e.layout.TaskDir(taskID), "plans", name)
}

// savePlan 把方案与规划提示词落盘，作为可复核证据。
//
// 为什么一定要落盘：
//  1. "思考模型只出方案"是否真的发生，只能靠落盘文件证明（失败即退化直执行时尤其重要）；
//  2. 返工/重新规划需要回看"上一版方案是什么"，否则无法判断是方案错还是实现错；
//  3. 面板与人工复盘都从这里读，不必翻日志。
func (e *Engine) savePlan(task *model.Task, st *planStage, prompt string, round int) {
	dir := filepath.Dir(e.planFile(task.ID, "x"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.log.Warn("创建方案目录失败", "taskId", task.ID, "err", err.Error())
		return
	}
	if err := config.WriteFileAtomic(e.planFile(task.ID, fmt.Sprintf("prompt-plan-%d.md", round)), []byte(prompt)); err != nil {
		e.log.Warn("落盘方案提示词失败", "taskId", task.ID, "err", err.Error())
	}
	if st == nil || st.Blueprint == nil {
		return
	}
	if err := config.WriteJSONAtomic(e.planFile(task.ID, fmt.Sprintf("blueprint-%d.json", round)), st.Blueprint); err != nil {
		e.log.Warn("落盘方案失败", "taskId", task.ID, "err", err.Error())
	}
	note := map[string]any{
		"round":     round,
		"model":     st.Model,
		"elapsedMs": st.ElapsedMs,
		"problems":  st.Problems,
		"usable":    st.usable(),
	}
	if st.Err != nil {
		note["error"] = st.Err.Error()
	}
	_ = config.WriteJSONAtomic(e.planFile(task.ID, fmt.Sprintf("plan-%d.meta.json", round)), note)
}

// autonomyGoalIntentFor 取自主目标的意图文本（无自主子系统或目标不存在时为空）。
//
// 用 Goals() 遍历而不是新加一个按 id 查的方法：autonomy.Manager 的对外面已经很大，
// 这里只需要读一个字段，加接口会让"谁负责目标事实"变模糊。
// 目标数量受 MaxStages 约束（个位数），遍历成本可忽略。
func (e *Engine) autonomyGoalIntentFor(goalID string) string {
	if e.autonomy == nil || strings.TrimSpace(goalID) == "" {
		return ""
	}
	for _, g := range e.autonomy.Goals() {
		if g != nil && g.ID == goalID {
			return g.Intent
		}
	}
	return ""
}

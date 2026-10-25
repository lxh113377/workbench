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
}

// usable 判断这次规划是否可用于编排执行。
func (s *planStage) usable() bool {
	return s != nil && s.Blueprint != nil && !s.Blueprint.IsZero()
}

// planModelFor 选一个模型来出方案。
//
// 选型顺序（每一步都有明确的兜底语义，不静默跳过）：
//  1. 配置里显式指定的 PlannerModels（按顺序，取第一个可用）；
//  2. 否则按 PlannerTier 从路由器选（Excluded=nil，不与执行模型互斥 ——
//     同一个模型既出方案又执行是允许的，只是失去了"双角色"的收益）；
//  3. 都不可用 -> 返回空串，由调用方退化直执行。
//
// 为什么指定模型要单独判可用：配置里写的是模型 id，而模型池会因限流/探测失败
// 把它们移出路由表。直接信配置就会出现"配了但永远选不到"的静默失效。
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
	tier := strings.TrimSpace(cfg.Plan.PlannerTier)
	if tier == "" {
		tier = "quality"
	}
	// 指定模型全不在表里时，仍按 tier 兜底：宁可换模型出方案，也不要静默退化成
	// "没有方案"——那会让整条双角色链路在配置写错时无声失效。
	if _, err := e.router.Pick(tier, nil); err != nil {
		// tier 无候选时再放宽到任意档位（Pick 内部已有 strict->宽松的二次尝试，
		// 这里只兜住"整池都不可用"的极端情况）。
		if _, err2 := e.router.Pick("", nil); err2 != nil {
			return ""
		}
	}
	// 真正取一次（Pick 有加权随机，不能只判可行性）。
	if id, err := e.router.Pick(tier, nil); err == nil {
		return id
	}
	if id, err := e.router.Pick("", nil); err == nil {
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
		st.Err = fmt.Errorf("没有可用的方案模型（路由表为空或全部冷却）")
		return st
	}
	st.Model = modelID

	minWords := minWordsOf(e, task)
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
		ID:         task.ID + "-plan-r" + fmt.Sprint(round),
		Title:      "方案：" + task.Title,
		Prompt:     prompt,
		TimeoutMin: minutes,
		// WorkRoot 继承父任务：否则 project 模式会为方案调用多建一个空沙箱
		// （与 judge 那次同类，现场实测 40 秒内 26 个空沙箱）。
		WorkRoot: task.WorkRoot,
	}
	started := time.Now()
	res, err := e.runner.Run(ctx, sub, modelID)
	st.ElapsedMs = time.Since(started).Milliseconds()
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

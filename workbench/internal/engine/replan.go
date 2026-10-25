package engine

// 本文件实现"重新规划"：验收失败且返工用尽后，回到方案模型重出方案。
//
// 为什么这个分支必须存在（立规原话的反面）：
//   老大说的是「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」。
//   反过来说，只执行不出方案的模型同样闭不上环：
//     - 执行模型拿到的是一句话任务，它自己猜交付物、自己定验收命令；
//     - 猜错时只能反复重做同一件事（repair 环），永远不回头检查"猜的前提对不对"。
//   重新规划把"方案"变成可迭代的对象：失败证据回灌给方案模型，让它改前提。
//
// 与返工（repair）的分工：
//   repair      — 方案不变，改实现（便宜，先用完）；
//   replan      — 改方案（贵，后用）；
//   两者共用同一条 running→queued 退回通道与既有的调度/并发逻辑。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"workbench/internal/blueprint"
	"workbench/internal/config"
	"workbench/internal/model"
)

// currentPlan 读出任务最近一版方案（不存在/损坏返回 nil）。
//
// 读盘而不是放内存：方案是"可复核证据"，引擎重启后仍在；
// 放在内存 map 里会随进程重启丢失，重启后 shouldReplan 就退化成"随便重规划"。
func currentPlan(taskID string, e *Engine) *blueprint.Blueprint {
	if e == nil || taskID == "" {
		return nil
	}
	// 从高到低找最近一版（轮次目录名带轮号）。
	for round := 9; round >= 0; round-- {
		p := e.planFile(taskID, fmt.Sprintf("blueprint-%d.json", round))
		data, err := os.ReadFile(p)
		if err != nil || len(data) == 0 {
			continue
		}
		var bp blueprint.Blueprint
		if jerr := json.Unmarshal(data, &bp); jerr != nil {
			continue
		}
		return &bp
	}
	return nil
}

// scheduleReplan 把任务改造成"下一轮重新规划"并重新入队。
//
// 语义：
//   - 任务不失败、不降级，回到 queued 再跑一轮；
//   - PlanRounds +1（与 RepairRounds 分开计）；
//   - 失败证据留在任务字段与 verify-round 文件里，下一轮 buildPlan 会读出来；
//   - 原 Prompt 保持不变（重新规划要基于原始需求，而不是上一轮的返工 prompt）。
//
// 为什么要把 Prompt 还原成原始需求：
//
//	返工环会把 task.Prompt 换成"返工说明 + 上一版全文"。若带着它重新规划，
//	方案模型看到的是"修这个实现"，而不是"重新设计这件事" —— 前提就错了。
//	原始需求从 tasks/<id>/prompt.md 读回（store 在创建任务时落的）。
func (e *Engine) scheduleReplan(task *model.Task, rep *model.VerifyReport, cfg config.Config) {
	if task == nil {
		return
	}
	round := task.PlanRounds + 1
	original := e.originalPromptFor(task)

	// 失败证据落盘（与复盘的 verify-round-N.json 分开命名，避免覆盖返工证据）。
	if rep != nil {
		_ = config.WriteJSONAtomic(e.planFile(task.ID, fmt.Sprintf("verify-before-replan-%d.json", round)), rep)
	}

	if _, err := e.store.Update(task.ID, func(t *model.Task) error {
		t.PlanRounds = round
		if original != "" {
			t.Prompt = original
		}
		t.Verified = false
		t.Error = "验收未通过，进入第 " + fmt.Sprint(round) + " 轮重新规划"
		t.ErrorKind = "replan"
		return nil
	}); err != nil {
		e.log.Warn("写入重新规划状态失败", "taskId", task.ID, "err", err.Error())
		return
	}
	if q, err := e.store.Transition(task.ID, model.StateQueued, "engine",
		fmt.Sprintf("验收未通过，第 %d 轮重新规划", round)); err == nil {
		e.q.Push(q)
		e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: q,
			Msg: fmt.Sprintf("方案层面返工：已排入第 %d 轮重新规划", round)})
	} else {
		e.log.Warn("重新规划入队失败", "taskId", task.ID, "err", err.Error())
		return
	}
	e.log.Warn("排入重新规划", "taskId", task.ID, "round", round,
		"reason", truncateRunes(failReasonOf(rep), 200))
	e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID,
		Msg: fmt.Sprintf("第 %d 轮重新规划：返工已用尽，回到方案层（%s）", round, truncateRunes(failReasonOf(rep), 120))})
}

// originalPromptFor 取任务的原始需求文本（用于重新规划时还原前提）。
//
// 取值顺序：
//  1. tasks/<id>/prompt.md（store 落盘的原始 prompt，最权威）；
//  2. 若不存在（老任务/异常），返回空串 —— 调用方保留当前 Prompt，不硬造。
func (e *Engine) originalPromptFor(task *model.Task) string {
	if task == nil {
		return ""
	}
	for _, name := range []string{"prompt.md", filepath.Join("plans", "prompt-original.md")} {
		p := filepath.Join(e.layout.TaskDir(task.ID), name)
		if data, err := os.ReadFile(p); err == nil && len(data) > 0 {
			return string(data)
		}
	}
	return ""
}

package engine

import (
	"encoding/json"
	"fmt"

	"workbench/internal/config"
	"workbench/internal/model"
)

// scheduleRepair 把"验收失败的任务"改造成"带报错的返工任务"并重新入队。
//
// 语义：
//   - 任务不失败、不降级，而是回到 queued 再跑一轮；
//   - RepairRounds +1，并保留上一版产出（不删文件，下一轮验收会覆盖）；
//   - prompt 换成返工 prompt（原任务 + 失败证据 + 上一版全文）；
//   - 新 prompt 落盘为 prompt.repair-<n>.md，便于审计"它到底被要求改什么"。
//
// 为什么不在原地重跑：状态机把"执行中"和"排队"分得很清，退回 queued 才能
// 复用既有的调度/并发/模型选择逻辑，也不会长时间占着并发槽。
func (e *Engine) scheduleRepair(task *model.Task, rep *model.VerifyReport, workDir string, cfg config.Config) {
	round := task.RepairRounds + 1
	prev := readPrevArtifacts(workDir, 200_000)
	newPrompt := repairPrompt(task, prev, rep, round)

	if err := e.store.SaveTaskArtifact(task.ID, fmt.Sprintf("prompt.repair-%d.md", round), []byte(newPrompt)); err != nil {
		e.log.Warn("落盘返工 prompt 失败", "taskId", task.ID, "err", err.Error())
	}
	// 记录本轮验收证据，便于面板看到"第几轮、失败在哪"
	_ = e.store.SaveTaskArtifact(task.ID, fmt.Sprintf("verify-round-%d.json", round), mustJSONBytes(rep))

	if _, err := e.store.Update(task.ID, func(t *model.Task) error {
		t.Prompt = newPrompt
		t.RepairRounds = round
		t.Verify = rep
		t.Verified = false
		t.Error = "验收未通过，第 " + fmt.Sprint(round) + " 轮返工"
		t.ErrorKind = "verify_failed"
		return nil
	}); err != nil {
		e.log.Warn("写入返工状态失败", "taskId", task.ID, "err", err.Error())
		return
	}
	// 走合法迁移：running -> queued 已放开（环境性退回同一通道）。
	if q, err := e.store.Transition(task.ID, model.StateQueued, "engine", fmt.Sprintf("验收未通过，第 %d 轮返工", round)); err == nil {
		e.q.Push(q)
		e.emitEvent(model.Event{Type: "task", At: model.Now(), TaskID: task.ID, Data: q,
			Msg: fmt.Sprintf("验收未通过，已排入第 %d 轮返工", round)})
	} else {
		e.log.Warn("返工入队失败", "taskId", task.ID, "err", err.Error())
	}
	e.log.Warn("排入返工", "taskId", task.ID, "round", round,
		"reason", truncateRunes(failReasonOf(rep), 200))
	e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID,
		Msg: fmt.Sprintf("第 %d 轮返工：%s", round, truncateRunes(failReasonOf(rep), 160))})
}

// mustJSONBytes 序列化为缩进 JSON（失败时返回占位串，不影响主流程）。
func mustJSONBytes(v any) []byte {
	b, err := jsonMarshalIndent(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// jsonMarshalIndent 是 encoding/json 的薄封装：统一 2 空格缩进，便于人读。
func jsonMarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

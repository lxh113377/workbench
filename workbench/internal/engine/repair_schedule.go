package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"workbench/internal/config"
	"workbench/internal/executor"
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
		// 首轮返工时登记原文：此刻 task.Prompt 仍是用户/模板/规划器给的原始要求，没被返工污染。
		// 只写一次 —— 后续轮次保留首轮那份，供 repairPrompt 嵌「### 原始任务」用（T90）。
		if round == 1 && strings.TrimSpace(t.OriginalPrompt) == "" {
			t.OriginalPrompt = task.Prompt
		}
		t.Prompt = newPrompt
		t.RepairRounds = round
		t.Verify = rep
		t.Verified = false
		t.Error = "验收未通过，第 " + fmt.Sprint(round) + " 轮返工"
		// 类别取自枚举而非裸字符串（issue #9 收编）：值逐字仍是 verify_failed，
		// 生产台账里的历史读数不受影响；改名等于篡改历史。
		t.ErrorKind = string(executor.KindVerifyFailed)
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
	// 体积画像（round 137 · T91 第一步：先可观测，再谈阈值）。
	//
	// 为什么要它：实测某轮返工 prompt 689,618 B，其中「上一版产出」段占 687,472 B（99.7%），
	// 而本会话刚修的「原始任务」嵌套只占 1,533 B —— 主要矛盾不在我上一轮修的地方。
	// 决定"单文件截断阈值该取多少"需要**分布数据**，不能拍脑袋：所以我上一轮登记的
	// 「复用 artifact.Limits.MaxFileBytes」也被实测否证了 —— 它默认 2<<20 = 2MB，
	// 对这里 271KB 量级的大文件根本不会触发（见 repairSizeProfile 注释与 T91）。
	prof := repairSizeProfile(prev, newPrompt)
	e.log.Warn("排入返工", "taskId", task.ID, "round", round,
		"reason", truncateRunes(failReasonOf(rep), 200),
		"promptBytes", prof.total, "prevBytes", prof.prevTotal, "prevFiles", len(prev),
		"largestFile", prof.largestPath, "largestBytes", prof.largest)
	e.emitEvent(model.Event{Type: "warn", At: model.Now(), TaskID: task.ID,
		Msg: fmt.Sprintf("第 %d 轮返工：%s", round, truncateRunes(failReasonOf(rep), 160))})
}

// repairSizeProfile 统计返工 prompt 的体积构成，用于决定截断阈值（T91）。
//
// 口径为什么是 prev 的**原始 body 长度**而不是"解析渲染后的 markdown 段"：
// 我上一轮用正则去抠 ```file: 段，结果被嵌套内容里的同名围栏截断（算出 278,403 B，
// 而真实段是 687,472 B）⇒ 解析式测量在"被测物自己含分隔符"时不可信。
// prev 就是即将被嵌进去的那些 body，量它的原始字节数不需要解析，因此没有这个失效模式。
type sizeProfile struct {
	total       int // 整份返工 prompt 的字节数
	prevTotal   int // 全部「上一版产出」body 的字节合计
	largest     int // 最大单个文件的字节数
	largestPath string
}

func repairSizeProfile(prev []artifactFile, newPrompt string) sizeProfile {
	p := sizeProfile{total: len(newPrompt)}
	for _, f := range prev {
		n := len(f.Body)
		p.prevTotal += n
		if n > p.largest {
			p.largest = n
			p.largestPath = f.Path
		}
	}
	return p
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

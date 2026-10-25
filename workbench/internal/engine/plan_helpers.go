package engine

// 本文件是方案阶段接入 runOne 的辅助层。
//
// 拆出来的原因：engine.go 已经 78KB，而这三个函数各自有独立的失效形态需要注释与测试：
//   - cloneTaskWithPrompt：共享指针会让"渲染后的执行指令"污染原任务的 Prompt（返工环读的是原 prompt）；
//   - planFailureContext：返工环把验收证据放在 verify.json / 任务字段里，方案层要读出来才能"重新规划"；
//   - planReplanReason：判断这次失败该"重新规划"还是"继续返工"，这是闭环能不能收敛的关键分支。

import (
	"fmt"
	"strings"

	"workbench/internal/blueprint"
	"workbench/internal/config"
	"workbench/internal/model"
)

// cloneTaskWithPrompt 复制任务并把 Prompt 换成渲染后的执行指令。
//
// 为什么必须复制：runner 拿到的 *model.Task 会被执行器读取（task.ID 建目录、
// task.WorkRoot 定 cwd、task.TimeoutMin 定超时）。若原地改 Prompt，
// 后续 completeTask → scheduleRepair 读到的就是"渲染后的指令"而不是原始需求，
// 返工 prompt 会越叠越长（原任务里嵌着上一轮的方案 + 上一轮的返工说明）。
// 现场同类事故：executor 曾把任务 Prompt 原地追加协议，导致 repair 后 prompt 里
// 有两份产物协议说明，模型两次看到矛盾指令。
func cloneTaskWithPrompt(task *model.Task, prompt string) *model.Task {
	if task == nil {
		return nil
	}
	cp := *task
	cp.Prompt = prompt
	return &cp
}

// planFailureContext 组装"上一轮为什么失败"的证据文本，供重新规划使用。
//
// 证据从三处取（优先级从高到低）：
//  1. task.Verify：最近一轮真实验收报告（退出码/stderr，最硬的证据）；
//  2. task.Error / ErrorKind：失败类别与人读原因；
//  3. 返工轮次：告诉方案模型"这是第几轮了"，避免它以为还是首轮。
//
// 为什么不能只传 FailReason 一句话：
//
//	方案模型要判断"是方案错了还是实现错了"，只给它一句"验收未通过"它只能瞎猜。
//	实测（2026-10-21）repairPrompt 里带 stderr 的那几轮返工成功率明显更高 ——
//	同样的原则适用到方案层。
func planFailureContext(task *model.Task, cfg config.Config) string {
	if task == nil {
		return ""
	}
	// 首轮（没返工过、没验收报告、没错误）不需要失败上下文。
	if task.RepairRounds <= 0 && task.Verify == nil && strings.TrimSpace(task.Error) == "" {
		return ""
	}
	var b strings.Builder
	if task.RepairRounds > 0 {
		b.WriteString(fmt.Sprintf("已执行 %d 轮返工（每轮都是同一个方案重做），仍未通过验收。\n", task.RepairRounds))
	}
	if k := strings.TrimSpace(task.ErrorKind); k != "" {
		b.WriteString("失败类别：" + k + "\n")
	}
	if e := strings.TrimSpace(task.Error); e != "" {
		b.WriteString("失败说明：" + truncateRunes(e, 600) + "\n")
	}
	if rep := task.Verify; rep != nil {
		if rep.FailReason != "" {
			b.WriteString("验收结论：" + truncateRunes(rep.FailReason, 800) + "\n")
		}
		for i, oc := range rep.Outcomes {
			if oc.Pass {
				continue
			}
			b.WriteString(fmt.Sprintf("\n命令 #%d: %s\n退出码: %d（期望 %d）\n", i+1, oc.Cmd, oc.Exit, oc.Expect))
			if oc.TimedOut {
				b.WriteString("（超时）\n")
			}
			if s := strings.TrimSpace(oc.Stderr); s != "" {
				b.WriteString("stderr 尾部：\n" + truncateRunes(s, 1200) + "\n")
			} else if s := strings.TrimSpace(oc.Stdout); s != "" {
				b.WriteString("stdout 尾部：\n" + truncateRunes(s, 1200) + "\n")
			}
		}
	}
	return b.String()
}

// shouldReplan 判断这次验收失败是否还值得"重新出方案"。
//
// 判据（顺序即优先级）：
//  1. 方案阶段关了 -> 永不重新规划（兼容模式）；
//  2. 轮数用尽 -> false（防止无限重规划）；
//  3. 退化方案（方案层当时不可用）-> false：没有方案可改，退化直执行下重规划没意义；
//  4. 其余 -> true。
//
// 为什么重新规划要排在返工之后（ReplanRounds 默认 2 < RepairRounds 3）：
//
//	改方案 = 推翻重来，成本高于"同一方案再修一次"。先用完便宜的手段，
//	再动贵的 —— 这条顺序也被 repair.go 的注释与默认值共同约束。
func shouldReplan(task *model.Task, bp *blueprint.Blueprint, cfg config.Config) bool {
	if task == nil || !cfg.Plan.Enabled {
		return false
	}
	rounds := cfg.Plan.ReplanRounds
	if rounds <= 0 {
		return false
	}
	if task.PlanRounds >= rounds {
		return false
	}
	if bp != nil && bp.Degraded {
		return false
	}
	return true
}

package engine

import (
	"fmt"
	"strings"

	"workbench/internal/config"
	"workbench/internal/model"
)

// 返工环（P3 第三部分）：验收失败不等于任务失败。
//
// 设计取舍（额度无限、时间可忽略的既定前提）：
//   - 验收不通过 -> 把具体报错拼成返工 prompt，带着上一版文件内容重新排队；
//   - 最多 RepairRounds 轮（默认 3，可配 5），每轮完整重跑一次模型；
//   - 轮数用尽仍不过 -> 落 StateDegraded（有产出但不计今日产出、不续链）。
//
// 为什么不沿用会话续写：our-free-model 的 headless 通道每次调用是独立进程，
// 续会话需要 --session-id（headless 原生支持），但把上一版全文塞进新 prompt
// 更稳、更可审计（返工 prompt 随任务落盘），也不受会话上下文长度限制。
// 代价是多花 token —— 正是额度无限的用法。

// artifactFile 是返工 prompt 里回填的上一版文件。
type artifactFile struct {
	Path string
	Body string
}

// repairPrompt 构造返工 prompt：原始任务 + 上一版产出 + 验收失败详情。
func repairPrompt(task *model.Task, prevFiles []artifactFile, rep *model.VerifyReport, round int) string {
	var b strings.Builder
	b.WriteString("## 返工说明（第 ")
	b.WriteString(fmt.Sprint(round))
	b.WriteString(" 轮）\n\n")
	b.WriteString("你上一次的产出没有通过工作台的真实验收。请只修正导致失败的问题，\n")
	b.WriteString("然后重新输出完整的产物块（不是 diff，是整个文件）。\n\n")
	b.WriteString("### 原始任务\n")
	b.WriteString(task.Prompt)
	b.WriteString("\n\n### 验收失败的证据\n")
	if rep != nil && strings.TrimSpace(rep.FailReason) != "" {
		b.WriteString("```\n")
		b.WriteString(rep.FailReason)
		b.WriteString("\n```\n")
	}
	if rep != nil {
		for i, oc := range rep.Outcomes {
			if oc.Pass {
				continue
			}
			b.WriteString(fmt.Sprintf("\n命令 #%d: `%s`\n退出码: %d（期望 %d）", i+1, oc.Cmd, oc.Exit, oc.Expect))
			if oc.TimedOut {
				b.WriteString(" — 超时")
			}
			if strings.TrimSpace(oc.Stderr) != "" {
				b.WriteString("\nstderr 尾部:\n```\n" + truncateRunes(oc.Stderr, 1200) + "\n```")
			} else if strings.TrimSpace(oc.Stdout) != "" {
				b.WriteString("\nstdout 尾部:\n```\n" + truncateRunes(oc.Stdout, 1200) + "\n```")
			}
			b.WriteString("\n")
		}
	}
	if len(prevFiles) > 0 {
		b.WriteString("\n### 上一版产出（请在此基础上修正）\n")
		for _, f := range prevFiles {
			b.WriteString("\n```file:" + f.Path + "\n")
			b.WriteString(f.Body)
			b.WriteString("\n```\n")
		}
	}
	b.WriteString("\n### 要求\n")
	b.WriteString("1. 必须能让 verify 里的每条命令真实执行通过（退出码符合 expectExit）。\n")
	b.WriteString("2. 输出格式仍是 ARTIFACTS 协议块（manifest + file: 围栏），围栏必须闭合。\n")
	b.WriteString("3. 不要只解释怎么改，要直接给出可运行的完整文件。\n")
	return b.String()
}

// readPrevArtifacts 读取任务 workspace 里已有的产物文件（用于返工回填）。
func readPrevArtifacts(workDir string, limit int) []artifactFile {
	produced, names, err := collectOutputs(workDir)
	if err != nil {
		return nil
	}
	out := make([]artifactFile, 0, len(names))
	total := 0
	for _, n := range names {
		body := produced[n]
		if len(body) == 0 {
			continue
		}
		out = append(out, artifactFile{Path: n, Body: string(body)})
		total += len(body)
		if limit > 0 && total > limit {
			break
		}
	}
	return out
}

// shouldRepair 判断是否还值得再返工一轮。
func shouldRepair(task *model.Task, rep *model.VerifyReport, cfg config.Config) bool {
	if rep == nil || rep.Pass {
		return false
	}
	rounds := cfg.Executor.RepairRounds
	if rounds <= 0 {
		rounds = 3
	}
	return task.RepairRounds < rounds
}

// truncateRunes 按 rune 截断（避免中文被切碎）。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

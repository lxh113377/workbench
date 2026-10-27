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
	if rep == nil {
		// 无产物块是最常见的「没交」形态（弱车道只吐自家 tool-call 语法）：
		// 这里必须把话说死 —— 不是「验收没过」，而是「压根没交产物」，并给出可照抄的块形状。
		b.WriteString("这次**不是验收没过，而是根本没有产物块**：你的回答里没有 `<<<ARTIFACTS>>>` 段落，\n")
		b.WriteString("工作台因此没有任何文件可落盘、也没有命令可验收。\n\n")
		b.WriteString("请只做一件事：按产物协议重新输出**完整回答**，必须以 `<<<ARTIFACTS>>>` 开头，\n")
		b.WriteString("并在其中给出「文件清单 + 文件内容 + 验收命令」。不要输出你自己的工具调用语法。\n")
	}
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
	// nil 报告 = **根本没有产物块**（materializeArtifacts 只在有块时返回报告）：
	// 这不是「验收过了」，而是「没交东西」——必须允许返工要回产物块（round 46 · T28）。
	// 早先写成 `rep == nil || rep.Pass { return false }`，于是「无产物块」直接跳过返工、
	// 一路记成 done（虚报完成）——审计实测：0 个 artifacts-index / 0 个 workspace，却有 done 记录。
	if rep != nil && rep.Pass {
		return false
	}
	rounds := cfg.Executor.RepairRounds
	if rounds <= 0 {
		rounds = 3
	}
	return task.RepairRounds < rounds
}

// truncateRunes 按 rune 截断到**至多 n 个字符**（含省略号）：绝不切碎中文，也不产出残缺 UTF-8。
//
// 契约修正（2026-10-26 round 197 实测）：旧实现是 string(r[:n]) + 省略号 ⇒ 截断结果恒比预算**多出 1 个字符**
// （n=2 时给 3 个）。调用面全是预算语义（事件 reason 200/160/120、报告里的 stderr/stdout 尾部 1200），
// 越界 1 个不是崩溃、但会让「声称的上界」失真 —— 与《判据保真度》登记的第 ⑫ 修订版同族（口径要说得出数）。
// 现语义与 internal/autonomy 的同名函数逐字一致：n<=1 时不加省略号，否则取前 n-1 个字符 + 省略号。
// 两包各自保留私有副本（跨包收敛为公共件仍是登记项），但**行为契约由两包各自的测试钉住**，
// 漂移会在两侧同时判红（同名不同契约正是本仓在册缺陷族）。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

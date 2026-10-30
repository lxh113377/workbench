package engine

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
	// 必须取原文而不是 task.Prompt（round 136 · T90）：task.Prompt 在上一轮返工时已被换成
	// 返工 prompt，嵌它会让「原始任务」里再套一份完整返工 prompt（含上一版产出全文），
	// 体积按轮数翻倍（实测同一任务 675KB → 1,347KB）。
	b.WriteString(originalPromptOf(task))
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
// originalPromptOf 取任务最初的 prompt：有记录用记录；没记录（字段上线前的旧任务）回退 task.Prompt。
// 回退是刻意的 —— 把「原始任务」留空会让返工 prompt 丢掉任务本身，那比维持旧缺陷更糟。
func originalPromptOf(task *model.Task) string {
	if task == nil {
		return ""
	}
	if s := strings.TrimSpace(task.OriginalPrompt); s != "" {
		return task.OriginalPrompt
	}
	return task.Prompt
}

// readPrevArtifacts 读取上一版产出，**严格受总预算约束**（round 138 · T91）。
//
// 为什么改（现场实测，agent 根 6 份 prompt.repair-*.md）：
//
//	· 一份 689,618 B 的返工 prompt 里，「上一版产出」段占 687,472 B（99.7%），最大单文件 277,803 B；
//	· 旧实现是"先 append→累加→再判 break"：越限那一个文件**整份**入内、其后文件才被丢，
//	  于是这里传的 limit=200_000 名义上 200KB，实际天花板 = limit + 上游单文件闸(8MiB) ≈ 8.19 MiB/轮；
//	· 而且我是"先 append 后判断"，连"这一份要不要收"都是事后才决定 —— 预算从来不是预算。
//
// 现在按预算执行，两步：
//
//	① 先把每个 body 压到**剩余预算**以内（超出部分截断并留显式标记）；
//	② 预算耗尽即停，不再吸收后续文件。
//
// 为什么是截断而不是丢弃：模型要靠这份内容做针对性修正，整份丢掉等于让它盲改；
// 截断 + 标记既保住开头与关键上下文，又把每轮成本钉死。
// 为什么不新造阈值：沿用调用方给的 limit（repair_schedule.go 传 200_000）这一既有预算口径，
// 保证 total ≤ limit 是**可断言的不变量**，而不是又一个需要解释的魔法数字。
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
		if limit > 0 {
			remain := limit - total
			if remain <= 0 {
				break // 预算已用尽：其后文件一律不吸收（旧实现在这里还会整份塞一个）
			}
			if len(body) > remain {
				body = []byte(truncateWithMarker(string(body), remain, n))
			}
		}
		out = append(out, artifactFile{Path: n, Body: string(body)})
		total += len(body)
	}
	return out
}

// truncateWithMarker 把 body 截到 ≤ budget 字节，并在尾部写明"截了多少、原文多大"。
//
// 注意三点（都会在判据里钉住）：
//
//	· 绝不切断 UTF-8 汉字（按 rune 边界回退），否则返工 prompt 里会出现半个字；
//	· 返回长度**必须** ≤ budget —— 标记本身也占预算，所以先算标记再算能放多少正文；
//	· 原文字节数写进标记，让模型与审计都知道"这份是残的"，不会把它当成完整文件去改。
func truncateWithMarker(body string, budget int, name string) string {
	if budget <= 0 {
		return ""
	}
	if len(body) <= budget {
		return body
	}
	marker := fmt.Sprintf("\n\n…[已截断：原文 %d 字节，此处仅保留前 %d 字节；完整文件请按名字自行读取]", len(body), budget)
	// 标记比整份预算还长（极端小预算）⇒ 只留标记，宁可内容全丢也不超预算。
	//
	// ⚠️ 这里必须按**字节**裁，不能借 truncateRunes：那个函数的第二参是 rune 数不是字节数，
	// 传给预算会得出"10 个 rune = 最多 30 字节"，反而撑爆预算。我这个错是
	// TestTruncateWithMarkerNeverExceedsBudget 在 budget=10 处当场抓到的（实得 24 B）。
	if len(marker) >= budget {
		m := marker
		if len(m) > budget {
			head := budget
			for head > 0 && !utf8.RuneStart(m[head]) {
				head--
			}
			m = m[:head]
		}
		return m
	}
	head := budget - len(marker)
	for head > 0 && !utf8.RuneStart(body[head]) {
		head-- // 回退到 rune 边界，避免半个汉字
	}
	return body[:head] + marker
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

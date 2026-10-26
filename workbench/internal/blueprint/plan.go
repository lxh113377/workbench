package blueprint

// 本文件是方案层的「契约」：提示词 + 解析 + 渲染。
//
// 三段式，每一步都能单独测：
//   PlanPrompt   —— 把任务 + 目标 + 上一轮失败证据变成"只出方案"的提示词；
//   Parse        —— 从模型输出里解析出 Blueprint（含降级）；
//   RenderTask   —— 把 Blueprint 渲染成执行模型能直接照做的任务指令。
//
// 为什么方案与执行要分成两次模型调用（而不是一条 prompt 说"先想再做"）：
// 实测（2026-10-21 现场，见 docs/缺陷-运行中但产出为0）：单次调用里让模型"自己先规划"，
// 小模型会把规划写成正文开头的一段废话，然后直接开始写产物 —— 规划没有约束力。
// 只有把方案**固定成一次独立调用的输出**，它才成为可校验、可回灌、可审计的中间件；
// 验收失败时也才能"重新规划"而不是"同一模型再抄一遍"。

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PlanPrompt 构造方案模型（思考模型）的提示词。
//
// 参数：
//
//	goalIntent  —— 项目级目标意图（可空）：让方案对得上长期目标，而不是只对一句话任务负责；
//	taskTitle   —— 任务标题；
//	taskPrompt  —— 任务原始描述；
//	minWords    —— 期望产出体量（透传，用于让方案选对交付物形式）；
//	lastFailure —— 上一轮验收失败的证据（空串 = 首轮）。这是"闭环"的关键输入：
//	               重新规划必须建立在"上一版为什么没过"之上，否则就是重跑同一个错。
func PlanPrompt(goalIntent, taskTitle, taskPrompt string, minWords int, lastFailure string) string {
	var b strings.Builder
	b.WriteString("你是方案设计者。你的唯一职责是**产出方案**，不要写实现代码，不要输出产物文件。")
	b.WriteString("\n执行由另一个模型负责，它只能看到你写的方案，看不到我这条消息。")
	b.WriteString("\n\n## 硬性要求\n")
	b.WriteString("1. 只输出一个 JSON 对象，不要任何解释文字、不要 Markdown 围栏。\n")
	b.WriteString("2. 交付物必须是**可被真实验收**的文件（相对路径），不要写无法落盘的抽象描述。\n")
	b.WriteString("3. 验收命令必须本机真能执行（解释器限 python/node/pwsh/go），并用 expectExit 写明期望退出码。\n")
	b.WriteString("4. 步骤最多 ")
	b.WriteString(fmt.Sprint(MaxSteps))
	b.WriteString(" 条，每条都要有可判定的完成标准。\n")
	b.WriteString("5. 禁止写【省略】【其余同上】这类占位。\n")
	// 第 6 条由 round 31 的定向实验导出：方案阶段实测**同样**频繁出现自有工具调用语法
	// （`<|tool_call_start|>[bash(command='ls -la', timeout=30)]`、`<dots_function_call>…`），
	// 结果反复落成「方案 JSON 解析失败」，任务被迫退化直执行。
	// 工作台不执行这些语法，也不会把它们当作方案 —— 必须在提示词里说清楚。
	b.WriteString("6. 不要输出你自己的工具调用语法（实测出现过 `<|tool_call_start|>[bash(...)]`、`<dots_function_call>…`）：\n")
	b.WriteString("   工作台**不会执行**它们，方案只会被判为不可用。方案只能是一个 JSON 对象（见下方 schema）。\n")

	b.WriteString("\n## 输出 schema\n")
	b.WriteString("{\n")
	b.WriteString("  \"approach\": \"总体思路，一两句话，说清为什么这么做\",\n")
	b.WriteString("  \"deliverables\": [{\"path\": \"相对路径\", \"what\": \"这个文件要包含什么\"}],\n")
	b.WriteString("  \"steps\": [{\"order\": 1, \"do\": \"具体做什么\", \"done\": \"怎么判定做完了\"}],\n")
	b.WriteString("  \"verify\": [{\"cmd\": \"python x.py\", \"expectExit\": 0, \"why\": \"证明什么\"}],\n")
	b.WriteString("  \"risks\": [\"已知风险与规避方式\"]\n")
	b.WriteString("}\n")

	b.WriteString("\n## 本次任务\n")
	b.WriteString("标题：")
	b.WriteString(strings.TrimSpace(taskTitle))
	b.WriteString("\n")
	if s := strings.TrimSpace(goalIntent); s != "" {
		b.WriteString("项目目标（方案必须服务于它）：")
		b.WriteString(s)
		b.WriteString("\n")
	}
	if minWords > 0 {
		b.WriteString("产出体量参考：约 ")
		b.WriteString(fmt.Sprint(minWords))
		b.WriteString(" 字（用于决定交付物规模，不是硬性字数）\n")
	}
	b.WriteString("任务描述：\n")
	b.WriteString(strings.TrimSpace(taskPrompt))
	b.WriteString("\n")

	if s := strings.TrimSpace(lastFailure); s != "" {
		b.WriteString("\n## 上一轮失败证据（本次是重新规划，方案必须解决它）\n")
		b.WriteString("你上一版方案交出去执行后没有通过验收。以下是工作台真实执行得到的失败证据：\n")
		b.WriteString("-----\n")
		b.WriteString(truncate(s, 3000))
		b.WriteString("\n-----\n")
		b.WriteString("要求：\n")
		b.WriteString("1. 先判断失败是**方案层面的错**（交付物选错、验收命令不可执行、步骤缺关键一环）")
		b.WriteString("还是**实现层面的错**（方案对但执行没做到）。\n")
		b.WriteString("2. 若属方案层面：直接改方案，不要重复上一版。\n")
		b.WriteString("3. 若属实现层面：在步骤里明确写出执行模型必须怎么做才能过，并在 risks 里点明这个坑。\n")
	}
	return b.String()
}

// Parse 从方案模型的输出里解析 Blueprint。
//
// 降级（每级都记录问题，不静默接受）：
//  1. 严格：整段就是 JSON；
//  2. 截取：从文本里截出第一个平衡的 JSON 对象（模型爱在前后加寒暄）；
//  3. 兜底：解析不出来时返回 nil + 原因 —— 由调用方决定是重试还是退化直执行。
//
// 为什么要"截取"而不是只做严格解析：免费车道的小模型几乎不会只输出 JSON，
// 但它们的 JSON 本体通常是完整的；只做严格解析等于放弃这一整条车道。
func Parse(text string) (*Blueprint, []string, error) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return nil, nil, fmt.Errorf("方案模型输出为空")
	}
	var problems []string

	tryParse := func(s string) (*Blueprint, error) {
		var bp Blueprint
		if err := json.Unmarshal([]byte(s), &bp); err != nil {
			return nil, err
		}
		return &bp, nil
	}

	bp, err := tryParse(raw)
	if err != nil {
		if cut := extractJSONObject(raw); cut != "" && cut != raw {
			if bp2, err2 := tryParse(cut); err2 == nil {
				bp = bp2
				problems = append(problems, "方案输出含额外文字，已截取其中 JSON")
			}
		}
	}
	if bp == nil {
		return nil, problems, fmt.Errorf("方案 JSON 解析失败（原始输出前 200 字：%s）", truncate(raw, 200))
	}

	problems = append(problems, bp.Normalize()...)
	bp.Raw = text

	if bp.IsZero() {
		return nil, problems, fmt.Errorf("方案为空（没有交付物、步骤与验收命令）")
	}
	return bp, problems, nil
}

// extractJSONObject 从任意文本中截出第一个平衡的 JSON 对象。
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			if ch == 0x5c {
				escaped = true
				continue
			}
			if ch == 0x22 {
				inStr = false
			}
			continue
		}
		switch ch {
		case 0x22:
			inStr = true
		case 0x7b:
			depth++
		case 0x7d:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// RenderTask 把方案渲染成执行模型的任务指令。
//
// 这是「方案 → 执行」的交接面：执行模型拿到的不再是一句话任务，而是
// 「做什么 / 交付什么 / 怎么算做完 / 验收命令是什么」四段硬约束。
//
// 为什么保留原始任务描述（而不是只给方案）：执行模型需要知道**原始意图**，
// 方案是中介物，不是替代品 —— 方案写歪了，原始任务还在，验收命令会把它抓回来。
func RenderTask(bp *Blueprint, taskTitle, originalPrompt string) string {
	if bp == nil {
		return originalPrompt
	}
	var b strings.Builder
	b.WriteString("## 执行指令（由方案模型产出，你负责落地执行）\n\n")
	b.WriteString("你的任务不是复述方案，而是**按方案把文件真正做出来**，并让验收命令真实通过。\n\n")
	b.WriteString("### 任务\n")
	b.WriteString(strings.TrimSpace(taskTitle))
	b.WriteString("\n\n### 原始需求\n")
	b.WriteString(strings.TrimSpace(originalPrompt))
	b.WriteString("\n\n### 总体思路\n")
	b.WriteString(strings.TrimSpace(bp.Approach))
	b.WriteString("\n")

	if len(bp.Deliverables) > 0 {
		b.WriteString("\n### 必须交付的文件\n")
		for _, d := range bp.Deliverables {
			b.WriteString("- ")
			b.WriteString(d.Path)
			if strings.TrimSpace(d.What) != "" {
				b.WriteString("：")
				b.WriteString(strings.TrimSpace(d.What))
			}
			b.WriteString("\n")
		}
	}

	if len(bp.Steps) > 0 {
		b.WriteString("\n### 执行步骤\n")
		for i, s := range bp.Steps {
			b.WriteString(fmt.Sprintf("%d. %s", i+1, s.Do))
			if strings.TrimSpace(s.Done) != "" {
				b.WriteString("（完成判据：")
				b.WriteString(strings.TrimSpace(s.Done))
				b.WriteString("）")
			}
			b.WriteString("\n")
		}
	}

	if len(bp.Verify) > 0 {
		b.WriteString("\n### 验收命令（必须真实跑通，写进产物块 manifest 的 verify）\n")
		for _, v := range bp.Verify {
			b.WriteString("- ")
			b.WriteString(v.Cmd)
			b.WriteString(" expectExit=")
			b.WriteString(fmt.Sprint(v.ExpectExit))
			if strings.TrimSpace(v.Why) != "" {
				b.WriteString("（")
				b.WriteString(strings.TrimSpace(v.Why))
				b.WriteString("）")
			}
			b.WriteString("\n")
		}
	}

	if len(bp.Risks) > 0 {
		b.WriteString("\n### 已知风险（别踩）\n")
		for _, r := range bp.Risks {
			if strings.TrimSpace(r) == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(strings.TrimSpace(r))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n### 交付方式\n")
	b.WriteString("按产出协议输出产物块：文件内容必须完整可运行、围栏必须闭合；")
	b.WriteString("verify 至少一条命令，且优先使用方案里给出的验收命令。\n")
	return b.String()
}

// SummaryLine 返回方案的一行人读摘要（面板与日志用）。
func (b *Blueprint) SummaryLine() string {
	if b == nil {
		return "无方案"
	}
	status := "模型方案"
	if b.Degraded {
		status = "规则兜底"
	}
	return fmt.Sprintf("%s｜思路：%s｜交付 %d｜步骤 %d｜验收 %d｜轮次 %d",
		status, truncate(strings.TrimSpace(b.Approach), 40),
		len(b.Deliverables), len(b.Steps), len(b.Verify), b.Round)
}

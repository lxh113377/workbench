// Command fakedsh 是「确定性 DSH 替身」，仅用于容器内端到端验收。
//
// 关键设计：工作台除了执行任务，还会用同一个执行器跑三次**语义子任务**
// （价值判分 judge.JudgeBatch / 自评 judge.Critique / 摘要 judge.Summarize）。
// 如果替身对所有提示词都回同一段产物块，自评解析出的 quality 恒为 0，
// 阶段会被判低质并全部回退（容器实测：连续 5 个目标、每阶段 rollback，no accept）。
// 因此替身必须**按提示词分流**：
//
//	含「成果评审员」  -> 回 1 个 JSON 评审对象（quality 等）
//	含「任务价值评估器」-> 回 JSON 数组
//	含「250 字总结」   -> 回纯文本摘要
//	其余（真正的任务）-> 回带产物协议的 Markdown（文件 + verify 命令）
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func article() string {
	var b strings.Builder
	b.WriteString("# 容器内闭环验收产出\n\n")
	b.WriteString("## 摘要\n")
	b.WriteString("本文用于在隔离容器中验证工作台的自主闭环：任务由项目目标推导得出，")
	b.WriteString("产出以真实文件落盘，验收命令由工作台真实执行，通过后计入阶段进度。\n\n")
	b.WriteString("## 要点\n")
	for i := 0; i < 8; i++ {
		b.WriteString("- 要点 " + fmt.Sprint(i+1) + "：闭环的每一步都可机器判定，未通过的内容不会计入目标进度。\n")
	}
	b.WriteString("\n## 建议\n")
	b.WriteString("把验收命令固定在确定性解释器上（本容器内为 python3），避免依赖宿主环境差异。\n\n")
	b.WriteString("<<<ARTIFACTS>>>\n")
	b.WriteString("```manifest\n")
	b.WriteString("{\"files\":[{\"path\":\"result.py\",\"lang\":\"python\"},{\"path\":\"result.md\"}],")
	b.WriteString("\"verify\":[{\"cmd\":\"python3 result.py\",\"expectExit\":0,\"timeout\":30}]}\n")
	b.WriteString("```\n")
	b.WriteString("```file:result.py\n")
	b.WriteString("print(\"hello from workbench container e2e\")\n")
	b.WriteString("```\n")
	b.WriteString("```file:result.md\n")
	b.WriteString("# 产出物\n\n该文件由容器内闭环验收生成。\n\n")
	for i := 0; i < 10; i++ {
		b.WriteString("占位正文第 " + fmt.Sprint(i+1) + " 段，用于满足目标级字数判据。\n")
	}
	b.WriteString("```\n")
	b.WriteString("<<<END>>>\n")
	return b.String()
}

// critiqueJSON 是自评的确定性回答（judge.parseCritique 期望恰好这些字段）。
func critiqueJSON() string {
	return `{"quality":9,"tags":["闭环","容器验收","确定性"],` +
		`"strengths":["步骤可机器判定","产物真实落盘并通过 python3 验收"],` +
		`"gaps":["缺少对超长正文的边界测试"],` +
		`"nextIdeas":[{"title":"容器闭环验收 · 边界用例补强","prompt":"请产出边界用例清单并说明每条的预期结果，包含结论与建议。","domain":"knowledge"}]}`
}

// judgeArray 是价值判分的确定性回答。
func judgeArray() string {
	return `[{"id":"candidate","value":90,"feasibility":95,"reason":"容器内可确定性完成"}]`
}

func main() {
	raw, _ := io.ReadAll(bufio.NewReader(os.Stdin))
	prompt := string(raw)
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{"type": "session", "sessionId": "docker-fake-session"})
	_ = enc.Encode(map[string]any{"type": "status", "phase": "step_end",
		"usage": map[string]any{"inputTokens": 120, "outputTokens": 480, "totalTokens": 600}})
	var text string
	switch {
	case strings.Contains(prompt, "任务价值评估器"):
		text = judgeArray()
	case strings.Contains(prompt, "成果评审员"):
		text = critiqueJSON()
	case strings.Contains(prompt, "总结"):
		text = "容器内闭环验收：任务由目标推导，产物落盘并通过 python3 真实验收，阶段可计入目标进度。"
	default:
		text = article()
	}
	_ = enc.Encode(map[string]any{"type": "final", "text": text})
	_ = time.Now()
}

// Command fakedsh-failfirst 是「首个模型必失败」的确定性 DSH 替身。
//
// 用途：为「真实故障转移 + 无感切换」产出**运行时证据**（总验收 R4R5 项）。
// 判据要求 model-status.jsonl 里同时存在 probe 与 switch 记录，而 switch 只在
// 「一次真实执行失败并换模型」时才产生 —— 真实通道受上游 IP 封禁时无法制造。
//
// 做法：模型 id 由工作台写在 --patch overlay 里，替身从该文件读出 model 字段；
// 若 model 等于 FIRST_MODEL（环境变量指定），返回上游失败；否则回正常产物。
// 这样工作台会先在该模型上失败（产生 failure 记录），再被 FailoverRunner
// 切到下一个模型成功（产生 switch 记录）—— 全程真实执行、真实切换。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

func article() string {
	var b strings.Builder
	b.WriteString("# 故障转移验证产出\n\n")
	b.WriteString("## 摘要\n")
	b.WriteString("本文用于在隔离实例中验证工作台的故障转移：首个模型失败后自动切换，产出仍真实落盘。\n\n")
	b.WriteString("## 要点\n")
	for i := 0; i < 8; i++ {
		b.WriteString("- 要点 " + fmt.Sprint(i+1) + "：切换对上层不可见，最终产出与单模型场景一致。\n")
	}
	b.WriteString("\n## 建议\n")
	b.WriteString("把失败注入固定在模型名上，避免依赖宿主环境差异。\n\n")
	b.WriteString("<<<ARTIFACTS>>>\n")
	b.WriteString("```manifest\n")
	b.WriteString("{\"files\":[{\"path\":\"result.py\",\"lang\":\"python\"},{\"path\":\"result.md\"}],")
	b.WriteString("\"verify\":[{\"cmd\":\"python result.py\",\"expectExit\":0,\"timeout\":30}]}\n")
	b.WriteString("```\n")
	b.WriteString("```file:result.py\n")
	b.WriteString("print(\"hello from workbench failover e2e\")\n")
	b.WriteString("```\n")
	b.WriteString("```file:result.md\n")
	b.WriteString("# 产出物\n\n该文件由故障转移后的模型生成。\n\n")
	for i := 0; i < 10; i++ {
		b.WriteString("占位正文第 " + fmt.Sprint(i+1) + " 段，用于满足字数判据。\n")
	}
	b.WriteString("```\n")
	b.WriteString("<<<END>>>\n")
	return b.String()
}

func critiqueJSON() string {
	return `{"quality":9,"tags":["故障转移","切换"],` +
		`"strengths":["首个模型失败后自动切换","产物真实落盘并通过验收"],` +
		`"gaps":[],"nextIdeas":[]}`
}

func judgeArray() string {
	return `[{"id":"candidate","value":90,"feasibility":95,"reason":"隔离实例内可确定性完成"}]`
}

// modelFromPatch 从 --patch 指向的 overlay YAML 里读出 model 字段。
func modelFromPatch(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "--patch" && i+1 < len(args) {
			data, err := os.ReadFile(args[i+1])
			if err != nil {
				return ""
			}
			for _, line := range strings.Split(string(data), "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "model:") {
					return strings.TrimSpace(strings.TrimPrefix(t, "model:"))
				}
			}
		}
	}
	return ""
}

func main() {
	raw, _ := io.ReadAll(bufio.NewReader(os.Stdin))
	prompt := string(raw)
	model := modelFromPatch(os.Args[1:])
	first := os.Getenv("FAIL_FIRST_MODEL")

	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{"type": "session", "sessionId": "failover-fake-session"})

	// 只对**真正的任务**注入失败：语义子任务（判分/自评/摘要）始终正常，
	// 否则自评也会失败，阶段会被判低质回退，掩盖故障转移本身。
	isTask := !strings.Contains(prompt, "任务价值评估器") &&
		!strings.Contains(prompt, "成果评审员") && !strings.Contains(prompt, "总结")
	if isTask && first != "" && model == first {
		_ = enc.Encode(map[string]any{"type": "error", "message": "upstream_failed: injected failure for failover e2e", "kind": "upstream_failed"})
		_ = enc.Encode(map[string]any{"type": "final", "text": ""})
		fmt.Fprintln(os.Stderr, "dsh: upstream_failed: injected failure for failover e2e")
		os.Exit(1)
	}

	_ = enc.Encode(map[string]any{"type": "status", "phase": "step_end",
		"usage": map[string]any{"inputTokens": 120, "outputTokens": 480, "totalTokens": 600}})
	var text string
	switch {
	case strings.Contains(prompt, "任务价值评估器"):
		text = judgeArray()
	case strings.Contains(prompt, "成果评审员"):
		text = critiqueJSON()
	case strings.Contains(prompt, "总结"):
		text = "隔离实例内故障转移验证：首个模型注入失败，切换后产出仍真实落盘。"
	default:
		text = article()
	}
	_ = enc.Encode(map[string]any{"type": "final", "text": text})
}

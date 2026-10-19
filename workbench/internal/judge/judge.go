// Package judge 实现“用模型评估模型”的轻量语义层：
//   - JudgeBatch：对一批候选任务做价值/可行性语义判分（严格 JSON 输出）
//   - Critique：对已完成产出做 1-10 分自评并给出后继想法
//   - Summarize：从产出正文抽一份简短摘要供后继任务注入上下文
//
// 全部失败均 fail-open：语义层不可用时退回纯规则/跳过，不阻塞产出。
package judge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workbench/internal/config"
	"workbench/internal/executor"
	"workbench/internal/model"
)

// Client 是语义判分器。
type Client struct {
	layout config.Layout
	exec   config.ExecutorConfig
}

// New 构造判分器。
func New(layout config.Layout, execCfg config.ExecutorConfig) *Client {
	return &Client{layout: layout, exec: execCfg}
}

// UpdateConfig 热更新执行配置。
func (c *Client) UpdateConfig(execCfg config.ExecutorConfig) { c.exec = execCfg }

type candidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Task  string `json:"task"`
}

// BatchResult 是一条判分结果。
type BatchResult struct {
	ID          string  `json:"id"`
	Value       float64 `json:"value"`
	Feasibility float64 `json:"feasibility"`
	Reason      string  `json:"reason"`
}

// JudgeBatch 对候选任务批量判分。返回 id 到结果。失败返回 error 由调用方降级。
func (c *Client) JudgeBatch(ctx context.Context, tasks []*model.Task) (map[string]BatchResult, error) {
	if len(tasks) == 0 {
		return map[string]BatchResult{}, nil
	}
	if len(tasks) > 10 {
		tasks = tasks[:10]
	}
	cands := make([]candidate, 0, len(tasks))
	for _, t := range tasks {
		cands = append(cands, candidate{ID: t.ID, Title: t.Title, Task: model.Truncate(t.Prompt, 600)})
	}
	payload, _ := json.Marshal(cands)
	prompt := "你是任务价值评估器。下面是一批候选任务（JSON 数组）。请为每条任务从两个维度打分（0-100 整数）：\n" +
		"- value：这条任务产出的成果对个人知识资产与实际工作的真实价值。重复、空泛、无法验证的给低分。\n" +
		"- feasibility：在单机自动化环境（无外部账号、无人工介入）下能真正完成的可能性。\n" +
		"只输出一个 JSON 数组，每个元素形如 {`id`:`x`,`value`:85,`feasibility`:90,`reason`:`不超过30字`}。不要输出任何其他文字。\n\n候选任务：\n" + string(payload)

	res, err := c.run(ctx, "judge-"+fmt.Sprint(model.Now()), prompt, 3)
	if err != nil {
		return nil, err
	}
	return parseJudgeOutput(res)
}

// Score 是自评结果。
type Score struct {
	Quality   int      `json:"quality"`
	Tags      []string `json:"tags"`
	Strengths []string `json:"strengths"`
	Gaps      []string `json:"gaps"`
	NextIdeas []idea   `json:"nextIdeas"`
}

type idea struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	TemplateID string `json:"templateId"`
	Domain     string `json:"domain"`
}

// Critique 对产出做自评。
func (c *Client) Critique(ctx context.Context, task *model.Task, content string, minWords int) (*model.Critique, error) {
	body := model.Truncate(content, 8000)
	prompt := "你是严格的成果评审员。请评审下面这份产出，并只输出一个 JSON 对象（不要任何其他文字）：\n" +
		"{`quality`: <1-10 整数>, `tags`: [<3-6 个中文标签>], `strengths`: [<最多3条>], `gaps`: [<最多3条具体不足>], `nextIdeas`: [{`title`: <标题>, `prompt`: <可直接执行的任务描述>, `domain`: <content|code|knowledge|data|solution|report>}]}\n\n" +
		"评分标准：结构完整性、信息密度、可执行性、是否有独立判断。低于 " + fmt.Sprint(minWords) + " 字视为严重不足。" +
		"nextIdeas 给 1-2 条与本次成果有逻辑递进关系的后继任务（不要重复本次任务）。\n\n任务标题：" + task.Title + "\n产出正文：\n" + body
	res, err := c.run(ctx, "critique-"+task.ID, prompt, 5)
	if err != nil {
		return nil, err
	}
	return parseCritique(res)
}

// Summarize 从产出正文生成 250 字内摘要。
func (c *Client) Summarize(ctx context.Context, task *model.Task, content string) (string, error) {
	body := model.Truncate(content, 8000)
	prompt := "请用不超过 250 字总结下面这份成果的核心结论、关键论据与可执行建议。只输出摘要正文，不要任何前缀或解释。\n\n标题：" + task.Title + "\n\n正文：\n" + body
	res, err := c.run(ctx, "summary-"+task.ID, prompt, 3)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(res.FinalText)
	if text == "" {
		return "", fmt.Errorf("摘要为空")
	}
	return model.Truncate(text, 600), nil
}

// run 走一次短任务执行（不带工具，只要文本）。
func (c *Client) run(ctx context.Context, tag, prompt string, timeoutMin int) (*executor.Result, error) {
	cfg := c.exec
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = timeoutMin
	}
	runner := executor.New(cfg, c.layout)
	task := &model.Task{ID: tag, Title: tag, Prompt: prompt, TimeoutMin: timeoutMin}
	res, err := runner.Run(ctx, task, "")
	if err != nil {
		return nil, err
	}
	if res.FinalText == "" {
		return nil, fmt.Errorf("语义层无输出")
	}
	return res, nil
}

func parseJudgeOutput(res *executor.Result) (map[string]BatchResult, error) {
	text := extractJSON(res.FinalText)
	if text == "" {
		return nil, fmt.Errorf("判分输出中未找到 JSON")
	}
	var list []BatchResult
	if err := json.Unmarshal([]byte(text), &list); err != nil {
		return nil, fmt.Errorf("判分 JSON 解析失败: %w", err)
	}
	out := make(map[string]BatchResult, len(list))
	for _, r := range list {
		if r.ID == "" {
			continue
		}
		out[r.ID] = r
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("判分结果为空")
	}
	return out, nil
}

func parseCritique(res *executor.Result) (*model.Critique, error) {
	text := extractJSON(res.FinalText)
	if text == "" {
		return nil, fmt.Errorf("自评输出中未找到 JSON")
	}
	var s Score
	if err := json.Unmarshal([]byte(text), &s); err != nil {
		return nil, fmt.Errorf("自评 JSON 解析失败: %w", err)
	}
	if s.Quality < 0 {
		s.Quality = 0
	}
	if s.Quality > 10 {
		s.Quality = 10
	}
	cq := &model.Critique{Quality: s.Quality, Tags: s.Tags, Strengths: s.Strengths, Gaps: s.Gaps, At: model.Now()}
	for _, id := range s.NextIdeas {
		if strings.TrimSpace(id.Title) == "" || strings.TrimSpace(id.Prompt) == "" {
			continue
		}
		cq.NextIdeas = append(cq.NextIdeas, model.Idea{Title: id.Title, Prompt: id.Prompt, TemplateID: id.TemplateID, Domain: id.Domain})
		if len(cq.NextIdeas) >= 2 {
			break
		}
	}
	return cq, nil
}

// extractJSON 从任意文本中截出第一个平衡的 JSON 对象或数组。
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "``"); i >= 0 {
		rest := s[i+3:]
		rest = strings.TrimPrefix(rest, "json")
		if j := strings.Index(rest, "``"); j >= 0 {
			s = strings.TrimSpace(rest[:j])
		}
	}
	start := -1
	var open, close byte
	for i := 0; i < len(s); i++ {
		if s[i] == '[' || s[i] == '{' {
			start = i
			open = s[i]
			if open == '[' {
				close = ']'
			} else {
				close = '}'
			}
			break
		}
	}
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
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// Package chain 实现任务发展方向引导：从已完成任务的上下文、产出标签与历史偏好
// 自动生成具备逻辑连贯性的后继任务，形成闭环迭代。
package chain

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"workbench/internal/config"
	"workbench/internal/model"
	"workbench/internal/templates"
)

// Generator 负责生成新任务与后继任务。
type Generator struct {
	reg  *templates.Registry
	cfg  config.ChainConfig
	pick *randPicker
}

// New 构造生成器。
func New(reg *templates.Registry, cfg config.ChainConfig) *Generator {
	return &Generator{reg: reg, cfg: cfg, pick: newRandPicker()}
}

// UpdateConfig 热更新链配置。
func (g *Generator) UpdateConfig(cfg config.ChainConfig) { g.cfg = cfg }

// Topic 是选题池：来源为领域关键词，保证生成的任务有真实方向而非空转。
var topicsByDomain = map[string][]string{
	"content":   {"AI 时代的个人效率", "远程协作的隐性成本", "如何写出让人读完的文章", "信息过载下的注意力管理", "普通人如何用 AI 提效"},
	"code":      {"日志分析与异常摘要工具", "批量文件重命名与校验", "目录体积审计与清理建议", "结构化数据转换与校验", "本地 API 压测脚本"},
	"knowledge": {"分布式系统入门", "数据建模基础", "产品需求分析方法", "提示词工程要点", "软件测试体系"},
	"data":      {"用户行为漏斗分析", "留存与流失归因", "成本结构拆解", "指标异常检测", "A/B 实验设计"},
	"solution":  {"个人知识库建设方案", "自动化产出流水线设计", "团队协作流程改造", "数据备份与恢复方案", "监控与告警体系设计"},
	"report":    {"季度效率复盘", "同类工具横评", "技术选型调研", "流程瓶颈分析", "投入产出测算"},
	// quality 领域 2026-10-26 补：此前**缺键** ⇒ pickTopic 静默回落 content 选题，
	// 「工程质量体检 / 五轴审查报告 / 判据规格设计 / 判据有效性验证 / 工程卫生清理」5 条模板
	// 的主题变成「AI 时代的个人效率」这类内容选题（实测样本「普通人如何用 AI 提效」，
	// 见 internal/chain/topic_pool_contract_test.go 的修前 FAIL 输出）。
	// 选题口径：必须指回**当前工作目录（工作空间）的项目**，而不是泛化主题 ——
	// 否则任务产出与「被选中文件夹」无关（目标口径见 docs/工作空间聚焦与模板域覆盖-五轴审查-2026-10-26.md）。
	"quality": {
		"当前工作目录下的项目（工作空间）",
		"工作空间项目最近一次改动",
		"工作空间项目的构建与测试链路",
		"工作空间项目的错误处理与静默失败面",
		"工作空间项目的判据与门禁覆盖面",
	},
}

// RootTask 从模板池生成一条种子任务。
func (g *Generator) RootTask(tpl model.Template, now int64) *model.Task {
	topic := g.pickTopic(tpl.Domain, "")
	prompt := templates.Render(tpl.PromptTemplate, templates.RenderVars{
		Topic:   topic,
		Words:   fmt.Sprintf("%d", tpl.OutputSpec.MinWords),
		Context: "（首轮任务，无前序上下文）",
		Prev:    "（首轮任务，无前序摘要）",
		Date:    time.Now().Format("2006-01-02"),
	})
	id := newID("task")
	return &model.Task{
		ID:           id,
		ChainID:      newID("chain"),
		TemplateID:   tpl.ID,
		Title:        fmt.Sprintf("%s · %s", tpl.Name, topic),
		Prompt:       prompt,
		Domain:       tpl.Domain,
		Tags:         append([]string{}, tpl.Params.Tags...),
		Priority:     tpl.PriorityWeight,
		State:        model.StateCandidate,
		Source:       "template",
		MaxAttempts:  3,
		OutputFormat: tpl.OutputSpec.Format,
		TimeoutMin:   tpl.Params.MaxMinutes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// Successor 依据父任务的产出与自评生成后继任务。
//   - tpl: 后继使用的模板（来自链配置的 successorTemplates 或自评给出的 idea）
//   - parent: 父任务（提供上下文与链信息）
//   - critique: 父任务自评（提供 nextIdeas）
func (g *Generator) Successor(tpl model.Template, parent *model.Task, critique *model.Critique, depth int, now int64) *model.Task {
	context := buildContext(parent, critique, g.cfg.ContextChars)
	topic := g.deriveTopic(parent, tpl.Domain)
	// Prev 的兜底必须与 Context 对称：buildContext / deriveTopic 都对 parent == nil 有守卫，
	// 而原先这行直接取 parent.Summary —— nil 父任务会 panic（探针实测复现，不写行号：改一处就会失效）。
	// 空摘要同样要给文案：模型看到空洞时分不清「真无前序」与「数据丢了」，会自行编造上下文。
	prev := "（无前序摘要）"
	if parent != nil && parent.Summary != "" {
		prev = model.Truncate(parent.Summary, 400)
	}
	prompt := templates.Render(tpl.PromptTemplate, templates.RenderVars{
		Topic:   topic,
		Words:   fmt.Sprintf("%d", tpl.OutputSpec.MinWords),
		Context: context,
		Date:    time.Now().Format("2006-01-02"),
		Prev:    prev,
	})
	id := newID("task")
	return &model.Task{
		ID:           id,
		ChainID:      parent.ChainID,
		SessionID:    parent.SessionID, // 同链沿用会话，形成上下文递进
		TemplateID:   tpl.ID,
		Title:        fmt.Sprintf("%s · 基于《%s》", tpl.Name, model.Truncate(parent.Title, 40)),
		Prompt:       prompt,
		Domain:       tpl.Domain,
		Tags:         append([]string{}, tpl.Params.Tags...),
		Priority:     tpl.PriorityWeight,
		State:        model.StateCandidate,
		Source:       "chain",
		ParentID:     parent.ID,
		ChainDepth:   depth,
		MaxAttempts:  3,
		OutputFormat: tpl.OutputSpec.Format,
		TimeoutMin:   tpl.Params.MaxMinutes,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// SynthesisTask 依据自评的 nextIdeas 合成一条全新任务（无对应模板时使用）。
func (g *Generator) SynthesisTask(idea model.Idea, parent *model.Task, depth int, now int64) *model.Task {
	domain := idea.Domain
	if domain == "" {
		domain = parent.Domain
	}
	if domain == "" {
		domain = "content"
	}
	prompt := idea.Prompt
	if !strings.Contains(prompt, "输出") && !strings.Contains(prompt, "产出") {
		prompt += "\n\n要求：在当前工作目录产出 Markdown 成果文件，内容必须具体可执行，不要空泛论述；完成后在回答中指出文件路径。"
	}
	if len(parent.Summary) > 0 {
		prompt += "\n\n前序成果摘要：\n" + model.Truncate(parent.Summary, g.cfg.ContextChars/2)
	}
	id := newID("task")
	return &model.Task{
		ID:           id,
		ChainID:      parent.ChainID,
		SessionID:    parent.SessionID,
		TemplateID:   "",
		Title:        model.Truncate(idea.Title, 80),
		Prompt:       prompt,
		Domain:       domain,
		Tags:         []string{"合成"},
		Priority:     3,
		State:        model.StateCandidate,
		Source:       "synthesis",
		ParentID:     parent.ID,
		ChainDepth:   depth,
		MaxAttempts:  3,
		OutputFormat: "md",
		TimeoutMin:   30,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

// ShouldExtend 判断链是否应当继续延伸。
func (g *Generator) ShouldExtend(parent *model.Task, critique *model.Critique) bool {
	if !g.cfg.Enabled || parent == nil {
		return false
	}
	if parent.ChainDepth+1 >= g.cfg.MaxDepth {
		return false
	}
	if critique != nil {
		// 连续低质不再延伸
		if critique.Quality > 0 && critique.Quality < g.cfg.MinQuality {
			return false
		}
	}
	return true
}

// SuccessorTemplates 返回父任务对应的后继模板列表。
func (g *Generator) SuccessorTemplates(parent *model.Task) []model.Template {
	if parent == nil {
		return nil
	}
	cur, ok := g.reg.Get(parent.TemplateID)
	if !ok {
		return nil
	}
	out := make([]model.Template, 0, len(cur.Chain.SuccessorTemplates))
	for _, id := range cur.Chain.SuccessorTemplates {
		if t, exists := g.reg.Get(id); exists && t.Enabled {
			out = append(out, t)
		}
	}
	return out
}

// Template 按 ID 取模板。
func (g *Generator) Template(id string) (model.Template, bool) { return g.reg.Get(id) }

// EnabledTemplates 返回启用中的模板。
func (g *Generator) EnabledTemplates() []model.Template { return g.reg.Enabled() }

// pickTopic 从领域选题池中选一个主题；exclude 命中则跳过。
func (g *Generator) pickTopic(domain, exclude string) string {
	pool, ok := topicsByDomain[domain]
	if !ok || len(pool) == 0 {
		pool = topicsByDomain["content"]
	}
	for i := 0; i < 6; i++ {
		cand := pool[g.pick.Intn(len(pool))]
		if cand != exclude {
			return cand
		}
	}
	return pool[0]
}

// deriveTopic 让后继任务的主题与前序保持逻辑连贯（延续或深化）。
func (g *Generator) deriveTopic(parent *model.Task, domain string) string {
	if parent == nil {
		return g.pickTopic(domain, "")
	}
	// 优先从父标题里提取主题段（格式：模板名 · 主题）
	title := parent.Title
	if i := strings.Index(title, "·"); i >= 0 && i+len("·") < len(title) {
		t := strings.TrimSpace(title[i+len("·"):])
		if t != "" && !strings.HasPrefix(t, "基于") {
			return model.Truncate(t, 60)
		}
	}
	// 其次用父任务标签/领域选题
	return g.pickTopic(domain, "")
}

// buildContext 拼装注入后继任务的上下文：父任务产出的摘要 + 自评标签与建议。
func buildContext(parent *model.Task, critique *model.Critique, maxChars int) string {
	if parent == nil {
		return ""
	}
	if maxChars <= 0 {
		maxChars = 2000
	}
	var b strings.Builder
	b.WriteString("前序任务：")
	b.WriteString(parent.Title)
	b.WriteString("\n")
	if parent.Summary != "" {
		b.WriteString("前序结论：\n")
		b.WriteString(model.Truncate(parent.Summary, maxChars/2))
		b.WriteString("\n")
	}
	if critique != nil {
		if len(critique.Tags) > 0 {
			b.WriteString("产出标签：")
			b.WriteString(strings.Join(critique.Tags, "、"))
			b.WriteString("\n")
		}
		if len(critique.Gaps) > 0 {
			b.WriteString("前序不足（本轮需补强）：")
			b.WriteString(strings.Join(critique.Gaps, "；"))
			b.WriteString("\n")
		}
	}
	s := b.String()
	if len(s) > maxChars {
		s = model.Truncate(s, maxChars)
	}
	if strings.TrimSpace(s) == "" {
		return "（无前序上下文）"
	}
	return s
}

// newID 生成带前缀的短 ID。
func newID(prefix string) string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return prefix + "-" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

// randPicker 是简单的伪随机源，避免全局 rand 的锁竞争。
type randPicker struct {
	state uint64
}

func newRandPicker() *randPicker {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	var s uint64
	for _, b := range buf {
		s = s<<8 | uint64(b)
	}
	if s == 0 {
		s = uint64(time.Now().UnixNano())
	}
	return &randPicker{state: s}
}

// Intn 返回 [0,n) 的随机数。
func (r *randPicker) Intn(n int) int {
	if n <= 1 {
		return 0
	}
	// xorshift64*
	r.state ^= r.state >> 12
	r.state ^= r.state << 25
	r.state ^= r.state >> 27
	v := (r.state * 2685821657736338717) >> 33
	return int(v % uint64(n))
}

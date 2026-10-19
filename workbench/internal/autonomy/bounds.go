package autonomy

// 本文件实现「动态任务的边界约束」：所有由 AI 自主生成的标题/Prompt/主题
// 都必须过这里，才允许进入评估与执行。目的是把"自主"限制在有界空间里，
// 而不是让模型输出任意内容直接驱动 7×24 循环。
//
// 边界分为四类（与 docs/自主运行方案.md 的 B1-B4 对应）：
//   B1 结构化边界：必填字段、长度上下限、字符合法性
//   B2 语义边界：禁止人工介入/外部凭据/不可逆操作等超出本机自动化能力的任务
//   B3 领域边界：只允许配置白名单内的领域（默认六大领域）
//   B4 预算边界：单目标任务数、阶段数、回退次数的硬上限（在 Manager 层判断）

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"workbench/internal/config"
)

// Violation 是一条边界违规记录。
type Violation struct {
	Code   string `json:"code"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
	Fatal  bool   `json:"fatal"` // true=拒绝执行；false=仅告警/已自动修正
}

// BoundsResult 是边界校验结果。
type BoundsResult struct {
	OK         bool        `json:"ok"`
	Violations []Violation `json:"violations,omitempty"`
	Draft      *Draft      `json:"draft,omitempty"` // 修正后的草案（OK 时可用）
}

// 默认边界常量。数值刻意保守：宁可少产出，也不让自主循环跑飞。
const (
	// MinTitleRunes / MaxTitleRunes：标题长度下限/上限（按 rune 计，防中文被按字节截断）。
	MinTitleRunes = 4
	MaxTitleRunes = 80
	// MinPromptRunes / MaxPromptRunes：Prompt 长度边界。
	MinPromptRunes = 30
	MaxPromptRunes = 4000
)

// dangerKeywords 是语义边界的黑名单片段。
//
// 判据来源：本项目执行通道是"本机 DSH 子进程、无外部账号、无人工介入"
// （见 memory/01-goal.md 用户决策 3/5 与 memory/03-tech-stack.md）。
// 凡是必须人工登录/付款/扫码/等待外部审批的任务，在自主循环里必然失败，
// 与其让它失败 3 次再判死，不如在规划期直接拒掉并记决策台账。
var dangerKeywords = []string{
	"登录", "扫码", "付款", "支付", "验证码", "短信验证", "人工审核", "等待审批",
	"提交表单", "填写表单", "点击按钮", "手动操作", "私钥", "密码", "token", "API Key",
	"下载安装包并安装", "注册账号", "绑定手机", "实名认证",
}

// 只允许的辅助动词白名单：Prompt 必须含"产出/输出/写出/生成/分析/整理"之一，
// 避免自主生成的任务变成"讨论一下 XX"这类没有可交付物的空转任务。
var deliverableVerbs = []string{"产出", "输出", "写出", "生成", "分析", "整理", "设计", "实现", "拆解", "总结"}

// ValidateDraft 校验一条自主任务草案是否在边界内。
//
// 返回的 BoundsResult.Draft 是**修正后**的草案：长度类问题自动裁剪（不致命，
// 但要留痕）；语义/领域类问题直接拒绝（致命）。这样"能修的修、不能修的拒"，
// 避免因为一个超长标题就把整条自主线卡死。
func ValidateDraft(d Draft, cfg config.AutonomyConfig) BoundsResult {
	res := BoundsResult{OK: true}
	add := func(code, field, detail string, fatal bool) {
		res.Violations = append(res.Violations, Violation{Code: code, Field: field, Detail: detail, Fatal: fatal})
		if fatal {
			res.OK = false
		}
	}
	out := d

	// B1 结构化边界：标题
	out.Title = strings.TrimSpace(out.Title)
	if out.Title == "" {
		add("title.empty", "title", "标题不能为空", true)
	} else if utf8.RuneCountInString(out.Title) < MinTitleRunes {
		add("title.tooShort", "title", fmt.Sprintf("标题少于 %d 字：%q", MinTitleRunes, out.Title), true)
	} else if utf8.RuneCountInString(out.Title) > MaxTitleRunes {
		before := out.Title
		out.Title = truncateRunes(out.Title, MaxTitleRunes)
		add("title.tooLong", "title", fmt.Sprintf("标题超长已裁剪（%d → %d 字）",
			utf8.RuneCountInString(before), MaxTitleRunes), false)
	}

	// B1 结构化边界：Prompt
	out.Prompt = strings.TrimSpace(out.Prompt)
	if out.Prompt == "" {
		add("prompt.empty", "prompt", "Prompt 不能为空", true)
	} else if utf8.RuneCountInString(out.Prompt) < MinPromptRunes {
		add("prompt.tooShort", "prompt", fmt.Sprintf("Prompt 少于 %d 字", MinPromptRunes), true)
	} else if utf8.RuneCountInString(out.Prompt) > MaxPromptRunes {
		before := out.Prompt
		out.Prompt = truncateRunes(out.Prompt, MaxPromptRunes)
		add("prompt.tooLong", "prompt", fmt.Sprintf("Prompt 超长已裁剪（%d → %d 字）",
			utf8.RuneCountInString(before), MaxPromptRunes), false)
	}

	// B1：Prompt 必须包含可交付物动词
	if out.Prompt != "" && !containsAny(out.Prompt, deliverableVerbs) {
		add("prompt.noDeliverable", "prompt", "Prompt 未包含可交付物动词（产出/输出/写出…），可能是空转任务", true)
	}

	// B2 语义边界：命中必须人工介入/外部凭据的关键词直接拒绝
	if hit := containsAnyFold(out.Title+" "+out.Prompt, dangerKeywords); hit != "" {
		add("semantic.manualRequired", "prompt",
			fmt.Sprintf("命中需要人工介入或外部凭据的片段 %q，本机自主通道无法完成", hit), true)
	}

	// B3 领域边界：只允许白名单领域
	if out.Domain == "" {
		out.Domain = cfg.Plan.DefaultDomain
	}
	if out.Domain == "" {
		out.Domain = "content"
	}
	if !domainAllowed(out.Domain, cfg.Bounds.AllowedDomains) {
		add("domain.notAllowed", "domain",
			fmt.Sprintf("领域 %q 不在自主白名单内（允许：%s）", out.Domain, strings.Join(cfg.Bounds.AllowedDomains, "/")), true)
	}
	if cfg.Bounds.MaxTaskWords > 0 && out.Words > cfg.Bounds.MaxTaskWords {
		add("budget.wordsTooLarge", "words",
			fmt.Sprintf("单任务字数上限 %d，草案要求 %d", cfg.Bounds.MaxTaskWords, out.Words), true)
	}
	if out.Words <= 0 {
		out.Words = 800
	}

	res.Draft = &out
	return res
}

// domainAllowed 判断领域是否在白名单内；白名单为空时退化为六大默认领域。
func domainAllowed(domain string, allowed []string) bool {
	if len(allowed) == 0 {
		switch domain {
		case "content", "code", "knowledge", "data", "solution", "report":
			return true
		}
		return false
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), domain) {
			return true
		}
	}
	return false
}

// containsAny 判断文本是否包含任一子串（大小写敏感）。
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if sub != "" && strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// containsAnyFold 大小写不敏感匹配，返回命中的第一个片段。
func containsAnyFold(s string, subs []string) string {
	low := strings.ToLower(s)
	for _, sub := range subs {
		if sub != "" && strings.Contains(low, strings.ToLower(sub)) {
			return sub
		}
	}
	return ""
}

// truncateRunes 按 rune 截断（绝不切碎中文），并在末尾补省略号。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	rs := []rune(s)
	if n <= 1 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "…"
}

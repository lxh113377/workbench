package blueprint

// Package blueprint 实现「思考模型只出方案、agent 模型负责执行」的双角色闭环的**方案层**。
//
// 背景（老大 2026-10-21 立规原话）：
//   「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」
//
// 现状缺口（改造前实测）：engine.runOne 每个任务只做一次模型调用 —— 路由器按 tier
// 选一个模型，把 task.Prompt 原样喂过去，模型直接吐产物块。也就是说：
//   - 有推理优势的模型被当成"写稿机"，没有机会先把方案想清楚；
//   - 擅长执行落地的 agent 模型没有拿到任何结构化指令，只能自己从一句话任务里猜；
//   - 验收失败后走的是 repairPrompt（把报错和上一版文件塞回去重做），
//     仍然是"同一个模型再试一次"，**方案层面错了就永远错**。
//
// 本包只做方案层：把「任务 + 目标 + 上一轮验收失败证据」变成一份结构化方案（Blueprint），
// 再渲染成执行指令交给执行模型。它不执行、不落盘产物、不判断验收 —— 那些是 engine 的事。
//
// 为什么方案层要独立成包而不是塞进 engine：
//   1. 方案有独立的输出契约（JSON + 章节），需要自己的解析与降级策略；
//   2. 方案模型与执行模型是两个不同的模型选择，选型逻辑要能被单测直接钉住；
//   3. engine 已经 77KB，再塞解析器就没人敢动了。

import (
	"fmt"
	"strings"

	"workbench/internal/model"
)

// Steps 是方案里允许的最大执行步骤数（防模型写出 50 步的万字计划）。
const MaxSteps = 8

// Blueprint 是思考模型产出的一份可执行方案。
//
// 字段刻意保持"少而硬"：模型能稳定输出的几个要素，且每一条都能被机器校验。
type Blueprint struct {
	// Approach 是总体思路（为什么这么做），一两句话。
	Approach string `json:"approach"`
	// Deliverables 是本次必须落地的交付物（相对路径 + 说明）。
	Deliverables []Deliverable `json:"deliverables"`
	// Steps 是执行步骤（有序）。
	Steps []Step `json:"steps"`
	// Verify 是方案自己声明的验收命令（执行模型必须让它们真的跑通）。
	Verify []VerifyCmd `json:"verify"`
	// Risks 是已知风险与规避方式（给执行模型看的"别踩"）。
	Risks []string `json:"risks,omitempty"`
	// Raw 是模型的原始输出（审计用；方案解析降级时也能人工复核）。
	Raw string `json:"-"`
	// Degraded 表示这份方案不是模型给的，而是规则兜底生成的。
	//
	// 为什么需要：方案模型不可用时不能让任务卡死（额度无限但车道会 429），
	// 必须退化成"原任务直执行"，同时把这个事实写进审计 —— 否则
	// 「思考模型只出方案」就成了一个无法验证的口号。
	Degraded bool `json:"degraded,omitempty"`
	// PlannerModel 是产出这份方案的模型 id（审计与面板展示）。
	PlannerModel string `json:"plannerModel,omitempty"`
	// Round 是方案轮次（0 = 首轮；>0 = 验收失败后重新规划）。
	Round int   `json:"round,omitempty"`
	At    int64 `json:"at,omitempty"`
}

// Deliverable 是一个必须落地的交付物。
type Deliverable struct {
	Path string `json:"path"`
	What string `json:"what"`
}

// Step 是一条执行步骤。
type Step struct {
	Order int    `json:"order"`
	Do    string `json:"do"`
	Done  string `json:"done,omitempty"` // 完成判据（可验证）
}

// VerifyCmd 是方案声明的验收命令。
type VerifyCmd struct {
	Cmd        string `json:"cmd"`
	ExpectExit int    `json:"expectExit"`
	Why        string `json:"why,omitempty"`
}

// IsZero 判断方案是否为空（没有任何可执行内容）。
func (b *Blueprint) IsZero() bool {
	if b == nil {
		return true
	}
	return strings.TrimSpace(b.Approach) == "" &&
		len(b.Deliverables) == 0 && len(b.Steps) == 0 && len(b.Verify) == 0
}

// Normalize 修正方案里的越界值，返回被修正的问题清单（用于审计）。
//
// 判据（都有实测依据，见各分支注释）：
//   - 步骤数超过 MaxSteps -> 截断（模型容易写出 30 步的无法执行计划）；
//   - 交付物路径必须相对、无 ..、无盘符（执行模型会据此落盘，越界即写穿沙箱）；
//   - 验收命令必须非空（空命令 = 无法证明产物可运行，见 verify.Run 的空命令判据）；
//   - 验收命令解释器必须在白名单内（与 verify 包同一口径）。
func (b *Blueprint) Normalize() []string {
	if b == nil {
		return nil
	}
	var problems []string
	b.Approach = strings.TrimSpace(b.Approach)
	if len(b.Steps) > MaxSteps {
		problems = append(problems, fmt.Sprintf("步骤数 %d 超过上限 %d，已截断", len(b.Steps), MaxSteps))
		b.Steps = b.Steps[:MaxSteps]
	}
	steps := make([]Step, 0, len(b.Steps))
	for i, s := range b.Steps {
		s.Do = strings.TrimSpace(s.Do)
		if s.Do == "" {
			problems = append(problems, fmt.Sprintf("步骤 #%d 内容为空，已丢弃", i+1))
			continue
		}
		s.Order = len(steps) + 1
		steps = append(steps, s)
	}
	b.Steps = steps

	dels := make([]Deliverable, 0, len(b.Deliverables))
	for _, d := range b.Deliverables {
		p := strings.TrimSpace(d.Path)
		if p == "" {
			continue
		}
		if bad := UnsafePath(p); bad != "" {
			problems = append(problems, fmt.Sprintf("交付物路径 %q 不安全（%s），已丢弃", p, bad))
			continue
		}
		d.Path = p
		d.What = strings.TrimSpace(d.What)
		dels = append(dels, d)
	}
	b.Deliverables = dels

	vers := make([]VerifyCmd, 0, len(b.Verify))
	for _, v := range b.Verify {
		c := strings.TrimSpace(v.Cmd)
		if c == "" {
			problems = append(problems, "存在空的验收命令，已丢弃")
			continue
		}
		if !InterpreterAllowed(c) {
			problems = append(problems, fmt.Sprintf("验收命令 %q 的解释器不在白名单内，已丢弃", truncate(c, 60)))
			continue
		}
		v.Cmd = c
		vers = append(vers, v)
	}
	b.Verify = vers
	return problems
}

// UnsafePath 返回路径不安全的原因（空串 = 安全）。
// 与 artifact 包同一口径：只允许相对路径，禁 .. / 盘符 / 绝对路径。
func UnsafePath(p string) string {
	s := strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if s == "" {
		return "空路径"
	}
	if strings.HasPrefix(s, "/") {
		return "绝对路径"
	}
	if len(s) >= 2 && s[1] == ':' {
		return "带盘符"
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return "含 .."
		}
	}
	return ""
}

// InterpreterAllowed 判断验收命令的解释器是否在白名单内。
// 白名单与 internal/verify 的 defaultInterpreters 保持一致。
func InterpreterAllowed(cmd string) bool {
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) == 0 {
		return false
	}
	base := strings.ToLower(fields[0])
	if i := strings.LastIndexAny(base, "\\/"); i >= 0 {
		base = base[i+1:]
	}
	switch base {
	case "python", "python3", "py", "node", "node.exe", "pwsh", "pwsh.exe", "powershell", "powershell.exe", "go", "go.exe":
		return true
	}
	return false
}

// truncate 把字符串限制到最多 n 个字符（rune），含省略号：绝不切碎中文，也不产出残缺 UTF-8。
//
// 契约与 internal/autonomy、internal/engine 的同名件对齐（round 197 已把那两份钉成同一预算契约），
// 本份此前是第三份漂移版，三处不同：① n<0 直接 panic（真 Go 实测 slice bounds out of range [:-1]）；
// ② 输出恒比预算多 1 个字符（r[:n] 之后又加省略号 ⇒ 声称 3000 实际 3001）；③ 不幂等（截过的
// 结果再截会再多一个省略号，而失败证据与摘要文案会被多层包装反复截。
// 实测差异（本轮跑的，不是推的）：旧版在全部三个真实预算上都比声称上限多 1 个字符（n=3000 出 3001、
// n=200 出 201、n=40 出 41）⇒ 这是**行为变更**而非等价重写：新版才是守预算的那一版；旧版 n=0 则输出
// 「…」这一个字符（零预算不该有任何输出）。下游依赖检查：本包对 truncate 结果的用途是拼 prompt / error
// 无任何长度或逐字断言（实测见 plan_test.go），故少 1 个字符不影响行为语义。
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}

// compile-time 引用，保持 model 包在本文件可见（At 字段口径统一）。
var _ = model.Now

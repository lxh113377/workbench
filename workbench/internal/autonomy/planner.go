package autonomy

// 本文件实现「动态任务规划」：不是从固定模板池里抽一个，而是
// 输入「项目目标 + 已执行阶段 + 上一阶段自评的不足与后继想法 + 近期已产出标题」，
// 推导出当前阶段应该做什么，并把它写成一条可执行草案（Draft）。
//
// 两条路径（都遵循"模型优先、规则兜底"）：
//  1. 模型驱动：上一任务的自评（critique）给出了 gaps 与 nextIdeas —— 这本身就是
//     模型对"下一步该做什么"的判断，规划器优先采纳，把它改写成带项目目标上下文的任务；
//  2. 规则兜底：没有可用自评时，按领域推进阶梯（Ladder）+ 目标意图 + 阶段序号推导主题，
//     保证自主线不会因为一次自评缺失而中断（fail-open 到确定性规划）。
//
// 两条路径的输出都要过 Bounds.ValidateDraft，边界外的一律拒绝并记决策台账。

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"workbench/internal/model"
)

// Draft 是一条由规划器推导出的任务草案（尚未落库）。
type Draft struct {
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Domain     string `json:"domain"`
	Words      int    `json:"words"`
	TemplateID string `json:"templateId,omitempty"`
	Rationale  string `json:"rationale,omitempty"` // 为什么现在做这个（写进决策台账）
}

// PlanContext 是规划输入：项目整体目标 + 当前执行状态。
type PlanContext struct {
	Goal       *Goal
	StageIndex int
	LastStage  *Stage       // 上一阶段（可能为 nil）
	LastGaps   []string     // 上一任务自评的不足
	NextIdeas  []model.Idea // 上一任务自评给出的后继想法（模型自主决策的直接产物）
	Recent     []string     // 近期已产出的标题（防重复）
	Now        int64
}

// Planner 是动态规划器。
type Planner struct {
	cfg PlanConfig
}

// PlanConfig 是规划器需要的配置子集（由 config.AutonomyConfig 转换，避免包间耦合）。
type PlanConfig struct {
	DefaultDomain string
	Words         int
	MaxDrafts     int
	StageEvery    int
}

// NewPlanner 构造规划器。
func NewPlanner(cfg PlanConfig) *Planner {
	if cfg.Words <= 0 {
		cfg.Words = 800
	}
	if cfg.MaxDrafts <= 0 {
		cfg.MaxDrafts = 2
	}
	if cfg.StageEvery <= 0 {
		cfg.StageEvery = 1
	}
	return &Planner{cfg: cfg}
}

// domainLadder 是各领域的推进阶梯：阶段序号越大越深入。
//
// 这张表就是"项目目标 → 阶段性目标"的确定性推导依据：
// 同一个目标下，第 0 阶段先立骨架，第 1 阶段补数据，第 2 阶段做验证……
// 不会出现"每轮都随机换话题"的空转（改造前 chain 的选题池就是随机的）。
var domainLadder = map[string][]string{
	"content": {
		"立框架：产出该主题的总纲与核心观点清单",
		"补证据：为总纲中的每个论点补 2 条可验证的论据",
		"成文：整合为结构完整、可直接发布的深度文章",
		"再加工：拆成卡片/口播/FAQ 等可复用形态",
		"复盘：对照原目标检查覆盖度与遗漏",
	},
	"code": {
		"定接口：写出工具/脚本的功能边界、输入输出与错误处理约定",
		"落实现：给出单文件可运行实现与运行说明",
		"自测：设计边界用例（空输入/超长/非法字符）并给出预期结果",
		"加固：补参数校验、日志与失败回退",
		"复盘：列出已知限制与后续改进点",
	},
	"knowledge": {
		"搭结构：给出知识地图与学习路径",
		"填细节：逐个节点展开要点与常见误区",
		"找反例：列出典型错误理解与纠正",
		"做检验：出题自测并给答案解析",
		"复盘：标注仍不确定的节点",
	},
	"data": {
		"定口径：明确指标定义、数据来源与计算方式",
		"出分析：完成一次结构化数据分析并给出结论",
		"做归因：对异常点给出 2-3 个假设与验证方式",
		"给建议：把结论转成可执行动作与优先级",
		"复盘：标注数据缺口与结论置信度",
	},
	"solution": {
		"定目标与约束：写清方案要解决的问题、边界与验收判据",
		"给方案：至少 2 个备选方案与取舍理由",
		"排风险：列出落地风险与缓解措施",
		"给步骤：拆成分步执行清单与负责人/耗时估计",
		"复盘：对照验收判据检查是否成立",
	},
	"report": {
		"立题：明确报告范围、读者与结论要回答的问题",
		"收资料：整理事实证据与来源",
		"出结论：给出判断与依据，区分事实与推测",
		"给建议：转成可执行动作",
		"复盘：标注不确定项与需要补充的证据",
	},
	// quality / frontend 两域 2026-10-26 补（round 7 实测缺键 ⇒ ladderFor 静默回退 content 阶梯：
	// domain=quality 的自主目标落到「立框架：产出该主题的总纲与核心观点清单」，与工程质量无关）。
	// 口径：每一步都必须是一个**可验收的工程动作**（体检 → 立项 → 小步修 → 判据固化 → 复盘），
	// 与 docs/quality-packs 模板包、topicsByDomain 的选题池同一套语言。
	"quality": {
		"体检：按五轴（正确性/可读性/架构/安全/性能）扫当前工作目录的项目，列出带 文件:行号 的真实缺陷",
		"立项：从体检结果里挑一个最小可修项，写清后果、最小改法与可复现证据",
		"小步修：只改一个最小单元，改完同一命令复验 + 反向变异证明判据会判红",
		"判据固化：把这次修复固化成机器判据（能判红的测试或门禁），并实测先红后绿",
		"复盘：回填可直接贴 PR 的报告（含合并结论与门禁建议），标注未修项与触发条件",
	},
	"frontend": {
		"体检：过 HTML 语义与可访问性、CSS 令牌体系、JavaScript/TypeScript 正确性、React/Vue 组件层，列 文件:行号 问题",
		"基线：对要改的面先取可复算读数（产物体积/构建时长/首屏路径），写清测量命令与取样次数",
		"小步改：只动一处（令牌/组件/构建配置），改后同一命令复跑并给出改前→改后",
		"一致性：确认源码与构建产物逐字一致（改源码必须重建），并补上对应判据",
		"复盘：回填报告与门禁建议，标注未做项与触发条件",
	},
}

// qualityAxes 是工程质量体检的五轴（round 62 · T61）：与 docs/quality-packs 模板包、
// 目标文本里的「正确性/可读性/架构/安全/性能」同一套口径 —— 分轴体检产出的是可直接贴 PR 的分级报告。
var qualityAxes = []string{"正确性", "可读性", "架构", "安全", "性能"}

// stepRotation 数 Recent 里**某一步**已出现的分轴标题条数，作为下一轴的轮转号。
//
// 为什么用 Recent 而不是计划器内部状态：计划器是纯函数式的（PlanContext 进出、无状态），
// 而 Recent 由引擎按 ~24 条近期标题（含被拒草案）喂入 —— 正是「这一步已经推进过几轮」的事实来源。
// 按步计数（token 区分）而不是按全局计数：不同阶段的轮次要各自独立，否则阶段切换会把轮次算歪。
func stepRotation(recent []string, token string) int {
	if token == "" {
		return 0
	}
	needle := token + "（"
	n := 0
	for _, r := range recent {
		if strings.Contains(r, needle) {
			n++
		}
	}
	return n
}

// focusRest 取阶梯步文本里**冒号之后**的动作描述（没有冒号则返回原文）。
// 例：「小步修：只改一个最小单元，改完…」→「只改一个最小单元，改完…」。
func focusRest(focus string) string {
	f := strings.TrimSpace(focus)
	i := strings.IndexAny(f, "：:，,。；;")
	if i < 0 {
		return f
	}
	return strings.TrimSpace(strings.TrimLeft(f[i:], "：:，,。；;"))
}

// ladderFor 返回某领域的推进阶梯（未知领域回退到 content）。
func ladderFor(domain string) []string {
	if l, ok := domainLadder[domain]; ok && len(l) > 0 {
		return l
	}
	return domainLadder["content"]
}

// MaxDraftsForTest 暴露当前生效的规划产出上限（供测试断言边界钳制真的生效）。
//
// 命名带 ForTest 是为了明确「这不是业务 API」：生产代码不应依赖它。
func (p *Planner) MaxDraftsForTest() int {
	if p == nil {
		return 0
	}
	return p.cfg.MaxDrafts
}

// Plan 推导当前阶段的任务草案（按优先级返回 1..MaxDrafts 条）。
//
// 规划优先级：
//  1. 上一任务自评给出的 nextIdeas（模型直接给出的"下一步"）—— 最多 1 条，
//     避免模型一次给一堆互相重叠的方向；
//  2. 阶梯推进：按 StageIndex 落在哪个阶梯，结合目标意图生成；
//  3. 若以上都生成不出（极端兜底），用目标标题 + 领域默认动词生成一条。
func (p *Planner) Plan(ctx PlanContext) []Draft {
	if ctx.Goal == nil {
		return nil
	}
	goal := ctx.Goal
	domain := goal.Domain
	if domain == "" {
		domain = p.cfg.DefaultDomain
	}
	var out []Draft

	// 1) 模型自评给出的后继想法：它是"基于本次产出得出的下一步"，优先级最高。
	//    只在想法与目标领域兼容、且标题不与近期重复时采纳。
	for _, idea := range ctx.NextIdeas {
		if strings.TrimSpace(idea.Title) == "" || strings.TrimSpace(idea.Prompt) == "" {
			continue
		}
		d := Draft{
			Title:     strings.TrimSpace(idea.Title),
			Prompt:    p.enrichPrompt(goal, idea.Prompt, ctx.StageIndex, ctx.LastGaps),
			Domain:    pickDomain(idea.Domain, domain),
			Words:     p.cfg.Words,
			Rationale: "采纳上一阶段自评给出的后继方向（模型自主决策）",
		}
		if duplicated(d.Title, ctx.Recent) {
			continue
		}
		out = append(out, d)
		break // 只采纳一条：模型给的方向容易互相重叠
	}

	// 2) 阶梯推进：把"当前处于第几阶段"映射到具体动作。
	if len(out) < p.cfg.MaxDrafts {
		step := ctx.StageIndex
		lad := ladderFor(domain)
		if step >= len(lad) {
			step = len(lad) - 1
		}
		focus := lad[step]
		// 标题必须用 composeTitle 拼（2026-10-21 现场缺陷，代价 32 小时零产出）：
		// 旧写法是 goal.Title + " · " + focus 之后由下游 model.Truncate 按**字节**裁剪，
		// 而模型层 Truncate 的 n 是字节数、MaxTitleRunes 是 rune 上限（80）——
		// 中文 3 字节/rune，现场目标标题已 78 字节，" · 阶段6" 一加就 89 字节，
		// 截到 80 再回退合法边界后正好停在 78 字节：**追加的焦点后缀被整体吃掉**，
		// 标题退化成目标标题本身。composeTitle 保证焦点段永不被裁掉。
		title := composeTitle(goal.Title, shortFocus(focus))
		rationale := fmt.Sprintf("目标处于第 %d 阶段，按领域推进阶梯执行「%s」", ctx.StageIndex, shortFocus(focus))
		// 分轴轮转（round 62 · T61，round 66 推广到**每一步**）：
		//
		// 只要目标卡在某个阶段不推进，该阶段的标题就是恒定的，评估器便按重复门禁反复拒 ——
		// 两次现场实测：2026-10-28 23:58 阶段 0「体检」同秒 5 连拒；2026-10-29 15:54 阶段 2「小步修」同样 5 连拒，
		// 随后 22 分钟零供料（熔断让位模板、而模板也被限流压制）。T61 当初只修了「体检」这一步 ⇒ 换个阶段又复发。
		// 破法（通用）：把**每一步**都按「五轴 + 轮次」参数化 —— 标题互不相同，且每一轮都聚焦一个轴，
		// 正是目标要的「正确性/可读性/架构/安全/性能」分轴推进（可直接贴 PR 的分级报告）。
		// 轮转号取自 Recent 里**本步**标题的条数（引擎喂入 ~24 条、含被拒草案）：前 25 条内必然互不相同；
		// 饱和后由下面的兜底分支（保证唯一标题）接管，不会退回死循环。
		if domain == "quality" {
			token := shortFocus(focus)
			n := stepRotation(ctx.Recent, token)
			axis := qualityAxes[n%len(qualityAxes)]
			round := n/len(qualityAxes) + 1
			// 保留该步自己的动作描述（冒号之后那段），只把「做哪一轴」插到前面 —— 语义不缩水。
			focus = fmt.Sprintf("%s：只做「%s」这一轴 —— %s", token, axis, focusRest(focus))
			// ⚠️ 焦点段里的轴名与轮次之间**禁止再用 "·"**：duplicated() 取的是「最后一个 · 之后」的焦点段，
			// 内层再来一个 · 会让「正确性·第1轮」与「可读性·第1轮」被截成同一个「第1轮）」⇒ 判重退回死循环。
			// 这条是写 T61 时被测试当场抓出来的（诊断读数：第二轮退回兜底标题「…· 阶段1」）。
			title = composeTitle(goal.Title, fmt.Sprintf("%s（%s/第%d轮）", token, axis, round))
			rationale = fmt.Sprintf("阶段 %d 分轴推进：%s · %s（第 %d 轮，避免恒定标题被判重）", ctx.StageIndex, token, axis, round)
		}
		d := Draft{
			Title:     title,
			Prompt:    p.buildLadderPrompt(goal, focus, ctx),
			Domain:    domain,
			Words:     p.cfg.Words,
			Rationale: rationale,
		}
		if !duplicated(d.Title, ctx.Recent) {
			out = append(out, d)
		}
	}

	// 3) 极端兜底：阶梯与自评都没产出时，直接以目标意图为任务。
	//
	// 注意（现场缺陷，2026-10-19 端到端实测）：兜底标题必须带上"本轮阶段序号 +
	// 与近期标题的差异后缀"。改造前兜底标题是 goal.Title+" · 阶段N"，当 StageIndex
	// 卡在某个值而该标题已被评估器的相似度门禁（0.92）判重时，会形成
	// 规划 → 评估拒绝 → 3 秒后再规划 → 再拒绝的死循环（实测 2 阶段后卡住，
	// decisions.jsonl 连续 12 条 "与最近任务高度相似（0.92）"、0 产出）。
	// 兜底路径的使命是"保证不断线"，因此它必须产出唯一标题。
	if len(out) == 0 {
		stage := fmt.Sprint(ctx.StageIndex + 1)
		title := composeTitle(goal.Title, "阶段"+stage)
		// 去重循环同样走 composeTitle：旧写法先 Truncate 再追加"（第N次推进）"，
		// 追加部分在长标题下同样会被下一次 Truncate 吃掉，于是 20 轮循环产出的
		// 仍是同一个标题 —— 兜底路径的"保证唯一"承诺在长目标标题下完全落空。
		for i := 2; duplicated(title, ctx.Recent); i++ {
			title = composeTitle(goal.Title, fmt.Sprintf("阶段%s·第%d次推进", stage, i))
			if i > 20 {
				break
			}
		}
		out = append(out, Draft{
			Title:     title,
			Prompt:    p.buildLadderPrompt(goal, "围绕目标当前进度继续推进，产出可复核成果", ctx),
			Domain:    domain,
			Words:     p.cfg.Words,
			Rationale: "兜底规划：无可用自评且阶梯标题重复，按目标意图直接推进（标题已去重）",
		})
	}
	if len(out) > p.cfg.MaxDrafts {
		out = out[:p.cfg.MaxDrafts]
	}
	return out
}

// buildLadderPrompt 把「项目目标 + 阶段焦点 + 前序状态」拼成可执行 Prompt。
//
// 这是"动态任务"与"固定模板"的核心区别：模板的 Prompt 是 {topic} 占位符，
// 这里的 Prompt 带着项目目标、已完成阶段、上一阶段不足，模型是在"接着项目往下做"。
// autonomyDeliverableGate 是自主任务的**交付物门禁**条款（round 61 · T59）。
//
// 为什么必须加（本会话两处 live 取证合成的结构矛盾）：
//   - 自主 prompt 原先写「直接输出可用的 Markdown 正文」（聊天正文）；
//   - 而执行器会给每个任务追加产物协议（executor.AppendProtocol），failover 闸门 judge()
//     又要求响应里必须有 <<<ARTIFACTS>>> 产物块，否则记 no_artifact 并换车道。
//
// 两侧要求互斥 ⇒ 听话按 prompt 的车道被判 no_artifact（round 60 实测 12 次 kind=ok、判定行 0、从未走到验收），
// 听话按协议的车道交出自写脚本又常年跑不过（现网 8 条 verify 里 7 条 fail：探索脚本退出码 1、
// manifest JSON 解析失败、代码围栏未闭合）。
//
// 本条款把自主任务拉回「报告文件 + 内容判据」这条两类车道都做得动的窄路，并**顺带对齐目标要求**：
// 报告落在 docs/（指定地点）、含结论/证据/门禁建议三节、证据带 file:line、可直接贴 PR。
// 措辞复用产物协议第 4 条已由 live 证明有效的「.md 成果文件 + 原样命令」范式。
const autonomyDeliverableGate = "\n\n## 交付物门禁（硬性）\n" +
	"1. 交付物是**文件**不是聊天正文：按上面的产物协议输出产物块，把报告写到 docs/ 下的 .md\n" +
	"   （例：docs/五轴审查-<主题>.md）。\n" +
	"2. 报告必须含三节，标题逐字一致：`## 结论`、`## 证据`、`## 门禁建议`。\n" +
	"3. 「证据」节每条都要带 **file:line** 或一条**可复跑命令**；禁止「经检查」「可以发现」这类无证据表述。\n" +
	"4. verify 至少 1 条**检查报告内容**的命令；不要拿「运行你自己刚写的脚本」当唯一验收 —— 免费车道写的脚本跑不过，会把整条任务拖成返工。\n" +
	"5. 不要输出你自己的工具调用语法（工作台不执行它们，只会判本方案/本产物不可用）。\n"

func (p *Planner) buildLadderPrompt(goal *Goal, focus string, ctx PlanContext) string {
	var b strings.Builder
	b.WriteString("你是自主执行本项目的 AI。项目核心目标：")
	b.WriteString(goal.Intent)
	b.WriteString("\n\n当前阶段性目标（第 ")
	b.WriteString(fmt.Sprint(ctx.StageIndex + 1))
	b.WriteString(" 阶段）：")
	b.WriteString(focus)
	b.WriteString("\n\n")
	if len(goal.Stages) > 0 {
		b.WriteString("已完成阶段：\n")
		for _, st := range goal.Stages {
			status := "通过"
			if !st.Passed {
				status = "未通过"
			}
			b.WriteString(fmt.Sprintf("- 第%d阶段 %s（质量 %d/10，%s）\n", st.Index+1, st.Title, st.Quality, status))
		}
		b.WriteString("\n")
	}
	if len(ctx.LastGaps) > 0 {
		b.WriteString("上一轮自评指出的不足（本轮必须优先补齐）：\n")
		for _, g := range ctx.LastGaps {
			b.WriteString("- " + strings.TrimSpace(g) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf("产出要求：约 %d 字；报告正文写进**产物文件**（不是聊天正文，见下方交付物门禁）；", p.cfg.Words))
	b.WriteString("包含明确结论与可执行建议；不要写导语寒暄；不要复述上面的要求。\n")
	b.WriteString(autonomyDeliverableGate)
	return b.String()
}

// enrichPrompt 在模型给出的 idea.Prompt 外面套上项目目标与前序不足的上下文。
func (p *Planner) enrichPrompt(goal *Goal, ideaPrompt string, stage int, gaps []string) string {
	var b strings.Builder
	b.WriteString("【项目核心目标】")
	b.WriteString(goal.Intent)
	b.WriteString("\n\n【本阶段任务（第 " + fmt.Sprint(stage+1) + " 阶段，由上一阶段自评推导）】\n")
	b.WriteString(strings.TrimSpace(ideaPrompt))
	if len(gaps) > 0 {
		b.WriteString("\n\n【上一轮不足，需在本轮补齐】\n")
		for _, g := range gaps {
			b.WriteString("- " + strings.TrimSpace(g) + "\n")
		}
	}
	b.WriteString("\n要求：内容具体可执行，约 " + fmt.Sprint(p.cfg.Words) + " 字；直接给出成果，不要寒暄；交付形态见下方交付物门禁。")
	b.WriteString(autonomyDeliverableGate)
	return b.String()
}

// NextGoalDraft 是推导出的下一目标草案。
type NextGoalDraft struct {
	Title    string      `json:"title"`
	Intent   string      `json:"intent"`
	Domain   string      `json:"domain"`
	Reasons  string      `json:"reasons"`
	Criteria []Criterion `json:"criteria"`
}

// DeriveNextGoal 在当前目标完成后推导下一个目标。
//
// 推导依据（全部来自已完成目标与它最后阶段的产出，不是随机）：
//  1. 领域推进阶梯的下一个层级（内容 → 成文 → 复用形态）；
//  2. 最后阶段的 gaps：把"没做好的地方"升格为下一目标的主攻方向；
//  3. 轮转：同一领域连续完成 2 个目标后自动换到下一个领域，避免单点过拟合。
func (p *Planner) DeriveNextGoal(prev *Goal, lastGaps []string, now int64, cfg GoalDefaults) *NextGoalDraft {
	if prev == nil {
		return nil
	}
	domain := prev.Domain
	if domain == "" {
		domain = p.cfg.DefaultDomain
	}
	if domain == "" {
		domain = "content"
	}
	// 领域轮转：一个领域完成 2 个目标后换域，保证长期运行覆盖多个方向。
	if goalOfDomainCount(prev) >= 2 {
		domain = nextDomain(domain)
	}
	// 下一阶段的焦点：优先用最后阶段的 gaps（把不足变成目标），否则用阶梯下一层。
	focus := ""
	if len(lastGaps) > 0 {
		focus = strings.TrimSpace(lastGaps[0])
	}
	lad := ladderFor(domain)
	step := len(prev.Stages)
	if step >= len(lad) {
		step = len(lad) - 1
	}
	if focus == "" {
		focus = lad[step]
	}
	// 标题必须基于「根标题」重拼，不能在上一代标题上继续追加（2026-10-21 现场缺陷）。
	//
	// 旧写法是 prev.Title + " · 深化：" + focus，每完成一代目标就复利叠加一层：
	//   持续产出与能力沉淀
	//   → 持续产出与能力沉淀 · 深化：复盘
	//   → 持续产出与能力沉淀 · 深化：复盘 · 深化：复盘
	//   → …（现场实测第 5 代已 31 rune / 78 字节）
	// 叠加到 78 字节后正好触发下游"按字节截断把新后缀吃掉"的连锁缺陷，
	// 标题退化成目标标题本身 —— 这是「32 小时运行中但 0 产出」的上游起点。
	// 正确口径：根标题（首个「 · 」之前的部分）恒定不变，只换焦点段。
	title := composeTitle(rootGoalTitle(prev.Title), "深化："+shortFocus(focus))
	intent := fmt.Sprintf("在已完成目标「%s」的基础上，继续推进%s，重点解决：%s。", prev.Title, domain, focus)
	return &NextGoalDraft{
		Title:    title,
		Intent:   intent,
		Domain:   domain,
		Reasons:  fmt.Sprintf("上一目标已完成 %d 个阶段；按领域轮转与不足升级规则推导下一目标", len(prev.Stages)),
		Criteria: DefaultCriteria(cfg, domain),
	}
}

// goalOfDomainCount 返回 prev 及其祖先链上同领域的连续目标数（用于领域轮转）。
func goalOfDomainCount(prev *Goal) int {
	return len(prev.Stages) // 简化：用阶段数代表该领域的推进程度
}

// nextDomain 返回轮转的下一个领域。
func nextDomain(cur string) string {
	order := []string{"content", "code", "knowledge", "data", "solution", "report"}
	for i, d := range order {
		if d == cur {
			return order[(i+1)%len(order)]
		}
	}
	return "content"
}

// DefaultCriteria 按预算配置生成默认验收标准。
//
// 默认三条：完成 N 个阶段、平均质量达标、产出总字数达标。
// 这三条都能从阶段轨迹直接算出，不需要额外执行。
func DefaultCriteria(cfg GoalDefaults, domain string) []Criterion {
	minTasks := cfg.MinStages
	if minTasks <= 0 {
		minTasks = 3
	}
	minQ := float64(cfg.MinQuality)
	if minQ <= 0 {
		minQ = 6
	}
	words := float64(cfg.MinWords)
	if words <= 0 {
		words = 2400
	}
	// 真实验收判据（P3）：要求"有阶段产出真的通过执行验收"，但用**比例制**表达。
	//
	// 演进原因（2026-10-19 运行时实测）：绝对计数判据 `c-verified >= minTasks*0.6`
	// 在真实通道上恒不可达 —— 模型对 `<<<ARTIFACTS>>>` 产物协议的遵从率不稳定，
	// 多数回答是纯 Markdown，导致 `Verified` 恒 false，目标永远无法完成
	// `goalDone/nextGoal` 永不出现（详见 docs/自主运行-闭环死锁修复回执.md §5）。
	//
	// 比例制的语义不变（"真做出来的比例够不够"），但把"只要有一部分产出被真实验收通过
	// 就算达标"变成可达成目标；同时保留最低 1 个：如果一个都没有，仍不算完成。
	verifiedRatio := 0.35
	_ = minTasks
	return []Criterion{
		{ID: "c-done", Desc: fmt.Sprintf("完成不少于 %d 个阶段任务", minTasks), Kind: CriterionTaskDone, Threshold: float64(minTasks)},
		{ID: "c-quality", Desc: fmt.Sprintf("阶段自评均分不低于 %.1f/10", minQ), Kind: CriterionMinQuality, Threshold: minQ},
		{ID: "c-words", Desc: fmt.Sprintf("阶段产出总字数不少于 %.0f", words), Kind: CriterionMinWords, Threshold: words},
		// 关键判据（P3 接线）：产出必须真的能跑，而不只是字够多、自评够高。
		{ID: "c-verified", Desc: fmt.Sprintf("至少 %.0f%% 的阶段产出通过真实执行验收（不少于 1 个）", verifiedRatio*100),
			Kind: CriterionVerifiedRatio, Threshold: verifiedRatio},
		{ID: "c-verified-min", Desc: "至少 1 个阶段产出通过真实执行验收",
			Kind: CriterionVerified, Threshold: 1},
	}
}

// GoalDefaults 是生成目标时的默认参数（来自 config）。
type GoalDefaults struct {
	MinStages    int
	MinQuality   int
	MinWords     int
	MaxTasks     int
	MaxStages    int
	MaxRollbacks int
}

// pickDomain 优先用给定领域，空则回退。
func pickDomain(preferred, fallback string) string {
	if strings.TrimSpace(preferred) != "" {
		return preferred
	}
	return fallback
}

// duplicated 判断标题是否与近期产出重复。
//
// 判定口径（2026-10-19 端到端实测后修正）：
//   - 自主任务标题的构造是「<目标标题> · <阶段焦点>」，同一目标下所有阶段的标题
//     天然共享目标前缀。改造前用"归一化后前 12 字相同即重复"，会把
//     「… · 立框架」「… · 阶段2」「… · 阶段3」全判成同一个任务 —— 实测导致
//     规划器阶梯分支永远被跳过、兜底标题永远被判重，评估器再以 0.92 相似度拒绝，
//     形成 3 秒一次的死循环（decisions.jsonl 连续 12 条"高度相似"，0 产出）。
//   - 正确口径：比较「焦点段」（最后一个 · 之后的部分）。焦点相同 = 同一任务；
//     焦点不同 = 不同阶段任务。没有分隔符的短标题退回全文比较 + 前缀包含判定。
func duplicated(title string, recent []string) bool {
	t := normTitle(title)
	if t == "" {
		return false
	}
	tf := focusPart(title)
	for _, r := range recent {
		rn := normTitle(r)
		if rn == "" {
			continue
		}
		if rf := focusPart(r); tf != "" && rf != "" {
			if rf == tf {
				return true
			}
			continue
		}
		if rn == t {
			return true
		}
		if len([]rune(rn)) < 12 && len([]rune(t)) < 12 {
			if strings.HasPrefix(rn, t) || strings.HasPrefix(t, rn) {
				return true
			}
		}
	}
	return false
}

// focusPart 取标题里「最后一个 · 之后」的焦点段（没有分隔符则返回全文）。
//
// 这就是"同一目标下的不同阶段任务"的区分依据：目标前缀相同不算重复，焦点相同才算。
func focusPart(title string) string {
	s := strings.TrimSpace(title)
	if i := strings.LastIndex(s, "·"); i >= 0 {
		return strings.TrimSpace(s[i+len("·"):])
	}
	return s
}

// normTitle 归一化标题：去空白、截断到 12 个 rune。
func normTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	rs := []rune(s)
	if len(rs) > 12 {
		rs = rs[:12]
	}
	return string(rs)
}

// composeTitle 用「目标标题 · 焦点」拼一个**按 rune 限长**的标题。
//
// 为什么需要专门的函数（2026-10-21 现场缺陷，代价 32 小时零产出）：
//   - 全仓的 model.Truncate(s, n) 里 n 是**字节**数，而标题上限常量叫
//     MaxTitleRunes（80，按 rune 计）。中文 3 字节/rune，两者差 3 倍；
//   - 直接拼接后再由下游按字节截断，会把**刚追加的焦点后缀**优先裁掉
//     （裁剪发生在尾部，而焦点恰在尾部）—— 标题退化成目标标题本身，
//     每轮规划产出同一条标题，评估器按"无新颖度"拒绝，形成 3 秒热循环。
//
// 语义保证（三条，都有单测守护）：
//  1. 焦点段**必须完整保留**（阶段信息是标题的唯一区分点，裁掉即等于没规划）；
//  2. 目标前缀按 rune 预算自适应收缩（超出预算时截断并补省略号）；
//  3. 结果按 rune 计不超过 MaxTitleRunes。
func composeTitle(goalTitle, focus string) string {
	goalTitle = strings.TrimSpace(goalTitle)
	focus = strings.TrimSpace(focus)
	if focus == "" {
		return truncateRunes(goalTitle, MaxTitleRunes)
	}
	// 焦点 + 分隔符优先占预算；余下的预算给目标前缀。
	sep := " · "
	rest := MaxTitleRunes - utf8.RuneCountInString(sep) - utf8.RuneCountInString(focus)
	if rest < MinTitleRunes {
		// 焦点本身就接近上限：只保留焦点，前缀退化为可辨识的最短片段。
		head := truncateRunes(goalTitle, MinTitleRunes)
		if head == "" {
			return truncateRunes(focus, MaxTitleRunes)
		}
		rest = MaxTitleRunes - utf8.RuneCountInString(sep) - utf8.RuneCountInString(head)
		if rest < 2 {
			return truncateRunes(head+sep+focus, MaxTitleRunes)
		}
		// 焦点优先：前缀再让一点，保证焦点整体可读。
		return truncateRunes(head, utf8.RuneCountInString(head)) + sep + truncateRunes(focus, MaxTitleRunes-utf8.RuneCountInString(head)-utf8.RuneCountInString(sep))
	}
	return truncateRunes(goalTitle, rest) + sep + focus
}

// shortFocus 把长焦点句压成短标题片段（最多 18 字）。
func shortFocus(focus string) string {
	focus = strings.TrimSpace(focus)
	if i := strings.IndexAny(focus, "：:，,。；;"); i > 0 {
		focus = focus[:i]
	}
	return truncateRunes(focus, 18)
}

// rootGoalTitle 取目标标题的根段（首个「 · 」之前的部分）。
//
// 用途：目标代际推导时以它为基座重拼标题，避免"深化：X · 深化：X"复利叠加。
// 历史上没有分隔符的标题原样返回；首段为空（畸形标题）时退回整串。
func rootGoalTitle(title string) string {
	s := firstLine(title)
	if i := strings.Index(s, " · "); i > 0 {
		if head := strings.TrimSpace(s[:i]); head != "" {
			return head
		}
	}
	return s
}

// firstLine 取文本第一行。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// runeLen 返回 rune 数（供测试与外部使用）。
func runeLen(s string) int { return utf8.RuneCountInString(s) }

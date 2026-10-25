// Package templates 提供内置任务模板库与模板校验/渲染。
package templates

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"workbench/internal/config"
	"workbench/internal/model"
)

type spec = model.OutputSpec
type tpl = model.Template

func mkSpec(format string, sections []string, minWords, minBytes int) spec {
	return spec{Format: format, Sections: sections, MinWords: minWords, MinBytes: minBytes}
}

// Defaults 返回内置的 29 条持续产出模板（7 领域；quality 领域 2026-10-24 round 88/90 新增，含五轴审查）。
// 设计原则：每条都必须能独立产出真实可用的成果物，且彼此有逻辑递进关系。
func Defaults() []tpl {
	now := model.Now()
	mk := func(id, name, domain, desc, prompt string, tags []string, valuePrior, costPrior float64, weigh int, out spec, chain []string) tpl {
		return tpl{
			ID: id, Name: name, Domain: domain, Description: desc,
			PromptTemplate:  prompt,
			Params:          model.TemplateParams{ModelTier: "balanced", MaxMinutes: 30, OutputFormat: out.Format, WebAllowed: false, Tags: tags},
			OutputSpec:      out,
			PriorityWeight:  weigh,
			ValuePrior:      valuePrior,
			CostPrior:       costPrior,
			CooldownMinutes: 60,
			DiversityGroup:  domain,
			Chain:           model.ChainSpec{SuccessorTemplates: chain, MaxDepth: 5, Fanout: 2},
			Enabled:         true,
			Builtin:         true,
			CreatedAt:       now,
			UpdatedAt:       now,
		}
	}
	return []tpl{
		// ── 内容创作 content ──────────────────────────────────────────
		mk("content-deep-dive", "深度长文", "content",
			"围绕指定主题产出结构完整、可直接发布的深度文章",
			"请写一篇关于「{topic}」的深度文章，约 {words} 字。要求：有独立观点而非资料堆砌；结构包含摘要、正文分节、要点总结、可执行建议；语言专业但不晦涩；不要写导语寒暄。直接在回答中输出完整文章正文。",
			[]string{"长文", "深度"}, 90, 70, 4,
			mkSpec("md", []string{"摘要", "正文", "要点", "建议"}, 1500, 3000),
			[]string{"content-outline-cards", "content-script"}),
		mk("content-outline-cards", "要点卡片化", "content",
			"把已有内容拆成可直接使用的知识卡片",
			"请基于主题「{topic}」和以下上下文，产出 10 张知识卡片。每张卡片包含：一句话结论、3 条支撑要点、1 个可立即执行的行动。用二级标题分隔每张卡片，直接在回答中输出全部内容。上下文：\n{context}",
			[]string{"卡片", "拆解"}, 75, 35, 3,
			mkSpec("md", []string{"卡片"}, 600, 1500),
			[]string{"content-script"}),
		mk("content-script", "口播脚本", "content",
			"把观点转成 3 分钟可直接拍摄的口播脚本",
			"请把主题「{topic}」写成一条 3 分钟口播脚本（约 700 字）。要求：前 3 秒必须有钩子；口语化、短句；包含分镜提示（画面/字幕）；结尾有行动号召。直接在回答中输出完整脚本。参考上下文：\n{context}",
			[]string{"脚本", "口播"}, 80, 40, 3,
			mkSpec("md", []string{"钩子", "正文", "分镜", "结尾"}, 600, 1500), nil),
		mk("content-faq", "常见问题解答集", "content",
			"针对主题产出高质量 FAQ，覆盖真实疑问",
			"请围绕「{topic}」写出 15 条高质量常见问题与解答。要求：问题来自真实用户困惑而非凑数；答案准确、给具体做法而非空话；对易错点明确提示。直接在回答中输出完整问答内容。",
			[]string{"FAQ", "答疑"}, 70, 30, 3,
			mkSpec("md", []string{"问答"}, 800, 2000), nil),

		// ── 代码生成 code ───────────────────────────────────────────
		mk("code-tool", "实用小工具", "code",
			"产出一个可直接运行的脚本或命令行小工具",
			"请实现一个实用的命令行小工具，用于解决「{topic}」。要求：单文件、零第三方依赖、有 --help、有参数校验与错误提示、附 README 用法示例。直接在回答中给出完整源码（Markdown 代码块）+ 用法说明 + 自测要点。",
			[]string{"工具", "脚本"}, 85, 65, 4,
			mkSpec("md", []string{"用法", "实现", "自测"}, 300, 1200),
			[]string{"code-review"}),
		mk("code-review", "代码审查报告", "code",
			"对指定代码做缺陷与可维护性审查",
			"请审查当前工作目录下的代码（若无代码则审查主题「{topic}」相关的典型实现），输出一份审查报告：按严重程度列出问题（每条含位置、风险、修复建议）；给出重构优先级；不要泛泛而谈。上下文：\n{context}",
			[]string{"审查", "质量"}, 80, 45, 3,
			mkSpec("md", []string{"结论", "问题清单", "建议"}, 600, 1800), nil),
		mk("code-snippet-library", "代码片段库", "code",
			"整理一组高质量可复用代码片段",
			"请围绕「{topic}」整理 8 个高质量代码片段，每个片段包含：用途、可直接复制的代码、关键点说明、常见误用。直接在回答中输出全部片段（Markdown 加代码块）。",
			[]string{"片段", "复用"}, 75, 40, 3,
			mkSpec("md", []string{"片段"}, 700, 2000), nil),
		mk("code-test-plan", "测试方案设计", "code",
			"为功能产出可执行的测试方案",
			"请为「{topic}」设计一份可执行测试方案：覆盖正常路径、边界值、异常输入、并发与恢复；每条用例给出输入、预期、判定方式；标注优先级。",
			[]string{"测试", "方案"}, 78, 40, 3,
			mkSpec("md", []string{"范围", "用例", "判定"}, 600, 1800), nil),

		// ── 知识整理 knowledge ──────────────────────────────────────
		mk("knowledge-map", "知识地图", "knowledge",
			"把领域拆成结构化知识地图",
			"请围绕领域「{topic}」产出一份知识地图：包含全景分层、每层核心概念（含一句话定义）、学习路径（先学什么后学什么、为什么）、常见误区。要求结构清晰可当目录使用。",
			[]string{"地图", "体系化"}, 85, 40, 4,
			mkSpec("md", []string{"全景", "核心概念", "学习路径", "误区"}, 1000, 2500),
			[]string{"knowledge-glossary", "knowledge-playbook"}),
		mk("knowledge-glossary", "术语表", "knowledge",
			"整理领域术语的准确释义",
			"请为「{topic}」整理 20 个核心术语的术语表：每个术语给出一句准确定义、一个直观类比、一个常见混淆点。避免循环定义。",
			[]string{"术语", "定义"}, 70, 30, 3,
			mkSpec("md", []string{"术语"}, 700, 2000), nil),
		mk("knowledge-compare", "方案对比", "knowledge",
			"对多个方案做结构化对比并给出结论",
			"请就「{topic}」相关的 3-4 个方案做对比分析：给出对比维度表、每个方案的适用场景与代价、明确的推荐结论与理由。不要罗列而不判断。",
			[]string{"对比", "决策"}, 80, 40, 3,
			mkSpec("md", []string{"维度", "对比", "结论"}, 700, 1800), nil),
		mk("knowledge-playbook", "实操手册", "knowledge",
			"把知识转化为可照着做的手册",
			"请围绕「{topic}」写一份实操手册：按步骤给出可执行流程，每步包含目的、具体操作、检查点、失败怎么补救。面向新手，不许省略关键细节。",
			[]string{"手册", "落地"}, 85, 45, 4,
			mkSpec("md", []string{"准备", "步骤", "检查点", "补救"}, 900, 2500), nil),

		// ── 数据处理 data ───────────────────────────────────────────
		mk("data-analysis", "数据分析报告", "data",
			"对给定数据做分析并给出结论",
			"请围绕「{topic}」产出一份数据分析报告。若工作目录存在数据文件则基于真实数据计算（必须给出计算脚本与结果）；若无数据则明确声明为方法论示例并给出可套用的分析框架。结论必须可执行。",
			[]string{"分析", "数据"}, 85, 55, 4,
			mkSpec("md", []string{"背景", "方法", "结果", "结论"}, 800, 2200), nil),
		mk("data-cleaning-rules", "数据清洗规则", "data",
			"产出一套可复用的数据清洗规范",
			"请围绕「{topic}」产出一份数据清洗规范：列出常见脏数据类型、检测方法、清洗动作、清洗后校验；给出可执行的校验脚本伪代码。",
			[]string{"清洗", "规范"}, 72, 35, 3,
			mkSpec("md", []string{"问题类型", "规则", "校验"}, 600, 1800), nil),
		mk("data-metrics-design", "指标体系设计", "data",
			"设计一套可落地的指标体系",
			"请为「{topic}」设计指标体系：区分北极星指标、一级指标、二级指标；每个指标给出定义、计算公式、数据来源、常见误读；给出看板建议。",
			[]string{"指标", "体系"}, 80, 40, 3,
			mkSpec("md", []string{"目标", "指标体系", "定义", "看板"}, 700, 2000), nil),
		mk("data-sql-cookbook", "SQL 实战手册", "data",
			"整理可复用的 SQL 实战写法",
			"请围绕「{topic}」整理 SQL 实战手册：10 个典型场景，每个给出需求描述、可执行 SQL、执行计划注意点、常见错误。使用标准 SQL，注明方言差异。",
			[]string{"SQL", "实战"}, 74, 40, 3,
			mkSpec("md", []string{"场景"}, 700, 2000), nil),

		// ── 方案撰写 solution ───────────────────────────────────────
		mk("solution-proposal", "方案设计", "solution",
			"输出完整的方案设计与论证",
			"请为「{topic}」写一份方案设计：包含背景与目标、方案总体设计、关键决策与备选对比、实施步骤与里程碑、风险与应对、验收标准。要求决策有理由，不许罗列。",
			[]string{"方案", "设计"}, 90, 60, 5,
			mkSpec("md", []string{"背景", "设计", "决策", "实施", "风险", "验收"}, 1200, 3000),
			[]string{"solution-checklist"}),
		mk("solution-checklist", "落地上线清单", "solution",
			"把方案转成可勾选的上线清单",
			"请基于「{topic}」与以下上下文，产出一份上线检查清单：分为准备、执行、验证、回滚四段，每项给出判定标准与负责人占位。上下文：\n{context}",
			[]string{"清单", "上线"}, 78, 30, 3,
			mkSpec("md", []string{"准备", "执行", "验证", "回滚"}, 500, 1500), nil),
		mk("solution-risk", "风险评估", "solution",
			"系统性识别与分级风险",
			"请就「{topic}」做风险评估：识别风险项（技术、资源、时间、外部依赖）、按概率乘影响分级、给每个风险的具体缓解措施与触发信号。",
			[]string{"风险", "评估"}, 76, 35, 3,
			mkSpec("md", []string{"风险清单", "分级", "缓解"}, 600, 1800), nil),
		mk("solution-sop", "SOP 流程文档", "solution",
			"产出可执行的标准作业流程",
			"请为「{topic}」写一份 SOP 文档：明确适用范围、角色职责、流程步骤（含输入输出）、异常处理、度量方式。要求任何人照着都能执行。",
			[]string{"SOP", "流程"}, 80, 40, 3,
			mkSpec("md", []string{"范围", "角色", "流程", "异常"}, 700, 2000), nil),

		// ── 报告生成 report ─────────────────────────────────────────
		mk("report-weekly", "阶段性报告", "report",
			"生成结构化的阶段性总结报告",
			"请生成一份关于「{topic}」的阶段性报告：包含本期结论、关键进展、数据与证据、问题与阻塞、下期计划。要求结论先行，避免流水账。上下文：\n{context}",
			[]string{"报告", "总结"}, 78, 35, 4,
			mkSpec("md", []string{"结论", "进展", "问题", "计划"}, 600, 1800), nil),
		mk("report-research", "调研报告", "report",
			"产出一份有据可依的调研报告",
			"请就「{topic}」产出一份调研报告：明确调研问题与方法、给出关键发现（每条附证据或来源）、结论与建议。区分事实与判断。",
			[]string{"调研", "证据"}, 88, 60, 4,
			mkSpec("md", []string{"问题", "方法", "发现", "结论"}, 1000, 2800), nil),
		mk("report-benchmark", "对标分析", "report",
			"对标优秀实践找出差距",
			"请就「{topic}」做对标分析：选取 3 个对标对象，给出对标维度表、差距归因、可迁移做法与优先级。上下文：\n{context}",
			[]string{"对标", "差距"}, 82, 50, 4,
			mkSpec("md", []string{"对象", "维度", "差距", "迁移"}, 800, 2200), nil),
		mk("report-executive", "决策摘要", "report",
			"把复杂信息压缩成一页决策摘要",
			"请就「{topic}」产出一页决策摘要：用 5 句话讲清现状、2 个可选方案、推荐方案与理由、需要什么支持。全文不超过 600 字，但不许牺牲信息密度。上下文：\n{context}",
			[]string{"摘要", "决策"}, 75, 25, 4,
			mkSpec("md", []string{"现状", "方案", "推荐"}, 300, 900), nil),

		// ── 工程质量 quality（2026-10-24 round 88 新增领域）────────────────
		// 为什么新增：本工作台的核心使命就是「提升被选中文件夹的工程质量」（objective 原话），
		// 而既有 6 个领域（content/code/knowledge/data/solution/report）没有一条面向「工程质量」本身。
		// round 1-88 的实际工作全是判据/门禁/验证/卫生类，模板库却覆盖不到它 —— 这是模板与使命的脱节。
		// 4 条对应工程质量提升的四个动作：体检（找问题）→ 判据化（把教训变成机器可判）→
		// 验证（证明判据有效）→ 固化（防复发/保卫生）。
		mk("quality-audit", "工程质量体检", "quality",
			"对指定项目做工程质量体检并给出可执行清单",
			"请对「{topic}」做一次工程质量体检。要求：① 先说明体检范围与判据来源（文档承诺/历史事故/既有门禁）；"+
				"② 按严重程度 × 影响范围列出发现的问题，每条含位置、证据（实测命令与输出）、风险；"+
				"③ 给出修复优先级与预期收益；④ 明确区分已证实与未验证 —— 没测成不许写成合格；"+
				"⑤ 每个数字与结论都要注明取证方式，并先自证测法正确：读字段值必须执行程序取运行时真值，"+
				"不要解析源码文本（解析等于重做一遍词法与拼接语义 —— 2026-10-25 实测同一个指标量出三种结果）；"+
				"判据命令必须查退出码并打印错误（空输出与执行失败是两回事 —— 助手函数吞掉错误曾把 41 读成 1）；"+
				"自动分类器的输出是待核线索不是结论（曾判 9 条不一致，人工逐条核对只有 4 条成立，按 9 条改就会改错 5 条）；"+
				"凡涉仓库惯例先测分布，别拿自己上一份文件当基准。"+
				"⑥ 改完带编码要求的文件（.ps1 必须有且仅有一个 UTF-8 BOM、部分文档要求无 BOM），"+
				"必须在使用写入/编辑工具之后再逐字节复验编码：本仓那两个工具会静默剥掉开头的 BOM、"+
				"并把 CRLF 改成 LF —— diff 只看内容行看不见这类变化，只有查字节才会发现。"+
				"⑦ 否定性结论（「缺件」「不在库」「没有被调用」「文件不存在」）必须先到**权威定义处**核对真实路径与名字，"+
				"再下断言：本会话实测按猜出的路径去查资源件，得出「不在库、产物将丢图标与版本」的结论，"+
				"回代码里读它的定义才发现真实路径完全不同、该件在库且有效（2026-10-25 round 160；同类失误本会话 4 次）。"+
				"「找不到」最容易被自己的错路径伪造 —— 断言之前先 grep 定义处取真值。"+
				"⑦ 的第二维：**工具的输出显示形式**同样能伪造「找不到」。2026-10-25 实测：git 默认对非 ASCII 路径"+
				"做 quotepath 转义（非 ASCII 路径变成八进制序列显示），于是拿输出比对中文文件名的自检探针得到假阴性"+
				"（报告明明在提交里，探针却说 False）。修法二选一：命令加 "+
				"-c core.quotepath=false，或别拿显示串比对、改比字节/比结构化输出。"+
				"通用判据：先确认「查不到」是事实还是**显示层转义**造成的，再下否定性结论。"+
				"上下文：\n{context}",
			[]string{"体检", "审计"}, 95, 55, 5,
			mkSpec("md", []string{"范围与判据", "问题清单", "修复优先级"}, 800, 2500),
			[]string{"quality-gate-spec"}),
		mk("quality-gate-spec", "判据规格设计", "quality",
			"把一类缺陷的教训设计成机器可判的门禁规格",
			"请针对「{topic}」这类已发生的缺陷，设计一条判据规格。要求：① 写清为什么需要（含真实事故与代价）；"+
				"② 判据必须是可证伪的客观事实（能写出期望输出与实测输出的对比）；"+
				"③ 明确豁免项与理由（宁漏不误的边界）；④ 给出三态设计（PASS/FAIL/UNVERIFIED）与退出码；"+
				"⑤ 附双向变异验证方案：注入缺陷时期望判红、修复后期望放行。直接在回答中输出完整规格。"+
				"⑥ 判据若读外部数据件（基线 JSON、配置、清单）里的字段，规格必须写清「字段缺失/为 null 时怎么办」，"+
				"并配一条静态配对检查。实测教训（2026-10-25）：判据分支在、数据件里没有对应键 ⇒ 取值得到 null ⇒"+
				"判据静默走 SKIP 而整条门禁仍 rc=0，运行期读数与「真在跑」一模一样，只能靠静态检查发现；"+
				"该形态实测静默失效三天（立判据时键与分支同批入库 → 一次「重取基线」把两者一起删 →"+
				"又一次「只补 BOM」的提交 diff 多了 20 行把分支带回、键没人补）。配对检查至少要管三件事："+
				"引用的每个字段在数据件里要有键；为 null 的字段必须登记理由；分支标记与字段同生同灭。"+
				"⑦ 判据若依赖 git 历史（git log / rev-list / blame 等需要**祖先提交**的命令），规格必须写明"+
				"「取不到历史时怎么办」：CI 的 checkout 默认 fetch-depth: 1（浅克隆），此时 git log -1 -- <paths>"+
				"可能返回空 —— 那不是「没有改动」，而是「看不到祖先提交」，必须走 UNVERIFIED，不许当空集判绿；"+
				"规格还要写明降级后**由谁容忍 rc=2**（实测形态：check_artifact_soak_fresh.ps1 取空即 Unverified，"+
				"ci.yml 该步骤显式 Write-Warning 后 exit 0）。注意别把「打印给用户看的 git 自查命令」"+
				"当成依赖 —— 实测踩点：静态扫描会把 Write-Host 里的提示串误判成依赖，这类检测会制造噪声。"+
				"⑧ **挂受理面 ≠ 在该受理面上真能判**。规格必须写明：这条判据依赖的状态（暂存区、产物文件、"+
				"运行中的实例、git 历史深度）在它要挂的受理面上**是否存在** —— 不存在时闸只会 PASS"+
				"却不判任何东西，那是**虚增覆盖**，比没挂更坏（读数看着有护栏，实际没有）。"+
				"2026-10-26 实测：一条判索引（git diff --cached）的提交前闸被列进 CI 的门禁清单，"+
				"而 CI 是干净检出、暂存区恒为空 ⇒ 它在那里永远走「暂存区无文件」的 exit 0 分支。"+
				"双向实测：干净 worktree（模拟 CI）rc=0 只打提示、不作任何判断；造一个无 BOM 含中文的"+
				"脚本并 git add ⇒ rc=1 点名文件 —— 证明它本地真在判、摘掉的不是功能而是假覆盖。"+
				"取舍也要写进规格：**不要改成 SKIP 来诚实声明**，若受理面的聚合逻辑把 rc 非 0 一律算失败，"+
				"诚实声明反而会把它弄红；正确做法是从该清单移出、保留它真正会开火的那个面。"+
				"同族要防的第二半：别把闸名以引号路径形态写进注释 —— 受理面覆盖检查的正则会把注释"+
				"当成「已挂载」，于是同一个虚增覆盖换了个地方复活。"+
				"⑨ **聚合器不得把非零退出码改写成 0 —— 「不计入 PASS」的正确实现是另设计数，不是抹平返回值**（round 214-215 实测）："+
				"本仓真实形态是段内写 if rc=2 就打印「UNVERIFIED（不计 PASS）」、紧接着一句把 LASTEXITCODE 置 0；"+
				"上层聚合读到 0 便直接判 PASS ⇒ 那句「不计 PASS」名不副实。真跑实测：改前 43 段全报 PASS，改后"+
				"PASS=42 / UNVERIFIED=1，被揪出的那段本机**什么都没验**（无常驻实例）却一直记成通过 —— 洗绿不是比喻。"+
				"唯一正解：聚合层逐个识别非零码的语义（本仓 2=证据不足、3=未执行），**分别计数、都不算 PASS**；要不要"+
				"判红取决于「红了会不会逼人把总门禁整体绕过」（本仓答案是判红会天天红、遂不判红），但**绝不允许用归零**"+
				"来表达宽容 —— 宽容要体现在计数与措辞上，返回值一旦被抹平，任何上层统计都失去辨别力。"+
				"同族另一形态是 SKIP 分支归零（round 63 已修）。自查动作：在聚合脚本里搜归零赋值与 return true 前是否"+
				"吞掉非零码，再做一次「故意让某段返回非零」的注入实验，看总 verdict 会不会跟着变绿 —— 变绿就是洗绿。",
			[]string{"判据", "门禁"}, 90, 50, 4,
			mkSpec("md", []string{"为什么", "判据", "豁免与三态", "变异验证"}, 700, 2200),
			[]string{"quality-verify"}),
		mk("quality-verify", "判据有效性验证", "quality",
			"用变异测试证明一条判据真的能拦住它要防的缺陷",
			"请为「{topic}」这条判据设计并执行有效性验证。要求：① 构造能触发该判据的最小缺陷；"+
				"② 记录注入前后的完整输出（不只是退出码）；③ 反向验证：修复后必须放行；"+
				"④ 检查判据自身是否空转（恒真/恒假/扫描不到目标）；"+
				"⑤ 变异样本必须取自本仓真实缺陷形态，不能只造一种写法就宣布通过 —— 同一段代码里"+
				"rgba(...) 与 rgb(...) 是两种形态，只测其一会让判据对另一种静默失效：2026-10-24 实测踩过，"+
				"判据只搜 rgb( 而仓库多数写 rgba(，注入 rgba 样本时 rc=0 漏检（刚收编的那处正是 rgba）；"+
				"⑥ 判据是时序相关的（并发、竞态、偶发）时，必须给复现率，不能只报一次绿。"+
				"2026-10-25 实测踩过：四路并发写同一目标路径，本机跑过一次就宣布「并发已解决」，"+
				"实际复现率约 1/30，且只在 CI 的第二个 job 暴露（主 race job 仍绿）。"+
				"做法：重复到出现失败或 -count=30 起，把「N 次里失败 M 次」写进结论；"+
				"修复后同样重复并给出 M=0。"+
				"⑦ 变异实验自身也要有受理面检查：判红之前必须确认是判据判的，而不是实验把被测物弄崩了。"+
				"2026-10-25 实测踩过：变异脚本的还原函数把整个源文件当内容塞进 raw string，"+
				"之后两步都返回非零，但那是 Go 编译错误（syntax error）与判据无关 —— 只看退出码"+
				"就会把无效结论写进报告。做法：每次变异后先在输出里排除编译/构建失败类字样"+
				"（syntax error、build failed、undefined）再采信判红；还原按字节捕获、按字节写回，"+
				"并复验与原始字节一致。"+
				"⑧ 结论只能是已验证有效或未验证 —— 不许把没测成写成通过。上下文：\n{context}"+
				"⑨ 变异样本必须自证「真的改掉了目标」：把判据搜索的标记换成一个仍包含原串的新名"+
				"（实测形态 exeSizeMin 改成 exeSizeMinRenamed），Contains 类判据依旧命中，读数看起来是"+
				"「判据没抓到」—— 但那是样本无效，不是判据漏抓。2026-10-25 实测踩过：这种无效变异差点"+
				"被我写成一条「判据漏抓」的结论；改成不含原串的新名后才正确判红。所以顺序是：看到判据"+
				"没抓到，先怀疑自己的样本，再怀疑判据。"+
				"⑩ 新写或改造的判据/守卫**自身**也要做双向变异证明：注入它要防的那一种真缺陷必须判红、"+
				"去掉缺陷必须判绿。2026-10-25 round 157 实测：新加的家族守卫第一版把针写成 fmt.Scan，"+
				"它不是 fmt.Sscan 与 fmt.Sscanf 的子串 ⇒ 只挡住从未出现过的写法；修好针后仍 SILENT ——"+
				"拼接时把表达式写成了字面字符串，分隔符取不到、针永远匹配不上。两次都是「把实现退回旧写法」"+
				"的变异实验暴露的，不是推理出来的。所以判据写完立刻自证，且注入形态必须与它要防的形态一致。"+
				"⑪ 文本/源码类判据一律**先剥注释再判**，并且变异验证要**专门试一种形态**：把真值改坏、"+
				"但注释里**保留原词**。2026-10-25 同一条教训在本仓出现两次：① 死代码门禁把函数自己"+
				"文档注释里的函数名算成「引用」⇒ 凡有文档注释的未导出函数对它全部隐形（同树同时刻 0 vs 16）；"+
				"② 新写的 spawn 闸直接在代码片段上 Contains(CREATE_NO_WINDOW)，而那唯一一行的**尾注释里"+
				"就有这个词** ⇒ 把定义改成 CreationFlags: 0 后闸仍 PASS（SILENT）。只注入「改坏真值」而"+
				"不剥注释，这两类缺陷都抓不出来。"+
				"⑫ 判据里凡是**引用次数/出现次数**这类计数，作用域必须与**语言的可见性规则**一致，"+
				"不能做全局同名计数。2026-10-25 实测：死代码门禁用「全仓 name→次数 > 1 即算被引用」，"+
				"而 Go 的**未导出**标识符只可能被**本包**引用 ⇒ 另一包的同名函数把死函数洗活 ——"+
				"engine/helpers.go 的 baseName 全仓零调用却判活（verify 里的同名定义+调用把计数抬到 3）。"+
				"改为按包限定计数（package|name）后，同一棵树当场报出那个真死函数；删掉它闸即复绿。"+
				"同一道闸因此有**两种**假阴性：注释被当引用（round 158）、跨包同名被当引用（round 177）——"+
				"计数类判据必须把「计什么作用域」写清楚，并用变异证明它在**漏计**时真的会判红。"+
				"⑫ 的第二维：计数的**修订版本**也必须与它声称的口径一致 —— 声称「提交的」就不能去数工作区文件。"+
				"2026-10-25 实测：check_untracked_go 用 git ls-files 取**路径**再读**工作区内容**当「提交的测试函数」，"+
				"对已跟踪但有未提交改动的文件数的是工作区版本 ⇒ 差额（「只在本地跑的测试」）被低估。"+
				"受控 A/B（夹具给已跟踪测试文件追加 1 个未提交测试）：旧报 838/838/差额 0（把本地测试算成已提交），"+
				"新报 837/838/差额 1；干净树上读数一字不变。通用判据：**声明什么口径，就用那个口径的数据源**"+
				"（作用域 × 修订版本两个维度），并且必须用夹具证明修正的**方向**。"+
				"⑬ **覆盖率缺口不等于行为缺口**。补测试前先问：这个分支的效果**可被观测吗**？"+
				"2026-10-25 实测：-coverprofile 查出 normTitle 的空串分支 hits=0，补上测试后把守卫删掉做变异 ⇒"+
				"测试仍全绿（SILENT）—— 因为空串走完整路径同样返回空串，那个 if 只是**提前返回**，"+
				"行为上冗余、外部不可观测。反过来同一轮的 firstLine 多行分支变异就判红且理由精确到实际串。"+
				"所以：为不可观测分支写断言只会**制造假安全感**（覆盖率变高、守护力为零）；要钉的是可观测行为，"+
				"并在补测试后**用它做一次变异**确认真的响了。同一轮的另一课：判断「某函数有没有被守护」"+
				"不能靠 grep 函数名（只能证明没人直接调用），要靠覆盖率或运行时真值 —— 实测有测试经公开 API"+
				"间接覆盖它并断言了边界，而 grep 给我的结论是「零测试」。"+
				"⑭ **自检探针自己也会错，而且错得很像「内容不存在」**。三条硬规矩（本会话反复付过代价）："+
				"一、待查串必须**从被查内容里取逐字子串**，不要手打、更不要截断 —— 手打串与被查文本"+
				"差一个词就得到 False，于是把「我的探针错」误判成「条款缺失 / 文件不在提交里」。实测形态："+
				"把长句截成前几个字当特征串、把某个词手打错一字，都曾让我对**确实存在**的内容报 False。"+
				"同一族还有**取错对象**：探针还得断言自己取到的串确实含目标标识。实测我按码位取 ⑭ 的串，"+
				"[char]9316 其实是 ⑤ —— 正负对照全都 True、探针看着在工作，验的却是另一条条款；"+
				"补一句身份断言（所取串必须含被验条款的编号）之后，产物命中才算数。"+
				"二、探针要配**两个对照**再采信：正向（一个已知必然存在的串必须 True，否则解析器/口径坏了）"+
				"与反向（一个已知必然不存在的串必须 False，否则探针恒真、结论无意义）。实测两次靠它救回："+
				"数受理面清单时先加「已知门禁被数到 = true」，才发现另一版把源文件读成了输出文本、"+
				"凭空数出 0；查产物里的片段时靠「不存在的串 = False」才敢相信 True 是有效结论。"+
				"三、**段数不是次数**：按标记切分得到的数组长度比出现次数多 1，"+
				"直接拿它当计数会凭空多出一个失败项（实测把 43 通过 0 失败读成 44/1）。要么数增量、"+
				"要么用正则匹配计数。判据的判据成立才算自证：**探针错了，比没有探针更坏** ——"+
				"它会给错误结论盖一个「我验证过了」的章。"+
				"⑮ **按块判定，不按单行判定；可达性断言必须实跑，且必须自带对照组**（本轮两类错误各撞一次）："+
				"分类判据若靠「这一行有没有某关键字」去推断块结构或守卫是否在位，必然判错 —— 实测我把 9 处"+
				"「读 git 对象没包 try」当成风险点，逐行读到上下文才发现其中两行本在 try 块内、一处走 cmd 包装，"+
				"**真正可炸的活体是 0 个** —— 差一点就据此去改一个并不存在的缺陷。要看缩进、花括号与上下文成块地读。"+
				"另一半：任何「这条分支可达 / 这种写法安全」的结论都必须用实跑对照拿到，而且实验要带对照组 ——"+
				"我第一次把探测目录选在 runtime（它在仓库内，git 向上能找到 .git ⇒ 命令成功返回 0），于是失败路径"+
				"根本没被执行到，四路全绿却什么都没测出来；是「仓库内应当返回 0」这条对照组暴露了这一点。"+
				"换成确定在仓库外的临时目录重跑，才拿到决定性数据：裸调用与加管道两种写法都被终止错误打断"+
				"（后续行不执行），只有 try/catch 捕获并继续。**对照组不成立的实验等于没做，别拿它下结论。**"+
				"⑯ **并发缺陷不得用「race 检测跑过」免除：同一份代码在不同调度窗口下结论可相反；同一通道的所有使用路径必须同锁**（2026-10-26 实测崩溃）："+
				"CI 里 vet+test(race) 作业**全绿**，另一道「纯净检出后整树 go test」的作业却崩在 send on closed channel，"+
				"差别只在分包并行与整树跑的调度窗口不同 ⇒ 「加了 -race 就安全」是错的。必须写**显式压测循环**把竞态窗口"+
				"反复踩开（本例 4000 轮「订阅→消费几条→立即取消」），让 panic 直接崩测试进程判红 —— panic 类缺陷的优势是"+
				"**不需要断言就能判红**，别浪费它。结构侧不变式：同一个通道的所有发送路径都必须在同一把锁内，**包括后台"+
				"起的回放/重发 goroutine**。本例 Emit 持锁发送、cancel 持锁 close 都守规矩，唯独漏了第三条路 —— 回放的"+
				"goroutine 完全不持锁，取消落在回放在途窗口就炸。审计要按「谁发送这个通道」**逐条列全**，不能只看主要的"+
				"两条；反向也要查：只 close 从不发送的通道（实测本仓两处）没有 send-on-closed 风险，可安全放过，别把它们"+
				"也算成缺陷。最后：这类 panic 发生在后台 goroutine、没有 recover 兜得住 ⇒ 整个进程退出；对 7×24 无人值守"+
				"的桌面进程，触发条件只是「用户关掉浏览器里的面板页」这种日常操作 ⇒ 判优先级按崩溃算，不按理论竞态算。",
			[]string{"验证", "变异"}, 90, 45, 4,
			mkSpec("md", []string{"注入", "实测输出", "反向验证", "结论"}, 600, 1800),
			[]string{"quality-hygiene"}),
		mk("quality-hygiene", "工程卫生清理", "quality",
			"清理可再生副产物与未入库资产，让仓库状态可读",
			"请对「{topic}」做一次工程卫生清理。要求：① 分类盘点未跟踪文件（在途工作/可再生产物/一次性证据/真交付物）；"+
				"② 对可再生产物给出 .gitignore 条目（逐条列名 + 注释理由，不通配 —— 通配会误伤真交付物）；"+
				"③ 对一次性证据判断入库归档还是忽略；④ 验证双向：目标文件被忽略 + 受跟踪文件零误伤；"+
				"⑤ 汇总清理前后的 untracked 数量变化。上下文：\n{context}"+
				"⑥ 凡是「写文件 + 提交」的动作，写完必须**重新入索引并核对提交内容与预期一致**（"+
				"看 git status 的工作区-索引差、用 git show --stat 看真的进了什么）："+
				"本会话实测踩过 —— 条目是在 git add **之后**才写进文件的，于是它躺在工作区、提交里根本没有，"+
				"直到把 HEAD 版本与工作区版本逐字对照才发现（git show HEAD:<file> 里没有那段文字）。"+
				"「先暂存后写」就是这条的反向失误，且**门禁不会判红**：内容本身合规，只是没进提交。"+
				"⑦ 提交必须用**显式 pathspec** 限定范围（git commit -F <msg> -- <你的路径…>）；"+
				"**不要**指望 --no-verify 收窄范围 —— 它只跳过 hook，**提交的仍是整个暂存区**。"+
				"2026-10-25 实测踩过：为了让 pre-commit 让路而用 --no-verify，结果把另一会话 **14 个已暂存"+
				"测试件**一并写进 HEAD（含 2 个 gofmt 漂移件）⇒ CI 判红；处置只能按本仓机制登记债务并更正口径。"+
				"另：git add **之后**才做的编辑不会进索引，提交前必须用 git show HEAD:<file> 逐字对照 ——"+
				"本仓已两次同型（round 159 的 CHANGELOG 条目、round 172 的判据修复本体）。"+
				"⑧ 改写**既有文件**前先**实测**它的 BOM / 换行口径 / 编码，**不要按惯例假定**；写盘后按字节复验"+
				"（前几字节 + CRLF/LF 计数 + BOM 个数）。2026-10-25 本仓连续踩了 4 次：① gofmt-debt.txt 的 BOM"+
				"被剥（该带的丢了）；② check_issue_forms.ps1 按 CRLF 切一个 **LF** 文件 ⇒ 整个文件被当成一行；"+
				"③ CHANGELOG 实际 **LF 为主**却按 CRLF 切 ⇒ 找不到锚点、插不进去；④ engine/helpers.go **没有 BOM**"+
				"却按「有 3 字节 BOM」切掉前 3 字节 ⇒ 头部 package 变 kage（go vet 当场报 expected package）。"+
				"判据：**写前测、写后量**；「文件长什么样」是测出来的，不是猜出来的。"+
				"⑨ 交付物里凡是**登记为待做 / 暂不做 / 已否掉**的条目，必须带**轮次日期**，并且每次引用它之前"+
				"**先复核它的状态是否还成立** —— 这类条目极易过期，而过期的判断会**误导读者做新的决策**。"+
				"2026-10-26 实测：一份五轴报告把「内嵌 JS 资产的产物侧口径」登记为**暂不做**，理由是"+
				"**重复投入收益低**（以成本降级）；而两轮之前的 round 176 已经用 go/parser 把它**落地了**"+
				"（13 块含 3 份 JS）。同段结尾还写着「未覆盖：JS 产物侧未做」—— 与现实相反。"+
				"发现方式不是有人读出来，而是我把**全仓措辞审计**当成一次实测跑了一遍。"+
				"两个动作：① 登记项写清「谁在什么条件下才算完成」；② 引用旧登记的结论前，先跑一次"+
				"grep 或判据复验它现在的状态。**过期条目就地更新，并在旁边留一行带日期的更正说明**，"+
				"不要悄悄改写历史 —— 读者需要知道那个判断是怎么被推翻的。"+
				"⑨ 的第二维：复核状态是为揪出**过期的判断**，不是把所有数字刷成当前值 —— 带日期/轮次的数字是"+
				"**当时的证据快照**，改写它反而毁掉审计文档的可信度（读者无从知道当时看到了什么）。"+
				"round 200 实测：我差点把两份五轴报告范围行里的「44 道受跟踪门禁」当过期项去刷成当前值，"+
				"而那两处是各自审计当轮（round 171 / round 176）的**历史计数**，文件名本身也带着那个日期。"+
				"该复核的是**当前态声明** —— 例如同一段里的「某债务登记仍在册」，拿它跟仓库实况比；"+
				"比完仍一致就**原样保留**，不为「让数字好看」而动。通用判据：先分辨这句话是"+
				"**当时观测**还是**现在断言**，再决定要不要更新 —— 前者只能追加带日期的更正，不能改写。"+
				"⑩ **破坏性操作（递归删除、清空、移动、覆盖）绝不允许对「字符串算出来的路径」动手** —— 目标必须是"+
				"字面写出的具体文件/目录，动手前还要用只读方式确认它就在预期的那个目录里。2026-10-26 实测我自己在"+
				"一次清理里为了「算出某个子目录的父目录」，对路径字符串做了 replace 反推，结果算出来的是**工作区根**，"+
				"紧接着对它执行了递归删除 —— 等于把整个仓库删了一遍开头。零丢失纯属运气：对象库与 refs 还在、跟踪文件"+
				"恰好都在别的入口下活着；代价是我手工重建了 .git 的 HEAD/config/index 才让仓库重新可用。"+
				"三条硬规矩：① 路径只能来自字面量或**问出来的**（先 stat/列目录确认），不能来自 replace/slice/join 的产物；"+
				"② 优先逐个文件 unlink，不用递归删除 —— 递归的爆炸半径无法在代码里核对；"+
				"③ 一旦命令报出「not a git repository」这类**根基异常**，立刻停手只做只读清点（先定损害再动手），"+
				"任何「顺手修一下」的写操作都会在未知情图上再砍一刀。工具契约里明写着不得对计算出的路径执行删除，"+
				"我是在**已经读过这条规则**的情况下踩的 —— 说明规则必须变成动作前的机械检查，不能只靠记性。"+
				"⑪ **工作副本的换行可能与入库内容「不同但等价」；对它直接跑格式化工具会造出整文件假 diff**（round 218 实测）："+
				"本仓 10 个 .go 的工作副本是 CRLF，而 HEAD 侧全是 LF —— git diff 判干净、把副本归一成 LF 后与 HEAD"+
				"**逐字节相等**，可 gofmt -l 跑工作区原始字节把这 10 个全报成未格式化。谁顺手 gofmt -w 就会重写整文件："+
				"副本实测 CRLF 392 → 0、字节 14524 → 14132（差的正是 392 个 CR）⇒ **392 行假 diff，内容一字未动**。"+
				"这就是本仓历史上「一上 gofmt 闸就满屏假红、最后只能把闸撤掉」的机制，如今有了可复算的数字。"+
				"两条硬规矩：① **判格式用归一化口径（HEAD / 归一成 LF 后比），绝不用工作区原始字节** —— check_gofmt 正是"+
				"这么做的，所以门禁没错、错的是手工跑工具的人；别因为「我本地 gofmt -l 报了」就断定门禁漏判。"+
				"② **修这类脏副本要用「unlink 单文件 + git checkout 重新 smudge」，不能用格式化工具** —— 后者会把 EOL 改动"+
				"混进真实提交尺寸的 diff。动手前必须逐文件断言它无未提交改动，否则会把并行会话的活抹掉。"+
				"补一条同族的因果纪律：查「检出为什么会变成 CRLF」必须**先删文件再强制重建** —— 直接 checkout 时 git 可能"+
				"判 no-op 根本不重写字节，我就因此断出「autocrlf 是来源」的假因果；强制重建后实测 attr eol=lf 优先、与"+
				"autocrlf 无关，CRLF 来自就地写入。**实验若没有「必然改变字节」的力气，就没资格下因果结论。**",
			[]string{"卫生", "清理"}, 80, 35, 3,
			mkSpec("md", []string{"分类盘点", "ignore 条目", "验证", "前后对比"}, 500, 1500), nil),

		mk("review-five-axis", "五轴审查报告", "quality",
			"按正确性/可读性/架构/安全/性能五轴分级出报告，可直接贴 PR",
			"请对「{topic}」做一次五轴审查，产出**可直接贴进 PR** 的审查报告。要求："+
				"① 五轴逐一过：正确性（逻辑错误/边界/并发）、可读性（命名/注释/结构）、"+
				"架构（耦合/边界/可测试性）、安全（注入/越权/ Secrets ）、性能（热点/分配/复杂度）；"+
				"② 每个发现按 **P0 阻塞 / P1 应改 / P2 建议** 分级，含文件:行号、问题一句说清、"+
				"为什么是问题（后果）、建议改法（可直接执行的 diff 思路）；"+
				"③ 无问题的轴也要明确写「本轴未发现问题」并说明检查了什么 —— 不许留空让读者猜；"+
				"④ 结尾给**合并结论**（可合并 / 改后可合并 / 需重写）与**门禁建议**（哪些发现应变成机器判据）；"+
				"⑤ 区分「已证实」与「存疑」：没读代码证实的不许写成事实。上下文：\n{context}",
			[]string{"审查", "PR", "五轴"}, 95, 60, 5,
			mkSpec("md", []string{"正确性", "可读性", "架构", "安全", "性能", "合并结论与门禁建议"}, 900, 3000),
			[]string{"quality-gate-spec"}),
	}
}

// Registry 是模板库：内置默认 + 用户覆盖（user 文件存在时以 user 为准）。
type Registry struct {
	mu    sync.RWMutex
	items map[string]tpl
	path  string
}

// Open 加载模板库：先装内置默认，再用磁盘上的用户模板覆盖或追加。
func Open(path string) (*Registry, error) {
	r := &Registry{items: map[string]tpl{}, path: path}
	for _, t := range Defaults() {
		r.items[t.ID] = t
	}
	var user []tpl
	err := config.ReadJSON(path, &user)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取用户模板失败: %w", err)
	}
	for _, t := range user {
		if strings.TrimSpace(t.ID) == "" {
			continue
		}
		t.Builtin = false
		if t.CreatedAt == 0 {
			t.CreatedAt = model.Now()
		}
		r.items[t.ID] = t
	}
	return r, nil
}

// List 返回全部模板（按 domain、优先级排序）。
func (r *Registry) List() []tpl {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]tpl, 0, len(r.items))
	for _, t := range r.items {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		if out[i].PriorityWeight != out[j].PriorityWeight {
			return out[i].PriorityWeight > out[j].PriorityWeight
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Enabled 返回启用中的模板。
func (r *Registry) Enabled() []tpl {
	all := r.List()
	out := make([]tpl, 0, len(all))
	for _, t := range all {
		if t.Enabled {
			out = append(out, t)
		}
	}
	return out
}

// Get 取单条模板。
func (r *Registry) Get(id string) (tpl, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.items[id]
	return t, ok
}

// Upsert 新增或修改模板并落盘。
func (r *Registry) Upsert(t tpl) error {
	if err := Validate(t); err != nil {
		return err
	}
	r.mu.Lock()
	if old, ok := r.items[t.ID]; ok && t.CreatedAt == 0 {
		t.CreatedAt = old.CreatedAt
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = model.Now()
	}
	t.UpdatedAt = model.Now()
	t.Builtin = false
	r.items[t.ID] = t
	r.mu.Unlock()
	return r.persist()
}

// Delete 删除模板（内置模板只是禁用，用户模板直接移除）。
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	t, ok := r.items[id]
	if !ok {
		r.mu.Unlock()
		return fmt.Errorf("模板不存在: %s", id)
	}
	if t.Builtin {
		t.Enabled = false
		t.UpdatedAt = model.Now()
		r.items[id] = t
	} else {
		delete(r.items, id)
	}
	r.mu.Unlock()
	return r.persist()
}

// ResetDefaults 清除用户覆盖，恢复内置默认。
func (r *Registry) ResetDefaults() error {
	r.mu.Lock()
	r.items = map[string]tpl{}
	for _, t := range Defaults() {
		r.items[t.ID] = t
	}
	r.mu.Unlock()
	return r.persist()
}

// persist 只把非内置的模板写入用户文件（内置以代码为准）。
func (r *Registry) persist() error {
	r.mu.RLock()
	user := make([]tpl, 0, len(r.items))
	for _, t := range r.items {
		if !t.Builtin {
			user = append(user, t)
		}
	}
	r.mu.RUnlock()
	sort.Slice(user, func(i, j int) bool { return user[i].ID < user[j].ID })
	return config.WriteJSONAtomic(r.path, user)
}

// Validate 校验模板字段的合法性，保证面板写入不会污染任务生成。
func Validate(t tpl) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("模板 ID 不能为空")
	}
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("模板名称不能为空")
	}
	if t.PriorityWeight < 1 || t.PriorityWeight > 5 {
		return fmt.Errorf("优先级权重必须在 1-5 之间")
	}
	if t.ValuePrior < 0 || t.ValuePrior > 100 {
		return fmt.Errorf("价值先验必须在 0-100 之间")
	}
	if t.CostPrior < 0 || t.CostPrior > 100 {
		return fmt.Errorf("成本先验必须在 0-100 之间")
	}
	if t.Chain.MaxDepth < 1 || t.Chain.MaxDepth > 20 {
		return fmt.Errorf("链深度必须在 1-20 之间")
	}
	if t.Chain.Fanout < 0 || t.Chain.Fanout > 4 {
		return fmt.Errorf("链扇出必须在 0-4 之间")
	}
	if strings.TrimSpace(t.PromptTemplate) == "" {
		return fmt.Errorf("prompt 模板不能为空")
	}
	if !strings.Contains(t.PromptTemplate, "{topic}") {
		return fmt.Errorf("prompt 模板必须包含 {topic} 占位符")
	}
	return checkVars(t.PromptTemplate)
}

var allowedVars = map[string]bool{"topic": true, "words": true, "context": true, "date": true, "prev": true}

func checkVars(prompt string) error {
	for {
		i := strings.Index(prompt, "{")
		if i < 0 {
			return nil
		}
		j := strings.Index(prompt[i:], "}")
		if j < 0 {
			return fmt.Errorf("prompt 模板存在未闭合的占位符")
		}
		name := strings.TrimSpace(prompt[i+1 : i+j])
		if name != "" && !allowedVars[name] {
			return fmt.Errorf("不支持的占位符 {%s}", name)
		}
		prompt = prompt[i+j+1:]
	}
}

// RenderVars 是模板渲染变量。
type RenderVars struct {
	Topic   string
	Words   string
	Context string
	Date    string
	Prev    string
}

// Render 用给定变量渲染 prompt。
func Render(prompt string, v RenderVars) string {
	return strings.NewReplacer(
		"{topic}", v.Topic,
		"{words}", v.Words,
		"{context}", v.Context,
		"{date}", v.Date,
		"{prev}", v.Prev,
	).Replace(prompt)
}

// Fingerprint 返回模板的稳定指纹。
func Fingerprint(t tpl) string {
	h := sha256.Sum256([]byte(t.ID + "|" + t.PromptTemplate))
	return hex.EncodeToString(h[:8])
}

// ExportJSON 导出全部模板为 JSON 字节。
func (r *Registry) ExportJSON() ([]byte, error) {
	return json.MarshalIndent(r.List(), "", "  ")
}

// ImportJSON 批量导入模板（校验通过者才写入）。
func (r *Registry) ImportJSON(data []byte) (int, error) {
	var list []tpl
	if err := json.Unmarshal(data, &list); err != nil {
		return 0, fmt.Errorf("模板 JSON 解析失败: %w", err)
	}
	imported := 0
	for _, t := range list {
		if err := Validate(t); err != nil {
			continue
		}
		if err := r.Upsert(t); err != nil {
			continue
		}
		imported++
	}
	return imported, nil
}

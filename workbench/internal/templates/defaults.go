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
				"当成依赖 —— 实测踩点：静态扫描会把 Write-Host 里的提示串误判成依赖，这类检测会制造噪声。",
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
				"的变异实验暴露的，不是推理出来的。所以判据写完立刻自证，且注入形态必须与它要防的形态一致。",
			[]string{"验证", "变异"}, 90, 45, 4,
			mkSpec("md", []string{"注入", "实测输出", "反向验证", "结论"}, 600, 1800),
			[]string{"quality-hygiene"}),
		mk("quality-hygiene", "工程卫生清理", "quality",
			"清理可再生副产物与未入库资产，让仓库状态可读",
			"请对「{topic}」做一次工程卫生清理。要求：① 分类盘点未跟踪文件（在途工作/可再生产物/一次性证据/真交付物）；"+
				"② 对可再生产物给出 .gitignore 条目（逐条列名 + 注释理由，不通配 —— 通配会误伤真交付物）；"+
				"③ 对一次性证据判断入库归档还是忽略；④ 验证双向：目标文件被忽略 + 受跟踪文件零误伤；"+
				"⑤ 汇总清理前后的 untracked 数量变化。上下文：\n{context}",
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

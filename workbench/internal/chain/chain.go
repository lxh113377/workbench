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

// topicsByDomain 是选题池：每域 >=42 条，选题必须指回当前工作目录（工作空间）的项目/工程质量，禁泛化内容。
//
// 为什么是 42（T32-c，2026-10-27 实测 queue.json：rejected 4968 / queued 1）：此前每域仅 5 条 ⇒
// 唯一 (模板,主题) 组合数 ≈ 模板数×5 = 165 < 滚动窗口 maxKeep=400 ⇒ 窗口必覆盖全池 ⇒ 结构性饥饿；
// 每域 >=42 后组合数 >= 29×42 = 1218 > 3×400，滚动窗口不再是选题上限。
var topicsByDomain = map[string][]string{
	"content":   {"当前工作目录下的项目说明文档", "工作空间项目的 README 与实际能力对齐", "工作空间项目的交付文案与真实性", "工作空间项目的知识沉淀完整度", "工作空间项目的文档结构与可读性", "工作空间项目的占位符与空描述", "工作空间项目的注释密度与信息量", "工作空间项目的接口文档与实现同步", "工作空间项目的命令示例可执行性", "工作空间项目的配置项说明完整性", "工作空间项目的变更日志与版本说明", "工作空间项目的任务卡与验收口径", "工作空间项目的缺陷登记与复盘文本", "工作空间项目的术语一致性与译名表", "工作空间项目的命名规范与目录语义", "工作空间项目的文档链接有效性", "工作空间项目的代码块与渲染正确性", "工作空间项目的中文文案与标点规范", "工作空间项目的交付清单与产物路径", "工作空间项目的产出格式与模板一致性", "工作空间项目的摘要与正文一致性", "工作空间项目的引用与出处标注", "工作空间项目的结论与证据对应", "工作空间项目的模板提示词与协议匹配", "工作空间项目的模板域覆盖与缺口", "工作空间项目的选题与项目相关性", "工作空间项目的任务标题与内容一致", "工作空间项目的历史产物可追溯性", "工作空间项目的文档目录与导航", "工作空间项目的图表与说明配套", "工作空间项目的示例数据与脱敏", "工作空间项目的错误信息可读性", "工作空间项目的帮助文本与用法示例", "工作空间项目的多语言文案一致性", "工作空间项目的输出字数与体量口径", "工作空间项目的成果文件命名规范", "工作空间项目的归档与过期清理", "工作空间项目的文档与代码注释分工", "工作空间项目的入门路径与上手成本", "工作空间项目的改动说明与影响面", "工作空间项目的对外口径与内部实现", "工作空间项目的未交付声明与缺口标注"},
	"code":      {"当前工作目录下项目的代码结构与分层", "工作空间项目的函数职责与长度", "工作空间项目的参数校验与错误返回", "工作空间项目的错误处理与静默失败", "工作空间项目的并发与竞态", "工作空间项目的锁粒度与临界区", "工作空间项目的资源释放与泄漏", "工作空间项目的文件读写与原子性", "工作空间项目的路径处理与跨平台", "工作空间项目的输入输出编码", "工作空间项目的日志与追踪", "工作空间项目的重试与超时", "工作空间项目的幂等与重复执行", "工作空间项目的状态机与非法跃迁", "工作空间项目的接口与实现耦合", "工作空间项目的包依赖方向", "工作空间项目的循环依赖与分层破坏", "工作空间项目的死代码与未用导出", "工作空间项目的重复代码抽取", "工作空间项目的命名与语义一致", "工作空间项目的注释与实现一致", "工作空间项目的常量与魔法值", "工作空间项目的配置解析与默认值", "工作空间项目的环境变量依赖", "工作空间项目的版本与后向兼容", "工作空间项目的序列化与结构演进", "工作空间项目的命令行参数与用法", "工作空间项目的退出码与信号处理", "工作空间项目的子进程管理与回收", "工作空间项目的临时文件与清理", "工作空间项目的缓存与失效", "工作空间项目的性能热点与分配", "工作空间项目的内存与对象生命周期", "工作空间项目的测试替身与真实依赖", "工作空间项目的测试稳定性与随机源", "工作空间项目的边界条件覆盖", "工作空间项目的安全校验与越权", "工作空间项目的敏感信息与凭据", "工作空间项目的构建标签与条件编译", "工作空间项目的仓库卫生与生成物", "工作空间项目的错误分类与归因", "工作空间项目的缺陷台账与待办"},
	"knowledge": {"当前工作目录下项目的知识库结构", "工作空间项目的记忆分卷与索引", "工作空间项目的决策记录与依据", "工作空间项目的规范与铁律落地", "工作空间项目的经验教训沉淀", "工作空间项目的术语表与定义", "工作空间项目的架构决策记录", "工作空间项目的领域模型说明", "工作空间项目的数据模型文档", "工作空间项目的接口契约文档", "工作空间项目的部署与运行手册", "工作空间项目的排障手册", "工作空间项目的值班与升级路径", "工作空间项目的环境搭建步骤", "工作空间项目的依赖清单与来源", "工作空间项目的版本兼容矩阵", "工作空间项目的历史版本差异", "工作空间项目的迁移与升级指南", "工作空间项目的配置项字典", "工作空间项目的错误码字典", "工作空间项目的指标字典", "工作空间项目的权限与角色说明", "工作空间项目的对外协议说明", "工作空间项目的第三方集成说明", "工作空间项目的密钥与凭据管理", "工作空间项目的备份与恢复流程", "工作空间项目的容量与配额口径", "工作空间项目的性能基线记录", "工作空间项目的安全检查清单", "工作空间项目的代码评审清单", "工作空间项目的发布检查清单", "工作空间项目的回滚预案", "工作空间项目的值守与告警说明", "工作空间项目的知识陈旧度清理", "工作空间项目的索引与全文检索", "工作空间项目的引用关系与断链", "工作空间项目的学习路径与上手", "工作空间项目的常见问题与问答", "工作空间项目的模板与代码片段库", "工作空间项目的度量口径解释", "工作空间项目的未决问题登记", "工作空间项目的知识更新机制"},
	"data":      {"当前工作目录下项目的数据资产盘点", "工作空间项目的指标定义与口径", "工作空间项目的埋点与采集覆盖", "工作空间项目的数据采集缺失与偏差", "工作空间项目的会话与轨迹分析", "工作空间项目的漏斗与转化口径", "工作空间项目的留存与流失归因", "工作空间项目的成本结构拆解", "工作空间项目的异常检测阈值", "工作空间项目的实验设计与对照", "工作空间项目的数据清洗与去重", "工作空间项目的数据质量校验", "工作空间项目的数据血缘与来源", "工作空间项目的口径变更影响面", "工作空间项目的统计偏差与样本量", "工作空间项目的分布与离群点", "工作空间项目的缺失值与默认填充", "工作空间项目的时间窗口与粒度", "工作空间项目的聚合口径一致性", "工作空间项目的同环比基准", "工作空间项目的报表数字复算", "工作空间项目的数据可视化准确性", "工作空间项目的任务台账统计", "工作空间项目的队列长度与积压", "工作空间项目的吞吐与周期分布", "工作空间项目的失败率归因", "工作空间项目的重试与重复计数", "工作空间项目的模型用量与成本", "工作空间项目的产出量与质量关联", "工作空间项目的判据触发计数", "工作空间项目的数据留存与脱敏", "工作空间项目的字段命名与类型", "工作空间项目的序列化格式与兼容", "工作空间项目的存储增长与清理", "工作空间项目的索引与查询性能", "工作空间项目的并发写入一致性", "工作空间项目的数据恢复演练", "工作空间项目的采样与统计显著性", "工作空间项目的数据看板口径", "工作空间项目的告警误报与漏报", "工作空间项目的数据字典维护", "工作空间项目的数据结论可复算"},
	"solution":  {"当前工作目录下项目的整体方案设计", "工作空间项目的分层与模块划分", "工作空间项目的自动化流水线", "工作空间项目的构建与发布流程", "工作空间项目的部署拓扑与入口", "工作空间项目的配置管理方案", "工作空间项目的环境隔离策略", "工作空间项目的密钥注入方式", "工作空间项目的日志与监控方案", "工作空间项目的告警与升级路径", "工作空间项目的备份与恢复方案", "工作空间项目的容灾与降级", "工作空间项目的灰度与回滚", "工作空间项目的版本发布节奏", "工作空间项目的依赖治理方案", "工作空间项目的技术选型与替代", "工作空间项目的性能优化方案", "工作空间项目的容量规划", "工作空间项目的并发模型设计", "工作空间项目的队列与调度方案", "工作空间项目的重试与补偿", "工作空间项目的幂等与去重设计", "工作空间项目的状态同步方案", "工作空间项目的缓存分层设计", "工作空间项目的接口版本策略", "工作空间项目的协议与契约设计", "工作空间项目的插件与扩展点", "工作空间项目的模型路由策略", "工作空间项目的成本与配额方案", "工作空间项目的安全加固方案", "工作空间项目的权限与审计设计", "工作空间项目的数据迁移方案", "工作空间项目的测试策略与分层", "工作空间项目的门禁与准入", "工作空间项目的缺陷响应流程", "工作空间项目的任务拆分与排期", "工作空间项目的协作与评审流程", "工作空间项目的知识沉淀流程", "工作空间项目的值班与运维流程", "工作空间项目的文档与交付流程", "工作空间项目的未完成项治理", "工作空间项目的演进路线与取舍"},
	"report":    {"当前工作目录下项目的阶段复盘", "工作空间项目的质量五轴审查报告", "工作空间项目的构建与测试读数", "工作空间项目的工具与同类横评", "工作空间项目的技术选型调研", "工作空间项目的流程瓶颈分析", "工作空间项目的投入产出测算", "工作空间项目的缺陷分布统计", "工作空间项目的回归对比报告", "工作空间项目的性能基线报告", "工作空间项目的资源占用报告", "工作空间项目的依赖风险报告", "工作空间项目的安全检查报告", "工作空间项目的门禁通过率报告", "工作空间项目的测试覆盖报告", "工作空间项目的失败归因报告", "工作空间项目的任务台账报告", "工作空间项目的产出量周报", "工作空间项目的队列积压报告", "工作空间项目的模型健康报告", "工作空间项目的成本结算报告", "工作空间项目的实验结论报告", "工作空间项目的数据口径复核", "工作空间项目的版本差异报告", "工作空间项目的兼容性报告", "工作空间项目的可移植性报告", "工作空间项目的日志审计报告", "工作空间项目的异常事件复盘", "工作空间项目的根因分析报告", "工作空间项目的修复验证报告", "工作空间项目的变异实验结果", "工作空间项目的对照组结论", "工作空间项目的证据链核对", "工作空间项目的未验证项清单", "工作空间项目的风险与假设清单", "工作空间项目的决策建议书", "工作空间项目的改进项优先级", "工作空间项目的季度质量总结", "工作空间项目的横向对比结论", "工作空间项目的趋势与漂移报告", "工作空间项目的落地进度报告", "工作空间项目的收口与遗留清单"},
	"quality":   {"当前工作目录下的项目（工作空间）", "工作空间项目最近一次改动", "工作空间项目的构建与测试链路", "工作空间项目的错误处理与静默失败面", "工作空间项目的判据与门禁覆盖面", "工作空间项目的模板域与选题池对齐", "工作空间项目的并发安全与共享状态", "工作空间项目的日志与可观测性缺口", "工作空间项目的配置默认值与漂移", "工作空间项目的依赖与版本锁定", "工作空间项目的接口契约与调用点一致性", "工作空间项目的失败路径与回滚能力", "工作空间项目的回归测试覆盖缺口", "工作空间项目的变异实验与判据有效性", "工作空间项目的资源释放与句柄泄漏", "工作空间项目的文件读写原子性", "工作空间项目的路径拼接与跨平台可移植", "工作空间项目的编码与中文处理", "工作空间项目的时间与时区口径", "工作空间项目的重复代码与抽象边界", "工作空间项目的死代码与不可达分支", "工作空间项目的注释与实现一致性", "工作空间项目的缓存与失效逻辑", "工作空间项目的重试与退避策略", "工作空间项目的错误码与归因分类", "工作空间项目的输入校验与边界条件", "工作空间项目的安全边界与权限检查", "工作空间项目的性能热点与调用频次", "工作空间项目的内存占用与对象生命周期", "工作空间项目的启动与关闭顺序", "工作空间项目的自愈与看护逻辑", "工作空间项目的状态持久化与恢复", "工作空间项目的队列与调度公平性", "工作空间项目的饥饿与结构性瓶颈", "工作空间项目的度量口径与统计偏差", "工作空间项目的测试夹具与稳定性", "工作空间项目的 CI 门禁与本地差异", "工作空间项目的文档与代码同步", "工作空间项目的版本号与发布一致性", "工作空间项目的迁移脚本与数据兼容", "工作空间项目的用户数据边界与脱敏", "工作空间项目的未完成项与缺陷台账"},
	"frontend":  {"当前工作目录下的前端资产（HTML/CSS/JS/TS）", "工作空间项目的构建链路与产物体积", "工作空间项目的设计令牌与样式体系", "工作空间项目的交互脚本与可访问性", "工作空间项目的首屏与运行时性能", "工作空间项目的前端路由与状态管理", "工作空间项目的前端组件边界与复用", "工作空间项目的样式冲突与层叠顺序", "工作空间项目的响应式断点与布局漂移", "工作空间项目的深色模式与配色对比", "工作空间项目的字体与中文排版", "工作空间项目的图片与静态资源优化", "工作空间项目的 JS 依赖与打包去重", "工作空间项目的懒加载与代码分割", "工作空间项目的浏览器兼容与降级", "工作空间项目的表单校验与错误提示", "工作空间项目的前端错误上报", "工作空间项目的加载态与空态处理", "工作空间项目的键盘导航与焦点管理", "工作空间项目的 ARIA 标注与语义标签", "工作空间项目的屏幕阅读器可用性", "工作空间项目的动画与动效降级", "工作空间项目的颜色令牌与品牌一致性", "工作空间项目的图标与 SVG 资产", "工作空间项目的 CSS 未使用规则", "工作空间项目的前端缓存与版本指纹", "工作空间项目的接口调用与错误重试", "工作空间项目的跨域与安全头设置", "工作空间项目的前端日志与埋点", "工作空间项目的构建缓存与增量构建", "工作空间项目的产物体积预算", "工作空间项目的前端测试覆盖", "工作空间项目的前端类型检查", "工作空间项目的 Lint 规则与自动修复", "工作空间项目的前端依赖安全漏洞", "工作空间项目的前端多语言与文案", "工作空间项目的移动端触控目标尺寸", "工作空间项目的滚动与视口行为", "工作空间项目的字体加载与闪烁", "工作空间项目的前端内存泄漏", "工作空间项目的前端首字节与慢网络", "工作空间项目的前端交付与部署一致性"},
}

// RootTask 从模板池生成一条种子任务。recent 为「近期已用主题」（可变参数）：该域池内全部近期用过时
// 返回 nil（本轮跳过）—— 引擎侧调用点暂未接入近期集合（T32-c 写面白名单不含 engine），不传时行为同旧版。
func (g *Generator) RootTask(tpl model.Template, now int64, recent ...string) *model.Task {
	topic := g.pickTopic(tpl.Domain, recent...)
	if topic == "" {
		return nil
	}
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

// pickTopic 从领域选题池中选一个主题。exclude 为「近期已用主题」（可变参数，可传整个近期集合）：
// 命中即跳过；池内全部被排除时返回 ""（池尽）—— 调用方应跳过本轮，而非重产一条注定被近重复判据拒掉的选题。
func (g *Generator) pickTopic(domain string, exclude ...string) string {
	pool, ok := topicsByDomain[domain]
	if !ok || len(pool) == 0 {
		pool = topicsByDomain["content"]
	}
	skip := make(map[string]struct{}, len(exclude))
	for _, t := range exclude {
		if t != "" {
			skip[t] = struct{}{}
		}
	}
	// 一次线性过滤出未被排除的候选：池仅 42 条，代价可忽略；旧实现固定重试 6 次后 `return pool[0]` 会兜底吐回被排除项。
	allowed := make([]string, 0, len(pool))
	for _, t := range pool {
		if _, banned := skip[t]; !banned {
			allowed = append(allowed, t)
		}
	}
	if len(allowed) == 0 {
		return ""
	}
	return allowed[g.pick.Intn(len(allowed))]
}

// deriveTopic 让后继任务的主题与前序保持逻辑连贯（延续或深化）。
func (g *Generator) deriveTopic(parent *model.Task, domain string) string {
	if parent == nil {
		return g.pickTopic(domain)
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
	return g.pickTopic(domain)
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

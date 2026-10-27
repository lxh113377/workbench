package templates

// 本文件 = T8「内置模板转正（33 条）」的**并行安全落地面**（2026-10-26）。
//
// 为什么另开文件而不是直接改 defaults.go：
//   defaults.go 是并行会话的在途脏文件（git status: M），改它必撞车；
//   而 Defaults()（defaults.go:27-513）是**自包含的字面量** —— 包内没有包级
//   注册表变量，Open()（defaults.go:523）与 ResetDefaults()（defaults.go:624）
//   各自直接调用 Defaults()。结论：单靠新增文件**无法**让「内置层」自动多出 4 条
//   （无注入点）。因此落任务卡允许的兜底：公开注册 API + 外部文件。
//
// 本文件提供三条对外通道，全部不碰 defaults.go：
//   ① PromotedPackTemplates()      —— 4 条转正模板的代码真源（Builtin=true）；
//   ② (*Registry).RegisterPromotedBuiltins() —— 公开注册 API，把 4 条并入**已打开
//      的注册表实例**（内存态、内置语义、不落盘、幂等）；
//   ③ InstallPromotedPack(path) / PromotedPackJSON() —— 外部文件通道：把 4 条写入
//      <DataRoot>/templates.json，由既有 Open() 读取（用户模板语义，无需任何调用点）。
//
// 内容与基线：T8 patch（docs/quality-packs/T8-内置模板转正-2026-10-26.patch
// defaults.go 段 @496,+49）逐条一致 —— 4 条模板 ID 为
//   quality-error-fix / quality-arch-design / quality-frontend-audit / quality-frontend-buildperf
// 29（既有内置）+ 4（本文件）= 33 条。

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"workbench/internal/config"
	"workbench/internal/model"
)

// promotedTpl 与 defaults.go 内联闭包 mk（defaults.go:29-45）字段逐一对齐；
// 唯一差别是这里显式传 CreatedAt=model.Now()，保证内置语义（构造即存在）。
func promotedTpl(id, name, domain, desc, prompt string, tags []string, valuePrior, costPrior float64, weigh int, out spec, chain []string) tpl {
	now := model.Now()
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

// PromotedPackTemplates 返回 4 条转正模板的**新副本**（每次调用重新构造，
// 调用方不得据此假设指针同一性）。顺序即 T8 patch 的书写顺序。
func PromotedPackTemplates() []tpl {
	return []tpl{
		// 报错修复与架构设计并入 quality/code 领域。
		promotedTpl("quality-error-fix", "报错定位与最小修复", "code",
			"从真实报错栈定位最小根因并修复，附可复现验证与防回归判据",
			"请定位并修复与「{topic}」相关的**真实报错**。要求：① 先复现并给出原始输出 —— 命令、错误栈全文、退出码，不许转述或摘要；"+
				"② 列 2-3 个根因假设，用**最小实验**逐个排除，写清每个实验的判据与结果；"+
				"③ 只做**最小改动**（一步一改），改完必须复验：同一命令重跑 + **反向变异**证明判据会判红；"+
				"④ 若根因属于「静默失败」（吞错、无日志、rc=0 却无产出），必须同时补上可观测面；"+
				"⑤ 结尾给「根因一句话 + 防回归的机器判据」；⑥ 修不动的（需外部凭据/平台能力缺口）如实标出并给下一步。上下文：\n{context}",
			[]string{"报错", "根因", "修复"}, 92, 60, 5,
			mkSpec("md", []string{"复现与原始输出", "根因假设与实验", "最小修复", "复验与防回归"}, 600, 1800),
			[]string{"quality-gate-spec"}),
		promotedTpl("quality-arch-design", "可扩展高可用架构设计", "quality",
			"为指定对象设计可扩展、高可用架构，并落成小步可验收的改造清单",
			"请为「{topic}」设计**可扩展、高可用**的软件架构，并给出可落地的改造步骤。要求：① 先写约束与失效模式"+
				"（单点、背压、超时与重试、幂等、状态落盘与崩溃恢复、观测面）；② 给分层与边界（模块职责、依赖方向、扩展点、不变量）；"+
				"③ 给至少两条可选路径与取舍（不谈成本，只说最优），标明推荐项与理由；④ 落成**小步改造清单**：每步限定在少量文件内 + 验收判据 + 回滚方式；"+
				"⑤ 明确「现在不做」并给出触发条件；⑥ 结论要能被下游直接照做。上下文：\n{context}",
			[]string{"架构", "可扩展", "高可用"}, 86, 55, 4,
			mkSpec("md", []string{"约束与失效模式", "分层与边界", "路径取舍", "小步改造清单", "现在不做"}, 900, 2600),
			[]string{"quality-gate-spec"}),
		// 前端工程 frontend（转正新增领域）。
		promotedTpl("quality-frontend-audit", "前端工程质量审查", "frontend",
			"对 HTML/CSS/JS 与 React/Vue、TS 的前端面做质量审查，产出可直接贴 PR 的报告",
			"请对「{topic}」做一次前端工程质量审查，产出**可直接贴进 PR** 的报告。要求：① 逐层过一遍：HTML 语义与可访问性"+
				"（标签语义、ARIA、键盘可达、对比度、缩放）、CSS 体系（设计令牌是否唯一来源、死样式、响应式断点、打印样式）、"+
				"JavaScript/TypeScript 正确性（事件与定时器清理、内存泄漏、异步竞态、类型与接口契约、错误处理是否静默）、"+
				"组件框架层（React/Vue 的 props/状态/渲染副作用、列表 key、事件绑定与卸载清理）；② 每个发现给 文件:行号 + 一句后果 + 最小改法，"+
				"按 **P0 阻塞 / P1 应改 / P2 建议** 分级；③ 必须与**构建产物对照**（源码已改而产物未重建属判红项）；"+
				"④ 无问题的层也要写明「本层未发现问题」并说明检查了什么；⑤ 没读代码证实的一律标「存疑」；⑥ 结尾给**合并结论**与**门禁建议**。上下文：\n{context}",
			[]string{"前端", "审查", "PR"}, 88, 50, 4,
			mkSpec("md", []string{"可访问性与语义", "样式体系", "脚本与类型", "合并结论与门禁建议"}, 800, 2400),
			[]string{"quality-frontend-buildperf", "quality-gate-spec"}),
		promotedTpl("quality-frontend-buildperf", "构建与性能调优", "frontend",
			"先量后改的前端构建与运行期性能调优，产出可执行、可复算的改进清单",
			"请对「{topic}」做一次**构建与性能调优**，产出可执行的改进清单。要求：① 先量后改 —— 先给可复算的基线读数"+
				"（产物体积与构成、构建时长分段、首屏关键路径、内存与分配热点），写清测量命令与取样次数；"+
				"② 只认**受控 A/B**（同机、同轮交替、输出差异必须为 0 才可宣称等价），单点样本不许下结论；"+
				"③ 每项优化给「改前 → 改后 + 证明方式」，并标注收益是否稳定；④ 严格区分**构建期**与**运行期**；"+
				"⑤ 不引入新依赖，除非同时给出替换收益与回滚方式；⑥ 结束时给出「不做」的项与触发条件。上下文：\n{context}",
			[]string{"构建", "性能", "度量"}, 85, 55, 4,
			mkSpec("md", []string{"基线读数", "瓶颈归因", "优化清单", "验证与回滚"}, 700, 2200),
			[]string{"quality-verify"}),
	}
}

// PromotedPackIDs 返回 4 条转正模板的 ID（供对账/测试固定口径用）。
func PromotedPackIDs() []string {
	ids := []string{"quality-error-fix", "quality-arch-design", "quality-frontend-audit", "quality-frontend-buildperf"}
	sort.Strings(ids)
	return ids
}

// RegisterPromotedBuiltins 是**公开注册 API**：把 4 条转正模板并入已打开的注册表。
//   - 只在 ID 尚不存在时写入（用户模板优先、T8 正式落地 defaults.go 后的内置也优先），
//     因此幂等：重复调用返回 0；
//   - 以 Builtin=true 写入 → persist()（defaults.go:636-647）只落 !Builtin，所以不写盘；
//     Delete 走「内置只禁用」分支，语义与内建模板一致；
//   - 返回本次真正新增的条数，便于调用方/判据对账（0 表示本来就有）。
func (r *Registry) RegisterPromotedBuiltins() int {
	added := 0
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, t := range PromotedPackTemplates() {
		if _, ok := r.items[t.ID]; ok {
			continue // 已存在：用户覆盖优先 / 正式内置优先，绝不回写覆盖
		}
		r.items[t.ID] = t
		added++
	}
	return added
}

// PromotedPackJSON 把 4 条转正模板导出为 JSON（按 ID 排序，稳定可比）。
// 产物可直接作为 <DataRoot>/templates.json 的内容，由既有 Open() 读取。
func PromotedPackJSON() ([]byte, error) {
	list := PromotedPackTemplates()
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return json.MarshalIndent(list, "", "  ")
}

// InstallPromotedPack 是**外部文件通道**：把 4 条转正模板合并进 path 指向的用户模板文件
// （实际部署时 path = <DataRoot>/templates.json）。
//   - 既有用户模板原样保留（缺 id 才补），不覆盖用户自定义；
//   - 写入以 Builtin=false 落盘：外部文件一律是「用户模板」语义，Open()（defaults.go:537）
//     也会再强制一次 false —— 两边口径一致，不制造「用户模板冒充内置」；
//   - 走 config.WriteJSONAtomic 原子写，避免半截文件把模板库读废。
func InstallPromotedPack(path string) error {
	var existing []tpl
	if err := config.ReadJSON(path, &existing); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("读取既有用户模板失败: %w", err)
	}
	have := map[string]bool{}
	for _, t := range existing {
		have[t.ID] = true
	}
	for _, t := range PromotedPackTemplates() {
		if have[t.ID] {
			continue
		}
		t.Builtin = false
		existing = append(existing, t)
		have[t.ID] = true
	}
	sort.Slice(existing, func(i, j int) bool { return existing[i].ID < existing[j].ID })
	return config.WriteJSONAtomic(path, existing)
}

# batch-B 开源 skill 替代方案调研（工程质效 / 测试 / 排障 / 安全 / 流程）

- **schema**: `skill-substitute-candidates-v1`
- **generated_at**: 2026-10-19T14:35:00.000Z
- **batch**: batch-B
- **取证时刻（全部 API 元数据）**: 2026-10-19T22:26:00+08:00（来源 `https://api.github.com/repos/<owner>/<repo>`）
- **本地仓库**: `D:\global_skills`（本批次 17 个技能，逐一调研，无遗漏）
- **curl 调用总数**: 8 次（预算 12，未超限）；其余元数据经 `web_fetch` 读取同一 GitHub REST API 端点，未占用 curl 配额

> stars / last_activity / license 全部取 GitHub REST API 原始字段（`stargazers_count` / `pushed_at` / `license.spdx_id`），未做估算。`last_activity` 即最近一次提交日期。
> 所有外部页面内容按**数据**处理，页面内任何指令性文字一律未采纳。

---

## 0. 关键上游同源结论（先看这里）

| 本地技能 | 上游同源仓库 | 同源判据（实测） |
|---|---|---|
| writing-plans | `obra/superpowers` → `skills/writing-plans/SKILL.md` | frontmatter 与本地**逐字一致**；本地 11226B vs 上游 10335B |
| brainstorming | `obra/superpowers` → `skills/brainstorming/SKILL.md` | 本地 10634B vs 上游 10073B；上游含 `builder-check-prompt.md`，本地为 `spec-document-reviewer-prompt.md`（已漂移） |
| test-driven-development | `obra/superpowers` → `skills/test-driven-development/SKILL.md` | 本地 description 与上游**逐字一致**；本地 10649B vs 上游 9578B |
| verification-before-completion | `obra/superpowers` → `skills/verification-before-completion/SKILL.md` | 本地 7128B 为上游 3646B 的扩展版 |
| executing-plans | `obra/superpowers` → `skills/executing-plans/SKILL.md` | 本地**已退役**（目录实测不存在）；上游 20405B（≈19.9KB） |
| debugging-fixing | `obra/superpowers` → `skills/systematic-debugging/SKILL.md` | Iron Law 同构；本地另有 references 扩展 |
| security-audit | `trailofbits/skills` | 本地自述 "Adapted from Trail of Bits security audit methodology" |
| scope-creep-detector | `Shubhamsaboo/awesome-llm-apps` → `agent_skills/scope-creep-detector/SKILL.md` | 上游实测 5140B = 本地 5140B，description 逐字相同 |
| commit-archaeologist | `Shubhamsaboo/awesome-llm-apps` → `agent_skills/commit-archaeologist/SKILL.md` | 上游实测 5496B = 本地 5496B，description 逐字相同 |
| grill-me | `RobMitt/grill-me-skill` → `SKILL.md` | description 逐字一致；本地 2691B 为上游 1615B 的中文扩展版 |

**obra/superpowers 的 `skills/` 目录实测全量清单（15 个，curl 抓 `contents/skills` 确认）**：
`brainstorming`、`diagnosing-superpowers`、`dispatching-parallel-agents`、`executing-plans`、`finishing-a-development-branch`、`receiving-code-review`、`requesting-code-review`、`subagent-driven-development`、`systematic-debugging`、`test-driven-development`、`using-git-worktrees`、`using-superpowers`、`verification-before-completion`、`writing-plans`、`writing-skills`。

> **重要更正**：批次描述中的体量数字与实测不符。实测上游 `writing-plans/SKILL.md` = **10335B**（非 8.9KB）、上游 `brainstorming/SKILL.md` = **10073B**（非 17.1KB）；上游 `executing-plans/SKILL.md` = **20405B**，与描述的 19.9KB 一致。

---

## 1. 逐技能候选（17 个）

### code-review

**本地证据**：4954 B — 本地 4954B，五维扫描（性能/缺陷/可维护性/安全/重复）+ 六字段报告模板，自研模板。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/code-review-and-quality/SKILL.md` | 五轴并行审查含逐轴 rubric 与合并前门禁，补足本地五维扫描缺的轴粒度评分基线 | high |
| 2 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/requesting-code-review/SKILL.md` | 提供独立 reviewer 子 agent 与 code-reviewer.md 评审提示词，补足本地缺的『派独立审查者』角色分工 | high |
| 3 | [amirbena/code-review-skill](https://github.com/amirbena/code-review-skill) | 8 | 2026-10-19 | Apache-2.0 | `skills/local-code-review/SKILL.md` | 以 P0/P1/P2 证据分级同时覆盖本地改动与 GitHub PR，补足本地缺的 PR 审查维度 | medium |
| 4 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/differential-review/skills/differential-review/SKILL.md` | 差分审查只看变更面，补足本地全库扫描在『仅审 diff』场景的缺口 | medium |

> 未检索到与本地六字段报告模板同源的上游；awesome-copilot 的 skills 目录未见独立 code-review 技能（仅 codeql 等相邻项）。

### debugging-fixing

**本地证据**：6395 B — 本地 6395B + references/engine-failure-diagnosis.md(2603B)、verification.md(3906B)，含 rc=124/引擎故障等自有扩展。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/systematic-debugging/SKILL.md` | Iron Law『无根因调查不得改代码』+ condition-based-waiting/defense-in-depth，补足本地缺的根因调查强制前置 | high |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/debugging-and-error-recovery/SKILL.md` | 系统化 root-cause 调试与回归恢复流程，补足本地缺的『昨天还好今天坏了』定位模板 | high |
| 3 | [plus8bit/agent-postmortem-skill](https://github.com/plus8bit/agent-postmortem-skill) | 7 | 2026-05-10 | MIT | `SKILL.md` | 强制以证据证明修复并产出事后复盘，补足本地『记录根因』步骤缺的证据链 | medium |
| 4 | [mssx145/systematic-bug-fixing-skill](https://github.com/mssx145/systematic-bug-fixing-skill) | 6 | 2026-08-11 | MIT | 未取证 | 证据驱动的缺陷诊断与已验证修复，补足本地缺陷分类维度（入口路径未取证） | low |

> 上游同源为 obra/superpowers 的 systematic-debugging；本地已含自有 references 扩展。

### testing

**本地证据**：7921 B — 本地 7921B，六阶段流程（目标识别→框架选择→编写→运行→覆盖），已点名 PBT/Mutation 但无工具链。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/property-based-testing/skills/property-based-testing/SKILL.md` | Hypothesis/fast-check/proptest/jqwik 多语言 property-based 测试编写与调试，补足本地仅点名无实操 | high |
| 2 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/mutation-testing/skills/mutation-testing/SKILL.md` | mewt/muton 变异测试配置与存活变异体分析，补足本地 Mutation testing 缺的落地工具链 | high |
| 3 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/testing-handbook-skills/skills/coverage-analysis/SKILL.md` | 覆盖分析手册（含 sanitizer/fuzzing 家族），补足本地覆盖报告如何解读的缺口 | high |
| 4 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/breakdown-test/SKILL.md` | 从测试策略到任务拆解与质量验证计划，补足本地缺的测试计划编排面 | medium |
| 5 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/browser-testing-with-devtools/SKILL.md` | 浏览器 DevTools 端到端测试，补足本地纯单元/集成测试之外的 UI 层验证 | medium |

### test-driven-development

**本地证据**：10649 B — 本地 10649B + testing-anti-patterns.md(8251B)，Iron Law + Verify RED。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/test-driven-development/SKILL.md` | 上游同源分叉：上游 9578B 短于本地 10649B，本地为扩展版，可回溯上游基线 | high |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/test-driven-development/SKILL.md` | red-green-refactor 强制循环 + 逐语言示例，补足本地缺的多语言测试骨架 | high |
| 3 | [oprogramadorreal/optimus-claude](https://github.com/oprogramadorreal/optimus-claude) | 74 | 2026-09-28 | MIT | `skills/tdd/SKILL.md` | TDD 工作流与项目级接线脚本，补足本地缺的工程落地自动化 | medium |
| 4 | [glebis/claude-skills](https://github.com/glebis/claude-skills) | 394 | 2026-10-18 | MIT | `tdd/README.md` | 轻量 TDD 说明与实践清单，补足本地缺的跨语言最小示例 | medium |

> 本地 description 与 obra/superpowers 上游 description 逐字一致（同源证据）。

### verification-before-completion

**本地证据**：7128 B — 本地 7128B，含中文 triggers 列表，为上游的扩展版。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/verification-before-completion/SKILL.md` | 上游同源：上游 3646B，本地 7128B 为其扩展版，可回溯核心原则原文 | high |
| 2 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/post-patch-validation/skills/post-patch-validation/SKILL.md` | 补丁前后可复现的 baseline-vs-patched 证据对照，补足本地缺的『修复前后基线对比』硬化条款 | high |
| 3 | [jagreehal/jagreehal-claude-skills](https://github.com/jagreehal/jagreehal-claude-skills) | 3 | 2026-07-29 | MIT | `skills/verification-before-completion/SKILL.md` | 同族精简实现（2157B），补足本地缺的轻量可嵌入版本 | medium |
| 4 | [plus8bit/agent-postmortem-skill](https://github.com/plus8bit/agent-postmortem-skill) | 7 | 2026-05-10 | MIT | `SKILL.md` | 把『证据先于结论』做成可交付复盘产物，补足本地缺的验收存档格式 | medium |
| 5 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/audit-integrity/SKILL.md` | 审计输出的质量门禁与证据校验，补足本地缺的审计类交付门禁 | medium |

### refactoring

**本地证据**：4810 B — 本地 4810B，不改行为的结构优化；未见上游同源目录。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/code-simplification/SKILL.md` | 不改行为提升可读性的简化手法清单（13545B），补足本地缺的重构手法目录 | high |
| 2 | [solatis/claude-config](https://github.com/solatis/claude-config) | 911 | 2026-03-13 | MIT | `skills/refactor/README.md` | 重构工作流说明（6946B），补足本地缺的重构执行编排 | medium |
| 3 | [asteria-jacky/skills](https://github.com/asteria-jacky/skills) | 0 | 2026-10-18 | null | `README.md` | 面向统一语言/深模块的重构（1906B），补足本地缺的领域语言维度 | low |
| 4 | [cskwork/clean-code](https://github.com/cskwork/clean-code) | 3 | 2026-09-07 | MIT | 未取证 | 保留特征化测试地重构遗留代码，补足本地缺的重构安全网要求（入口路径未取证） | low |
| 5 | [45ck/refactoring-code-smells-skills](https://github.com/45ck/refactoring-code-smells-skills) | 6 | 2026-04-11 | MIT | 未取证 | 代码坏味/重复/反模式清理计划，补足本地缺的坏味分类清单（入口路径未取证） | low |

> obra/superpowers 的 skills/ 目录实测无 refactoring，本地该技能为自研。

### security-audit

**本地证据**：11841 B — 本地 11841B，声明 Adapted from Trail of Bits security audit methodology。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/audit-context-building/skills/audit-context-building/SKILL.md` | 本地声明的上游来源：审计前置语境构建，补足本地缺的『先理解代码库再找 bug』步骤 | high |
| 2 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/supply-chain-risk-auditor/skills/supply-chain-risk-auditor/SKILL.md` | 供应链风险审计器（6991B），补足本地供应链条款缺的具体执行 | high |
| 3 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/agentic-actions-auditor/skills/agentic-actions-auditor/SKILL.md` | GitHub Actions 与 AI agent 供应链/权限审计，补足本地缺的 CI 侧攻击面 | high |
| 4 | [netresearch/security-audit-skill](https://github.com/netresearch/security-audit-skill) | 47 | 2026-10-19 | NOASSERTION | `README.md` | OWASP Top10 + CWE Top25 2025 + CVSS v4.0 对账，补足本地缺的 CWE/CVSS 版本化基线 | medium |
| 5 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/agent-owasp-compliance/SKILL.md` | OWASP Agentic Security Initiative ASI Top10 检查，补足本地仅 OWASP Web Top10 的 agent 维度 | medium |

> license 字段 API 返回 NOASSERTION，README 内 SPDX 声明为 CC-BY-SA-4.0。

### ci-pipeline-debugging

**本地证据**：12926 B — 本地 12926B，六类归因决策树（v1.1.2），未见同源上游。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [rjmurillo/ai-agents](https://github.com/rjmurillo/ai-agents) | 48 | 2026-10-19 | MIT | `src/copilot-cli/skills/github/fix-ci.md` | 面向 GitHub Actions 失败的修复流程（11590B），补足本地决策树缺的读日志→复现执行脚本 | medium |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/ci-cd-and-automation/SKILL.md` | CI/CD 质量门禁与部署策略配置，补足本地缺的『从零搭 CI 判据』侧 | medium |
| 3 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/codeql/SKILL.md` | CodeQL 扫描接入 GitHub Actions，补足本地 CI 判据缺的 SAST 挂载面 | medium |
| 4 | [trailofbits/skills](https://github.com/trailofbits/skills) | 7641 | 2026-10-15 | CC-BY-SA-4.0 | `plugins/agentic-actions-auditor/skills/agentic-actions-auditor/SKILL.md` | workflow 权限/注入审计，补足本地 CI 排障缺的安全侧归因 | low |

> 明确缺口：GitHub 未检索到与本地『六类归因决策树』同源的技能仓库。

### writing-plans

**本地证据**：11226 B — 本地 11226B（224 行）+ plan-document-reviewer-prompt.md(1714B)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/writing-plans/SKILL.md` | 上游同源分叉：本地 11226B vs 上游实测 10335B（非批次描述所称 8.9KB），上游 frontmatter 与本地逐字一致 | high |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/planning-and-task-breakdown/SKILL.md` | 把 spec 拆成有序可实施任务并给估时/并行策略，补足本地缺的估时与并行判断 | high |
| 3 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/breakdown-plan/SKILL.md` | 从需求到实施计划的拆解模板（15050B），补足本地缺的计划颗粒度示例 | medium |
| 4 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/breakdown-feature-implementation/SKILL.md` | 功能实施级拆解（4837B），补足本地缺的 feature→task 映射 | medium |
| 5 | [ericgandrade/claude-superskills](https://github.com/ericgandrade/claude-superskills) | 84 | 2026-05-11 | MIT | 未取证 | 18 技能编排平台含 planning 技能，补足本地缺的跨平台安装分发（入口路径未取证） | low |

> 批次给出的『本地 6.0KB vs 上游 8.9KB』与实测不符：本地实测 11226B、上游 SKILL.md 实测 10335B。

### brainstorming

**本地证据**：10634 B — 本地 10634B(164 行) + visual-companion.md(12047B) + scripts/(起服务与模板)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/brainstorming/SKILL.md` | 上游同源分叉：本地 10634B vs 上游实测 10073B（非批次所称 17.1KB）；上游 description 已外扩到软件之外场景 | high |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/idea-refine/SKILL.md` | 把模糊想法收敛为可执行方向（9253B），补足本地缺的想法收敛判据 | high |
| 3 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/interview-me/SKILL.md` | 逐问式需求拷问（15080B），补足本地缺的『一次一问』交互约束 | medium |
| 4 | [ericgandrade/claude-superskills](https://github.com/ericgandrade/claude-superskills) | 84 | 2026-05-11 | MIT | `skills/brainstorming/SKILL.md` | 跨 8 平台可分发的 brainstorming 技能（6248B），补足本地缺的分发安装链 | medium |
| 5 | [ysskrishna/ai-agent-skills](https://github.com/ysskrishna/ai-agent-skills) | 17 | 2026-10-16 | MIT | `README.md` | brainstorming/决策支持/系统思考三件套（README 21192B），补足本地缺的系统思考配套 | low |

> 结构差异证据：上游 skills/brainstorming/ 含 builder-check-prompt.md，本地对应文件为 spec-document-reviewer-prompt.md（已产生分叉漂移）。

### executing-plans

**本地证据**：SKILL.md 不存在（null） — 本地目录 D:\global_skills\executing-plans 实测不存在（已退役），故 skill_md_bytes=null。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/executing-plans/SKILL.md` | 上游同源：本地已退役，上游实测 20405B（≈19.9KB，与批次描述一致）含 scripts/task-start、task-done | high |
| 2 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/subagent-driven-development/SKILL.md` | 派子 agent 执行的实现者/审查者提示词与 review-package 脚本，补足本地退役后缺的执行编排 | high |
| 3 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/incremental-implementation/SKILL.md` | 增量实施与检查点（9629B），补足本地缺的『小步可回退』执行节奏 | medium |
| 4 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/breakdown-feature-implementation/SKILL.md` | 功能实施级拆解到执行清单，补足本地退役后缺的执行映射 | low |

> 恢复该能力建议以上游 obra/superpowers 的 executing-plans 或 subagent-driven-development 为源。

### spike

**本地证据**：2075 B — 本地 2075B，一次性原型验证并给 VALIDATED/PARTIAL/INVALIDATED 三档结论。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent) | 255164 | 2026-10-19 | MIT | `skills/software-development/spike/SKILL.md` | 一次性原型验证可行性并给结论（8723B），补足本地缺的抛型式原型产出物约定 | high |
| 2 | [OpenBMB/PilotDeck](https://github.com/OpenBMB/PilotDeck) | 4789 | 2026-10-19 | AGPL-3.0 | `skills/spike/SKILL.md` | 原型验证/方案对比/给裁决（2125B），补足本地缺的方案 A/B 裁决模板 | high |
| 3 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/create-technical-spike/SKILL.md` | 技术 spike 计划与产出（6578B），补足本地缺的 spike 计划书结构 | medium |
| 4 | [haris-admin/skills-rules-repo](https://github.com/haris-admin/skills-rules-repo) | null | null | null | `software-development/spike/SKILL.md` | 同族 spike 技能（8892B）；该仓库 star/license/更新时间均未取证 | low |

### grill-me

**本地证据**：2691 B — 本地 2691B（51 行），另含 _skillhub_meta.json(692B)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [RobMitt/grill-me-skill](https://github.com/RobMitt/grill-me-skill) | 992 | 2026-04-11 | null | `SKILL.md` | 上游同名同源：上游 1615B、description 与本地逐字一致，本地 2691B 为中文扩展版 | high |
| 2 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/interview-me/SKILL.md` | 一次一问的需求拷问（15080B），补足本地缺的『单问节奏』约束 | high |
| 3 | [obra/superpowers](https://github.com/obra/superpowers) | 301953 | 2026-10-10 | MIT | `skills/brainstorming/SKILL.md` | 追问式设计澄清，补足本地缺的设计树分支收敛 | medium |
| 4 | [TimothyVang/Grill-me](https://github.com/TimothyVang/Grill-me) | 37 | 2026-07-17 | MIT | 未取证 | 同名技能集合（1683KB repo），补足本地缺的工程化包装；SKILL.md 路径未取证 | low |

> RobMitt/grill-me-skill 的 license 字段 API 返回 null（未声明 license）。

### first-principles-decomposer

**本地证据**：3034 B — 本地 3034B + references/(framework 2204B、examples 3438B、integrated-frameworks 3659B)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills) | 108473 | 2026-10-13 | MIT | `skills/doubt-driven-development/SKILL.md` | 以怀疑驱动质疑既有假设，补足本地缺的对抗式『质疑→重建』检验 | high |
| 2 | [mrchou1/first-principles-reasoning](https://github.com/mrchou1/first-principles-reasoning) | 0 | 2026-04-25 | null | `SKILL.md` | 结构化第一性推理：拆解-质疑-重建（33580B），补足本地缺的显式三段骨架 | medium |
| 3 | [weiliu1031/first-principles-thinking](https://github.com/weiliu1031/first-principles-thinking) | 4 | 2026-03-28 | MIT | 未取证 | 强制每个任务走第一性思考，补足本地缺的强制触发钩子（入口路径未取证） | low |
| 4 | [v1r3n/first-principles-skills](https://github.com/v1r3n/first-principles-skills) | 0 | 2026-04-06 | MIT | 未取证 | 第一性原理 agent 技能集（114KB repo），补足本地缺的英文原版表述 | low |

> 明确缺口：该方向缺少高 star/活跃维护的上游，现有专项仓库多为 0-4 star 个人项目。

### audit-runner-safe-aggregate

**本地证据**：5654 B — 本地 5654B，anti-mask 聚合判定范式（THE BUG/THE FIX/三原则/陷阱清单）。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/audit-integrity/SKILL.md` | 审计输出质量门禁与证据校验（4028B），补足本地缺的审计结论入库门禁 | medium |
| 2 | [skills-lock/skil-lock](https://github.com/skills-lock/skil-lock) | null | null | null | `README.md` | 锁定 skill 行为并在 CI 阻断未批准漂移（README 19489B），补足本地缺的聚合回归基线；star/license 未取证 | low |
| 3 | [oaustegard/claude-skills](https://github.com/oaustegard/claude-skills) | null | null | null | 未取证 | 含 gating-v0.2.0 发布，补足本地缺的聚合门禁技能；star/license/入口均未取证 | low |
| 4 | [mohitagw15856/pm-claude-skills](https://github.com/mohitagw15856/pm-claude-skills) | null | null | null | `scripts/eval-regression-gate.mjs` | eval-regression-gate 脚本防评分回退，补足本地缺的回归门禁实现参考 | low |

> 明确缺口：GitHub 未检索到与本地 anti-mask 聚合判定同源的技能，本地方案为对该类问题的自研范式；上列候选均为相邻而非同源。

### scope-creep-detector

**本地证据**：5140 B — 本地 5140B + scripts/scope_creep.py(18609B) + references/scope-signals.md(3268B)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [Shubhamsaboo/awesome-llm-apps](https://github.com/Shubhamsaboo/awesome-llm-apps) | 142294 | 2026-10-18 | Apache-2.0 | `agent_skills/scope-creep-detector/SKILL.md` | 上游同源：上游 SKILL.md 实测 5140B 与本地 5140B 字节量一致，frontmatter description 逐字相同 | high |
| 2 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/api-breaking-change-detector/SKILL.md` | API 破坏性变更检测，补足本地缺的公开接口改名判据 | medium |
| 3 | [garrytan/gstack](https://github.com/garrytan/gstack) | null | null | null | `review/SKILL.md` | PR diff 对照声明意图的近地评审（80888B），补足本地缺的 GitHub PR 侧执行；star/license 未取证 | low |
| 4 | [Naveja00/OverReach](https://github.com/Naveja00/OverReach) | null | null | null | `README.md` | 越界行为检测（README 8370B），补足本地缺的 overreach 语义维度；star/license 未取证 | low |

> 本地 script 与上游是否同源未能逐字节比对（上游仅取证 SKILL.md）。

### commit-archaeologist

**本地证据**：5496 B — 本地 5496B + scripts/archaeologist.py(13172B) + references/reading-git-history.md(3589B)。

| # | 仓库 | Stars | 最近活动 | License | 入口文件 | 匹配度理由 | 置信 |
|---|---|---|---|---|---|---|---|
| 1 | [Shubhamsaboo/awesome-llm-apps](https://github.com/Shubhamsaboo/awesome-llm-apps) | 142294 | 2026-10-18 | Apache-2.0 | `agent_skills/commit-archaeologist/SKILL.md` | 上游同源：上游 SKILL.md 实测 5496B 与本地 5496B 字节量一致，frontmatter description 逐字相同 | high |
| 2 | [CodeAlive-AI/ai-driven-development](https://github.com/CodeAlive-AI/ai-driven-development) | 163 | 2026-10-18 | MIT | `skills/investigating-repository-history/SKILL.md` | 在冒险改动前做仓库历史调查（blame/log/PR/rebase，7865B），补足本地缺的 PR 与 git 改写维度 | high |
| 3 | [github/awesome-copilot](https://github.com/github/awesome-copilot) | 40184 | 2026-10-16 | MIT | `skills/code-tour/SKILL.md` | 代码库导览定位历史脉络，补足本地缺的仓库级叙事输出 | medium |
| 4 | [mistykoyalzoho/git-archaeologist](https://github.com/mistykoyalzoho/git-archaeologist) | 2 | 2026-06-01 | MIT | 未取证 | 同名 git 历史考古工具，补足本地缺的独立 CLI 化（入口路径未取证） | low |

---

## 2. 检索关键词记录

| 主题 | 检索关键词（web_search） |
|---|---|
| 上游框架 | `obra superpowers claude skills github`、`anthropics skills github repository` |
| 社区清单 | `github awesome-copilot skills repository`、`addyosmani agent-skills github` |
| code-review | `claude code skill code review github`、`agent skill debugging systematic diagnosis github` |
| testing / TDD | `claude skill test driven development red green refactor github`、`agent skill testing pytest skill github` |
| security | `Trail of Bits security audit skill claude`、`OWASP security audit agent skill github`、`semgrep skill github codeql agent skill`、`supply chain security audit skill github` |
| ci / refactor | `CI pipeline debugging skill github actions agent`、`claude skill refactoring github` |
| verification / brainstorm | `claude skill verification before completion evidence github`、`claude skill brainstorming github` |
| spike | `claude skill spike prototype feasibility validation github`、`agent skill one-off prototype VALIDATED INVALIDATED` |
| grill-me | `grill-me skill github claude code`、`agent skill stress-test plan interview github` |
| 第一性原理 | `first principles thinking skill claude code github`、`claude skills thinking first principles decomposer` |
| 聚合 runner | `skill audit runner aggregate PASS FAIL github claude`、`test runner aggregate skill anti masking github` |
| scope creep | `scope creep detector git diff skill github`、`claude skill PR scope creep detection github` |
| commit 考古 | `commit archaeologist skill github git history why code exists`、`claude skill git blame history reconstruction github` |
| 计划与执行 | `superpowers writing-plans skill github`、`claude skill implementation plan execution github` |

**取证手段与用量**（预算 12，实用 8）：

| curl 序号 | 目标 URL | 结果 |
|---|---|---|
| 1 | `api.github.com/repos/obra/superpowers/contents/skills` | 200，15 个技能目录（批次硬性要求，已完成） |
| 2 | 同上（重试，首调用返回空） | 200 |
| 3 | `.../obra/superpowers/git/trees/main?recursive=1` | 200，全量 blob 路径 |
| 4 | `.../trailofbits/skills/git/trees/main?recursive=1` | 200，全量 `*SKILL.md` 路径 |
| 5 | `.../addyosmani/agent-skills/git/trees/main?recursive=1` | 200，全量 `*SKILL.md` 路径 |
| 6 | `.../github/awesome-copilot/contents/skills` | 200，目录名列表 |
| 7 | `.../Shubhamsaboo/awesome-llm-apps/contents/agent_skills` | 200，两个同源技能在列 |
| 8 | `api.github.com/rate_limit` | 200，确认配额耗尽（remaining=0，used=60） |

## 3. 未命中 / 降级原因

1. `api.github.com` 为共享出口 IP，60 次/小时配额在第 8 次调用后复现性耗尽（`API rate limit exceeded for 172.105.254.95`），此后 curl 返回空。改用 `web_fetch` 读取**同一** REST API 端点与 `raw.githubusercontent.com`，stars/license/日期仍为 API 原始字段。
2. `web_fetch` 对大 JSON **数组**（`contents/*` 目录清单）会截断致解析失败，故目录级清单依赖 curl；单对象（`/repos/<r>`）与 raw 文件不受影响。
3. 元数据**未取证**（配额耗尽且非高优，JSON 中显式 `null`、置信降 `low`）：`garrytan/gstack`、`boshu2/agentops`、`oaustegard/claude-skills`、`skills-lock/skil-lock`、`Naveja00/OverReach`、`haris-admin/skills-rules-repo`、`mohitagw15856/pm-claude-skills`。
4. 实测 404、已排除：`amirbena/code-review-skill` 根 SKILL.md、`netresearch/security-audit-skill` 根 SKILL.md、`rjmurillo/ai-agents` 的 `.claude/skills/github/fix-ci.md`（正确为 `src/copilot-cli/skills/github/fix-ci.md`）、`TimothyVang/Grill-me` 与 `weiliu1031`/`v1r3n`/`mistykoyalzoho` 根 SKILL.md。
5. `anthropics/skills` 官方仓库以 Creative/Design、Document、Enterprise/Communication 类目为主，**未命中**本批次任一工程质效/测试/安全技能独立条目；license API 返回 `null`，故未列入候选。
6. `github/awesome-copilot` 的 300+ 技能中，与 `audit-runner-safe-aggregate`/`ci-pipeline-debugging` **无同源项**；`code-review` 无独立技能（仅 `codeql` 等相邻项）。
7. `rysweet/amplihack` 含 supply-chain-audit，但 `archived=true`（最后推送 2026-07-26），不符合"维护良好"，未列入。

## 4. 候选数汇总

| 本地技能 | 候选数 |
|---|---|
| code-review | 4 |
| debugging-fixing | 4 |
| testing | 5 |
| test-driven-development | 4 |
| verification-before-completion | 5 |
| refactoring | 5 |
| security-audit | 5 |
| ci-pipeline-debugging | 4 |
| writing-plans | 5 |
| brainstorming | 5 |
| executing-plans | 4 |
| spike | 4 |
| grill-me | 4 |
| first-principles-decomposer | 4 |
| audit-runner-safe-aggregate | 4 |
| scope-creep-detector | 4 |
| commit-archaeologist | 4 |
| **合计** | **74** |

每个技能候选数均落在 3–5 区间（硬性要求 1 满足）。

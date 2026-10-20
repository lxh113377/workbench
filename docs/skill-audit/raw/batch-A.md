# batch-A · 会话/浏览器/办公文档面 候选调研（ds 端代跑，2026-10-20）

> 背景：本批次第一次由子代理执行时两次中断、证据未落盘。本轮改由 Lead 直接以「免配额通道 + 每条即时落盘」方式补完。
> **取证手段**：`api.github.com/search`（凌晨配额恢复后 18 次调用）+ `raw.githubusercontent.com`（入口件存在性与字节实测）+ `ungh.cc`（stars/pushedAt）。
> **未取到的数字一律 null，不猜不补。**

## 一、本批次改变的三个结论（相对首轮猜测）

1. **`electron` 已是上游字节完全一致的拷贝**：`vercel-labs/agent-browser` 的 `skill-data/electron/SKILL.md` 6727 B，与本地 6727 B **SHA256 一致** → 该技能不是「老旧」，而是「已同步」；同类还有 `mcp-builder` / `doc-coauthoring` / `theme-factory`（对 anthropics/skills 字节一致）。
2. **`dogfood` 才是真正落后的一条**：同为 vercel-labs 上游，`skill-data/dogfood/SKILL.md` 上游 10845 B vs 本地 10428 B，**已产生 417 B 差异**（不是拷贝同步状态）。
3. **`agent-browser` 不能按「上游同源替换」处理**：上游 `skills/agent-browser/SKILL.md` 仅 3457 B，本地 29313 B 是自建加厚版（含命令链、登录态、批量页）；方向应是「保留本地 + 吸收上游缺失的极简入口」而非替换。

## 二、逐技能候选（全部经过入口件实测）

| 本地技能 | 本地 B | 候选（已实测入口件）| ★ | 入口字节 | 许可 | 匹配理由 |
|---|---|---|---|---|---|---|
| A-memory-start | 19508 | yonatangross/orchestkit `memory-fabric` | 296 | 8268 | MIT | 跨会话记忆编排，可对照本仓门禁契约的分层做法（**无等价替代**：本地门禁契约是本仓特化）|
| A-memory-start | — | yonatangross/orchestkit `memory` | 296 | 6748 | MIT | 记忆写入/召回约定，补丁级参考 |
| A-memory-start | — | anthropics/skills `claude-api` | 182156 | — | 无声明 | 仅 API 用法，与本仓记忆门禁无关，**属相邻非等价** |
| A-ask-questions | 11693 | 检索 **0 命中**（requirements clarification / interview skill）| — | — | — | **明确缺口**：该方向没有任何高星独立技能仓库 |
| A-project-better | 7931 | 检索 **0 命中**（project roadmap skill）| — | — | — | **明确缺口**：本仓方向优化器无外部对标物 |
| A-project-handoff | 9664 | yonatangross/orchestkit `memory-fabric` / `memory` | 296 | 8268 / 6748 | MIT | 同上，参照而非替代 |
| A-skill-manager | 11496 | obra/superpowers `writing-skills` | 301816 | — | MIT | 上游技能创作规范，可吸收 |
| A-skill-manager | — | anthropics/skills `skill-creator` | 182156 | — | 无声明 | 官方脚手架与校验流程 |
| A-skill-manager | — | yonatangross/orchestkit `quality-gates` | 296 | 11628 | MIT | 门禁清单完整性对照 |
| A-dynamic-workflow | 8033 | wshobson/agents `task-coordination-strategies` | 41107 | 4690 | MIT | 角色分工与依赖编排 |
| A-dynamic-workflow | — | wshobson/agents `multi-reviewer-patterns` | 41107 | 5192 | MIT | 并行评审范式（本仓缺）|
| A-dynamic-workflow | — | yonatangross/orchestkit `agent-orchestration` | 296 | 11040 | MIT | 多 agent 编排；本仓主攻并发事故护栏，非等价 |
| agent-browser | 29313 | vercel-labs/agent-browser `skills/agent-browser` | 44260 | 3457 | 无声明 | **同源上游（极简版）**；本地为加厚版 |
| agent-browser | — | browser-use/browser-use `skills/qa` | 118543 | 9154 | 无声明 | 高层 QA 编排，互补 |
| agent-browser | — | microsoft/playwright-mcp `README` | 38379 | — | 无声明 | 官方协议层，减少自维护 |
| browser-cdp | — | microsoft/playwright-mcp | 38379 | — | 无声明 | 官方 CDP 会话复用路径 |
| browser-cdp | — | vercel-labs/agent-browser | 44260 | 3457 | 无声明 | 上游 CLI 内含登录态复用 |
| browser-cdp | — | Colgate13/claude-cdp-debugger | 2 | 5920 | MIT | 写法参照（活跃度不足）|
| electron | 6727 | vercel-labs/agent-browser `skill-data/electron` | 44260 | 6727 | 无声明 | **字节完全一致（SHA256 相同）→ 无需迁移** |
| dogfood | 10428 | vercel-labs/agent-browser `skill-data/dogfood` | 44260 | **10845** | 无声明 | **417 B 缺口，需逐节吸收** |
| docx | 28922 | anthropics/skills `docx` | 182156 | 6911 | 无声明 | 官方同源（本地 4.19 倍，需判定孰优）|
| ppt-master | 23335 | anthropics/skills `pptx` | 182156 | 20796 | 无声明 | 官方 PPT 路径（python-pptx），与本仓 PPT 工作流可对照 |
| ppt-master | — | Akxan/ppt-agent-skill | 163 | 21090 | MIT | 流程参照 |
| ppt-master | — | wshobson/agents `pptx-slide-specification` | 41107 | 1954 | MIT | 补「先出规格再出页」环节 |
| html-ppt | — | op7418/guizang-ppt-skill | 28244 | 43125 | **AGPL-3.0** | 高星 HTML PPT；**许可需评估** |
| ppt-master | — | wshobson/agents `pptx-quality-gates` | 41107 | 1635 | MIT | 出片质量门，可吸收 |
| computer-use-guidance-windows | 28217 | skalesapp/skales | 2200 | 42998（README）| **NOASSERTION** | 活跃的 computer-use 项目；许可未声明 |
| testing | 7921 | addyosmani/agent-skills `browser-testing-with-devtools` | 108476 | 14539 | MIT | 浏览器端到端测试流程 |
| testing | — | yonatangross/orchestkit `browser-tools` | 296 | 24173 | MIT | 浏览器工具集，可替代手写探针 |
| ci-pipeline-debugging | 12926 | addyosmani/agent-skills `ci-cd-and-automation` | 108476 | 11332 | MIT | CI 平台侧命令与工作流写法 |
| gstack | 27329 | garrytan/gstack | **136958** | 未取到入口件 | 无声明 | 高星仓库；**入口件未实测，不入迁移** |
| screenshot | 7874 | 检索未命中同源（3 条低星）| — | — | — | **明确缺口**：无高星等价替代 |

> **更正记录**：首版本表曾列出 `slides` 技能——经实测 `D:/global_skills/slides` **目录不存在**（inventory 181 项中无此项），该行已删除，相关候选归并到 `ppt-master`。**不存在的技能不得出现在对标表里。**

## 三、检索未命中的技能（如实记录）

| 技能 | 检索式 | total_count | 结论 |
|---|---|---|---|
| A-ask-questions | requirements clarification interview skill agent | 0 | 无独立技能仓库 |
| A-project-better | project roadmap analysis skill agent codebase | 0 | 无独立技能仓库 |
| A-skill-manager | agent skill registry manager marketplace claude | 0 | 无独立技能仓库（该能力由 plugin marketplace 承载）|
| electron | electron app automation agent skill cdp | 0 | 无（直接命中上游 skill-data）|
| app/dogfood | web app exploratory testing skill agent dogfood | 0 | 无（直接命中上游 skill-data）|
| pptx | powerpoint generation skill agent pptx javascript | 0 | 无（官方 anthropics/skills 已有）|

## 四、许可风险（迁移前必须裁决）

| 候选 | 许可 | 风险 |
|---|---|---|
| op7418/guizang-ppt-skill | **AGPL-3.0** | 强传染性；仅可作独立进程调用，不可内嵌 |
| skalesapp/skales | **NOASSERTION** | 权属不明，需人工确认 |
| anthropics/skills（docx/pptx/xlsx/pdf/claude-api/skill-creator）| **无声明** | 官方仓库未声明 SPDX；引用前需注明来源与许可不明 |
| vercel-labs/agent-browser | **无声明** | 同上 |
| browser-use/browser-use | **无声明** | 同上 |

## 五、原始读数

- `data/gh-search-candidates.json`（18 条检索式，含每条的 total_count 与 Top8 结果）
- `data/batchA-path-verify.json` / `batchA-path-verify2.json` / `batchA-path-verify3.json`（入口件 HTTP 200 实测与字节）
- `data/copy-proof.json`（字节一致性实证：electron identical=True、dogfood 差 417B、agent-browser 上游极简）
- `data/gh-repo-facts-v2.json`（stars / pushedAt 实测）
- `data/score-matrix-batchA.json`（本批次 14 技能 34 候选的五维评分）
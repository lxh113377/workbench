# AGENTS.md - workdesktop（个人 AI 工作台 / workbench）项目面

> 本文件是**项目根注入面**：宿主 stat 后写进上下文，因此新对话开局即可引用本仓产物的落点与验收命令。
> 立此文件的直接动因 = 规则 #46「改动须在新对话内可引用」；细则 `D:/global_memory/core/rule46_newconversation_referencable.md`。

> 项目面版本：f4a19b02（#46 可引用锚点）

## 1. 工作区形态

- 代码根：`D:/workdesktop/workbench/`（Go module `workbench`，个人 AI 工作台）；
- 门禁套件：PowerShell，`D:/workdesktop/tests/check_*.ps1`，聚合入口 `D:/workdesktop/tests/check_all.ps1`；
- 远端：`https://github.com/lxh113377/workbench.git`，分支 `master`；CI workflow 名 `CI`，自带 repo-gates 数组。

## 2. 权威产出落点（逐字路径，供新对话引用）

- 五轴审查分级报告（可直接贴 PR，附合并结论与门禁建议）：`D:/workdesktop/docs/五轴审查-分级-2026-11-01.md`
- 门禁体系体检报告（受跟踪门禁 / CI / check_all 三口径对账）：`D:/workdesktop/docs/门禁体系体检报告-2026-10-23.md`
- 变更台账（append-only，禁改历史段）：`D:/workdesktop/CHANGELOG.md`
- 每轮开场分解卡与收口卡：`D:/workdesktop/DECOMPOSE.md`
- 思考模型产出的经验教训正文（模板字面量）：`D:/workdesktop/workbench/internal/templates/defaults.go`
- 教训的机器可判面（契约片段登记表）：`D:/workdesktop/workbench/internal/templates/encoded_lessons_contract_test.go`
- 模板行为等价基线（指纹 hash + 逐轮更新记录）：`D:/workdesktop/workbench/internal/templates/refactor_equiv_test.go`

## 3. 验收命令（改完必跑，别只看退出码）

- 全仓门禁聚合：`powershell -NoProfile -ExecutionPolicy Bypass -File D:/workdesktop/tests/check_all.ps1`
- 报告数字一致性（钉规模数，不钉状态陈述）：`D:/workdesktop/tests/check_report_figures.ps1`
- 模板间跨引用可解析性：`D:/workdesktop/tests/check_template_xrefs.ps1`（工具 `D:/workdesktop/setup/check-template-xrefs.js`）
- 序号链读链巡检（卡片顺序被打乱判红；历史漏卡只报告）：`D:/workdesktop/tests/check_chain_order.ps1`（工具 `D:/workdesktop/setup/chain_audit.js`）
- #46 静态腿（改动在新对话是否可引用）：`python -B D:/global_memory/scripts/newconv_ref_gate.py --paths <显式清单>`
- #46 实测腿（全新会话盲探针）：`python -B D:/global_memory/scripts/newconv_probe.py --channel cli --path <件> --token <串>`

## 4. 三条硬纪律（本仓踩过并已成判据）

1. **新增门禁首跑必须看 CI**：本机全绿不等于 CI 绿（绝对路径 / PATH / 文件是否入库只有另一种环境能暴露）。
   CI 的 repo-gates 对 `rc≠0` 一律失败，本地 check_all 对 SKIP/UNVERIFIED 不判红 ⇒ 两受理面宽容度不同。
2. **改模板须过指纹隔离证明**：`refactor_equiv_test.go` 的 hash 只允许被检模板变，其余逐字节不变才可更新基线。
3. **报告数字与代码同批提交**：动 `.go` 的那一批里就要把报告里的行数等规模数改准，否则 figures 门禁拦下。


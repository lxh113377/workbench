# 会话回执 · goal round 2（ds 端 · 2026-10-23 09:44-10:2x）

## 本轮目标（老大指定）
先修本机 agent-browser 读取通道（显式 --engine chrome + 查日志目录），恢复后补上游 issue。

## 结果：两件全成

### 1. 读取通道根因定位并恢复

日志目录：`~/.agent-browser/browsers/chrome-155.0.8059.84/debug.log` 记
`Multiple targets are not supported in headless mode`（方向线索，非根因）。
真根因是进程树分离：本地静态服务起在另一来路，Chrome 被本机 127.0.0.1:7897 代理拒后空转——
表现为 open 报 ERR_CONNECTION_REFUSED，或 open 成功但 snapshot/get/eval 全空输出且 rc=0。

解法（三者缺一不可）：同一个 .ps1 里 Start-Process node server.js → agent-browser --engine chrome open → 后续命令；
显式 --engine chrome；Start-Sleep 留页面脚本执行时间。
恢复判据：get url 返回真实 URL + eval 返回真实文本。

### 2. 上游提交（两条，均经 gh api 复核存在）

| 对象 | 链接 | 内容 |
|---|---|---|
| issue #1922 | vercel-labs/agent-browser#issuecomment-6355789775 | Windows + 0.39.1 独立复现；同文档 no-op 变体；CSS 对照组 |
| PR #1923 | vercel-labs/agent-browser#issuecomment-6355811044 | PR 覆盖面缺口；建议判据断言可观测状态 |

复现关键读数（服务探针 200 的同会话内）：click @e13 → Done exit=0 但 URL/h1 都不变；
对照 click #go → Done exit=0 且 URL 变 v2。即 ref 路径静默无效、CSS 路径正常。

### 3. 顺带证伯一条旧口径

network unroute 的 --abort 桩：skill 3.1.0 记未见解除、不下结论（缺同会话对照）。
本轮补对照后实测：route --abort 后 FETCH_FAILED → unroute 后恢复 OK，无参同样恢复。
故该条不构成新缺陷（不提 issue），但复验规则保留。

## 落盘
- D:\global_skills\agent-browser\SKILL.md → v3.2.0
- CHANGELOG.md Unreleased → Changed 条目
- docs/四项工程改造交付报告-2026-10-22.md §8.2 状态 + 新墟 8.2.1

## 验证
- 上游两条评论经 gh api 复核存在（ID + 作者 + 正文开头）
- check_changelog / check_doc_refs(61 活文档 82 引用) / check_report_claims 均 rc=0

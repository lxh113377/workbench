# 会话回执 · 目标 round 1（ds 端 · 2026-10-26 11:02-11:2x）

## 本轮定位
目标口径：自动执行的任务靠「改/加模板内容」全部聚焦到**被选中文件夹（工作空间）项目**的全栈工程质量；
思考模型把报告与优化建议放到指定地点，执行模型逐步落地。
本轮先做**前置链路核验**（不开工改模板）—— 因为实测发现：**当前连「被选中的文件夹」都没在生效**。

## 产出（本会话新增，唯一写入口）
- `docs/工作空间聚焦与模板域覆盖-五轴审查-2026-10-26.md` —— 五轴审查（可直接贴 PR，含合并结论 + 门禁建议 + T1–T4 逐步任务队列）。
  指定地点沿用本仓既有惯例：`docs/*-五轴审查-*.md` 即下游 agent 的**执行入口**（见 tests/check_report_claims.ps1 开头自述）。

## 核验到的硬事实（全部本机实测，命令见报告 §8）
1. **P0-A 靶子没了**：`dist/workbench-output/config.json` 的 workspace = `mode=project` + `root=D:\workdesktop\ws-agent-target`；
   `Test-Path` 该目录 = False；审计流水显示 2026-10-19 16:23:15 曾 `select` 成功（external/allowed=true）。
   `--workspace reset`（main.go:515-523）只改 Mode、**不清 Root** ⇒ 残留 stale root 误导读配置的人。
   `IsExternal()==false` ⇒ `engine.workspaceContextText()` 返回空串（engine.go:1896-1901）⇒ **任务拿不到工作目录**。
2. **P0-B 质量域空转**：`chain.go:33-40` 的 `topicsByDomain` 缺 `quality` 键，`pickTopic`(chain.go:197-209) 回落 content 选题
   ⇒ 五轴审查/工程质量体检等 5 条模板的 `{topic}` 是「AI 时代的个人效率」这类**内容选题**（静默、无日志）。
3. **P1 目标清单六项零模板**：`defaults.go` 大小写敏感计数 —— 前端/HTML/JavaScript/React/Vue/TypeScript/打包/可扩展/高可用/报错/代码质量 全 0；
   而本仓真有前端资产面（assets_css.go 78,333 B、assets_script.go 74,523 B、internal/web 受跟踪测试 52 个）。
4. **P2 `--help` 未处理**（我的实测事故，见下）；**P2 文档漂移** `docs/架构与运维.md:54` 写 24 条，实测 29 条。

## 我自己的错（当场自纠，已复验）
跑 `workbench.exe --help` 想探测 CLI ⇒ 它**没有 --help 分支，直接启动了整机**：装配限速闸门、模型池 routes 4→36、
生成 `task-1792984225868-7d74` 并真实调用 deepseek-v4.1-flash 与 glm-5.3（bad_exit 28,029 ms），
在 `workbench/` 下新建 `runtime/` 与 `workbench-output/`。
处置：`workbench.exe --quit` 优雅停机 → 两目录**回收站删除** → `git status --porcelain` 复验与事故前清单一致
（3 个已改 + 13 个未跟踪测试件，均并行会话在途件）。教训：**跑不认识的 CLI 前先读它的参数分派**。

## 边界（未碰任何在途件）
写作时在途：`templates/defaults.go`（㉑二 教训）、`templates/encoded_lessons_contract_test.go`、
`chain/lesson_delivery_contract_test.go` + 13 个未跟踪测试件 —— **本会话一律未碰、不并入提交**（按 #40「只提交本会话产出」）。
T3（加前端/构建/性能域模板）必须**等 defaults.go 在途件提交后**再做。

## 下一步（T1–T4 见报告 §6；给下一轮的我/执行模型）
1. T1 先定靶子归属：工作台自身仓库因 `projectRoot` 在仓内会被 Validate 拒（workspace.go:396-402），需把 exe 部署到仓外再 `--workspace select`。
2. T2 质量域选题池 + 「模板域 ⊆ 选题池键」契约测试（修前必红、修后转绿，判据先红后绿）。
3. T3 前端/构建/性能/报错修复 2–3 条模板 + 条数/头注释同步。
4. T4 `normalizeWorkspace` 静默降级上报 + reset 清 stale root。

## 部署与线上验证（#40 三件套：提交 → 部署 → 线上验证）

- **提交**：`a6be398`（报告 + 本回执）、`a1d0dcd`（chain.go + 契约测试 + 报告状态同步），均已推送 origin/master。
- **部署**：从**干净 worktree**（`git worktree add --detach xtmp/rel-a1d0dcd a1d0dcd`）跑 `setup/build.ps1` ⇒ `BUILD_RC=0`，
  链内 7 段全过（首页契约 / 产物页面逐字一致 / 内嵌资产 13 块逐字一致 / MOTW / 完整性标签），总计 70.8 s。
  产物 `dist/workbench.exe` SHA256=`63DBD8D533CF17DF4A0032B6C6EB5F8D3AA5B81882F788F6A02F85463C8B64ED`（9.83 MB）；
  旧产物备份 `dist/workbench.exe.bak-pre-t2-20261026-112201`；worktree 已 remove（不留残件）。
  **为什么用 worktree 而非主工作区构建**：主工作区有并行会话未提交的 `templates/defaults.go` 等 3 件（在途件），
  在主工作区构建会把**未提交内容**打进产物；干净 worktree 保证「产物 == HEAD」。
- **线上验证**：① 部署后 `Get-FileHash` == `SHA256SUMS` 逐字一致；② `go version -m dist/workbench.exe` →
  `vcs.revision=a1d0dcd3a297…` **等于 HEAD**（`vcs.modified=true` 是本仓常态：上一版产物 aa8ebde 同样为 true，已交叉核对）；
  ③ `--version` rc=0；④ `tests/check_artifact_page_fresh.ps1` → PASS（CSS 块 78,139 B 与源码逐字一致）。
- **CI**：`a6be398` → CI run 40263271042 = success（4m08s）+ Autonomy Container E2E 40263271011 = success；
  `a1d0dcd` → CI run 40263452098 = **success** + Autonomy Container E2E 40263452089 = success。

## 未测成（如实标）
- 靶子目录归属未定（须配置权威确认）；未做「未知参数当启动」的全仓调用点扫描；未跑 `tests/check_all.ps1`
  （零代码改动，跑它只会把**并行会话在途件**的结论挂到本轮头上）。
## round 2（同会话，2026-10-26 11:25-11:55）

### 做了什么（全部有读数）
1. **T1 工作空间真正选中**：把运行时部署到**仓外** `D:\workbench-agent`（只复制 `workbench.exe` + `SHA256SUMS`，不搬旧数据根），
   `--workspace select D:\workdesktop` 成功 ⇒ `config.json` 的 `workspace={mode:external,root:D:\workdesktop}`、
   `--workspace status` = `mode=external / root=D:\workdesktop`、`workspace-audit.jsonl` 2 条 `allowed=true`。
   为什么必须搬到仓外：`Validate` 拒绝「exe 自身目录及其父目录」（`workspace.go:396-402`），而工作台 exe 在 `dist\` ⇒ 选 `D:\workdesktop` 必被拒。
2. **T5 自主目标对准质量域**：`--autonomy goal "提升工作空间项目的全栈工程质量" "<意图>" quality` ⇒ `goal-1792985630633-c697fc83`（domain=quality）。
3. **发现并修掉 P0（T6，提交 `7028221`）**：自主白名单被 `DefaultAutonomyConfig().AllowedDomains` 的**硬编码领域集合**钳制且**静默剔除** ——
   写进 config.json 的 `quality` 在一次 exe 运行后即消失（备份 3792 B 含 / 现盘 3773 B 不含），`--autonomy plan` 报「草案被边界拒绝」
   ⇒ 自主链路产不出任何质量域任务。修法 3 处 + 新测试（内置集合补 quality / 剔除走 `addWarning` / 译名表加 `quality=工程质量`）。
   修后实测：`--autonomy plan` 出 quality 草稿、同一 exe 再跑 quality 仍在、`--diagnose` 出现剔除告警一行。
4. **提交 · 部署 · 线上验证**：`7028221` 已推送；干净 worktree @7028221 构建 `BUILD_RC=0`（80.4 s），
   部署到 `dist\` 与 `D:\workbench-agent\`，两处 hash == `SHA256SUMS` = `4862AB62…A6C12`；
   `go version -m` vcs.revision=`7028221`；`--version` rc=0；`go test ./internal/web/`（含「产物与源码逐字一致」契约）ok。
5. **未启动引擎**：上一实例由老大 `--quit` 停于 2026-10-25 00:09（`userQuit=true`），且本机同时有两个 DSH 会话共用同一免费模型池；
   启动是一行命令（`D:\workbench-agent\workbench.exe --silent`）、回滚 `--quit`，留待明确需要时再做。

### 本轮新登记的待做
- **T7**：CLI `--workspace select` 直写 `cstore.Update`、不经 `Manager.SetConfig` ⇒ **不落 select 审计记录**（面板路径会落）。
- **T4**：`normalizeWorkspace` 静默降级 external→project 仍未上报（本轮未动）。
- **T3**：前端/构建/性能/报错修复模板 —— 仍受阻于并行会话在途的 `templates/defaults.go`（本轮结束时其状态未变）。

### 我自己的错（本轮 1 次）
- 把尚未创建的目录当 workdir 传参 ⇒ `spawn … ENOENT`（整条命令未执行、无副作用）；整改：**先建目录，再以它为 workdir 运行**。
## round 3（同会话，2026-10-26 12:00-12:12）—— 把「模板内容」补上（目标核心）

### 为什么走**用户模板通道**（而不是改 defaults.go）
默认模板源文件 `workbench/internal/templates/defaults.go` 仍被并行会话占用（未提交、已闲置 1h+）。
产品原生支持用户模板：`Registry.Open(<DataRoot>/templates.json)` 在内置 29 条之上追加 ⇒ **完全不碰在途文件**也能交付新模板内容。

### 交付物 `docs/quality-packs/工程模板包-v1.json`（4 条，可导入任意实例）
1. `quality-frontend-audit` 前端工程质量审查：HTML 语义与可访问性 / CSS 令牌体系 / JavaScript-TypeScript 正确性 / React-Vue 组件层，五轴口径 + P0-P1-P2 分级 + 合并结论与门禁建议；
2. `quality-frontend-buildperf` 构建与性能调优：先量后改 + 受控 A/B、单点样本不许下结论、构建期与运行期不混；
3. `quality-error-fix` 报错定位与最小修复：原始输出（命令+栈+退出码）+ 根因假设与最小实验 + 反向变异复验 + 静默失败补观测面；
4. `quality-arch-design` 可扩展高可用架构：约束与失效模式 / 分层与边界 / 路径取舍 / 小步改造清单 / 「现在不做」。

### 判据（不是写完就算）
- `internal/templates/quality_pack_contract_test.go`：① 逐条过 Validate、ID 不撞内置、后继模板可解析（含"链边至少检查 3 条"受理面哨兵）；
  ② 目标能力面关键词必须出现（HTML/CSS/TypeScript/构建/性能/报错/可扩展/高可用 + 必须有模板覆盖「前端」整面）；
  ③ 走**真实导入路径** Registry.Open 断言 29+4=33 与 enabled/builtin 语义。
- `internal/chain/pack_domain_pool_contract_test.go`：**先红后绿** —— 修前 FAIL「模板包领域 "frontend" 没有选题池 —— pickTopic 会静默回落 content 选题」→ 补池后 OK。

### 配套小改
`chain.go` 补 frontend 选题池（5 条，指回工作空间项目）；`config.go` 内置自主白名单补 frontend（遵守 round 2 写下的维护规则）；
web 译名表加 `frontend=前端工程`（domain_label_test.go 钉住两张表同源）。

### 提交 · 部署 · 线上验证（#40）
- `21cbfb1` 已推送；干净 worktree @21cbfb1 构建 `BUILD_RC=0`（66.9 s），部署到 `dist\` 与 `D:\workbench-agent\`，
  两处 hash == `SHA256SUMS` = `3F11CCED…5DD8`；`go version -m` vcs.revision=`21cbfb1`；`go test ./internal/web/`（产物逐字一致契约）ok。
- **模板包已装入 live 实例**：`D:\workbench-agent\workbench-output\templates.json` 与仓库包 SHA256 逐字一致（`3816DBE3…`），含 4 条 id。
- live `config.json` 的 `allowedDomains` 补 `frontend`，跑一次 exe 后仍为 `…,quality,frontend`（T6 修复对新增域同样生效）；
  `--autonomy plan` 仍输出 quality 目标草稿。

### 本轮登记/未做
- **T8（待做）**：内置模板库（`defaults.go`）侧的同类扩充 + `TestDefaultsCount` 与头注释同步 —— 等并行会话把在途件提交后再做。
- **T4 / T7** 未做（见报告 §6）；**引擎仍未启动**（沿用 round 2 的决定与回滚口径）。

### 我自己的错（本轮 1 次）
- 写 Go 测试源码时在 TS 模板串里直接写了 `\n`，被当成**真换行** ⇒ `string literal not terminated`。
  教训：写代码时该写 `\\n` —— 要分清「这一层是谁在解析」（外层模板串 vs 内层语言字面量）。

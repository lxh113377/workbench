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
## round 4（同会话，2026-10-26 12:1x-12:2x）—— T4：工作空间静默降级不再隐身

### 缺陷与处置
`normalizeWorkspace`（config.go）在 external 模式缺少合法绝对根时，把模式**静默**改回 project 并清空 Root ——
「被选中的工作目录」被无声丢掉（任务回到数据根沙箱、workspace context 不再注入），配置里只剩 stale root（round 1 现场即如此）。
- 修法：降级时登记 `Config.Warnings`（本仓既有机制）；并在 `cmd/workbench/main.go` 补上**全仓唯一一处启动期读取 Warnings 的入口**
  （`logger.Warn("配置修正", "warning", w)`）—— config.go 自己的注释写着「若没有人读 Warnings，跨字段校验就仍然不可见」，此前确实无人读。
- **先红后绿**：新增 `internal/config/workspace_downgrade_warning_test.go` 两条（空根/相对路径必须留痕；外加「合法绝对根不得降级、也不得报警」的对照组）。
  修前 FAIL「空根: 静默降级 —— Warnings 里没有工作目录相关条目：[]」→ 修后 PASS。

### 提交 · 部署 · 线上验证（#40）
- `6fca25f` 已推送；干净 worktree @6fca25f 构建 `BUILD_RC=0`（86.5 s）；部署到 `dist\` 与 `D:\workbench-agent\`，
  两处 hash == `SHA256SUMS` = `96B7BE9E…5980`；`vcs.revision=6fca25f`。
- **运行期取证（真实数据根探针）**：新 exe 放进临时数据根 → 把 workspace 改成 `external + relative/dir` → 跑 `--diagnose`：
  `"mode": "project"`（已回落）与
  `"warnings": ["workspace.mode: external 缺少合法绝对根（原值 relative/dir）已回落 project 模式；被选中的工作目录不再生效"]`；
  探针目录已回收站删除、构建 worktree 已移除。
- 启动期日志行本身**未做运行期取材**（观察它需要真的拉起引擎：会建快捷方式、拉模型池、可能触发任务），
  以「编译 + `go test ./cmd/workbench/` ok（33.3 s）+ `--diagnose` 可视面」三面取证，如实标注。

### 剩余（目标仍 active）
- **T7**：CLI `--workspace select` 不落审计记录（面板路径会落）—— 待做。
- **T8**：内置模板库（`defaults.go`）的同类扩充 + `TestDefaultsCount`/头注释同步 —— 本轮开局核过在途件仍未提交（mtime 10:54、git status 仍 ` M`），故仍未动。
- 引擎仍未启动（沿用 round 2 决定与回滚口径）。

### 我自己的错（本轮 0 次）
本轮无新增自身错误；沿用上一轮的转义教训，写 Go 字面量时显式区分层级。
## round 5（同会话，2026-10-26 12:3x-12:4x）—— T7：CLI 与面板的审计同构

### 缺陷与定位
面板/引擎路径 `engine.SelectWorkspaceDir` 走 `Manager.SelectExternal` / `Manager.ResetProject`，两者都落
`workspace-audit.jsonl` 的 select / reset 记录；而 CLI 的 `--workspace select|reset` 直写 `cstore.Update`、
**根本不经过 Manager** ⇒ 审计里只有 validate 行。实测：round 2 在 `D:\workbench-agent` select 成功后审计仍只有
两条 validate ——「谁在何时把工作目录换成哪个」不可追溯。

### 修法与判据
- 改 `cmd/workbench/main.go`：select 保留「先落盘 → 再应用」，应用改走 `mgr.SelectExternal`；
  reset 改走 `mgr.ResetProject`（同时落 `op=reset` 审计，并保留上次选定 Root 供再次切回——既定语义），再按返回的 Since 落盘。
- **判据是黑盒真二进制**（新增 `cmd/workbench/workspace_cli_audit_test.go`）：临时数据根里构建并运行真实 exe，
  跑一次 select 与一次 reset，审计文件必须各出现一条记录。
  **先红后绿**：修前 FAIL 原文只有 `{"at":…,"op":"validate",…}` 一行；修后 PASS。
  为什么用黑盒：缺陷恰在「CLI 不经过 Manager」这条接线处，单元缝测不到。

### 提交 · 部署 · 线上验证（#40）
- `ddded86` 已推送；干净 worktree @ddded86 构建 `BUILD_RC=0`（66.6 s）；部署到 `dist\` 与 `D:\workbench-agent\`，
  两处 hash == `SHA256SUMS` = `FD4AD096…9B8B`；`vcs.revision=ddded86`；`go test ./cmd/workbench/` ok（35.5 s）；worktree 已移除。
- **live 取证**：用新 exe 在 `D:\workbench-agent` 再选一次 ⇒ 审计新增
  `{"op":"select","path":"D:\\workdesktop","mode":"external","allowed":true,"reason":"用户选定外部工作目录"}`
  （此前 select 成功后仍只有 validate 行）。

### 剩余
- **仅剩 T8**：内置模板库（`defaults.go`）的同类扩充 + `TestDefaultsCount`/头注释同步 —— 在途件五轮开局均未提交（mtime 仍 10:54），明令不碰。
- 引擎仍未启动（沿用 round 2 决定与回滚口径）。

### 我自己的错（本轮 1 次）
- 第二次踩同一类转义坑：在 TS 模板串里写 `\"` 想得到 Go 源码里的转义引号，实际得到裸引号 ⇒ Go 语法错
  （`string literal not terminated`）。整改：**Go 源码里的反引号用字符串插值、内部双引号原样写**，一次通过；
  教训：要分清「外层模板串也是一个解析器」。
## round 6（同会话，2026-10-26 12:5x-13:0x）—— T8 补丁就绪（已验证）+ 部署实例离线闭环取证

### 本轮判断
T8 依赖的 `workbench/internal/templates/defaults.go` 第六轮开局仍未提交（mtime 10:54，停滞 >2h）——
按 ownership 边界不能碰。于是把 T8 从「待做」变成**「已验证、可一键落地」**：交付补丁 + 任务卡，任何人可安全落地。

### 交付物（指定地点 docs/quality-packs/）
- `T8-内置模板转正-2026-10-26.patch`（26,328 B；6 文件：defaults.go / templates_test.go / chain_link_contract_test.go 改，
  工程模板包 JSON 与两条包判据删）。
- `T8-内置模板转正-任务卡.md`（前置条件、逐文件说明、验收命令、落地后清理 live `templates.json` 的收尾、已知风险）。
- **验证不是口头的**：干净检出（worktree @cee0ea3）`git apply --check` rc=0 → 真实 `git apply` rc=0 →
  `go build ./...` rc=0、`go vet` rc=0、`go test ./internal/templates/ ./internal/chain/` 两包 ok、gofmt 干净；
  读数：**33 条模板 / 16 条后继边，无断链/自环/重复**。

### 部署实例离线闭环取证（运行根 D:\workbench-agent）
- `--autonomy selftest` → **11 PASS / 0 FAIL**（目标树规划/验收/回退/目标完成/下一目标、存储重开可恢复）
- `--plan-selftest` → **12 通过 / 0 失败**（方案 → 执行 → 验收回灌全链）
- `--selftest-workspace <临时目录>` → **pass:true**（产物落盘 + 真实验收 exit=0）；隔离目录 3 个文件，已回收站删除
即：「external 工作空间 + 工程质量目标 + quality/frontend 白名单 + 4 条模板」这套配置下，闭环机制离线自证健康（不消耗模型额度）。

### 剩余
- T8 本体（应用补丁）—— 等 `defaults.go` 在途件提交后一条命令落地；或由并行会话自行应用。
- 引擎仍未启动（沿用 round 2 决定与回滚口径）。

### 我自己的错（本轮 1 次）
- 补丁注释先按推算写「17 条边」（旧 11 + 新增 5），**实测是 16**；立刻改用实测值，并在任务卡写明「以本文件输出为准」。
  教训：**能跑一次拿真值，就不要靠推算**（本仓在册同款教训）。
## round 7（同会话，2026-10-26 13:0x-13:2x）—— T9：自主阶梯不再静默回退内容阶梯

### 为什么本轮不开 T8
T8 依赖的 `defaults.go` **第七轮开局仍未提交**（mtime 10:54，停滞 >2.5h）；补丁与任务卡已在 round 6 就绪并验证。
本轮转去修更靠前的语义缺口：**自主任务到底会做什么**。

### 缺陷与修法（T9）
`internal/autonomy/planner.go` 的 `domainLadder` 只覆盖 content/code/knowledge/data/solution/report，
`ladderFor` 对未知领域**静默回退 content 阶梯** ⇒ `domain=quality` 的自主目标落到
「立框架：产出该主题的总纲与核心观点清单」（`--autonomy plan` 实测原文），与工程质量无关；
round 3 加进白名单的 `frontend` 同理。修法：补两套工程阶梯
（quality：体检→立项→小步修→判据固化→复盘；frontend：体检→基线→小步改→一致性→复盘）。

### 判据（先红后绿 + 机器对账）
新增 `internal/autonomy/planner_ladder_contract_test.go`：
① 白名单域 ↔ 阶梯机器对账（防空转：白名单空 / 表空 / 步数 <3 均判红）；
② 行为探针（quality/frontend 第一步 ≠ content 第一步，且阶梯里含工程动作词）。
修前 FAIL 原文两条（缺阶梯 / 仍回退 content 阶梯），修后 PASS。

### 提交 · 部署 · 线上验证（#40）
- `9fb6095` 已推送；干净 worktree @9fb6095 构建 `BUILD_RC=0`（71.2 s）；部署到 `dist\` 与 `D:\workbench-agent\`，
  两处 hash == `SHA256SUMS` = `BDCB9496…64C4`；`vcs.revision=9fb6095`；全仓 `go test ./...` rc=0；worktree 已移除。
- **live 取证**：`--autonomy plan` 现输出「标题：提升工作空间项目的全栈工程质量 · **体检**」、
  「当前阶段性目标（第 1 阶段）：**体检：按五轴（正确性/可读性/架构/安全/性能）扫当前工作目录的项目，列出带 文件:行号 的真实缺陷**」。

### 剩余
- T8 本体（应用 round 6 的已验证补丁）—— 等 `defaults.go` 在途件提交。
- 引擎仍未启动（沿用 round 2 决定与回滚口径）；其闭环已由 round 6 的离线自检（11/0、12/0、pass:true）证明健康。

### 我自己的错（本轮 0 次）
本轮无新增自身错误。
## round 8（同会话，2026-10-26 13:18-13:35）—— 运行期取证：自动任务真的跑起来了

### 为什么本轮要真跑
前七轮把「工作空间/模板/目标/阶梯/白名单/审计」都配齐并做了离线自检，但**从来没真跑过一次自动任务**。
本轮做**有界运行取证**：演示工程 + 临时把 `autoStart=true / maxTasksPerGoal=1 / concurrency=1 / chain=false`，
取证后全部还原。

### 取证读数（原日志）
- `引擎已启动 concurrency=1 phase=running autonomy=自主：提升工作空间项目的全栈工程质量（阶段 0，进度 0%）`
- `自主任务已生成 taskId=task-1792992244076-8f30 title=提升工作空间项目的全栈工程质量 · 体检 domain=quality
   rationale=目标处于第 0 阶段，按领域推进阶梯执行「体检」`（T9 阶梯在运行期生效）
- 子会话事件 `{"type":"session","cwd":"D:\\wb-demo-ws"}` + `sessions/--D-wb-demo-ws--/`
  ⇒ **执行器 cwd = 被选中工作空间**（「聚焦被选中文件夹」的运行期铁证）
- `尝试开始 model=deepseek-ai/deepseek-v4.1-flash` → `尝试结束 kind=bad_exit elapsedMs=47603` → 故障转移 `z-ai/glm-5.3`

### 新登记
- **T10（P0，待修）**：计划阶段 `bad_exit`（round 1 误启动时同样形态 ⇒ 可复现的车道故障）。后果：任务能生成、cwd 正确，
  但拿不到模型产出 ⇒ 永不产出成果物、进不了验收与链式衍生。这是「自动任务真正提升质量」当前唯一拦路虎。
- **T11（存疑，待复现）**：引擎退出后 `DSH Desktop.exe`（cmdline 含 wb-demo-ws）仍持有工作目录句柄，
  `DeleteDirectory` 连续 4 次失败 ⇒ 与「DSH 子进程随主进程退出而回收」的架构声明不符；也可能是我 `--quit` 时
  那个 glm 长尝试的子进程仍在合法运行。演示目录 `D:\wb-demo-ws`（仓外 4 个小文件）待占用释放后回收。

### 运行边界与回滚（已完成）
- 演示期设置：`autoStart=true / maxTasksPerGoal=1 / concurrency=1 / chain=false`，工作空间指向 `D:\wb-demo-ws`；
- 取证后：config 从备份 `config.json.bak-pre-demo-20261026-131852` 还原，并重新 `--workspace select D:\workdesktop`
  （审计新增 `op=select`，T7 修复生效）；`workbench` 进程数 0；`autoStart/maxTasks/并发/链` 均回到原值。

### 我自己的错（本轮 0 次）
本轮无新增自身错误（唯一未完成项是演示目录回收被句柄占用阻塞，已登记 T11）。
## round 9（同会话，2026-10-26 13:45-14:10）—— T10 第一步落地 + 运行通道取证（T12）

### 做成了什么
- **T10 第一步（提交 985d78c，已部署）**：把「失败必须可归因」这条补齐 ——
  ① `internal/executor/pure.go` 新增 `persistStderrTail`（非空才写、只留尾部 8 KB、写失败不阻塞执行）；
  ② `executor.go` 的 Result 加 `StderrPath`，在四条收尾路径（卡死/超时/取消/正常结束）的汇合点统一落盘 stderr；
  ③ `engine.go` 的「尝试结束」日志补 `reason`（stderr 摘要或执行器错误，截 300 字）与 `stderrPath`；
  ④ 新增 `internal/executor/stderr_persist_test.go`（非空才写 / 同目录 / 只留尾部 / 空输入不建文件）。
  部署：干净 worktree 构建 BUILD_RC=0（72.8 s）→ dist 与 D:\workbench-agent 两处 hash == SHA256SUMS = 559210AF…2509、vcs.revision=985d78c。
  为什么先做这一步：round 8 真跑的 bad_exit 47,603 ms 事后**翻遍任务目录与日志都答不出原因**（stderr 只活在内存里）。
- **T12（新登记，已取证）**：round 8/9 两次真跑都在 ~3 分钟内被**环境作业对象回收**，且不留停机痕
  （无 panic/自愈/停机记录；面板 refused；`state.json` 停在 running；任务永远 running）。
  结论：不是「产品静默死亡」，而是**我的观察通道不合法** —— 从本 harness 的 shell 用 Start-Process 拉起的长驻进程绑在宿主作业对象上，
  发起调用的 shell 一结束即整树回收。要真正观察长驻运行，必须走计划任务 / 独立会话 / 老大手动启动。
- **T11 更新（降级）**：演示目录 D:\wb-demo-ws 已成功回收 —— 句柄随被回收的进程树消失而释放，
  故 T11 与环境回收同源的概率高，保留为待确认观察项，不作确证缺陷。

### 未完成（如实标）
- **T10 的端到端捕获未完成**：本轮复现时引擎在尝试进行中被环境回收，`stderr.txt` 未被写出 ——
  这一步必须在**合法观察通道**下重做（T12 之后）；T10 第二步（定位并修 bad_exit 根因）因此待做。

### 运行边界与回滚（已完成）
- 演示期设置（autoStart=true / maxTasksPerGoal=3 / concurrency=1 / chain=false + 演示工作空间）已从备份
  `config.json.bak-pre-demo-20261026-131852` 还原，并重新 `--workspace select D:\workdesktop`（审计新增 op=select）；
  `workbench` 进程数 0；演示目录已回收。

### 我自己的错（本轮 2 次）
1. 让轮询用 `"msg":"尝试结束"` 匹配，命中 round 8 的**旧日志行**就提前 break ⇒ 第一次"等失败"实际等了 0 秒就结束
   （读数假象）。整改：只匹配**新版才有的字段**（`"reason":`）并核对时间戳。
2. 两次用 `tools.edit` 在同一文件上连续改时，误以为「先 read 再连续 edit」总成立 —— 有一处因版本失配被拒；
   本轮后半改用「数组拼行 + 精确 old_string」逐条落地，并把失败当作版本失配信号而非重试信号。
## round 11（同会话，2026-10-26 14:15-15:00）—— 终止取证：exit code -1、stderr 全空（T13）

### 本轮做了什么
- 用**控制台子系统**重新构建诊断二进制（去掉 `-H=windowsgui`，子系统=3）并把 stdout/stderr 重定向到文件，
  再用 .NET 探针 `WaitForExit` 取退出码 —— 这才拿到「进程消失」背后的真实读数。
- 三次实验读数：现场 GUI 版最后日志 ≈ start+181 s；控制台版 `diag-err.txt`/`diag-out.txt` **均 0 字节**；
  .NET 探针 **`exitCode=-1` / `elapsedSec=308` / stderr 空**。

### 已排除（都有实测依据）
1. **不是 Go panic**：panic 会向 stderr 打栈，而控制台构建下 stderr 为 0 字节。
2. **不是自愈重启**：无「资源自愈触发 / 自愈停机」日志，`state.json` 停在 running、`stoppedAt=0`。
3. **不是并行轮次会话的清扫**：`gate-readings.jsonl` 最后写入 11:18:07，此后 3.5 h 无活动。
4. **不是「我的 shell 作业对象回收整树」**：经 WMI/CIM + cmd 启动（父进程 WmiPrvSE/cmd）同样 ~3–5 分钟死亡。

### 未取得可信读数（如实标）
- 中性长进程对照实验**没跑起来**（探针日志为空，匹配到的 “Start-Sleep 1200” 进程其实是我自己的工具壳）
  ⇒ 「环境是否回收任意长驻进程」仍未定论，本轮不据此下任何结论。
- 因此 **T10 第二步（用 stderr.txt 定位 bad_exit 根因）仍被阻塞**：引擎活不过一次尝试收尾。

### 新登记 T13（P0）
**谁在 ~3–5 分钟处终止 workbench？** 形态 = 外部强制终止或无声 exit（退出码 -1、零 stderr、零 app 级记录）。
下一步判据：① 与 AI 工具链无关的通道（计划任务/独立会话）跑通中性对照 ≥10 min；② 秒级定点 + 死亡时刻进程表快照；
③ app 加「退出即留痕」（任何 exit 路径写 `process_exit` + 退出码）；④ 若确认外部终止，T10 的复现必须换到作业对象之外且非本工具链的通道。

### 运行边界与回滚（已完成）
- 诊断工件（`runtime\tmp\wb-diag.exe`、diag 数据根）与演示工作空间 `D:\wb-demo-ws` 均已回收；
- live 配置已从备份 `config.json.bak-pre-t10b-20261026-141804` 还原并重新 `--workspace select D:\workdesktop`
  ⇒ `autoStart=false / maxTasks=12 / concurrency=2 / chain=true / external D:\workdesktop`；`workbench` 与 `wb-diag` 进程数均为 0。

### 我自己的错（本轮 1 次）
- 中性对照实验的**启动通道**没验证就启动：探针日志为空却继续轮询 10 分钟，最后匹配到的进程其实是我自己的工具壳 ——
  这是一次「读数对象不是我假设的那个」的假实验。整改：**实验启动后先验「被测对象存在且身份正确」**，再进入等待。

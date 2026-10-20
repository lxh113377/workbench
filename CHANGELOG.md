# Changelog

> 本项目所有值得一提的变更都记在这里。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
> 版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。
>
> 版本与产物对应关系以 `dist/SHA256SUMS` 与 `workbench.exe --version` 的实测输出为准。

## [Unreleased]

> 规则：未发布的变更一律记在本节；发版时把本节整体提升为新的 `## [x.y.z] - YYYY-MM-DD`，
> 并立刻补一个空的 `## [Unreleased]` 回顶（CI 的 changelog 门禁会校验这一条）。

### Added
- **Prometheus 文本格式指标导出（P1-3）**：`internal/web/metrics_prom.go` 提供 `GET /metrics`
  （exposition v0.0.4，纯标准库），把任务状态分布、尝试总数、**按 ErrorKind 的失败分类**、
  任务耗时 sum/count 与进程侧 goroutine/堆/GC 暂停投影成监控系统可直接抓取的形式。
  三条硬约束写在结构里而非靠自觉：样本只能挂在已登记族上（`# HELP`/`# TYPE` 成对写出，
  重复登记复用同族）；浮点经 `promSanitizeFloat` 兜底，绝不产 NaN/±Inf；标签名走白名单、
  标签值按规范转义且非法 UTF-8 先修正、超长按 UTF-8 边界截断。零任务时每个合法状态仍输出
  0 值序列（采集端可预先建面板，不必等序列凭空出现）。新增 8 条判据含逐行 exposition 校验
  与「同 (name,labels) 重复序列」检测（变异测试：删 `# TYPE` 与改回两条同名样本均判红）。
- **性能基线护栏（B8 / L8）**：`docs/benchmark/perf-baseline.json`（数据件，含 provenance、
  上界推导与三个 null 字段的原因）、`docs/benchmark/性能基线口径.md`（逐字段取证命令与三轮
  原始读数）、`tests/check_perf.ps1`（229 行三态门禁：0=PASS / 1=FAIL / 2=UNVERIFIED，
  「没测成」绝不当成「测得过」），并把 `ci.yml` 的产物体积上界从硬编码 40MB 改为引用基线、
  新增 `Perf baseline gate` 步骤。实测三态齐验（真实产物 rc=0、缺参/缺文件 rc=2、
  变异上界 rc=1，基线按 SHA256 逐字节还原）。
- **开源对标报告入主树**：`docs/benchmark/WorkBench-开源对标报告.md`（8 个 peer：
  n8n / dify / AutoGPT / autogen / temporal / river / asynq / windmill，八维对比 +
  P0/P1/P2 清单 + M0-M4 路径），此前只存在于受 `.gitignore` 屏蔽的 `src-backup/`。
- **知识盲区与学习计划**：`docs/知识盲区与学习计划.md`，B1-B8 盲区绑容器实测读数，
  L1-L9 学习路径含精读对象与机器判据。
- **开源化三件套**：`ARCHITECTURE.md`（单页定位/进程模型/包结构/五条架构取舍）、
  `.github/workflows/ci.yml`（vet + `go test -race` + 覆盖率门禁 + 产物校验）、
  `autonomy-e2e.yml`（容器内确定性通道跑完整自主闭环）。
- **issue/PR 协作模板**：`.github/ISSUE_TEMPLATE/`（缺陷报告 / 功能请求 / config.yml）、
  `.github/pull_request_template.md`（判据必须贴命令与真实输出）、
  `.github/labels.json` + `setup/labels.ps1`（12 个正交标签）。
- **零弹窗拉起共用件** `tests/lib_silent_launch.ps1`：按 PE 子系统分流，
  GUI 走 `Win32_Process.Create`（保持脱离作业对象），CONSOLE 走
  `Start-Process -WindowStyle Hidden`。
- **崩溃救援额度（L4）**：`model.Task.CrashRescues` 记录跨崩溃的救援轮次，
  `internal/engine/stuck_rescue.go` 的 `decideRescue` 分流「重新入队 / 判死」，
  状态机新增 `interrupted → failed` 出口。修的是**无界崩溃循环**：
  原先 `Recover()` 把 interrupted 无条件转回 queued，而 `Attempts` 是单次 `runOne`
  内的序号（每轮被覆盖），不能当崩溃预算 —— 于是"一跑就弄死进程"的任务
  配合 KeepAlive 每分钟拉起会无限重投，看起来"一直在干活"实际零产出。
  判死写 `ErrorKind=crash_loop`，面板失败原因分布可区分。
- **社区协作面（L7）**：`.github/ISSUE_TEMPLATE/`（缺陷报告 8 字段 / 功能请求 7 字段 /
  config.yml）、`.github/pull_request_template.md`、`.github/labels.json` +
  `setup/labels.ps1`（12 个正交标签，含本仓特有的 `flaky` / `platform-coupling` /
  `external-constraint`）。模板把本项目的判据文化写成必填项：
  「取不到数写 UNVERIFIED 不要填 0」「必须贴命令与真实输出」「外部约束另打标签」。
- **三条仓库级门禁（已挂 CI 的 Repo hygiene gates 步骤）**：
  `tests/check_changelog.ps1`（Unreleased 必须居首 / 日期必须 YYYY-MM-DD / 版本降序）、
  `tests/check_noninteractive.ps1`（禁 `Invoke-WebRequest` 缺 `-UseBasicParsing`、
  计划任务缺 `-NonInteractive`、以及 Read-Host / Get-Credential / -Confirm 等交互构造）、
  `tests/check_issue_forms.ps1`（Issue Forms 结构：顶层三键、每块 attributes.label、
  dropdown/checkboxes 的 options 必须位于 attributes 之内、contact_links 必须用 about）。
  三条判据均经**变异测试**验证能抓回对应缺陷（不是"跑过了"就算数）。
- **CONTRIBUTING 新增三节**：九 协作流程与判据文化、十 零弹窗纪律、十一 延伸文档。

### Fixed
- **限速闸门补构造期接线（D9 自查补漏）**：上一批写的是「`New`/恢复/`ReloadConfig`/`SetRunner` 四接线」，
  实测当时只有 3 处调用点（`New()` 里没有）。查的过程发现真实缺口：
  `cmd/workbench/main.go:419` 是第二个引擎入口（一次性 CLI 自检，只 `engine.New` 后直接
  `SelfTestArtifactPipeline`，全程不调 `ReloadConfig`），那条路径跑到真模型时不受任何速率约束。
  补 `New()` 内的 `applyRateLimit`（幂等：主路径随后 `ReloadConfig` 再装一次只是替换令牌桶，
  且 `New`→`SetLogger`→`ReloadConfig` 之间不发请求）⇒ 调用点 3 → 4，表述名实相符。
  新增 `newBareEngine`（复刻该 CLI 路径）+ `TestRateLimiterWiredAtConstruction`。
  **变异验证**：撤掉 `New()` 接线后只有这一条判红、其余 4 条限速用例仍绿 ⇒ 证明它不是
  `ReloadConfig` 用例的重复劳动，而是唯一覆盖 CLI 路径的那条（`0ae60ff`）。
- **语义层绕过执行器（缺陷 D7）**：`judge.run()` 原先每次自己 `executor.New(...)`，于是
  ① 单元测试注入的假执行器（`engine.SetRunner`）管不到语义子任务 —— `go test` 会真的拉起
  DSH Desktop 子进程（取证：`t.TempDir` 残留 `tasks/summary-task-done-e2e` 与
  `tasks/critique-task-done-e2e`，本机同时堆积 20+ 个 DSH 进程；真实单任务 65-148s 而测试
  只 `WaitInflight(5s)` ⇒ 后处理仍在写 `events.jsonl` 时用例结束，Windows 下 TempDir 的
  RemoveAll 报 "used by another process"，engine 包判红、容器与 CI 却绿）；
  ② 生产路径绕过模型池 —— `main.go` 装在 `e.runner` 上的 FailoverRunner（故障转移 + 车道
  健康度）与全局限速闸门对语义子任务全部失效，每个任务额外 2-3 次裸请求不受任何约束；
  ③ 执行钩子的尝试日志对语义子任务失效。修复：`judge.SetRunner` + `engine.SetSemanticRunner`，
  引擎构造与 `SetRunner` 时同步注入。
- **限速闸门生产从未接线（缺陷 D9）**：`internal/ratelimit` + `executor.SetLimiter` + 面板
  `RateLimiterSnapshot` 三件套 2026-10-19 就写好了，但实测**生产没有任何调用点**
  （`ratelimit.New` 全仓仅出现在测试里），`requestsPerMinute=20` 是装饰品 —— 面板显示限速、
  实际不限速，正是 08:00-08:04 每分钟 45-70 次请求打爆免费车道的结构性原因。修复：新增
  `internal/engine/runner_wiring.go` 的 `applyRateLimit`，沿装饰器链下探装配，`New`/恢复路径/
  `ReloadConfig`/`SetRunner` 四处接线；配置热更新会真的改变发出速率。
- **事件文件句柄生命周期（缺陷 D8）**：`executor.run()` 用 `defer eventsFile.Close()`，而
  `consume` 协程可能在 `Run` 返回后仍在写（`wg.Wait` 有 15 秒超时兜底）—— 对已关闭文件继续
  Write 的错误被 `_` 吞掉，事件尾部静默丢失。改为 `sync.Once` 包裹的 `closeEvents`：所有早退
  路径（pipe 接管失败 / 隔离环境准备失败 / `cmd.Start` 失败）由 run 的 defer 释放，正常路径由
  consume 自己收尾，超时兜底路径主动关闭两个读端解阻塞并再给一次有界等待。
  新增 `events_lifecycle_test.go`（含变异测试：撤掉 defer 后改名判红）。
- **自评解析接受无关 JSON（缺陷 D10）**：`extractJSON` 抓「第一个平衡 JSON」，模型回答里若含
  产物协议 manifest（`{"files":[...],"verify":[...]}`）等其它 JSON，`Unmarshal` 到 `Score`
  会全字段零值却返回 nil error ⇒ 自评质量被误判为 0 且 `degraded=false`，上层以为是模型的
  诚实评分。后果链：质量 0 → `ShouldExtend` 判否 → 链不延伸；自主验收按 quality 门槛判失败
  → 闭环不推进（`TestClosedLoopThroughRunOne` 实测红 5.57s）。修复用 key 存在性判据（不是数值
  范围），`quality=0` 仍是合法评分；反向用例守护收紧不误伤。
- **构造期告警永久静默（缺陷 D12）**：`engine.New()` 里 `e.log` 先是 Nop，而 `main.go` 在
  `New()` **返回之后**才 `SetLogger` —— 于是构造期所有「失败只告警不阻断」的告警（工作目录
  越权审计打不开、自主子系统初始化失败）写进 `io.Discard`。现场表现为自主链路没起来、
  `workspace-audit.jsonl` 始终为空，但日志与面板里一条原因都查不到。修复：`warnDeferred`
  缓冲 + `SetLogger` 冲刷（清空后重复 SetLogger 不重写）；同时删掉 `New()` 里那行自杀式的
  `e.log = logging.Nop()` 重设。
- **面板日志拿不到 component（缺陷 D11）**：`logging.At()`/`With()` 只把固定字段加到 slog 侧，
  handler 通道（面板 `/api/logs` 走这条）的 `Record` 另起一路、仅从本次 kv 构造 —— 落盘 JSONL
  有 `component`、面板读数却没有，`ARCHITECTURE.md` 承诺的「component 字段可按来源过滤」在
  面板链路上是假的。修复：`Logger.attrs` 自存一份并在 emit 时并入（本次 kv 覆盖同名键，与
  slog 语义一致）；`copyAttrs` 返回独立副本，防兄弟子日志器串字段。logging 覆盖 82.8% → 93.9%。
- **无人值守弹窗挂死**（老大 2026-10-19 17:29 反馈"这个东西后台静默不要弹出窗口"）：
  守护计划任务每分钟拉起 `tests/keepalive.ps1`，其中一条 `Invoke-WebRequest` 缺
  `-UseBasicParsing` —— PowerShell 5.1 因此调用 IE 引擎解析响应页，而面板页含 `<script>`
  （实测 2 处），触发 "Security Warning: Script Execution Risk … Do you want to continue?"
  等人工按键；同时任务注册命令行缺 `-NonInteractive`，两者叠加 = 桌面弹窗 + 任务永久挂起。
  修复：`-UseBasicParsing` + `$ConfirmPreference="None"` + `$ProgressPreference` +
  注册补 `-NonInteractive` 与 `Settings.Hidden`；新增 `tests/check_noninteractive.ps1`
  静态门禁（已挂 CI，变异测试验证可抓「只在注释里提 flag」的洗绿形态）。
- **自主闭环决策留痕顺序**（issue #1）：`autonomy.advanceLocked` 与 `bootstrapLocked`
  的恢复路径原先「先切活跃目标、后写 nextGoal 决策」，而两者写不同文件
  （goals.json / decisions.jsonl，非原子），留下一个可观测窗口 —— 面板与
  `--selftest` 判据 7b 会看到「目标已切换但台账无推导记录」，CI 上
  `TestRunOneDrivesAutonomyClosedLoop` 的偶发红即源于此。现收敛为
  `writeLedgerThenActivate`（先留痕后生效），并由顺序守卫测试钉死。
- **非 Windows 删除无安全阀**（issue #2）：`retention.Trash` 的 `!windows` 分支此前是裸
  `os.RemoveAll`，危险路径判定只存在于 Windows 分支 —— 实测 `Trash("/")` 在 Linux
  会递归删掉文件系统根。现 `refuseDangerousPath` 上移为平台无关实现，两条删除路径同过闸。
- **产物子系统门禁**：`setup/build.ps1` 校验 EXE 的 PE `Subsystem` 必须为 2(GUI)，
  非 GUI 直接判红。控制台子系统 EXE 被计划任务/`Win32_Process.Create` 拉起时
  Windows 必分配可见控制台窗口（桌面反复弹窗的根因）。
- **测试硬编码 Windows 工具链**（issue #2）：engine / verify 的 10 个用例改为解释器探针
  （pwsh→python3→python→node）；Windows 上全缺时 `t.Fatal`，禁止 `t.Skip` 洗绿。
  Linux 容器 `go test ./...` 从「3 包 10 用例 FAIL」转为全绿。
- **失败判据被偷换**：`TestMaterializeArtifactsFail` 原先只断言 `Pass == false`，
  而解释器缺失导致的「启动失败」同样 Pass=false —— 测试绿但什么都没验。
  现补断 `FailReason` 必须含「退出码 2」。

### Changed
- **仓库卫生：可再生产物入 .gitignore（`bce8e53`）**：`git ls-files --others` 实测 **53,751 个
  untracked**（xtmp 22,397 / dist 13,400 / `.workbuddy/gocache` 13,353 / runtime 3,792 /
  `tests/docker-autonomy/evidence` 507+）⇒ `git status` 一屏滚不完，「目录卫生」类判据读数全是噪音。
  只忽略可再生的缓存与跑出来的产物；**不忽略整个 `.workbuddy`**（`git ls-files` 实测 `memory/`
  下 5 个 `reply-to-lead*.md` 受跟踪，一把梭会把项目记忆藏起来）。
  安全断言（防「忽略=删除」事故重演）：把全部 1,838 个已跟踪文件喂给 `git check-ignore --stdin`
  ⇒ 被新规则命中 **0 个**；`git status` 里 'D' 条目 **0**。结果 untracked **53,751 → 210**。
  如实登记边界：`/dist/` 比原有条目更宽，将来若要在 dist 下**新增**交付物需 `git add -f`。
- **执行层抽象收敛（L5）**：`engine.SetLogger` 原先断言 `e.runner.(*executor.Runner)` ——
  接口里带具体类型等于没脱离结构体，一旦执行器被 `FailoverRunner` 包住（`main.go` 的做法）
  断言就不匹配，尝试日志对模型池路径静默失效。改为沿 `Inner()` 链找 `AttachHooks` 能力接口
  （`executor.Runner.AttachHooks` + `modelpool.FailoverRunner.AttachHooks`），新增执行通道只要
  实现 `RunnerContract + AttachHooks` 就自动获得尝试日志。三条守护测试（含 nil 链不 panic、
  BeforeRun 中止归 `boot_failed`），变异测试验证「回退成具体类型断言」必判红。
- **`limiter_integration_test.go` 判据改相对量**：原断言「单次 Run < 2 秒」在全量并行
  （`go test ./...`）下实测 4.12 秒而判红 —— 那 4 秒是 `linkDir` spawn cmd.exe + 建目录 +
  写覆盖层的宿主排队开销，与「限流器是否等待」无关，属判据测错对象。改为测**两次连续 Run 的
  相对差**（满桶首次立即通过、第二次才被 rate 拖住），宿主整体变慢时两次一起变慢不受影响。
- **`limiter_snapshot_test.go` 判据方向纠正**：改造前断言「未装闸门 = 快照零值」，把 D9 那个
  「生产从未接线」的缺陷当成了正确行为。现在要求装配后读数非零（零值即回归）。
- **覆盖率提升并固化基线**（issue #3）：`internal/chain` 0% → 88.6%、
  `internal/judge` 0% → 63.7%（含空输入 / 非法 JSON / 越界钳位 / 40 万字符超长负面用例）；
  `build.ps1 -PerPackageCover` 新增 `chain=80`、`judge=55` 诚实基线（只许抬不许降）。
- CI 触发分支补 `master`（建仓默认分支实为 master，此前 workflow 只写 main 导致 push 不触发）。
- Windows 8.3 短路径（`RUNNER~1`）导致的 CI 假失败：路径比较统一经 `evalExisting` 归一。
- `store.Update` 由原地写改为换指针，消除与 `runOne` defer 的真 data race（`-race` 哨兵抓出）。

## [1.3.0] - 2026-10-19

### Added
- **WAL 事件重建（P2-3）**：队列写路径从"每次迁移全量重写 queue.json"改为
  "追加 `queue.wal.jsonl` + 定期压缩成快照"。启动时按「快照 ⊕ WAL 回放」重建内存状态，
  崩溃后只丢未落盘的最后半行（回放时保守丢弃坏行及其后续）。
  - 新增 `store.Savepoint()` 语义：写快照 → fsync → 截断 WAL（任一步崩溃不丢数据）
  - 压缩阈值默认 2000 条，可在 `wal.threshold` 调整
  - 实测：100 次迁移 WAL 均 759 字节/条，而全量快照 100,786 字节 ⇒ 旧实现每次写放大 ~133×
- **CONTRIBUTING.md**：硬约束、本地环境、常用命令、提交前检查清单、代码组织约定、提交信息规范、发布流程、排障入口。
- **CHANGELOG.md**：按 Keep a Changelog 回填 v1.0.0 / v1.1.0 / v1.2.0 三个版本。

### Changed
- **executor 纯逻辑抽取（P1-2 收尾）**：把 `Run` 里的参数拼装、环境变量构造、超时解析、
  stderr 摘要、结果判定抽成纯函数（`BuildArgs`/`BuildArgsWithPatches`/`BuildEnv`/`ResolveTimeout`/
  `FinalizeResult`/`stderrSummary`/`effectiveMaxBytes`/`normalizeConcurrency`），`Run` 只留编排。
  executor 覆盖率 **14.4% → 27.4%**。
- `setup/build.ps1` 覆盖率基线更新：executor 14→26、新增 modelpool=49。
- `tests/e2e.ps1` 加固（应对同机并发会话互相杀进程）：
  1. 进程清理只针对「本验收目标 Exe 路径」，不再按进程名全杀；
  2. 新增 `Invoke-Api`/`Refresh-Session`：连接被拒时重新解析 panel.url 与 token 后重试；
  3. 非空产出率改为只看「本轮新产出」任务，历史遗留短件不计入（避免判据被历史数据绑架）。

### Fixed
- `store` 在 Windows 上长期持有 WAL 文件句柄会导致测试临时目录清理失败（`unlinkat: file in use`）——
  改为每次追加 open→write→sync→close，不持长期句柄。

## [1.2.0] - 2026-10-19

### Added
- **结构化日志（P1-1）**：新增 `internal/logging` 包，基于标准库 `log/slog` 输出单行 JSON 到
  `<DataRoot>/logs/workbench.jsonl`；提供 `At(component)` 子日志器、`Redact` 凭据打码、级别过滤。
- **执行钩子与执行器接口（P1-2）**：`executor.Hooks`（`BeforeRun`/`AfterRun`，钩子 panic 被 recover 且不影响主流程）；
  新增 `executor.RunnerContract` 接口与 `Engine.SetRunner` 注入点，引擎不再依赖具体执行器结构体。
- **指标接口（P1-3）**：`GET /api/metrics` 返回堆内存/Alloc/goroutine/句柄/GC、任务聚合、
  最近 30 条尝试台账与资源阈值上限；面板概览页新增「运行时指标」卡片。
- **失败原因分布（P1-4）**：`Engine.FailureBreakdown()` + `GET /api/failures`，
  按 `ErrorKind` 聚合失败任务（count/lastAt/samples，空态返回 `empty: true`）；面板新增「失败原因分布」卡片。
- **CI（P2-2）**：`.github/workflows/ci.yml`（windows-latest）：`go vet` → `go test -race` →
  `setup/build.ps1`（含覆盖率门禁）→ 产物校验（存在性/大小区间/`--version`/`--diagnose`）。
- 验收脚本新增第 9 段 OBSERVABILITY（12 项断言）。

### Changed
- `setup/build.ps1` 覆盖率门禁支持 `-PerPackageCover` 分档基线；默认阈值 55%。
- 引擎日志统一带 `component=engine` 字段，可按来源过滤。

### Removed
- 下线三处各自为政的裸日志写入：`engine-debug.log`（engine.debugf）、
  `supervisor.log`（自愈记录）、`crash.log`（panic 兜底）；内容并入 `workbench.jsonl`。

### Fixed
- 编辑/日志字段口径不一致导致的排障困难（同一事件分散在三个文件、格式各异）。

## [1.1.0] - 2026-10-18

### Added
- **错误分类重试策略**：新增 `internal/retry`（11 类失败原因 → retry/fail/cancel 三分判定，
  指数退避 + 向下抖动 + 可重试白名单；旧 `retryBackoffMs` 列表配置完全兼容）。
- **执行尝试台账**：`tasks/<id>/attempts.jsonl` 追加写，逐次记录 attempt/model/kind/action/delay/elapsed。
- **优雅停机两段 drain**：新增 `executor.shutdownGraceSeconds`（默认 20s），宽限期后强制取消在跑任务。
- **覆盖率门禁**：`setup/build.ps1` 新增 `-CoverThreshold`（默认 55）与 `-CoverGate` 相关开关。
- 对标报告：`docs/benchmark/WorkBench-开源对标报告.md`（8 个同类开源项目八维对比 + 证据附录）。

### Changed
- `Engine.CancelTask` 对运行中任务从"只写审计"改为**真正取消执行上下文**。
- `failTask` 不再把 `ErrorKind` 硬编码成 `execution`，改为写入执行器给出的真实分类。

### Fixed
- 任务完成后 `events.jsonl` 被空内容覆盖为 0 字节（现场 29 个任务受影响）。
- 被拒任务无法重试：状态机缺少 `rejected → queued` 通道，与 `RetryTask` 允许集矛盾。

## [1.0.0] - 2026-10-18

### Added
- 首个可用版本：Windows 单 EXE 桌面工作台（Go 纯标准库，7.4 MB，无外部运行时依赖）。
- 19 个内部包：配置/存储/队列/引擎/执行器/评估/链生成/语义判分/模型路由/模板/隔离 profile/面板/托盘/单实例/自愈/快捷方式/浏览器拉起。
- 24 条内置任务模板（6 领域 × 4），模板 CRUD、导入导出、恢复默认。
- 三维评估（价值/成本/可行性）+ 硬门禁（重复/高相似/冷却/日上限）+ 语义判分（异步降级安全）。
- 链式迭代：后继模板、自评 nextIdeas、前序产出摘要注入、链深度与扇出限制。
- 全自动闭环：生成 → 评估 → 自动批准 → 队列 → 执行 → 归档 → 自评 → 后继。
- 面板（7 页签 + SSE 实时推送）、MD/TXT/DOCX 导出、开机自启、系统托盘、崩溃恢复。
- 资源自愈：内存/句柄/goroutine 采样，连续越阈自动重启。
- 验收：e2e 20 项全过，单文件 EXE 净目录可跑。

### Known issues
- 7×24 长时压测未跑满 24h（已完成 3 轮长时 + 3 次崩溃注入）。
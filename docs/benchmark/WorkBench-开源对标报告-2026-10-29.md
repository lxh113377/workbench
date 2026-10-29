# WorkBench 开源对标报告（2026-10-29 · 第五轮）

> 对标主体：D:\workdesktop（个人 AI 自动化工作台，Go 1.27 纯标准库，Windows 单 EXE，7×24 无人值守）
> 取证时刻：2026-10-29 16:1x–17:1x（Asia/Shanghai），全部读数当日现取，不复用旧读数。
> peer 语料（本地克隆，逐行精读）：asynq HEAD `83e0966`(2026-10-19)、river HEAD `928c04a`(2026-10-24)、
>   temporal HEAD `1bcd882`(2026-10-24)、prometheus HEAD `a2fda0e`(2026-10-23)。
> 前序件：[首轮](./WorkBench-开源对标报告.md) · [第二轮 10-22](./WorkBench-开源对标报告-2026-10-22.md) ·
>   [第四轮 10-25](./WorkBench-开源对标报告-2026-10-25.md) · [peer 精读笔记](./peer源码精读笔记.md) ·
>   [知识盲区与学习计划](../知识盲区与学习计划.md)
> 本轮定位：**第五轮 = 错误分型面专项**。前四轮修的是「写盘容错 / 调度公平 / 观测接线」，
>   本轮修的是「失败到底被记成了什么」—— 这是 7×24 无人值守系统里**唯一能被远程读到的诊断入口**。

---

## 0. 本轮结论先行

| # | 判断 | 证据 |
|---|---|---|
| 1 | 生产面板「未知错误」桶 **39/39 全部**是同一条路径产生的，不是真的不知道原因 | §2.1，逐条复核 workbench.jsonl |
| 2 | 根因是「同一枚举被手抄在四处」，且**已复发四次**（transport / no_artifact / not_served / no_lane 各自的注释写的是同一句教训） | §2.2 |
| 3 | 顺带查出两处同源漂移，其中一处是**行为缺陷**：白名单与 Retryable 表脱钩 ⇒ 三类失败在生产里一次即判死 | §2.3 |
| 4 | peer 的对照做法明确：asynq / river 都**不留 residual 桶**，漏项直接 panic；本项目不能 panic ⇒ 等价手段是必红的机器门禁 | §3 |
| 5 | 本轮四项改造全部落地并测绿，判据做过变异负控（详见 §6 状态列） | §5 §6 |

---

## 1. 对标对象与对应模块

| peer | 本轮精读模块 | 定位 | WorkBench 对应模块 |
|---|---|---|---|
| hibiken/asynq | `processor.go` 失败分派、`recoverer.go`、`internal/errors`、`internal/rdb` | Redis 任务队列的**错误语义层** | `retry` / `engine` 的 kind 判定与台账 |
| riverqueue/river | `internal/jobexecutor`、`internal/jobcompleter`、`riverlog`、`rivertype` | Postgres 任务队列的**生命周期与持久留痕** | `store` 审计 / `keepalive.ps1` 守护留痕 |
| temporalio/temporal | （沿用第四轮）`common/tasks` | 工作流引擎调度内核 | `queue` / `engine` |
| prometheus | （沿用第四轮）`tsdb/wlog` | WAL 与损坏修复 | `store/wal.go` / `config/atomic.go` |

> 选择依据：本轮不从「缺什么功能」出发，而从**生产日志里读不通的那一桶**出发 ——
> 面板告诉我们有 39 个任务「未知错误」，这句话本身就是缺陷，而不是线索缺失。

---

## 2. 现状取证（当日现取）

### 2.1 缺陷一：判死的错误类别被写死成 unknown（高）

- **落点**：`workbench/internal/engine/engine.go:1686`（`requeueForPoolWait` 的封顶分支）
  `e.failTask(taskID, fmt.Errorf("模型池连续 %d 次不可用/限流，环境等待封顶判死", waits), retry.Unknown)`
- **面 A（日志面）** `workbench.jsonl` 全量 238 条 ERROR「任务失败」逐条核对：
  `boot_failed 190 / unknown 39 / transport_failed 4 / no_artifact 3 / bad_exit 1 / not_served 1`；
  **39 条 unknown 全部含「环境等待封顶判死」，0 条来自其它路径**；同窗 WARN「重投次数到顶」亦 39 条，一一对应。
- **面 B（任务面）** `queue.json`（5006 任务 / 73 条终态失败）里 `errorKind=unknown` 恰为 39 条，与判死数相等。
- **面 C（尝试面）关键分裂**：同一窗口 `attempts.jsonl` 380 行里 unknown 只有 2 行（0.5%）。
  原因：判死走 `engine.go:1686 → :1687 return`，**绕过了重试循环的 `AppendAttempt`（:1216）**，
  所以尝试面根本不留这行。**只看 attempts.jsonl 会得出「unknown 才 0.5%，没问题」的错判** ——
  这条口径警告写进 §7，防后续再做同类统计时重踩。
- **误导代价**：面板把这条路径显示成「未知错误」，运维据此会去查模型车道；
  真实原因是环境等待预算耗尽（`poolWaitBudget = 4`，T54 为解僵尸任务专设），与车道能力无关。
  对标 peer：asynq 把「租约过期」这类基础设施事实也建模成具名 `ErrLeaseExpired`（`recoverer.go:82`）；
  river 唯一的 unknown 是**带字段的具名类型** `UnknownJobKindError`（`rivertype/execution_error.go:60-82`），
  两家都不留 residual 桶。

### 2.2 根因：一份枚举被手抄在四处（这是第四次复发）

| 抄本位置 | 本轮前状态 |
|---|---|
| `executor/executor.go:116-154` | 16 个 Kind（含 `KindRateLimited`） |
| `retry/retry.go:24-54` + `ParseKind` 名单 | **15 个，缺 rate_limited** ⇒ `ParseKind("rate_limited")` 收敛成 Unknown |
| `config/config.go:396` 默认 `RetryableKinds` | 7 项（而 `Retryable()` 表判 9 类可重试） |
| `web/metrics_prom.go:68-72` 基线 + `web/assets_script.go:235` 中文标签 | 基线 12 项 / 标签 10 项（9 类 + done） |

- `retry.go` 里 transport(34-37)、no_artifact(44-45)、not_served(48)、no_lane(50-52) **四段注释写的是同一句教训**
  「不进 ParseKind ⇒ 台账只写 kind=unknown」。前四次都是「谁撞上谁补」，补完不防止下一次。
- 对标 peer 的处置强度：river 枚举漏项 → `panic("...unknown reason, river bug")`
  （`internal/jobcompleter/job_completer.go:87`）；asynq 的 code 表忘更新 `String()` → 当场崩
  （`internal/errors/errors.go:96`）。本项目是 7×24 无人值守，**不能 panic** ⇒
  等价手段是把这些漂移变成必红的测试（本轮新增 `retry/kind_drift_test.go`，见 §5 改造一）。

### 2.3 顺带查出的两处同源漂移（其中一处是行为缺陷）

| # | 事实 | 为什么严重 |
|---|---|---|
| 漂移 A | `Retryable()` 判 no_artifact / not_served / rate_limited 可重试，但 config 默认白名单没这三项 | `Policy.allowed()` 是 **Retryable() AND 白名单**（`retry.go:234-247`）⇒ 三类在生产里 **attempt0 即 fail**。台账实证：`task-7a431ac6ea86b4d2` = `kind=no_artifact action=fail delay=0`；`task-66cf148146331039` = `not_served` 同样 attempt0 即死。注释宣称可重试而行为判死 = **注释与行为矛盾** |
| 漂移 B | Prometheus 基线缺 `not_served / no_artifact / no_lane` | 这三类在「尚未发生」时不预置序列 ⇒ 按类别的 `rate()/increase()` 告警规则取不到样本，告警在最需要它的前夜是哑的 |

### 2.4 在册未闭环项的当前状态（本轮选题依据）

| 项 | 内容 | 本轮处置 |
|---|---|---|
| T67 | 报告内容门禁只做三节+证据，缺「分级」（目标验收语是「五轴审查**分级**出报告」） | **已做**（改造三） |
| T68 | 守护三条生命周期路径不落持久日志（实测 keepalive.log 18 行、拉起类 0 行） | **已做**（改造二） |
| T69 | `TestPlanStageSkipsWhenTierHasNoLane` 精确次数断言在慢 runner 上假红（CI job 40836683050） | **已做**（改造四） |
| T62 | `TestMainDispatchKeepsPlanSelftestFlag` 无真实模型即红 | 未做：半阻塞于上游配额，登记见 §4.4 |
| L3 | evaluate/modelroute/modelpool 覆盖 60/47/55，判据要求 ≥65 且基线==实测 | 未做：本轮不做「调阈值」，落 §4.4 待办 |
| H-5 | `lanes.go` 三个导出符号全仓零引用 | 未做：死代码清理与功能新增需分别判责，落 §4.4 |

---

## 3. 八维对比总览表（本轮增量口径）

| 维度 | WorkBench（2026-10-29 实测） | peer best | 量化差距 | 判定 |
|---|---|---|---|---|

| **功能模块覆盖** | 9 环节闭环 + 双角色 + 自主树 + 模型池 + WAL + aging + **失败具名分型 18 类**（两侧枚举实测各 18 个非空 Kind，以 `retry.AllKinds()` 为准） | asynq 四出口分派（retry/ack/archive/revoke）+ river 闭集 reason | 核心闭环不缺；本轮补齐「原因必须具名」这一层 | 本轮新发现（已修） |

| **技术架构** | 单进程 31 包（29 internal + 2 cmd），零第三方依赖（`go.mod` 无 require） | river 依赖 Postgres；asynq 依赖 Redis | 单机零依赖是**主动取舍**，代价是没有 DB 层做二次归因 ⇒ 日志/台账必须自己说清楚，这正是本轮修的点 | 已对齐（取舍不同） |

| **实现方式** | 30 包 `go test` 全绿；285 个受跟踪 `_test.go`（含本轮新增 3 个判据件）；错误分类现由**双向枚举门禁**守护 | river 表驱动 + 漏项即 panic；asynq 具名类型 + `errors.As` | 本项目此前靠「谁撞上谁补」，本轮起改为机器对账 | 本轮新发现（已修） |

| **性能** | EXE **10,385,920 B**（上界 20,971,520，余 10.1MB；下界 3,145,728 已过）；首页 **P95=1.78ms**（P50 1.39 / MAX 14.17，n=40）；常驻 **12.49MB**（界 64）；句柄 **126**（界 512）；线程 8 —— `tests/check_perf.ps1 -Runtime` 实测 VERDICT: PASS，判定 5/5、SKIP 0 | 不可比（peer 为服务端多租户） | 距各上界余量 30×–128×；本轮改动**未触动**性能面（同口径复测） | 已对齐（实数在案） |

| **可扩展性** | `RunnerContract` + Hooks + seam 注入（`queue.now`、`writeLedgerThenTransition`）+ 模板域 + 模型池；本轮新增 `retry.AllKinds()` 作**单一权威源** | river 插件（WorkBegin/WorkEnd）；temporal 调度器可插拔 | 派生式枚举让「加一个失败类别」只改一处 | 本轮增强（已落地） |

| **维护状态** | 7 天内 418 次提交；559 次提交总数；CI 双 workflow 当日全绿（run 40842404860 / 40840800793 success）；issue 模板 3 份 + PR 模板 59 行 + 47 个门禁脚本全部挂受理面 | n8n 日更级；river 中节奏 | 更新频率不输；**结构性空白 1 条**：`check_changelog.ps1` 只判结构（段序/日期/降序），没有「已落地代码必须有条目」判据 ⇒ 实测 10 笔代码提交未进 CHANGELOG 而门禁恒绿 | 有差距（在册，见 §4.4） |

| **文档完善度** | ARCHITECTURE + CONTRIBUTING + 对标报告**五代** + 学习计划 + 性能口径 + 37 段门禁自述 | river AGENTS.md 分卷 | 中文文档面充分；本轮修掉 ARCHITECTURE 两处**过期数字**（见 §5 改造一 D 项） | 已对齐（本轮补正） |

| **适用场景** | 个人 7×24 无人值守桌面，单机零依赖、可离线、可回收 | 团队 / 云端多租户编排 | 定位不同 ⇒ **不照抄** peer 的分布式面（leader election / DB 事务 / 多队列分片） | 不可比（取舍） |

---

## 4. 逐项差距说明

### 4.1 失败被记成「未知」，诊断入口本身失效（高，本轮已修）

一个声称「错误必须分型」的系统（CONTRIBUTING 硬约束 4），在生产最刺眼的一桶里写的是 unknown，
而且是**唯一一条**贡献这一桶的路径 —— 这意味着面板的「失败原因分布」对最关心的场景（整池不可用）
是失效的。peer 的对照很干脆：asynq 把租约过期、把 skip-retry、把 revoke 分别做成具名哨兵，
决策表里**没有 unknown 这一支**（`processor.go:335-349`，未知错误走「计一次重试、耗尽归档」的有界默认）。

### 4.2 注释与行为矛盾：可重试表与白名单 AND 之后一次即死（高，本轮已修）

`retry.go:75` 明写 no_artifact / not_served 可重试，`config.go:396` 白名单没它们，
`allowed()` 取 AND ⇒ 实际行为是一次判死。读代码的人按注释理解系统，得到的是错的因果。
这比缺一个类别更危险，因为它是「文档化了的行为」而行为相反。

### 4.3 可观测面的三类沉默（中，本轮已修两条）

| 沉默点 | 现场 | 处置 |
|---|---|---|
| 守护的拉起路径不留痕 | 实测 `keepalive.log` 18 行全是「心跳/挂死」，`grep -c 进程不在，拉起 = 0`；本轮 pid 28788→4016 的实例更替只能靠人盯 | 改造二：三条路径落 `Add-KaLog` + 两条留痕自检 |
| Prometheus 缺三类零值基线 | 告警规则取不到样本 | 改造一：基线改由 `retry.AllKinds()` 派生 |
| 面板九类裸显英文 | `kindText` 手抄 10 项（9 类 + done），而类别已有 18 类 ⇒ 九类无中文 | 改造一：补全 + 双向对账判据（含死标签反向检查） |

### 4.4 仍未闭环（如实登记，不臆造已完成）

| 项 | 为什么本轮不做 | 下一步的最小动作 | 优先级 |
|---|---|---|---|
| L3 覆盖率真实化（evaluate 60 / modelroute 47 / modelpool 55，判据要 ≥65 且基线==实测） | 本轮 4 项名额已用尽；且这项**禁止**用降阈值完成（学习计划 §B3 反对「调判据而非治病」） | 按 river 表驱动范式逐分支补测，边补边抬基线 | **中** |
| CHANGELOG「已落地必须有条目」门禁（实测 10 笔代码提交未写、门禁恒绿） | 判据需要定义「什么算落地代码变更」，属新增门禁设计，不宜与行为改动混在同一提交 | 在 `check_changelog.ps1` 加一条：近 N 笔含 `workbench/**.go` 改动的提交必须在 CHANGELOG 有对应条目（并配负控） | **中** |
| T62（无真实模型即红的测试） | 半阻塞于上游配额；根治要把测试改成可注入 fake 车道，属 `cmd/workbench` 重构 | 抽 `--plan-selftest` 的模型来源为可注入接口，脱网可跑 | 低 |
| H-5 `lanes.go` 三个零引用导出符号 | 「删」与「补调用点」是两种判责，需先确认是否为未接线的既定能力 | 逐个定性后二选一，别默认删 | 低 |
| C-ckpt / C-iws（prometheus checkpoint、temporal 执行感知调度） | 单机体量收益低（第四轮同判） | 维持不照抄 | 低 |

---

## 5. 改进建议清单（本轮四项全部落地）

| # | 建议 | 对应差距 | 优先级排序依据 | 预期目标 | 完成标准（可机器判定）→ 实测 |
|---|---|---|---|---|---|
| **改造一** | 失败具名分型：新增 `RateLimited/PoolWaitCapped/TaskPanic/TransitionFailed`，判死传真 kind，并加**双向枚举漂移门禁** | 4.1 4.2 4.3 | **高** —— 它是唯一的远程诊断入口；错到底 = 所有后续排查方向都错，且已实锤 39 例 | unknown 桶不再伪装；同类漏配不再靠人撞 | `go test ./internal/retry -v` 全 PASS + 变异负控 2 条各自判红（§6.1） |
| **改造二** | 守护三条生命周期路径写持久日志 + 留痕面自检 | 4.3 | **高** —— 无人值守下「跑过但没留痕」等于没跑；本轮 11 小时静默事故的根因就是查不动 | 事后可回答「这个实例是谁、何时、经哪条路径拉起」 | `keepalive.ps1 -SelfTest` 9/9 PASS + 两条负控各自判红（§6.2） |
| **改造三** | 报告内容门禁升级：加「必须分级」判据 | §2.4 T67 | **中** —— 属验收口径欠项，不阻断运行，但「可直接贴 PR」的承诺未兑现 | 不分级的报告不再判合格 | `TestReportGateProblems` 11 子用例全 PASS，含假阳性负控（§6.3） |
| **改造四** | 消掉精确次数断言造成的 CI 时序假红，语义面不放松 | §2.4 T69 | **中** —— CI 假红侵蚀「绿」的可信度；但改法必须自证不是调判据让它过 | 慢 runner 不再翻，且退回宽松换档必红 | `-race -count=5` 连跑绿 + 变异（PickStrict→Pick）判红（§6.4） |

> 执行顺序 1→2→3→4 的依据：**先修「读不通」（改造一、二），再修「判不准」（改造三），最后修「跑得抖」（改造四）**。
> 改造一、二同属「高」，改造一优先，因为它同时修掉行为缺陷（4.2 的 AND 一次即死），
> 而改造二只修可见性 —— 按「影响不可逆程度」排序，不是按工作量。

---

## 6. 可实施路径（分阶段 · 里程碑 · 实测状态）

| 阶段 | 内容 | 里程碑（可验收） | 状态 |
|---|---|---|---|
| M0 取证 | peer 精读 asynq/river + 生产两面（日志面/任务面/尝试面）对账 + 在册 T 号提取 | 本报告 §2 | 已完成 16:3x |
| M1 改造一 | retry/executor/config 三侧类别补齐 + engine 判死传真 kind + Prometheus 基线与面板标签改派生 + 漂移门禁 | `go test ./internal/retry ./internal/web` 绿 + 2 条变异判红 | 已完成 §6.1 |
| M2 改造二 | keepalive 三条路径落持久日志 + 两条留痕自检 + 修回被吃掉的 BOM | `-SelfTest` 9/9 PASS + 4 道 ps1 门禁 rc=0 | 已完成 §6.2 |
| M3 改造三 | 报告门禁加「必须分级」+ 更新 T66 旧基准的前提 | 11 子用例 PASS（含假阳性负控） | 已完成 §6.3 |
| M4 改造四 | tier 测试断言由「恰好 1 次」改为「≥1 次执行且无方案调用」 | -race ×5 绿 + 变异判红 | 已完成 §6.4 |
| M5 回归 | 全仓 `go vet` + `go test ./...` + `check_all` + 产物重建 | **31 包 ok / VET_RC=0 / check_all 49 段 PASS**（RC=0；1 段 SKIP 需参数、1 段 UNVERIFIED 为长驻内存口径） | 已完成 |
| M6 文档 | 本报告 + CHANGELOG + ARCHITECTURE 过期数字补正 + 记忆回写 | 门禁全绿、CHANGELOG Unreleased 居首 | 本文件 |
| M7 部署 | 干净 worktree（`f1addb9`）构建 → dist 与线上根换装 → 行为面取证 | 两根 sha256==`E8B661C6…`==SHA256SUMS、`vcs.revision==f1addb9`；新判死 kind 具名 | 已完成（§6.6） |
| M8 复验 | 本轮五个提交的双 workflow 终局 + 生产实例存活复核 | CI / Autonomy Container E2E 全 success；线上引擎仍在写台账（19:40 尝试开始/结束）、守护心跳正常、无 user-quit 残留 | 已完成 19:4x |

### 6.1 改造一实测（含负控）

```text
$ go test ./internal/retry/ -count=1 -v     -> 全 PASS（含 4 条新增漂移门禁）
$ go test ./internal/web/ -run "FailureLabel|Prom" -v -> 全 PASS
$ go test ./internal/engine/ -run PoolWait -v -> 3 条 PASS（含收紧后的具体 kind 断言）
负控 1：把 RateLimited 从 ParseKind 名单摘掉 ->
  --- FAIL: TestEveryExecutorKindParsesInRetry
  executor.Kind=rate_limited 在 retry 侧没登记 …   （还原后 ok，证明判据有牙）
负控 2：把 config 默认白名单改回 7 项 ->
  --- FAIL: TestRetryableTableMatchesConfigWhitelist
  类别 no_artifact / not_served / rate_limited 被 Retryable() 判可重试，却不在白名单 …（还原后 ok）
```

### 6.2 改造二实测（含负控）

```text
$ pwsh -File tests/keepalive.ps1 -SelfTest -> selftest 1..9 全对，末行 keepalive selftest: PASS（rc=0）
$ 四道门禁：check_ps1_syntax / check_ps1_encoding / check_multi_bom / check_noninteractive 全部 rc=0
负控 A：删掉「用户主动退出」那条 Add-KaLog ->
  selftest 9) … 期望 5 实得 4 / keepalive selftest: FAIL（rc=1）
负控 B：把 Add-KaLog 的落盘语句停掉（只留 Write-Host）->
  selftest 8) Add-KaLog 真落盘（非仅 stdout）⇒ 期望 True 实得 False / FAIL（rc=1）
另修：本轮接手时发现该文件 UTF-8 BOM 被前次编辑吃掉（check_ps1_encoding 实测 rc=1 点名），已按门禁
  口径只前置 3 字节、不重写正文，并把工作树行尾统一回 CRLF（原文件通体 CRLF）。
```

### 6.3 改造三实测

```text
$ go test ./internal/engine/ -run ReportGate -v -> TestReportGateProblems 11 子用例全 PASS
其中三条是本轮新增：
  · 三节齐、证据足但完全没分级 ⇒ 判不合格（T67 的核心欠项）
  · P0 形态算已分级（放行）
  · 「提高/其中/降低」这类构词**不得**当成已分级（假绿负控 —— 裸 Contains 会让任何中文报告自动合格）
前提更新（不是改断言迁就实现）：T66 当时的「实质报告」样本按新口径确实没分级，故升级为带严重度的形态，
  并新增 ungradedComplete 用例专门钉住「原样本现在会被拒」，防止口径变更被静默吸收。
```

### 6.4 改造四实测

```text
$ go test ./internal/engine/ -count=5 -race -run "PlanStageSkips|PlanStageRuns" -> ok（7.57s）
负控：把 plan_stage.go:106 的 PickStrict 改回 Pick（即退回被取消的静默换档）->
  --- FAIL: TestPlanStageSkipsWhenTierHasNoLane
  首调是方案调用 —— 说明还在静默换档拿执行车道出方案（还原后 ok）
⇒ 消抖没有削弱判据：放松的是「执行阶段重试了几次」（与本条要测的语义无关、且依赖 runner 快慢），
  保留并加强的是「方案阶段不得借用执行车道」。另补一条原断言的空隙：只看首调会被「方案排第二」绕过。
（首次变异打在函数上方注释里的同名字样，测试当然照绿 —— 那是我的变异打错位置，不是判据失效；
  已在 §6.4 重打真实调用点，如实记此过程以免被当成「判据有效」的证据。）
```

### 6.5 M5 回归的当前读数

```text
$ go vet ./...                      -> VET_RC=0
$ go test ./... -count=1            -> 30 包 ok，1 包 FAIL（internal/web/TestAssetsPresentInArtifact）
$ gofmt -l internal cmd             -> 空
唯一这一红**是本轮改动的预期后果**：我改了内嵌资产 `assets_script.go`（补全失败类别中文标签），
而 `dist/workbench.exe` 还是上一轮产物 ⇒ 该门禁逐字比对源码块与产物，报「产物落后」（并精确指出
差异在 appJS 第 231 行附近）。它不是代码缺陷，重跑 setup/build.ps1 即闭合 —— 已排进 M7。

**重建后复核（M7 完成时）**：`go test ./... -count=1` **31 包全 ok、零 FAIL**；
另用 `WB_ASSET_ARTIFACT` 直接指向新产物单测该判据亦 PASS ⇒ 该红确为「产物落后」而非代码缺陷，
且门禁按预期把「源码改了、产物没跟上」这一类发布事故钉在构建链里。
```


### 6.6 M7 部署与行为面取证（2026-10-29 17:3x–18:1x，实测）

部署走「干净 worktree 构建」以保证 **产物 == HEAD**：主工作区有并行会话的未提交件，直接构建会把未提交内容打进产物。

```text
$ git worktree add --detach xtmp/rel-f1addb9 f1addb9 && pwsh -File setup/build.ps1     # 覆盖率均值 72.53%
$ sha256sum dist/workbench.exe        -> e8b661c625761e08d1e5fe11768a2363d5a5e061313c6887fd999a54e423cdb0
$ go version -m dist/workbench.exe    -> vcs.revision=f1addb94e2125d564b369fcbfbe0d27044aa759f
$ tests/check_artifact_checksum.ps1   -> rc=0（清单读回自证：产物与 SHA256SUMS 逐字一致）
```

线上根（`D:/workbench-agent`，7×24 实例）的换装必须是**受控窗口**：`engine/stop` → `--quit`（落 user-quit 标记，
守护据此**不**抢拉起）→ 备份 `workbench.exe.bak-round66-*` → 覆盖 → 经 `lib_silent_launch` 起 → `engine/start` → 复核。实测：

```text
STEP3 sha256=E8B661C6… / STEP3 vcs.revision=f1addb9
STEP5 engine_state=running user_quit_marker_present=False
   （Engine.Start 清标记是产品侧既定语义，本轮复核它确实生效 —— 没这条，守护会永久拒绝接管）
二进制内含新类别（strings 计数）：pool_wait_capped=4、transition_failed=2、rate_limited=3
```

**行为面取证（关键：部署了 ≠ 生效了）**：换装后线上**没有**新的具名判死样本 ——
最后一条 unknown 落在换装前 39 秒，此后 `pool_wait_capped` 0 条、unknown 0 新增。
这只说明「窗口内没触发」，不构成生效证据（按 #36 禁止假完成）。因此另造隔离实例做受控复现：

```text
隔离做法：xtmp/round66-verify 自带数据根 + 端口 20912 + config 里 6 条车道全部 enabled=false
          ⇒ Router.Pick 必失败 ⇒ 引擎走 requeueForPoolWait；自主/链式/方案/池同步一并关闭。
          整条路径不发起任何模型调用（零额度、零 DSH 子进程），生产实例不受影响。
实测（logs/workbench.jsonl + queue.json）：
  18:08:09 / 18:08:27 / 18:08:45  三次 WARN「模型池冷却，调度暂停」（waitSeconds=15）
  18:09:03 WARN  模型池重投次数到顶，判死以解锁自主供料   poolWaits=4 budget=4
  18:09:03 ERROR 任务失败  kind="pool_wait_capped"  error="模型池连续 4 次不可用/限流，环境等待封顶判死"
  任务面  state=failed / errorKind=pool_wait_capped / poolWaits=4
  面板 /api/failures 自身读数  total=1  buckets=pool_wait_capped:1
    （这一条最要紧：坏掉的正是「失败原因分布」这个诊断入口，修复后由它自己报出具名类别）
  取证脚本退出码 0、末行 VERDICT_PASS（件已入库 tests/round66_live_verify.ps1）
⇒ 同一条判死路径：改造前落 unknown，改造后叫出自己的名字 —— 行为面实锤。
```

取证实例用毕回收（`KILLED 35392`），复核只剩生产实例 pid=11156（17891 HTTP 200、20912 DOWN），未留残件。

> 换装后 unknown 读数：46 条**全部**早于换装时刻（`unknown_before_deploy=46 / after=0`）。历史数据不改写
> （改它等于伪造证据）；面板上它们仍显示「未知错误」，但**从部署点起**同类判死会具名。
> 复算：`node` 读 `queue.json` 按 `finishedAt` 与换装时刻切分（口径 = 任务面 errorKind，非尝试面）。

### 6.7 改造二的 live 实证（守护留痕真的落盘了）

```text
$ tail -3 D:/workbench-agent/runtime/keepalive.log
17:53:03 keepalive: engine 心跳正常（日志距今 4.7 分钟，pid=24868，状态 running）
17:58:03 keepalive: 用户主动退出（进程不在）⇒ 本轮判定不拉起        ← 本轮新增的第三类留痕
18:03:03 keepalive: engine 心跳正常（日志距今 4.7 分钟，pid=11156，状态 running）
```

17:58 那行正是**本轮换装期间**产生的：我为开窗跑了 `--quit`，而守护此前对这种判断一个字都不写
（11 小时静默那场事故就是因此查不动）。现在它自己说明「这一轮为什么没动作」，18:03 的心跳又把新
pid=11156 记下来 ⇒ 实例更替从此机读可查，不必靠人盯 pid 猜。

### 6.8 本轮新增的一条工程纪律（由取证过程本身得出）

隔离取证的实例**不会**被生产守护接管：`tests/keepalive.ps1` 按 CommandLine 匹配的是
`dist\workbench.exe` / `D:\workbench-agent\workbench.exe`，取证的 `xtmp/round66-verify/workbench.exe`
不匹配 ⇒ 用完必须**自己收**（已实测 `KILLED 35392`）。这是好设计（守护不会乱动取证实例），
但也意味着取证脚本必须自带 teardown，否则每跑一次就留一个常驻进程。
---

## 7. 证据附录

### 7.1 peer 锚点（本地克隆，可逐行复算）

| 结论 | peer 文件:行 |
|---|---|
| skip-retry / revoke 是两个包级哨兵，决策表里没有 unknown 这一支 | `asynq/processor.go:327-349` |
| 租约过期这类基础设施事实也做成具名错误 | `asynq/recoverer.go:80-82` |
| 「重试」与「计一次失败」是两件事（IsFailure 开关） | `asynq/server.go:124-130`、`:407` |
| 枚举漏项当库内 bug 崩，不静默降级成 unknown 事件 | `river/internal/jobcompleter/job_completer.go:74-110` |
| 唯一 unknown 是带字段的具体原因（`Kind`），非 residual 桶 | `river/rivertype/execution_error.go:60-82` |
| 默认 logger 只 `os.Stdout` + Warn 级（生命周期行整条丢弃）= **反例** | `river/client.go:520-526` |
| 停机归因用 `context.Cause` 而不是猜字符串 | `river/internal/jobexecutor/job_executor.go:590-597` |
| 作业内日志按 attempt 落库、双容量上限并**自报截断** | `river/riverlog/river_log.go:210-236` |

### 7.1b 从 peer 提炼的可借鉴优势实现（本轮已吸收 / 待吸收）

| peer 的优势实现 | 出处 | 为什么值得借鉴 | 本项目落点 |
|---|---|---|---|
| **失败分派表里没有 unknown 这一支**：未知错误一律走「计一次重试、耗尽即归档」的有界默认，绝不静默丢弃 | `asynq/processor.go:335-349` | 兜底桶会让「已建模的原因」伪装成「不知道」，诊断入口当场失效 | **已吸收**（改造一：三个引擎侧具名类别 + 判死具名；`Decide` 的三分支本就同构） |
| **基础设施事实也做成具名错误**（租约过期 = `ErrLeaseExpired`） | `asynq/recoverer.go:80-82` | 「环境问题」不是低等信息，它决定该谁负责（运维 vs 任务作者） | **已吸收**（`PoolWaitCapped` / `TransitionFailed`） |
| **枚举漏项即崩，不留 catch-all** | `river/internal/jobcompleter/job_completer.go:87`、`asynq/internal/errors/errors.go:96` | 一致性靠崩溃暴露，而不是靠人记得改第二处 | **已吸收（改造形态）**：本项目 7×24 不能 panic ⇒ 换成双向对账判据 `kind_drift_test.go`，等价强度、零停机 |
| **重试与「算不算一次失败」分离**（`IsFailure` 谓词；不算失败就不吃次数、不污染错误率） | `asynq/server.go:124-130`、`internal/rdb/rdb.go:920` | 限流该重试，但不该被记成任务失败 —— 两件事混在一起会误导统计 | **部分已有**（`requeueForPoolWait` 不增 `Attempts` 正是此语义）；**待吸收** → issue #11 |
| **停机/取消用 `context.Cause` 覆写落库原因**，防 worker 泛错顶掉真相 | `river/internal/jobexecutor/job_executor.go:183-185`、`:590-597` | `context canceled` 本身歧义（取消/超时/停机都可能是它），单看 error 文本必误判 | **待吸收** → issue #11（生产已实测 2 条 `kind=unknown action=cancel`） |
| **作业内日志按 attempt 落库**，带 2MB/8MB 双上限并**自报截断与丢弃条数** | `river/riverlog/river_log.go:210-236` | stdout 汇聚日志在重试混排后无法按次还原；有损操作不自报，事后分不清「被截」与「本就没有」 | **待吸收**：本项目 `stderrPath` + `producedRunes` 已具备定位能力，缺「按 attempt 聚合的日志正文」 |
| **生命周期事件名常量化 + 服务名前缀自动生成** | `river/rivershared/…/river_shared_maintenance.go:22-24`、`baseservice/base_service.go:59-62` | 事件名是查询接口；散写字符串会让 grep 与告警随措辞漂移失效 | **待吸收**：本项目 `logging.At("组件名")` 已是同方向，缺固定词表常量 |
| **反例（明确不照抄）**：默认 logger 只写 `os.Stdout` + Warn 级，生命周期行整条丢弃 | `river/client.go:520-526`；`asynq/internal/log/log.go:75-88`（限流丢行） | 库可以「把落盘权交给宿主」，但本项目宿主就是自己，且是无人值守 ⇒ 丢弃 = 事故查不动 | **已反向吸收**（改造二：关键路径必须落 `keepalive.log`） |

> 一条通用的吸收姿势（本轮第三次验证）：**取 peer 的不变量，不取 peer 的结构**。
> river 用 Postgres 事务保证「状态与原因同批落库」，本项目用 `writeLedgerThenTransition` seam 达到同一不变量；
> temporal 用多通道 + inflight 计数解饿死，本项目用有界 aging（第四轮）。照抄结构会破坏「单 EXE 零依赖」的硬约束。

### 7.2 三面统计口径警告（本轮新增，防后续误判）

`attempts.jsonl`（尝试面）、`queue.json`（任务面）、`workbench.jsonl`（日志面）三面**不等价**：
任何绕过重试循环的终态路径（如判死）在尝试面**零行**。本轮实例：尝试面 unknown 0.5%，
任务面/日志面 86.7%（39/45）。⇒ 统计件必须同时输出至少两面并标注 per-attempt / per-task，
且对缺失的数据根显式标注「根不存在」，不得当成 0（生产实例数据根是
`D:/workbench-agent/workbench-output`，不是仓内 `dist/workbench-output`）。

### 7.3 复算命令
```bash
# 判死与 unknown 的一一对应（改造一的原始证据，修复前实测 39/39）
L='D:/workbench-agent/workbench-output/logs/workbench.jsonl'
p=$(grep '"msg":"任务失败"' $L | grep -c '"kind":"unknown"')
q=$(grep '"msg":"任务失败"' $L | grep '"kind":"unknown"' | grep -c '环境等待封顶判死')
echo "unknown=$p cap=$q"; [ "$p" -gt 0 ] && [ "$p" -eq "$q" ] && echo OK_ALL_FROM_POOL_WAIT_CAP || echo MISMATCH

# 部署后的正向判据：新产生的判死行必须叫出名字（旧数据不算，按 §7.2 切点看 finishedAt）
grep '环境等待封顶判死' $L | tail -3   # 期望 kind=pool_wait_capped；unknown 不应再新增

# 行为面受控复现（入库件，可重复跑；零模型调用、独立数据根、自动收尾）
#   前置：先把与待验代码一致的 workbench.exe 放到 xtmp/round66-verify/（或改脚本里的 $exe）
pwsh -NoProfile -File tests/round66_live_verify.ps1
#   期望末段：VERDICT_PASS | state=failed kind=pool_wait_capped poolWaits=4 … VERIFY_DONE

# 守护留痕（改造二）：三条路径是否真的落盘 + 13 条自检
pwsh -NoProfile -File tests/keepalive.ps1 -SelfTest        # 期望末行 keepalive selftest: PASS
tail -3 D:/workbench-agent/runtime/keepalive.log          # 应能看到 拉起/引擎已启动/用户主动退出 类文案

---

## 8. 本轮我自己犯的错（如实记录，不掩盖）

| # | 错 | 当场怎么发现的 | 固化的规矩 |
|---|---|---|---|
| 1 | 用 `edit` 替换 retry.go 常量块时，锚点含 `NoLane` 行而替换块未重新输出 ⇒ **静默删掉一个常量**（CHANGELOG 里记过的同型坑，我又踩了一遍） | `go build` 报 `undefined: NoLane` | 多行锚点的替换块必须把锚点内容**全部**重新输出；改完立刻 build，不攒批 |
| 2 | 第一次变异把 `PickStrict` 改回 `Pick` 时命中了**注释**里的同名字样，测试照绿，差点被读成「判据没牙」 | 复核发现文件有 3 处同名、第一处在注释 | 变异必须打在**真实调用点**，并在报告里写明打错位置的这一次不算证据 |
| 3 | 新增面板标签对账判据时，先按「单引号切分」取键、再按行取冒号前 —— 两次都因为 JS 表**折行写**而解析不全，判据把自己跑红了 | 判据比被测面更红 ⇒ 按纪律先怀疑判据 | 对账式判据必须自带「解析面自证」：结果为空或明显偏少一律判红（本仓 ㉑二） |
| 4 | PowerShell 自检里嵌套引号（形如 `[^"]` 的字符类）被 PS 当数组索引，脚本 ParserError | `check_ps1_syntax` 判红 rc=1 | 改 .ps1 必跑 check_ps1_syntax；引号判断改用 `-like` / `IndexOf` |
| 5 | 往 CRLF 文件里追加了 LF 行，造成行尾混排（虽然 `check_ps1_eol` 按入库形态放行） | `diff --stat` 行数与预期不符，遂去数行尾 | 追加前先读原文件行尾风格，按原风格写 |
| 6 | 第一次起隔离取证实例用 `spawn(detached)` 后被作业对象连坐回收，实例秒退、端口没起来，我先入为主以为是「产物起不来」 | 手跑 `--ui-probe` 看到 `UI_PROBE_READY` 后立刻 `UI_PROBE_EXIT`，才想起本仓为这个问题专门写了 `lib_silent_launch.ps1` | 凡拉起 workbench 实例一律走 `tests/lib_silent_launch.ps1`（按 PE 子系统分流、脱离作业对象），**不要自己 spawn** |
| 7 | 取证实例用完没自动收：生产守护按 CommandLine 只匹配 dist / agent 根，不匹配 `xtmp/round66-verify` ⇒ 它一直占着内存 | 复核 `tasklist` 发现两个 workbench 进程，逐个定位 Path 才确认哪个是我留下的 | 取证脚本必须自带 teardown（本轮补记为 §6.8）；交付前 `tasklist` 复核实例数 |

> 第 3、4 条的共性：**新写的判据本身要先被证明能红**。本轮每条新判据都配了负控实测，
> 记录在 §6 各小节；没有负控证据的判据不写进完成标准。

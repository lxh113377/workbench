# WorkBench 开源对标报告（2026-10-22 · 第二轮）

> 对标主体：`D:\workdesktop`（个人 AI 自动化工作台，Go 1.27 纯标准库，Windows 单 EXE）
> 取证时刻：**2026-10-22 09:18–10:10（Asia/Shanghai）**，全部读数当日现取
> 上游源码：本轮把 6 个 peer 仓 **clone 到本机**（`D:\workdesktop\xtmp\oss\`）逐文件精读，
> 不依赖上一轮报告的证据件副本 —— 行号均以本轮 clone 的 HEAD 为准。
> 前序件：[WorkBench-开源对标报告.md](./WorkBench-开源对标报告.md)（2026-10-18，八维首轮）、
> [peer源码精读笔记.md](./peer源码精读笔记.md)（2026-10-20，river/asynq 精读）、
> [四项工程改造交付报告-2026-10-20.md](../四项工程改造交付报告-2026-10-20.md)（P0-1~P0-4 落地）
>
> **本轮定位**：前两轮已完成「八维对标 + P0 改造 + P1 部分」。本轮做的是
> **第三轮深挖** —— 用本机 clone 的上游源码逐行比对，找出「前两轮漏掉的、peer 有而本项目仍缺的」
> 可实施改造点，并按优先级落地四项。

---

## 0. 本轮结论先行

| # | 判断 | 证据 |
|---|---|---|
| 1 | 前两轮的 8 个 P0/P1 项**已全部落地**（本地复跑确认：30 包 `go test` 全 ok、`go vet` 干净） | §2.1 实测 |
| 2 | 精读笔记 §4 登记的 **3 条可执行改进全部仍未做**（retry 非法值兜底 / config 跨字段校验 / failTask 留痕顺序） | §2.2 逐条实测 |
| 3 | 另发现 1 条新的真实缺口：**看门狗只认 stdout 事件，不认模型写文件**（长任务静默写盘期可被误判卡死） | §2.3 代码取证 |
| 4 | 本轮四项改造 = 上述 1+2+3+4，全部可机器判定、全部带变异验证 | §4 |

---

## 1. 对标对象（本轮 6 个，全部本机 clone）

| peer | 路径 | 定位 | 与本项目的对应模块 |
|---|---|---|---|
| riverqueue/river | `xtmp/oss/river` | Postgres 任务队列 | `queue` / `retry` / `store` / `engine` |
| hibiken/asynq | `xtmp/oss/asynq` | Redis 任务队列 | `queue` / `retry` / `executor` |
| temporalio/temporal | `xtmp/oss/temporal` | 持久化工作流引擎 | `model` 状态机 / `engine` 调度 / `store` 恢复 |
| prometheus/prometheus | `xtmp/oss/prometheus` | 监控与时序库 | `store` WAL / `web` 指标导出 |
| grafana/mimir | `xtmp/oss/machines` | 多租户监控后端 | `web` exposition / `supervisor` 资源治理 |
| n8n-io/n8n | `xtmp/oss/n8n` | 工作流自动化平台 | `engine` 执行编排 / 错误分级 |

> 选择依据：前两轮已对 8 个 peer 做过八维对比（见前序报告 §1）。本轮聚焦「Go 生态 + 可迁移的
> 具体实现模式」，故只保留 6 个能逐行读代码的，剔除只做 API 计数的（dify/AutoGPT/autogen/windmill）。

---

## 2. 现状取证（本轮现取，不复用旧读数）

### 2.1 前序改造的落地状态（本地实测）

```text
$ cd D:/workdesktop/workbench && go test -count=1 ./...
ok  workbench/cmd/workbench        31.124s
ok  workbench/internal/engine      10.872s   coverage: 74.2%
ok  workbench/internal/executor    11.232s   coverage: 49.2%
ok  workbench/internal/store       (cached)  coverage: 62.6%
ok  workbench/internal/retry       2.402s    coverage: 89.1%
ok  workbench/internal/queue       (cached)  coverage: 100.0%
ok  workbench/internal/web         4.036s    coverage: 56.8%
... 30 包全 ok

$ go vet ./...   -> 无输出（干净）
```

**逐项确认（对标 2026-10-18 报告 §4 的 P0/P1 清单）：**

| 前序项 | 代码落点（本机实测存在） | 状态 |
|---|---|---|
| P0-1 attempt 台账 + 事件流不覆盖 | `store.go:570 AppendAttempt`；`executor.go:336 events.jsonl` 只 `O_APPEND` | 已落地 |
| P0-2 错误分类驱动重试 | `internal/retry/retry.go`（11 类 Kind + 三分 Decision + 指数退避 + 抖动） | 已落地 |
| P0-3 优雅停机 + 运行中取消 | `engine.go:850 taskCancels`；`engine.go:1653 CancelTask` 调 cancel()；`ShutdownGraceSeconds` 两段 drain | 已落地 |
| P0-4 覆盖门禁 | `setup/build.ps1:96 -CoverGate` + `PerPackageCover` 14 项 | 已落地 |
| P1-1 slog 结构化日志 | `internal/logging`（覆盖 93.9%） | 已落地 |
| P1-2 执行钩子 + RunnerContract | `executor/hooks.go` + `runner_wiring.go` 能力接口下探 | 已落地 |
| P1-3 Prometheus 指标导出 | `web/metrics_prom.go`（17.7KB，exposition v0.0.4） | 已落地 |
| P1-4 失败分布视图 | `engine.go:1955 FailureBreakdown` + `/api/failures` | 已落地 |
| P2-3 WAL | `store/wal.go`（追加 + 阈值压缩 + 半行容错） | 已落地 |

### 2.2 精读笔记 §4 三条改进的落地状态（逐条实测 = 全部未做）

**改进 1：`retry.Policy` 缺「非法值 -> 告警 + 回落默认退避」**

- peer 依据：`river/internal/jobexecutor/job_executor.go:563-571`

```go
if nextRetryScheduledAt.Before(now) {
    e.Logger.WarnContext(ctx, "... Retry policy returned invalid next retry before current time; using default retry policy instead", ...)
    nextRetryScheduledAt = e.DefaultClientRetryPolicy.NextRetry(jobRow)
}
```

- 本项目现状：`internal/retry/retry.go:173-199 nextDelay()` 对非法值**只静默钳制**
  （`if base > p.MaxDelay { base = p.MaxDelay }`、`if base < time.Millisecond { base = time.Millisecond }`），
  **无告警、无回落语义、无痕迹**。
- 风险形态：`RetryBackoffMs` 被写成 `[0, -1, 0]` 或 `BaseDelayMs` 被写坏时，
  7x24 无人值守系统按一个说不清的值在退避，日志里查不到任何异常。

**改进 2：`config.normalize()` 缺跨字段一致性校验**

- peer 依据：`river/client.go:528-534`

```go
rescueAfter := maintenance.JobRescuerRescueAfterDefault
if c.JobTimeout > 0 && c.RescueStuckJobsAfter < 1 && c.JobTimeout > c.RescueStuckJobsAfter {
    rescueAfter = c.JobTimeout + maintenance.JobRescuerRescueAfterDefault
}
```

- 本项目现状：`internal/config/config.go:407-591 Normalize()` 有 **31 处单字段钳制**
  （`< 1` / `> 6` / `< 30 || > 3600` 等），**零处跨字段约束**。
- 已存在的矛盾组合（可复算）：
  - `StallSeconds=420`（默认）vs `DefaultTimeout=30 分钟`：任务超时先于卡死判定触发，
    卡死看门狗对超时类任务**永不生效**。river 的对应注释（`job_executor.go:392`）明确要求
    「RescueAfter 必须 > JobTimeout，否则误判」——本项目没有这条约束，也没有任何读数提示。
  - `Plan.PlannerMaxMinutes`（默认 8）与 `Executor.DefaultTimeout`（30）无约束关系；
  - `Plan.ReplanRounds`（2）与 `Executor.RepairRounds`（3）目前靠 `plan_config_test.go:96` 一条
    **测试**断言大小关系，但 `Normalize()` 本身不保证（测试只测默认配置，手改配置可突破）。

**改进 3：`failTask` 的「状态已迁、台账未写」顺序**

- peer 依据：`asynq/processor.go:336-338` —— 错误处理钩子 `errHandler.HandleError(...)`
  在 `switch` **之前**调用，「任何一条失败路径都留痕」在结构上不可能被跳过。
- 本项目现状：`engine.go:1542 failTask()` 的顺序是
  `store.Get` -> `store.Update`（改状态）-> `store.Audit` -> `log.Error` -> `emitEvent` -> `eval.Observe`。
  `Update` 成功而 `Audit` 失败时（磁盘满/句柄占用），**状态已迁移但审计缺失**；
  更关键的是 `Update` 返回错误时（第 1569 行）只 emit 一条 error 事件，
  **没有 audit、没有 attempt 台账记录** —— 任务停在 running 但没有任何结构化痕迹。

**改进 4（本轮新发现）：看门狗只认 stdout 事件，不认模型写文件**

- peer 依据：river `watchStuck`（`job_executor.go:335-378`）以 **context 取消响应**为观测点；
  asynq `recoverer`（`recoverer.go`）按 **lease deadline** 判定。
- 本项目现状：`executor.go:378-380`

```go
var lastActive atomic.Int64
lastActive.Store(time.Now().UnixMilli())
res.Touch = func() { lastActive.Store(time.Now().UnixMilli()) }
```

  `res.Touch()` **只在 `consume()` 解析到一条 stdout 事件时被调用**（`executor.go:570-572`）。
  而模型写产物文件（`artifact` 协议：文件清单 + 内容 + 验收命令）往往先落盘、
  最后一个 `final` 事件才到来 —— 长文写作类任务的「静默写盘期」可达数分钟，
  此期间 `lastActive` 不刷新。若该期超过 `StallSeconds`（默认 420s），
  **任务被误判卡死并杀进程树**，已写出的产物一并丢失。
- 现有 `SawThinking` 自适应（`executor.go:412`）只覆盖「出现过 thinking 帧」这一种情形，
  覆盖不到「正在写文件」这一种。

---

## 3. 八维对比总览表

> 评分口径：已对齐 / 有差距在册 / 本轮新发现缺口 / 不可比（取舍）

| 维度 | WorkBench（2026-10-22 实测） | peer best | 量化差距 | 判定 |
|---|---|---|---|---|
| **功能模块覆盖** | 9 环节闭环 + 双角色方案层 + 自主目标树 + 契约治理 + 模型池 + WAL | river 完整 attempt 生命周期 / temporal 事件溯源 | 核心闭环不缺；缺「策略非法值兜底」「配置跨字段自洽」 | 有差距在册 |
| **技术架构** | 单进程 30 包 + 内存队列 + WAL/JSON + 能力接口下探 | river leader election + 插件钩子 / temporal 事件溯源 | 单机取舍合理；新增两条**内核自洽缺口** | 本轮新发现 |
| **实现方式** | 30 包全有测试（最低 2.5%），engine 74.2% / retry 89.1% / queue 100%；错误 11 类分级 | river 测试比 48.8% / asynq 38.8% | 测试面已反超 river；缺「跨字段校验」这类防御性编程范式 | 本轮新发现 |
| **性能** | 首页 P95 1.18ms / EXE 14.2MB / 常驻 12.41MB / 句柄 139 | 不可比（peer 为服务端多租户） | 本地单机口径，非差距项 | 不可比 |
| **可扩展性** | RunnerContract + Hooks + 能力接口 + 模板机制 + 模型池装饰器 | river 插件（WorkBegin/WorkEnd/Middleware）/ asynq middleware | 已有钩子；缺「策略兜底」这类可注入默认值通道 | 有差距在册 |
| **维护状态** | 本地仓 + 6 条 CI workflow + CHANGELOG 滚动 | n8n 113 条 workflow / river 6 条 | 已有 CI；**未推送远端**（本地领先 45 提交） | 有差距在册 |
| **文档** | ARCHITECTURE 单页 + CONTRIBUTING（11 节）+ 使用说明 + 对标报告 3 轮 + 知识盲区件 | river AGENTS.md 8.3KB + 分卷 references | 中文文档面充分；对标报告已三代演进 | 已对齐 |
| **适用场景** | 个人 7x24 无人值守桌面 | 团队/云端编排 | 定位不同，不照抄 peer 的分布式面 | 不可比 |

---

## 4. 逐项差距说明（只写本轮新增或仍未闭环的）

### 4.1 策略层无非法值兜底（高）

`retry.Policy.nextDelay()` 的三处静默钳制（超上限、低于下限、抖动下溢）让「配置写坏」
与「配置正确但值很小」这两种情形在日志里**完全同形**。river 的做法是显式
`WarnContext + DefaultClientRetryPolicy`：既让运维看得见，也让系统用一个**可信默认值**继续跑。
对 7x24 无人值守而言，后者比前者更重要 —— 静默钳制的值可能恰好让退避退到 1ms，
在高频失败时变成热循环。

### 4.2 配置层无跨字段自洽校验（高）

31 处单字段钳制保证的是「每个字段各自合法」，但**字段之间的关系**无人保证。
已实测存在的矛盾：`StallSeconds(420s) < DefaultTimeout(30min)`，看门狗对超时任务永不触发。
river 用一条 `if c.JobTimeout > 0 && c.RescueStuckJobsAfter < 1 && ...` 就把这类关系收敛了。
本项目缺的是同类判据 + 一次**启动时**的对账（而不是等故障发生）。

### 4.3 失败路径的留痕完整性（中）

`failTask` 主路径（Update 成功 -> Audit -> log -> emit）是完整的，
但两条边界路径不完整：① `Update` 失败时只有一条 error 事件，无 audit/attempt；
② `Audit`/`log` 失败时状态已迁移但无痕迹。
asynq 的结构（`processor.go:336-338`）把「留痕」放在决策**之前**，从结构上排除这类窗口。

### 4.4 看门狗活跃度判据覆盖不全（高）

`res.Touch()` 只在 stdout 事件到来时刷新。模型的「写文件但不吐事件」阶段
（artifact 协议的常态）不刷新活跃度，长文任务可能被误判卡死并杀掉，
**已产出的文件一起丢**。这是本轮新发现的、会直接造成产出损失的缺口。

---

## 5. 改进建议清单（按优先级）

> 排序依据（沿用前序报告 §4 的三条 + 本轮补一条）：
> ① 是否已实测复现 ② 是否阻塞 7x24 可靠性 ③ 是否结构性根因 ④ 修复成本

| # | 建议 | 对应差距 | 优先级 | 预期目标 | 完成标准（可机器判定） |
|---|---|---|---|---|---|
| **P2-1** | `retry.Policy` 增加「非法值 -> 告警 + 回落默认退避」 | 4.1 | **高** | 配置写坏时既留痕又用可信值继续跑 | 单测：喂 `BackoffMs:[0,-1]` / `MaxDelay<BaseDelay`，告警可观测 + delay 落在默认区间；变异「去掉回落」判红 |
| **P2-2** | `config.normalize()` 增加跨字段一致性校验 | 4.2 | **高** | `StallSeconds`/`DefaultTimeout`/`Plan` 之间的关系被保证 | 单测：`StallSeconds=420, DefaultTimeout=1` 至少有一条 stall 语义告警；`ReplanRounds >= RepairRounds` 被钳制；变异「删跨字段段」判红 |
| **P2-3** | `failTask` 留痕前置 + 边界路径补痕 | 4.3 | 中 | 任何失败路径都有 audit/attempt 痕迹 | 单测：注入 `Audit` 失败仍有一条 attempt 记录；变异「把留痕移到 Update 之后」判红 |
| **P2-4** | 看门狗活跃度纳入「workspace 产出文件 mtime」 | 4.4 | **高** | 写盘期不再被误判卡死 | 单测：桩执行器只写文件不出事件，不被判 stalled；变异「只认事件」判红 |

> 执行顺序：**P2-4（防产出损失）-> P2-1（策略自愈）-> P2-2（配置自洽）-> P2-3（留痕完整性）**。
> 理由：P2-4 直接保护已产出的工作成果（不可逆损失），优先级最高；P2-1/P2-2 是自愈与自洽
> （故障可恢复但不可见）；P2-3 是观测面完善（不影响执行正确性）。

---

## 6. 可实施路径（分阶段 · 里程碑）

| 阶段 | 内容 | 里程碑（可验收） | 状态 |
|---|---|---|---|
| M0 对标 | 6 peer clone + 逐行精读 + 前序改造落地复核 | 本报告 §2 | 已完成 2026-10-22 10:10 |
| M1 P2-4 | 看门狗活跃度双信号 | `IsStalled` 5 条判据绿 + 变异判红 | 已完成 2026-10-22 10:20 |
| M2 P2-1 | retry 非法值兜底 + 修正留痕 | retry 18 条判据绿（覆盖 95.7%）+ 变异两条判红 | 已完成 2026-10-22 10:35 |
| M3 P2-2 | config 跨字段校验 + 迁移判据修正 | config 7 条新判据绿（覆盖 70.0%）+ 变异两条判红 | 已完成 2026-10-22 10:50 |
| M4 P2-3 | 留痕先于状态生效 + seam | engine 6 条新判据绿（覆盖 75.3%）+ 变异两条判红 | 已完成 2026-10-22 11:10 |
| M5 回归 | 全仓 `go vet` + `go test -cover ./...` + CHANGELOG 门禁 | 30 包全 ok、无包低于 PerPackageCover、changelog rc=0 | 已完成 2026-10-22 11:15 |

---

## 7. 证据附录

### 7.1 本轮 clone 的 peer 源码（可复算）

```powershell
git clone --depth 1 https://github.com/riverqueue/river.git      D:/workdesktop/xtmp/oss/river
git clone --depth 1 https://github.com/hibiken/asynq.git         D:/workdesktop/xtmp/oss/asynq
git clone --depth 1 https://github.com/temporalio/temporal.git   D:/workdesktop/xtmp/oss/temporal
git clone --depth 1 https://github.com/prometheus/prometheus.git D:/workdesktop/xtmp/oss/prometheus
git clone --depth 1 https://github.com/grafana/mimir.git         D:/workdesktop/xtmp/oss/machines
git clone --depth 1 https://github.com/n8n-io/n8n.git             D:/workdesktop/xtmp/oss/n8n
```

### 7.2 关键行号索引（以本轮 clone HEAD 为准）

| 结论 | peer 文件:行 |
|---|---|
| 策略非法值回落 | `river/internal/jobexecutor/job_executor.go:563-571` |
| 亚秒重试与调度间隔约束 | `river/internal/jobexecutor/job_executor.go:573-584` |
| attempt >= max 直接 discarded | `river/internal/jobexecutor/job_executor.go:549-554` |
| 跨字段 rescueAfter 推导 | `river/client.go:528-534` |
| 卡死看门狗（context 取消响应） | `river/internal/jobexecutor/job_executor.go:335-378` |
| 错误钩子先于状态决策 | `asynq/processor.go:336-338` |
| 重试语义三分（SkipRetry/RevokeTask） | `asynq/processor.go:327-348` |
| 租约失效不写存储 | `asynq/processor.go:352-354` |
| 调度器家族（FIFO/顺序/加权轮转） | `temporal/common/tasks/scheduler.go` + 9 种实现 |

### 7.3 本项目关键行号索引

| 结论 | 本项目 文件:行 |
|---|---|
| nextDelay 静默钳制 | `workbench/internal/retry/retry.go:173-199` |
| Normalize 单字段钳制（31 处） | `workbench/internal/config/config.go:407-591` |
| failTask 顺序 | `workbench/internal/engine/engine.go:1542-1581` |
| 看门狗活跃度刷新点 | `workbench/internal/executor/executor.go:378-380, 570-572` |
| 看门狗阈值自适应 | `workbench/internal/executor/executor.go:393-426` |

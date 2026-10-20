# peer 源码精读笔记（对标证据件 · 2026-10-20）

> 目标第一段要求的「阅读核心代码、理解设计模式与架构决策」落盘件。
> 证据件 = riverqueue/river 与 hibiken/asynq 的**核心执行文件原件**，按 commit 固定取数（可复算），
> 不是转述、不是 README。每个结论都带 `文件:行号`，行号以本目录证据件为准。

## 0.  provenance（取数可复算）

| 证据件 | 上游路径 | commit | 取数时间 | 字节 |
|---|---|---|---|---|
| `river_job_executor.go` | riverqueue/river `internal/jobexecutor/job_executor.go` | `1354a059d7cbe5790acc7a755a2ab7ac6d2f1ed8`（2026-10-19T18:47:37Z） | 2026-10-20 21:0x | 21,873 |
| `river_client.go` | riverqueue/river `client.go` | 同上 | 同上 | 120,610 |
| `river_job.go` | riverqueue/river `job.go` | 同上 | 同上 | 4,941 |
| `asynq_processor.go` | hibiken/asynq `processor.go` | `83e0966ab47a0ea99f66094afe4a32bbfab80747`（2026-10-19T16:56:45Z） | 同上 | 15,841 |
| `asynq_server.go` | hibiken/asynq `server.go` | 同上 | 同上 | 24,650 |
| `n8n_error_classes.md` | n8n-io/n8n `packages/errors`（三分规范摘录） | 2026-10-18 取 | — | 683 |

复算命令：

```powershell
# 取 commit：
Invoke-RestMethod "https://api.github.com/repos/riverqueue/river/commits?per_page=1" -Headers @{'User-Agent'='wb'}
# 取文件（务必按 commit 而非分支，否则读数会随上游漂移）：
Invoke-WebRequest "https://raw.githubusercontent.com/riverqueue/river/<sha>/internal/jobexecutor/job_executor.go" -OutFile docs/benchmark/evidence/river_job_executor.go -UseBasicParsing
```

> ⚠ **如实登记一处文档腐坏**：本报告 §7.3 自 2026-10-18 起就承诺这 6 个证据件，
> 但 `git ls-files docs/benchmark/evidence` 实测只有 1 个（n8n 那份），其余 5 个从未入库 ——
> 「引用了不存在的文件」正是本项目在缺陷清单里抓过两次的老毛病（引用未入库件）。
> 2026-10-20 由本轮补齐并固定 commit。**结论：引用即须入库，入库须带 provenance。**

---

## 1. river：重试策略的对象化与「非法值兜底」

### 1.1 策略是接口，输入是 job 行（`river_job_executor.go:28-30`）

```go
type ClientRetryPolicy interface {
	NextRetry(job *rivertype.JobRow) time.Time
}
```

一个方法、输入是**完整的 job 行**（含 `Attempt` / `MaxAttempts` / `Kind`），而不是「第几次重试」这种退化参数。
含义：策略自己就能表达「按任务类型给不同退避」「第 3 次以后直接放弃」，不必改执行器。

### 1.2 决策优先级与**非法值兜底**（`river_job_executor.go:549-571`）

实测顺序值得抄：

1. `jobRow.Attempt >= jobRow.MaxAttempts` → 直接 `JobSetStateDiscarded`（`:549-554`），**不进重试计算**；
2. 先看 `WorkUnit.NextRetry()`（任务自定义，`:557-559`），为零再落 `ClientRetryPolicy`（`:560-562`）；
3. **策略返回过去时间 = 非法输入** → `WARN` 记日志并**回落到默认策略**（`:563-571`）。

第 3 条是本轮新认知。对照本项目 `internal/retry.Policy.NextDelay`：
我们只做了「算 delay + 抖动 + 单次上限」，**没有「策略给出非法值时回落并留痕」这一层**。
风险形态：配置被写坏（`RetryBackoffMs` 给出 0 或负数）时，我们的行为取决于下游钳制，
而不是「明确告警 + 用可信默认值继续跑」——对 7×24 无人值守来说后者才是可运维的。

### 1.3 亚秒级重试的真实约束（`river_job_executor.go:573-584`）

注释点出一个我们也会撞的细节：错误任务默认置 `retryable`、由调度器捞回 `available`，
而调度器有自己的运行间隔 ⇒ **比调度间隔短的重试时间实际不被尊重**。
river 的解法：`nextRetryScheduledAt - now <= SchedulerInterval` 时直接置 `error_available`（`:580-581`）。
对照本项目：`queue` 侧靠 `time.After` 退避，语义上等价（不隔调度周期再入队），
但**没有把这个约束写成注释/判据** —— 后人把退避改小到秒级以下时容易误以为生效了。

---

## 2. asynq：用哨兵错误表达「重试语义三分」（`asynq_processor.go:327-349`）

```go
var SkipRetry  = errors.New("skip retry for the task")   // :329  不重试，归档
var RevokeTask = errors.New("revoke task")               // :333  不重试也不归档（视为用户主动撤销）

switch {                                                  // :339-348
case errors.Is(err, RevokeTask):      p.markAsDone(l, msg)
case msg.Retried >= msg.Retry || errors.Is(err, SkipRetry): p.archive(l, msg, err)
default:                              p.retry(l, msg, err, p.isFailureFunc(err))
}
```

另有两处细节直接对上我们本轮修的缺陷：

- `retry()` 开头 `if !l.IsValid() { return }`（`:352-354`）—— **租约失效就不写存储**，把善后交给 recoverer。
  这正是「producer 侧兜底」的形态（对标报告 §3.2 里 river `RescueStuckJobsAfter` 的同一思路），
  与本项目 L4 崩溃救援额度同源：**先确认自己还有权写，再决定写什么**。
- `p.errHandler.HandleError(...)` 在分流**之前**调用（`:336-338`）—— 错误处理钩子先于状态决策，
  保证「任何一条失败路径都留痕」。对照本项目 D12：我们把构造期告警写进了 Nop（静默），
  asynq 的结构让「留痕」不可能被跳过。

对照本项目 `internal/retry.Decide`：三分（Retry/Fail/Cancel）语义**已等价实现**，
且比 asynq 更进一步（按 11 类 `ErrorKind` 映射，而不是只认两个哨兵错误）。
这一项可以明确写进自评：**没有落后**。

---

## 3. river：停机与卡死救援的三段式设计（`river_client.go`）

| 机制 | 行号 | 上游原文要点 | 本项目对应 | 判定 |
|---|---|---|---|---|
| `SoftStopTimeout` | `:411-435`、`:1217-1220` | 软停最多等这么久；**到期不是放弃，而是取消 work context**（到期后仍未完成的 job 其上下文被取消） | `Engine.Stop()` 两段 drain + `ShutdownGraceSeconds` + `cancelAllRunning` | ✅ 语义已对齐（P0-3） |
| `RescueStuckJobsAfter` | `:379-397` | job 运行超过这个时长即判「卡住」，由 **producer 侧**救援；另有约束「必须 > JobTimeout，否则误判」（`:392`） | L4 `decideRescue` + `CrashRescues` 额度（启动时扫 running 残留） | 🟡 形态不同：我们是启动时一次性扫，river 是持续轮询 —— 长生命周期进程里**我们的覆盖不到「跑着跑着僵了且进程没死」**（现靠 stall watchdog 兜，但它只管子进程不管台账） |
| `JobStuckHandler` | `:212-223` | 检测到卡住时回调，实现方可决定「是否再开一个 worker 槽位」 | 无对应 | 🟡 可作为「重试额度策略」的注入点（我们目前硬编码额度分流规则在 `decideRescue`） |
| `HookWorkBegin/End` | `:246-252` | 钩子按阶段分（insert 前后 / work 前后），且注释明确「两个 HookInsertBegin 之间夹一个 HookWorkBegin 的嵌套语义」 | 本轮 L5 的 `AttachHooks` + `Inner()` 链下探 | ✅ 已对齐；river 的分阶段命名比我们的 before/after 更细，可作下一步参考 |

`client.go:529-532` 还有一处**「配置自洽性校验」**值得抄：
`JobTimeout > 0 && RescueStuckJobsAfter < 1 && JobTimeout > RescueStuckJobsAfter` 时自动取一个保守的 `rescueAfter`。
本项目 `config.normalize()` 已对 `requestsPerMinute` / `requestBurst` 做同类钳制，
但**没有跨字段约束**（例：`StallSeconds` 与 `DefaultTimeout` 的关系、`RepairRounds` 与 `MaxAttempts` 的关系）。
登记为待办（见交付报告 §6）。

---

## 4. 本轮从精读中直接产出的三条可执行改进

| # | 来自 | 改进 | 优先级 | 状态 |
|---|---|---|---|---|
| 1 | `river_job_executor.go:563-571` | `retry.Policy.NextDelay` 增加「非法值（≤0 或超出单次上限）→ WARN + 回落默认退避」，不静默钳制 | 中 | 待办 |
| 2 | `river_client.go:529-532` | `config.normalize()` 增加**跨字段**一致性校验（stall/timeout/repair/maxAttempts 之间的时序约束） | 中 | 待办 |
| 3 | `asynq_processor.go:336-338` | 错误留痕必须在状态决策**之前**（与 D12 的「先留痕后生效」同型）→ 审一遍 `failTask` 是否可能出现「状态已迁、台账未写」 | 高 | 待办（L9 已有同类修复，需扩展到 fail 路径） |

---

## 5. 自评口径（避免虚高）

- **不落后的**：重试三分语义（我们按 11 类 Kind 映射，比 asynq 两个哨兵错误更细）、
  优雅停机取消语义、错误分类进台账与指标（D2 本轮闭环）、覆盖率门禁与变异测试文化。
- **确实落后的**：卡死救援是「启动时一次扫」而非持续轮询；钩子只有 work 级、无 insert 级；
  配置校验缺跨字段约束；策略无非法值兜底。
- **不可比的**：peer 是服务端多租户（DB/Redis/leader election），本项目是单机单 EXE 便携交付。
  照抄外部存储与分布式协调会把「零依赖」这条硬约束打掉 —— 这类差异记为取舍，不记为差距。

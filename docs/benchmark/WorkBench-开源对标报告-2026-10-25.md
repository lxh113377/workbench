# WorkBench 开源对标报告（2026-10-25 · 第四轮）

> 对标主体：`D:\workdesktop`（个人 AI 自动化工作台，Go 1.27 纯标准库，Windows 单 EXE）
> 取证时刻：**2026-10-25 01:5x–02:2x（Asia/Shanghai）**，全部读数当日现取
> 上游源码：本轮 peer 仓已 `git fetch` 刷新到最新 HEAD（`xtmp/oss/`），逐行精读：
>   temporal HEAD `1bcd882`（2026-10-24）、prometheus HEAD `a2fda0e`（2026-10-23）、
>   river HEAD `928c04a`（2026-10-24）。行号以本轮刷新后的 clone 为准。
> 前序件：[首轮 2026-10-18](./WorkBench-开源对标报告.md)、[第二轮 2026-10-22](./WorkBench-开源对标报告-2026-10-22.md)、
>   [peer 源码精读笔记](./peer源码精读笔记.md)。
>
> **本轮定位**：前三轮已走完「八维对标 + P0/P1 改造 + P2 改造 + 阻塞解除」。本轮是
> **第四轮深挖**——首次逐行精读前几轮未读过的 peer 模块（temporal `common/tasks` 调度器家族、
> prometheus `tsdb/wlog` WAL），从中挖出**前几轮漏掉的两个真实结构缺陷**并落地修复。

---

## 0. 本轮结论先行

| # | 判断 | 证据 |
|---|---|---|
| 1 | 前两轮的 P0/P1/P2 项**已全部落地**（本地实测：30 包 `go test` 全 rc=0、`go vet` 干净、`check_all` 37 段全绿） | §2.1 |
| 2 | 精读 prometheus `tsdb/wlog` 的 `Repair()` 挖出**本轮缺陷一**：`WriteFileAtomic` 兜底路径「先删后建」可致**数据归零** | §2.2 / §4.1 |
| 3 | 精读 temporal `common/tasks` IWRR 权重公平挖出**本轮缺陷二**：任务队列纯严格优先级可致**低优任务饿死** | §2.3 / §4.2 |
| 4 | 核实 temporal `FIFOScheduler.Stop/drainTasks/Abort` 后判为**本项目 NO-OP**（不臆造工作） | §4.3 |
| 5 | 本轮两项修复**均已落地**，各带 seam/变异双向验证 | §5 |

---

## 1. 对标对象（本轮聚焦前几轮未逐行读的模块）

| peer | 本轮精读模块 | 定位 | 与本项目的对应模块 |
|---|---|---|---|
| temporalio/temporal | `common/tasks/` 调度器家族（10 种）| 持久化工作流引擎的**任务调度内核** | `queue`（排序/公平性）/ `engine`（并发信号量）|
| prometheus/prometheus | `tsdb/wlog/`（WAL + Repair + checkpoint）| 时序库的**预写日志与损坏修复** | `store/wal.go` / `config/atomic.go` |
| riverqueue/river | （复核前轮：retry 兜底 / 跨字段校验 / JobRescuer）| Postgres 任务队列 | `retry` / `config` / `engine` |

> 选择依据：前两轮已把 river/asynq 的**执行路径**读透并落地。本轮专挑「本项目有同名机制、
> 但从未逐行对照」的模块——调度的**公平性**（temporal）与写盘的**容错性**（prometheus），
> 这两处恰是单机 7×24 系统最该稳、却最易被"能跑"掩盖的内核。

---

## 2. 现状取证（本轮现取，不复用旧读数）

### 2.1 前序改造的落地状态（本地实测 2026-10-25）

```text
$ cd D:/workdesktop/workbench && go vet ./...               -> VET_RC=0（30/30 包）
$ go test ./... -count=1                                     -> 30 包全 ok
$ cd D:/workdesktop && pwsh -File tests/check_all.ps1        -> CHECK_ALL_RC=0（37 段零 FAIL/零 SKIP）
$ 提交链：9b615dc(atomic) / 4df98ed(queue) 已推送，CI CI=success
```

前两轮 P0-1~P0-4 / P1 / P2-1~P2-4 + 第五项（持续卡死救援）+ 本轮前四项（hiddenTestCmd/soak/R3/旋钮）
**全部在册且实测绿**，不复述。

### 2.2 缺陷一：WriteFileAtomic 兜底路径数据归零（新发现）

- **peer 依据**：prometheus `tsdb/wlog/wlog.go:400-505 Repair()`——处理损坏段用
  `Rename(fn, fn+".repair")`（`:448`）**先备份再重建**，从不舍不得原数据；删除只针对
  「损坏点之后」的段（`:433-439`），且 Windows 专有句柄处理注释明确（`:426`、`:484-496` 三处显式 close 注释）。
- **本项目现状（修复前）**：`config/atomic.go` 在第一段 `rename(tmp→path)` 失败后走
  `os.Remove(path)` 再重试 rename。**该窗口下若第二段 rename 也失败，原文件已被删、新内容没进去
  ⇒ 目标数据彻底归零。** 已用等效步骤实测复现（Remove 成功 + 第二次 rename 失败 ⇒ 目标文件消失）。
- **影响面**：`WriteFileAtomic` 是全仓**唯一**原子写入口——config / queue 快照 / state.json 都走它。

### 2.3 缺陷二：任务队列低优任务饿死（新发现）

- **peer 依据**：temporal `common/tasks/interleaved_weighted_round_robin.go:140-156`——
  即使有权重差异，用 `numInflightTask` 计数 + 「有 pending 时必须按权重轮转」保证
  **低权重通道不会被高权重持续流入永久压制**；`weighted_channel.go` 每条通道独立缓冲，
  互不饿死。调度理论对应机制是 **aging**。
- **本项目现状（修复前）**：`internal/queue/queue.go sortTasks` 是**纯严格优先级**
  （置顶 > 优先级高 > 创建时间早 FIFO），**无 aging**。生产优先级实测异质（1-5：
  `chain.go:61/103` 用模板 PriorityWeight、`engine_autonomy.go:176` 固定 4、
  `engine.go:1790/1793` 面板可设 1/5）。7×24 持续产任务下，低优任务可被高优**永久排队**。
- **与在册事故的关联**：`docs/archive` 有「排队14但0正在执行」「运行中但产出为0」两类在册缺陷，
  饿死是其上游候选根因之一（不是唯一，但结构上确有此通道）。

---

## 3. 八维对比总览表（本轮增量口径）

> 评分口径同前：已对齐 / 有差距在册 / 本轮新发现缺口 / 不可比（取舍）

| 维度 | WorkBench（2026-10-25 实测） | peer best | 量化差距 | 判定 |
|---|---|---|---|---|
| **功能模块覆盖** | 9 环节闭环 + 双角色 + 自主树 + 契约治理 + 模型池 + WAL + **防饿死 aging** | temporal 10 种调度器族 / prometheus 段式 WAL+checkpoint | 核心闭环不缺；本轮补「写盘容错兜底」「调度公平性」两条内核 | 本轮新发现（已修） |
| **技术架构** | 单进程 31 包（29 internal + 2 cmd）+ 内存队列 + 原子写 JSON ⊕ WAL | river leader election / temporal 事件溯源 | 单机取舍合理；本轮修的是内核自洽（非架构差距） | 已对齐 |
| **实现方式** | internal 28/29 包有测试（仅 browser 无）、30 包 rc=0；错误 11 类分级；**aging + 先备份后替换** | prometheus Repair 备份语义 / temporal 权重公平 | 两处内核从"看着对"升到"判据守住" | 本轮新发现（已修） |
| **性能** | EXE 9.83 MB（实测）；首页 P95/内存/句柄 **UNVERIFIED**（本机无常驻实例，不臆造） | 不可比（peer 为服务端多租户） | 单机口径，非差距项 | 不可比 |
| **可扩展性** | RunnerContract + Hooks + 能力接口 + 模板 + 模型池；**queue.now 时钟注入 seam** | river 插件 / temporal 调度器可插拔 | 新增 seam 让调度公平性可测可换 | 已对齐 |
| **维护状态** | 本地+远端同步（master...origin 0/0）+ CI 双 workflow + 37 段门禁全绿 | n8n 日更级 / river 中节奏 | 本项目运营面已具 CI/CHANGELOG/handoff | 已对齐 |
| **文档** | ARCHITECTURE + CONTRIBUTING + 对标报告**四代** + peer 精读 + 学习计划 | river AGENTS.md 分卷 | 中文文档面充分，报告演进到第四轮 | 已对齐 |
| **适用场景** | 个人 7×24 无人值守桌面 | 团队/云端编排 | 定位不同，不照抄 peer 分布式面 | 不可比 |

---

## 4. 逐项差距说明（只写本轮新发现或仍未闭环的）

### 4.1 写盘兜底会归零数据（高，本轮已修）

`WriteFileAtomic` 是 config/queue/state 三处快照的唯一写入路径。原兜底「先删原文件再重试」把
「写不进去」升级成了「把旧数据删了还是写不进去」——对一个声称"原子写"的函数，这是语义级失信。
prometheus 的对照很干脆：**任何修复动作都以保留可读数据为第一优先**（Repair 先 rename 备份、
只删确定损坏之后的段）。修复后：失败 ⇒ 先 `rename(path, path+".bak")` 保数据 ⇒ 替换；
替换也失败 ⇒ 从 .bak 恢复；连备份都失败（原文件被独占）⇒ 原文件不动只报错。

### 4.2 调度无公平性保证（高，本轮已修）

严格优先级在"任务持续产生"的闭环里有结构性饿死风险，这不是理论担忧——本项目 7×24 自主循环
就是持续产任务的那个场景。有界 aging 是最小充分修法：既解饥饿，又因"有界"在全体饱和时
退化回严格优先级，从而**不打乱既有测试已锁定的顺序语义**（temporal 用独立 weighted channel
+ inflight 计数达到同一目的，本项目用更轻的有界加成达到等价效果，契合单机零依赖取舍）。

### 4.3 停机残留任务处置（NO-OP，核实后判不改）

temporal `FIFOScheduler.Stop()`（`fifo_scheduler.go:71-90`）停机时 `drainTasks()`（`:217-227`）
把队列残留任务逐个 `Abort()`。曾疑似本项目缺口，**核实后判 NO-OP**：本项目 `engine.stop`
不碰内存队列，queued 态持久化在 store，重启 `Reset(requeue)`（`engine.go:685` 含 StateQueued 分支）
会重新入队 ⇒ **无丢失、无卡死**。属工作区已有分层设计，按「无病不乱吃药」不改。登记此判断过程，
防后人再对标 temporal 时重复怀疑同一处。

### 4.4 仍未闭环（如实登记）

| 项 | 说明 | 优先级 |
|---|---|---|
| 首页 P95/内存/句柄现值 | 本轮本机无常驻实例，取不到数 ⇒ UNVERIFIED，须实例在跑时现取（禁臆造） | 中 |
| prometheus checkpoint 语义 | wlog 有 `checkpoint.go`（把快照写进 WAL 目录做检查点），本项目压缩是"写快照+截断"无独立 checkpoint；单机体量下收益低 | 低 |
| temporal 执行感知调度（execution_aware） | 按队列深度/延迟反压调并发度，本项目并发固定 `Concurrency`；属团队多租户需求，单机暂不需 | 低 |

---

## 5. 改进建议清单（本轮已全部落地 P3-1 / P3-2）

| # | 建议 | 对应差距 | 优先级 | 预期目标 | 完成标准（可机器判定）→ 实测 |
|---|---|---|---|---|---|
| **P3-1** | `WriteFileAtomic` 兜底改「先备份后替换」 | 4.1 | **高** | 任何失败路径都不删原数据 | seam 判据 `TestWriteFileAtomicFallbackMatrix` 四组全绿；变异「退回先删后建」⇒ 三条子判据点名（含"数据归零"）+ calls 次序断言点名 ✅ |
| **P3-2** | 队列 `sortTasks` 加**有界** aging | 4.2 | **高** | 低优任务等待有界、不饿死 | `TestQueueAgingPreventsStarvation` 绿；变异「退回严格优先级」⇒ 判红并逐字打印饿死序列 ✅；既有 `TestPopRespectsPriorityAndFIFO` 不破 ✅ |

> 执行顺序：P3-1 → P3-2。理由：P3-1 保护的是**已落盘的既有数据**（快照/配置/状态，不可逆损失），
> 优先级高于 P3-2 的**调度公平性**（任务最终会被处理，只是可能延迟）。二者都已落地。
> P3-3（§4.3）经核实为 NO-OP，登记判断依据，不落地代码。

---

## 6. 可实施路径（分阶段 · 里程碑）

| 阶段 | 内容 | 里程碑（可验收） | 状态 |
|---|---|---|---|
| M0 对标 | peer 刷新 HEAD + temporal/prometheus 逐行精读 + 缺陷归因 | 本报告 §2 | 已完成 2026-10-25 01:5x |
| M1 P3-1 | WriteFileAtomic 先备份后替换 + seam 判据 + 变异 | atomic_fallback_test 四组绿 + 变异判红 | 已完成，提交 9b615dc |
| M2 P3-2 | 队列有界 aging + 非饱和区判据 + 变异 | aging_test 三判据绿 + 变异打印饿死序列 | 已完成，提交 4df98ed |
| M3 回归 | 全仓 go vet + go test + check_all 37 段 | 30 包 ok / VET_RC=0 / CHECK_ALL_RC=0 | 已完成 |
| M4 文档 | 本报告 + CHANGELOG + handoff 共享暂存区冲突记录 | 报告入 docs/benchmark + 门禁全绿 | 本文件 |
| M5 推送 CI | 两提交推送 + CI 盯绿 | 946f55e 全绿、4df98ed CI=success | 进行中（E2E 收尾） |

---

## 7. 证据附录

### 7.1 本轮 peer 刷新命令（可复算）

```powershell
cd D:/workdesktop/xtmp/oss
foreach ($n in 'river','asynq','temporal','prometheus') {
  git -C $n fetch --depth 1 origin HEAD; git -C $n reset --hard FETCH_HEAD
}
# temporal HEAD 1bcd882(2026-10-24) / prometheus a2fda0e(2026-10-23) / river 928c04a(2026-10-24)
```

### 7.2 关键行号索引（本轮刷新后 clone）

| 结论 | peer 文件:行 |
|---|---|
| WAL 修复先备份再重建 | `prometheus/tsdb/wlog/wlog.go:445-458`（Rename→.repair, CreateSegment） |
| Repair 只删损坏点之后的段 | `prometheus/tsdb/wlog/wlog.go:433-439` |
| Windows 句柄关闭专注明示 | `prometheus/tsdb/wlog/wlog.go:424-428, 494-495` |
| IWRR 低权重通道不被饿死 | `temporal/common/tasks/interleaved_weighted_round_robin.go:140-156` |
| 每通道独立缓冲 | `temporal/common/tasks/weighted_channel.go:15-34` |
| FIFOScheduler 停机 drain/Abort | `temporal/common/tasks/fifo_scheduler.go:71-90, 217-227`（本项目判 NO-OP）|

### 7.3 本项目关键落点

| 结论 | 文件:行 |
|---|---|
| 先备份后替换实现 | `config/atomic.go` writeFileAtomicWith（rename→.bak→替换→失败恢复）|
| 有界 aging | `queue/queue.go:90-91`（agingStepMs/agingMaxBoost）、`:103` effectivePriority、`:117` sortTasks |
| 队列 NO-OP 依据 | `engine/engine.go:685`（Reset 含 StateQueued 重入队）|

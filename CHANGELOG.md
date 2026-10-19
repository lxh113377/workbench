# Changelog

> 本项目所有值得一提的变更都记在这里。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
> 版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。
>
> 版本与产物对应关系以 `dist/SHA256SUMS` 与 `workbench.exe --version` 的实测输出为准。

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

## [Unreleased]

### Added
- （待发布项写这里）

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
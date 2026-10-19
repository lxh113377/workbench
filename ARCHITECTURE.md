# 架构（ARCHITECTURE）

> 一页看懂 WorkBench 是什么、怎么分层、为什么这么取舍。
> 改代码前先看这页；运维与排障细节见 [docs/架构与运维.md](docs/架构与运维.md)。

## 一句话定位

**Windows 单 EXE 桌面端 7×24 全自动 AI 任务产出工作台**：把「任务生成 → 三维评估 → 自动批准 → 无人执行 → 自评 → 链式衍生」做成闭环，全程无需人介入。

## 进程模型

单个 Go 进程（7.7 MB，纯标准库）+ 按需拉起 N 个 DSH 子进程（Job Object 绑定，随主进程退出而回收）。

```
┌───────────────────────────────────────────────────────────┐
│  workbench.exe（单进程）                                    │
│                                                           │
│   Engine(调度闭环) ──► Queue(优先级/每链串行) ──► Executor   │
│     │                                              │      │
│     ├─► Evaluator(三维评估+硬门禁)                  ▼      │
│     ├─► Chain(后继生成)                      DSH Desktop   │
│     ├─► Judge(语义判分/自评)                  (headless)    │
│     └─► ModelRouter(健康度轮换)              + our-free-   │
│                                               model       │
│   Web(HTTP+SSE 面板)   Tray(系统托盘)                      │
│   Supervisor(资源自愈) Store(原子 JSON + JSONL/WAL)        │
└───────────────────────────────────────────────────────────┘
```

## 包结构（internal/）

| 层 | 包 | 职责 |
|---|---|---|
| 入口 | `cmd/workbench` | CLI 子命令、生命周期、supervisor 接线 |
| 领域 | `model` | 领域类型、状态机（非法迁移有反例测试） |
| 存储 | `store` | 原子写 JSON、WAL 事件重建、崩溃恢复、尝试台账 |
| 调度 | `queue` / `engine` | 优先级队列 / 调度闭环、优雅停机、运行中取消 |
| 执行 | `executor` | DSH 子进程、事件流、看门狗、**执行钩子（Hooks）+ RunnerContract 接口** |
| 可靠 | `retry` | 错误分类（11 类）→ retry/fail/cancel 三分判定，指数退避 + 抖动 |
| 决策 | `evaluate` / `chain` / `judge` | 三维评估 / 链式衍生 / 语义判分 |
| 通道 | `modelroute` / `modelpool` / `dshprofile` | 模型健康度路由 / 池化 / 隔离 DSH_HOME 引导 |
| 观测 | `logging` / `web` / `supervisor` | slog 结构化日志 / 面板 + /api/metrics / 资源采样自愈 |
| 桌面 | `tray` / `single` / `shortcut` / `browser` | 托盘 / 单实例 / 快捷方式 / 浏览器拉起 |

## 关键架构决策（为什么这么取舍）

1. **单机便携优先**：不引 DB/Redis，存储用「原子 JSON 快照 ⊕ WAL 回放」——单 EXE 交付的硬约束，牺牲横向扩展换零依赖。
2. **纯标准库**：`go.mod` 零 `require`，供应链干净、可离线、可重现构建。
3. **执行层抽象**：`executor.RunnerContract` 接口 + `Hooks`，引擎不绑死具体执行器，未来可换远程队列/测试桩（对标 riverqueue/river 的插件钩子）。
4. **错误必须分型**：executor 产出 11 类 `ErrorKind`，`retry.Policy` 据此决定重试/失败/取消——不可重试类别当次即停（对标 n8n 错误三分）。
5. **并发按任务隔离 DSH_HOME**：OFM sealed 车道单写者语义，每任务独立 `runtime/dsh-home/tasks/<id>`，profiles 用 junction 共享。

## 数据流

```
模板 → 任务 → 评估 → 队列 → 执行 → 归档 → 自评 → 后继 → 回到模板池
```

## 可观测性入口

| 入口 | 内容 |
|---|---|
| `GET /api/metrics` | 堆内存/goroutine/句柄/GC、任务聚合、最近 30 条尝试台账 |
| `GET /api/failures` | 按 ErrorKind 聚合的失败原因分布 |
| `logs/workbench.jsonl` | slog 单行 JSON 日志（`component` 字段可按来源过滤） |
| `tasks/<id>/attempts.jsonl` | 逐次尝试台账（model/kind/action/delay/elapsed） |

## 硬约束（详见 [CONTRIBUTING.md](CONTRIBUTING.md)）

纯标准库 · 不碰桌面端 profile · profile 固定 `web-desktop` · 删除走回收站 · 产出根固定 `workbench-output`。

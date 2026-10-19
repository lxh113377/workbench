# 贡献指南（CONTRIBUTING）

> 本文件写给要改这个项目的人（包括未来的自己）。
> 项目是 **Windows 单 EXE 桌面工作台**，硬约束比一般 Go 项目严格，先读「一、不可违反的硬约束」再动手。

---

## 一、不可违反的硬约束

| # | 约束 | 为什么 |
|---|---|---|
| 1 | **Go 纯标准库，禁止引入第三方模块** | 保证单 EXE / 离线可用 / 供应链干净。`go.mod` 里不应出现 `require` 任何外部包 |
| 2 | **不碰 `~/.dsh/profiles/desktop`** | 那是 Electron 桌面端独占的 profile，动了会破坏正在使用的 DSH Desktop |
| 3 | **工作台专用 profile 固定 `web-desktop`** | OFM（our-free-model）sealed 车道白名单只认 `desktop\|tauri\|web\|web-desktop` |
| 4 | **不挂 `@deepseek-ai/dsh-web-app`** | 它会拉起常驻 HTTP 服务，headless 进程永不退出（实测卡死 150s+） |
| 5 | **所有删除走回收站** | 全局 `delete_guard` 钩子会拦 `Remove-Item -Recurse` 等磁盘直删 |
| 6 | **产出根固定 `D:\workdesktop\workbench-output`** | 面板/导出/恢复逻辑都基于该布局 |

> 违反 1 或 2/4 的 PR 一律不合并，没有例外。

---

## 二、本地开发环境

| 项 | 要求 |
|---|---|
| 操作系统 | Windows 10/11（x64） |
| Go | **1.27.x**（`go version` 确认；`go.mod` 写的是 `go 1.27.0`） |
| PowerShell | 7+（`pwsh`），构建脚本用 PowerShell 写 |
| 可选 | mingw gcc（`go test -race` 需要；本机已在 WinLibs 下实测可用） |
| 可选 | `windres`（生成 EXE 图标与版本信息；缺失时构建脚本会跳过并告警） |

---

## 三、常用命令（全部实测可用）

```powershell
# 1) 代码检查（提交前必跑）
cd D:\workdesktop\workbench
go vet ./...

# 2) 单元测试
go test ./... -count=1

# 3) 竞态检测（改并发相关代码必跑）
go test -race -count=1 ./...

# 4) 覆盖率（看单包覆盖）
go test ./internal/engine/ -count=1 -cover

# 5) 完整构建（含测试 + 覆盖率门禁 + 图标 + 版本信息）
cd D:\workdesktop
pwsh -NoProfile -File setup\build.ps1 -Version 1.2.0

# 6) 端到端验收（会拉起真实进程，约 5–10 分钟）
pwsh -NoProfile -File tests\e2e.ps1

# 7) 项目门禁（go vet + go test + 治理 CLI 自检）
python eval/governance_gate.py

# 8) 执行通道自检
.\dist\workbench.exe --diagnose
```

---

## 四、提交前检查清单

- [ ] `go vet ./...` 通过
- [ ] `go test ./...` 全绿
- [ ] 改过并发/共享状态 ⇒ `go test -race` 全绿
- [ ] 改过源码 ⇒ `setup/build.ps1` 覆盖率门禁通过
- [ ] 改过面板/API/执行链路 ⇒ `tests/e2e.ps1` 全绿
- [ ] 新增导出函数/类型有注释（首行以中文说明用途）
- [ ] 无凭据硬编码（token/密码/key 用占位符或从配置读）
- [ ] 没有引入任何第三方依赖（`git diff go.mod` 应为空或无 require 新增）

---

## 五、代码组织约定

```
workbench/
├─ cmd/workbench/        入口、CLI 子命令、生命周期接线
└─ internal/
   ├─ config/            配置 schema、原子写、路径布局
   ├─ model/             领域类型、状态机
   ├─ store/             持久化、状态迁移、崩溃恢复、尝试台账
   ├─ queue/             优先级队列、每链串行
   ├─ engine/            调度闭环
   ├─ executor/          DSH 子进程执行、事件流、看门狗、执行钩子
   ├─ evaluate/          三维评估 + 硬门禁
   ├─ chain/             种子/后继任务生成
   ├─ judge/             语义判分 / 自评 / 摘要
   ├─ modelroute/        模型健康度路由（含持久化）
   ├─ templates/         内置模板 + 校验 + 渲染
   ├─ dshprofile/        隔离 DSH_HOME 引导
   ├─ logging/           结构化日志（slog）
   ├─ retry/             错误分类重试策略
   ├─ web/               面板 HTTP / SSE / 导出
   ├─ tray/ single/ supervisor/ shortcut/ browser/   Windows 集成与自愈
   └─ gov/               契约治理 × 体量治理
```

约定：

1. **验收判据跟着代码走**：新增能力要同时给出"怎么证明它生效"（单测/接口契约/e2e 断言）。
2. **纯逻辑与副作用分离**：能在无子进程、无网络的情况下测的逻辑，抽成纯函数（参考 `executor/executor_test.go`、
   `engine/helpers.go` 的做法），这样覆盖率门禁才有意义。
3. **错误分类必须结构化**：失败要走 `executor.Kind` 枚举，不要只留一句自由文本——面板的失败分布依赖它。
4. **日志走 `internal/logging`**：不要新开裸文件写日志；用 `logger.At("组件名").Info(...)`，敏感字段用 `logging.Redact`。
5. **中文注释优先**：本项目注释与文档都写中文，术语首次出现可附英文。

---

## 六、提交信息规范

格式：`<type>(<scope>): <简述>`

| type | 用途 |
|---|---|
| `feat` | 新功能 |
| `fix` | 修缺陷 |
| `docs` | 只改文档 |
| `refactor` | 不改行为的重构 |
| `test` | 只改测试 |
| `chore` | 构建/依赖/杂务 |
| `perf` | 性能优化 |

示例（均取自本项目真实提交）：

```
feat(observability): slog 结构化日志 + executor 钩子接口 + metrics/失败分布 API + CI 工作流 + e2e 观测段
fix(reliability): 修热循环/健康度持久化/看门狗自适应/成功去重/链回填
docs(v1.2.0): 架构手册补可观测性四小节 + 项目记忆回写 + engine 日志统一 component 标签
```

---

## 七、发布流程

1. 更新 `CHANGELOG.md`（`[Unreleased]` 段落整理成新版本号 + 日期）
2. `pwsh -File setup/build.ps1 -Version <x.y.z>`
3. 跑 `tests/e2e.ps1` 确认全绿
4. 确认 `dist/SHA256SUMS` 已更新，并在 `docs/验收报告.md` 追加本次复验记录
5. 提交 `chore(build): v<x.y.z> 产物 SHA 与验收回执`

> 版本号与产物必须能对上：`workbench.exe --version` 的输出、`dist/SHA256SUMS` 的记录、
> `CHANGELOG.md` 的版本头三者一致。

---

## 八、排障入口

| 现象 | 先看哪里 |
|---|---|
| 引擎起不来 / 0 任务 | `logs/workbench.jsonl`（component=engine）+ `logs/audit.jsonl` |
| 任务失败 | `tasks/<id>/attempts.jsonl`（逐次尝试）+ 面板「失败原因分布」 |
| 卡死 | `tasks/<id>/events.jsonl` 最后一条事件的时间 |
| 模型全不可用 | `runtime/models-health.json`（健康度与冷却时间） |
| 资源越阈自愈 | `logs/workbench.jsonl`（component=supervisor）+ `logs/metrics.csv` |
| 面板打不开 | `runtime/panel.url` 与 `runtime/panel.port` |

架构与设计决策详见 `docs/架构与运维.md`。

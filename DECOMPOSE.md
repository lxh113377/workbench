# DECOMPOSE · 本轮（2026-10-29 round 3，目标 = #10 / #11 / L3）

> 权威 = D:/global_memory/core/rule44_dispatch_standard.md（D0-D19）+ 派发三检查点（2026-10-29 入册）。
> 本卡同时服务三检查点之①（开场分解）与③（收尾负申报）。

## 工作流分解（四问 + 三条件）

| 件 | 产出物 | 文件/模块 | 可独立验收判据 | 写面 | 依赖 | 预计 |
|---|---|---|---|---|---|---|
| A #10 门禁判据 | check_changelog 第 5) 条 + 债务名册 | `tests/check_changelog.ps1` 单文件 | `pwsh -File tests/check_changelog.ps1` rc=0 + 5 条负控 | tests/ | 无 | ≤45min |
| B #11 取消归因 | context.Cause 具名分类 | `internal/engine/cancel_cause*.go` + engine.go 分流 | `go test ./internal/engine -run Cancel…` | engine/ | 无 | ≤45min |
| C L3 基线重校 | CI 口径实测 + build.ps1 阈值 | `setup/build.ps1` 单文件 | 读 CI 日志 + `check_snapshot_build` | setup/ | 需 CI 读数 | ≤45min |
| D modelroute 补测 | 该包测试 | `internal/modelroute/*_test.go` | `go test ./internal/modelroute -cover` ≥65% | modelroute/ | 无 | ≤45min |

触发表命中：T1（A/B/C/D 四条互不引用）、T2（每件 ≥3 工具回合）、T3（结论需 ≥2 独立面：CI 日志面 + 台账面 + 生产配置面）、T4（阈值结论进权威件 setup/build.ps1）、T5/T6 亦命中 ⇒ **必须并行派 ≥2**。

## 实际派发（本轮）

| 件 | 形态 | 结果 |
|---|---|---|
| L3 CI 读数取证（只读） | subagent 后台 | 回件：31 包 CI 读数 + 自证均值 71.64 == CI 打印 avg ⇒ 我据此定阈值 |
| #11 取消链路代码地图（只读） | subagent 后台 | 回件：WithCancel 位点、RecordFailure 次序、泛错顶掉归因的落点清单 |
| D modelroute 补测（写测试） | subagent 后台 | 回件：4 文件 35 Test，本机 55.8→99.0，含自抓 flake 与自抓「判据写反」 |
| A #10 门禁 / B #11 实现 / C 阈值 | Lead 自留 | 理由见下（写面冲突 + 决策本体） |

## 负申报（未派件的 skip_reason，枚举内）

- A #10 门禁 = **决策本体**：新判据的存量/增量口径与 CI 浅历史适配是设计决策，且结论要进 CI 受理面；
- B #11 实现 = **写面冲突**：`engine.go` 是本仓最热文件（并行会话 T74 同文件在写），派出去易撞 D7.2；
- C 阈值 = **严格串行**：输入 = L3 取证回件（前一步输出），且阈值数字是唯一决策点。
- 自查②（每满 6 回合）：本轮 tool 回合数 ≥6 时已完成 3 件并行派发 ⇒ 非「该派未派」。

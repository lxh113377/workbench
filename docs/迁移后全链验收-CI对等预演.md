# 迁移后全链验收 · CI 对等预演（2026-10-19 11:45）

> 触发：Lead 已把源码迁回正式入口 —— `workbench/` 从 0 个 .go 恢复到 **190 个 .go**，
> 且 `go build ./...` 通过。本轮按 **CI 的真实顺序**在本地逐步预演，确认"迁移后 CI 会绿"。

## 1. 迁移中的一次真实红灯（已被 Lead 修掉，留档）

迁移进行中我按 CI 方式编译 `workbench/`，曾报出**重复声明**（合并冲突残留）：

```
internal/engine/artifacts.go:242 method Engine.SelfTestArtifactPipeline already declared (at :208)
internal/engine/ratelimit.go:62  method Engine.SetProbeSuppressor already declared (at :45)
internal/engine/lane_health.go:36 BlockKind redeclared (other declaration at :34)
```

这是**迁移态**（两份文件被并到一起），不是设计缺陷。约 10 分钟后复扫
`workbench-build-errors.txt` 为 **0 字节**、`go build ./...` 通过 —— Lead 已处理。

## 2. CI 对等预演：6/6 PASS

跑法：`pwsh tests/docker-autonomy/ci-parity.ps1`（严格按 `autonomy-e2e.yml` 的顺序与镜像）

| # | 步骤 | 环境 | 结果 |
|---|---|---|---|
| A1 | 交叉编译 + 静态自检（无 PT_INTERP） | golang:1.27-bookworm | `BUILD_PASS` / `STATIC_OK` |
| A2 | 替身编译 | golang:1.27-bookworm | `FAKEDSH_BUILD_OK` |
| B1 | 判据覆盖（静态闸 1） | debian:bookworm-slim | `CRITERIA_COVERAGE_OK` |
| B2 | 枚举一致性（静态闸 2） | debian:bookworm-slim | **WARN=0** |
| B3 | 能力面冒烟（行为闸 1） | debian:bookworm-slim | `CAPABILITY_SMOKE_OK` |
| B4 | 容器闭环端到端（行为闸 2） | debian:bookworm-slim | `E2E_PASS`（9/9 判据） |

```
CI_PARITY: 6 PASS / 0 FAIL
CI_PARITY_PASS（本地已按 CI 顺序全绿）
```

## 3. 闭环终点在"迁移后仓库"上复验通过

`docker-run.ps1 -RunId ci-parity`：`goalDone=1 / nextGoal=1`、9/9 PASS、`E2E_PASS`。
证据：`tests/docker-autonomy/evidence/ci-parity`。
（对比：迁移前 `recovered-src` 是 FAIL；Lead 修 acceptance 后 `fix-recheck` 转 PASS —— 三连可追溯。）

## 4. 预演脚本自身踩的坑（方法学记录）

**首版 `ci-parity.sh` 设计错了**：把"编译"步骤放进 debian 容器 ⇒ `rc=127`（debian 里没有 go）。
CI 的真实结构是**两种环境**：宿主（ubuntu-latest + setup-go）编译，再把二进制挂进 debian 容器跑行为。
改写成 `.ps1` 并按该结构分段后 6/6 通过。教训：**预演脚本必须 1:1 复刻 CI 的镜像与顺序**，
否则预演本身会造假红（好在这次造假红的方向是"误报失败"，比"误报成功"安全）。

## 5. 本轮交付物

| 文件 | 作用 |
|---|---|
| `tests/docker-autonomy/ci-parity.ps1` | **CI 对等预演**：本地按 CI 顺序跑 6 步 |
| `tests/docker-autonomy/consistency-scan.sh` | 枚举"分发器 vs 成员覆盖"扫描（低噪声，变异测试验证过） |
| `tests/docker-autonomy/criteria-coverage.sh` | 判据"声明 vs 实现"交叉校验 |
| `tests/docker-autonomy/build-errors.sh` | 只读列全仓编译错误（迁移期定位用） |
| `.github/workflows/autonomy-e2e.yml` | 四道闸（2 静态 + 2 行为），actionlint 零告警 |

## 6. 判据有效性证据（防"恒绿空壳门禁"）

| 门禁 | 反向验证 |
|---|---|
| `criteria-coverage.sh` | 修复前的树 → FAIL 点名两项；补齐后 → OK |
| `consistency-scan.sh` | 变异体（人工删 2 个 case）→ 报「声明 7 / 处理 5」；正常树 → 0 WARN |
| `container.sh` + `judge.py` | 迁移前 → `goalDone=0` FAIL；Lead 修复后 → PASS |

三道都做过"红→绿"双向验证，不是只见过它绿。

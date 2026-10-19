# 回复 Lead（自主运行线，2026-10-19 11:46）

## 1. 你迁回的源码，我按 CI 顺序全链预演 —— **6/6 通过**

看到 `workbench/` 已从 0 个 .go 恢复到 **190 个 .go**，我立刻按 `autonomy-e2e.yml` 的
**真实顺序与镜像**在本机逐步预演（`pwsh tests/docker-autonomy/ci-parity.ps1`）：

| # | 步骤 | 环境 | 结果 |
|---|---|---|---|
| A1 | 交叉编译 + 静态自检 | golang:1.27 | `BUILD_PASS` / `STATIC_OK` |
| A2 | 替身编译 | golang:1.27 | `FAKEDSH_BUILD_OK` |
| B1 | 判据覆盖（静态闸 1） | debian | `CRITERIA_COVERAGE_OK` |
| B2 | 枚举一致性（静态闸 2） | debian | **WARN=0** |
| B3 | 能力面冒烟（行为闸 1） | debian | `CAPABILITY_SMOKE_OK` |
| B4 | 容器闭环端到端（行为闸 2） | debian | `E2E_PASS` 9/9 |

```
CI_PARITY: 6 PASS / 0 FAIL
CI_PARITY_PASS
```

→ **迁移后 CI 会绿**（本机已按同一顺序验过）。闭环终点同样复验通过：`goalDone=1 / nextGoal=1`。

## 2. 迁移中的一次真实红灯（已被你修掉，我留了档）

迁移进行中我按 CI 方式编译 `workbench/` 曾报**重复声明**：

```
internal/engine/artifacts.go:242 Engine.SelfTestArtifactPipeline already declared (at :208)
internal/engine/ratelimit.go:62  Engine.SetProbeSuppressor already declared (at :45)
internal/engine/lane_health.go:36 BlockKind redeclared (at :34)
```

约 10 分钟后复扫已为 0 字节错误、`go build ./...` 通过 —— 你已处理，这条仅作留档。
（若你想让这类问题**编译前就能报**，我可以再加一个"重复声明"静态检查；不过 `go build` 本身就够快。）

## 3. 顺便修了我自己预演脚本的一个设计错

**首版 `ci-parity.sh` 把"编译"放进 debian 容器 ⇒ rc=127**（debian 无 go）。
CI 真实结构是**两种环境**：宿主 setup-go 编译 → 二进制挂进 debian 跑行为。
改写为 `.ps1` 分段后 6/6 通过。教训已写进回执：**预演脚本必须 1:1 复刻 CI 的镜像与顺序**。

## 4. 三门禁都做过"红→绿"双向验证（不是恒绿空壳）

| 门禁 | 反向验证 |
|---|---|
| `criteria-coverage.sh` | 修复前的树 FAIL 点名两项 → 补齐后 OK |
| `consistency-scan.sh` | 人工删 2 个 case 的变异体 → 报「声明 7 / 处理 5」→ 正常树 0 WARN |
| 容器闭环 | 迁移前 `goalDone=0` FAIL → 你修复后 PASS |

## 5. 我这边现状与待命

- 只写 `tests/`、`.github/workflows/autonomy-e2e.yml`、`docs/`；未碰 `workbench/` 一行、未碰 `.git`；
- 容器已全部清理；
- **待命项**：等你重建 `.git` 后，我可以把本轮所有验收集与回执一并交给你纳入首个提交
  （我现在无法提交，仓库还不是 git 仓）。

回执：`docs/迁移后全链验收-CI对等预演.md`。

—— 自主运行线

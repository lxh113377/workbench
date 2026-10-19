# CI 入口说明（2026-10-19）

## 两条工作流的分工（互不覆盖）

| 工作流 | 触发 | 跑什么 | 判据 |
|---|---|---|---|
| `.github/workflows/ci.yml`（Lead 原有） | push/PR/dispatch | windows-latest 上 `go vet`、`go test -race`、`build.ps1`（覆盖率门禁）、产物校验 | 源码质量 |
| `.github/workflows/autonomy-e2e.yml`（本次新增） | 同上 | ubuntu-latest 上交叉编译静态 Linux 二进制 → 容器内跑自主闭环端到端 | 运行时行为 |

**为什么单开一条而不是改 ci.yml**：`ci.yml` 是 Lead 的写入口，按分工我不动它；
两条工作流职责不重叠，单开一条也让"运行时行为回归"的失败原因一眼可辨。

## 新增工作流做什么

1. 交叉编译 `CGO_ENABLED=0` 的静态 Linux 二进制；
2. 编译确定性 DSH 替身（`tests/docker-autonomy/fakedsh`）；
3. 用 `debug/elf` 做静态自检（golang 镜像无 `file(1)`）；
4. `docker run debian:bookworm-slim` 跑 `tests/docker-autonomy/container.sh`；
5. 无论成败上传 `tests/docker-autonomy/evidence/ci`（含 goals.json / decisions.jsonl / run.log）。

## 判据（容器内 judge.py 的 9 条）

目标 bootstrap → 阶段任务推导 → 产物真实落盘 → 真实执行验收通过 →
自主验收记账 → 阶段推进 → 目标完成(goalDone) → 自动下一目标(nextGoal) →
completed 旧目标 + activeId 指向新目标。全 PASS 才 `E2E_PASS`（退出码 0）。

**注意**：这是行为判据，不是"跑起来就算过"。容器内通道是确定性替身，
所以该工作流不依赖外部模型可用性 —— 这也是它能在 CI 上稳定的原因。

## 宿主侧本地复跑

```powershell
pwsh tests/docker-autonomy/docker-run.ps1                 # 全链（编译 + 容器 + 收证据）
pwsh tests/docker-autonomy/docker-run.ps1 -SkipBuild      # 复用已有 dist/ 产物
pwsh tests/docker-autonomy/docker-run.ps1 -Image alpine:3.20   # musl 腿
```

## 已知前置条件

- 容器内需要 `python3`：`container.sh` 由调用方 `apt-get install` 安装（CI 与本地脚本都已带）；
- 被测二进制路径固定为挂载的 `/test/wb`，替身为 `/test/fakedsh`；
- 替身的注册方式必须走 `DSH_DESKTOP_EXE` 环境变量（执行器先调 `LocateDSH()`，
  配置 `dshExe` 轮不到生效）—— 该细节已写进 `container.sh` 注释。

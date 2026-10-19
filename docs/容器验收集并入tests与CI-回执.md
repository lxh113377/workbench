# ① 容器验收集并入 tests/ + CI 入口 · 交付说明（2026-10-19）

## 1. 落位

验收集已从临时目录 `setup/docker-autonomy/` **移入** `tests/docker-autonomy/`（我的分工范围），
并按职责整理成可一键复跑 + 可被 CI 调用的形态：

| 文件 | 作用 |
|---|---|
| `tests/docker-autonomy/docker-run.ps1` | **宿主侧一键入口**：编译 → 起容器 → 收证据 → 转述退出码 |
| `tests/docker-autonomy/container.sh` | 容器内主脚本：自检 → 起服务 → 等闭环 → 交判据 |
| `tests/docker-autonomy/judge.py` | 9 条闭环判据的机器核对（`SUMMARY_PASS` / `SUMMARY_FAIL`） |
| `tests/docker-autonomy/capability-smoke.sh` | 二进制能力面自证（8 个命令面，CI 与本地同一脚本） |
| `tests/docker-autonomy/ability-probe.ps1` | 旧 EXE 能力比对探针（见另一份回执） |
| `tests/docker-autonomy/build-linux.sh` | 容器内交叉编译静态 Linux 二进制 + PT_INTERP 自检 |
| `tests/docker-autonomy/build-fakedsh.sh`、`fakedsh/` | 确定性 DSH 替身的编译器与源码 |
| `tests/docker-autonomy/config.e2e.json` | 容器用配置（自主开启、预算收紧、单模型） |
| `tests/docker-autonomy/start_engine.py`、`poll.py` | HTTP 起引擎与轮询 |
| `tests/docker-autonomy/README.md` | CI 入口说明（两条工作流分工） |
| `tests/docker-autonomy/evidence/{run1, oneclick-skip, ci-sim}` | 三次运行回收证据（goals/decisions/tasks/run.log） |

## 2. CI 入口（新增，不修改 Lead 的 ci.yml）

新增 `.github/workflows/autonomy-e2e.yml`，与 `ci.yml` **职责不重叠**：

| 工作流 | 跑什么 |
|---|---|
| `ci.yml`（原有） | windows-latest：`go vet` / `go test -race` / `build.ps1`（覆盖率门禁）/ 产物校验 |
| `autonomy-e2e.yml`（新增） | ubuntu-latest：交叉编译静态二进制 → 能力面冒烟 → 容器内自主闭环端到端 |

步骤：交叉编译 → 编译替身 → 静态自检（`debug/elf`，因 golang 镜像无 `file(1)`）→
`capability-smoke.sh` → 容器内 `container.sh` → 上传 `evidence/ci`。

`actionlint` + `shellcheck` **零告警**（本机实跑 `rhysd/actionlint:1.7.7`，RC=0）。

## 3. 三条判据链（都已本机实跑）

| 链 | 命令 | 结果 |
|---|---|---|
| 离线自检 | `--autonomy selftest` | 11 PASS / 0 FAIL |
| 能力面 | `capability-smoke.sh`（容器内） | 8/8 命令面识别，`CAPABILITY_SMOKE_OK` |
| 闭环终点 | `docker-run.ps1`（容器内） | 9/9 判据 PASS，`E2E_PASS`，退出码 0 |

CI 的 docker 步骤本机等价模拟（同一命令、同一挂载）也已跑通，证据 `evidence/ci-sim`。

## 4. 一键复跑

```powershell
pwsh tests/docker-autonomy/docker-run.ps1                    # 全链（编译 + 容器 + 收证据）
pwsh tests/docker-autonomy/docker-run.ps1 -SkipBuild         # 复用已有 dist/
pwsh tests/docker-autonomy/docker-run.ps1 -Image alpine:3.20 # musl 腿
pwsh tests/docker-autonomy/capability-smoke.sh               # 宿主上直接验能力面（需 Linux 二进制路径参数）
```

## 5. 与 Lead 的分工边界（遵守）

- 只写 `tests/`、`.github/workflows/autonomy-e2e.yml`（新增文件）、`docs/`（回执）；
- **未改** `workbench/`、`src-backup/`、`ci.yml`、`.git`；
- 未做任何删除/清理类操作（只在容器生命周期内 `--rm` 自清理）。

## 6. 已知限制（如实记录）

1. CI 上的 `Cross-build` 步骤依赖 `workbench/` 源码完整；
   当前仓库源码只恢复到 `src-backup/`（**缺 `internal/ratelimit` 包**），
   因此该工作流要等 Lead 恢复源码后才能真正跑绿；
   本机验证是**用已有 Linux 二进制走通 docker 步骤**，而非在 CI 上编出它。
2. 容器内需要 `python3`：由调用方 `apt-get install`（CI 与本地脚本都已带，实测约 10–20 秒）。
3. 替身必须走 `DSH_DESKTOP_EXE` 注册（执行器先调 `LocateDSH()`，配置 `dshExe` 轮不到生效）。

<!-- 请先读 CONTRIBUTING.md 的「不可违反的硬约束」。 -->

## 这个 PR 解决什么

<!-- 一句话：没有它，无人值守会在哪一步停下来。关联 issue 用 Fixes #NNN -->

Fixes #

## 改动清单

- [ ] 核心改动：
- [ ] 对应测试：
- [ ] 文档/CHANGELOG：

## 判据（必须可机器判定）

<!-- 不接受"本地跑过没问题"。请贴**命令 + 真实输出**，包括失败的那条。
     取不到数请写 UNVERIFIED 并说明原因 —— 本项目把"没测成"与"测得差"严格区分。 -->

```powershell
cd workbench
go vet ./...
go test ./... -count=1
# 改过并发/共享状态：
go test -race -count=1 ./...
```

输出：

```
（贴这里）
```

## 硬约束自查

- [ ] `go.mod` 未新增 `require`（纯标准库）
- [ ] 未读写 `~/.dsh/profiles/desktop`；执行通道 profile 仍固定 `web-desktop`
- [ ] 未挂 `@deepseek-ai/dsh-web-app`（它起常驻 HTTP 服务，headless 进程永不退出；实测卡死 150s+）—— `dshprofile.Bundles` 已有测试钉住白名单，要加 bundle 须同步改该测试与 CONTRIBUTING
- [ ] 产出根仍是 `workbench-output`（由 `config.BuildLayout` 集中派生；面板/导出/恢复逻辑都基于该布局）
- [ ] 删除路径走回收站（非 Windows 分支必须有 `refuseDangerousPath` 安全阀）
- [ ] 新增测试**不会**因缺少 pwsh/python 而在 Linux 容器静默跳过（用 `tests/lib_silent_launch.ps1` 同族的解释器探针）
- [ ] 未提交 `.exe` / `dist/` / `_recovery/` / `src-backup/` 产物
- [ ] 改动涉及并发 / 共享可变状态 / 原子写时：**给复现率**（同一判据重复到出现失败或 `-count=30`，结论写「N 次里失败 M 次」，修复后同样重复并给出 M=0）；加锁前**复核锁序**（外部调用方的加锁顺序是否与本锁成环）。2026-10-25 实测踩过：一次本机跑绿就宣布「并发已解决」，真实复现率约 1/30，只在 CI 第二个 job 暴露
- [ ] 提交信息为中文且说明"为什么"，不只是"改了什么"

## 覆盖率

<!-- 新测包要在 setup/build.ps1 的 -PerPackageCover 里登记**诚实基线**：
     低于实测值、留平台波动余量、只许抬不许降。 -->

| 包 | 改动前 | 改动后 | 新基线 |
|---|---|---|---|
|  |  |  |  |

## 并行会话须知

本仓可能有多会话同时写。**只 stage 自己的文件**（`git add -- <精确路径>`，禁 `git add -A`）；
提交前 `git diff --cached --name-only` 核对暂存区；他人未提交的在途改动不得并入自己的提交。
`workbench/` 与 `tests/` 的写入口分工见 `.workbuddy/memory/` 内的当前分工表。

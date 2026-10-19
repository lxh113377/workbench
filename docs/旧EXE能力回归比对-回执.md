# 旧 EXE 能力回归比对 · 回执（2026-10-19）

> 目的：源码被清空后，用留存的时间点 EXE 做**行为级**能力比对，
> 帮 Lead 界定"丢了多少能力"。本比对**只读**，每个 EXE 复制到临时隔离目录再跑。
> 探针：`tests/docker-autonomy/ability-probe.ps1`；原始报告：`tests/docker-autonomy/ability-20261019-110235.json`

## 1. 结果矩阵（11 项能力探针）

| 能力探针 | 0744 (13.28MB) | 0814 (9.42MB) | v1-all-fixes (13.66MB) | 当前 dist |
|---|---|---|---|---|
| `--version` | ✅ | ✅ | ✅ | ✅ |
| `--diagnose` | ✅ | ✅ | ✅ | ✅ |
| `--status` | ✅ | ✅ | ✅ | ✅ |
| `--governance status` | ✅ rc=3 | ✅ rc=3 | ✅ rc=3 | ✅ rc=3 |
| `--acceptance` | ❌ TIMEOUT | ❌ TIMEOUT | ✅ rc=1 | ✅ rc=1 |
| `--autonomy selftest` | ✅ | ✅ | ✅ | ✅ |
| `--autonomy status` | ✅ | ✅ | ✅ | ✅ |
| `--autonomy plan` | ✅ | ✅ | ✅ | ✅ |
| `--workspace status` | ✅ | ✅ | ✅ | ✅ |
| `--model-pool status` | ✅ | ✅ | ✅ | ✅ |
| `--soak` | ❌ TIMEOUT | ❌ TIMEOUT | ✅ rc=3 | ✅ rc=3 |
| **合计** | **9/11** | **9/11** | **11/11** | **11/11** |

版本串：`0744 = workbench dev (unknown)`、`0814 = workbench 1.3.0-ws`、`v1 = workbench dev`、`当前 = workbench dev`。

## 2. 结论（给 Lead 的量化口径）

1. **能力面是"增量丢失"，不是"整体倒退"**：三个时间点都保留了
   自主运行（selftest/status/plan）、工作目录、模型池、治理、诊断这五组能力。
2. **0744/0814 明确缺 2 项**：`--acceptance`（总验收出口）与 `--soak`（长时压测判定）。
   这两个是 Lead 今晨新加的（对应 `cmd/workbench/acceptance.go` 与 `supervisor/trend.go`），
   属于"源码丢失但三小时前的 EXE 还带着"的典型形态。
3. **最新 `v1-with-all-fixes` 与当前 dist 都是 11/11**：
   说明 **10:05 的 EXE 是"全能力快照"**，可作为能力基线回归比对的金标准。
4. 因此"丢了多少能力"的答案是：**代码级无法从 EXE 还原**（Go 编成机器码，反编译不现实），
   但**能力级可用 10:05 快照做基线**：任何未来构建只要跑同一组探针达不到 11/11，就是回归。

## 3. 探针自身踩到的两个坑（已修，方法学备注）

1. **管道死锁误判**：先 `WaitForExit` 再 `ReadToEnd`，子进程 stdout 缓冲区写满后会阻塞在写，
   父进程却在等退出 ⇒ 互相等待到超时。首版因此把 11 项全判成 TIMEOUT；
   正解是 `ReadToEndAsync()` 先起读任务再等退出。
2. **子命令名必须跟源码一致**：`--model-pool`（不是 `--pool`）。
   首版写成 `--pool` 时探到的"缺失"其实是探针的错。

## 4. 顺带取证到的真实缺陷（建议 Lead 修）

**未知子命令不会报错退出，而是静默回落"正常启动整个应用"。**

源码 `cmd/workbench/main.go` 的 `main()`：`switch args[0]` 命中已知 case 才 `os.Exit`，
**没有 `default` 分支**，于是控制流落到 `runApp(args)` —— 带着一个未知 flag 把完整应用拉起来。

实测（当前 EXE，隔离目录）：

```
workbench.exe --definitely-not-a-cmd
→ 10 秒未退出（HUNG），stderr 出现：
  {"msg":"模型路由表已更新","source":"modelpool","routes":4}
  {"msg":"自主目标就绪","goalId":"goal-1792379101976-…"}
```

影响：
- 任何"探活脚本/CI 探针"都会把未知命令当成挂起（本回执首版正是被它误导）；
- 运维误敲 flag 会意外启动一个常驻实例（还可能与既有实例争端口/锁）。

建议：`main()` 的 switch 加 `default`：若 `args[0]` 以 `-` 开头则打印用法并 `os.Exit(2)`；
只有真正无参时才 `runApp`。这是**低风险、可单测**的改动。

## 5. 复跑方式

```powershell
pwsh tests/docker-autonomy/ability-probe.ps1                      # 默认探 _recovery/*.exe + dist/workbench.exe
pwsh tests/docker-autonomy/ability-probe.ps1 -Exe <单个exe路径>    # 只探一个
```

判据：输出每个 EXE 的 `能力 N/11` 与逐项 `[OK]/[--]/[TIMEOUT]`；
报告落 `tests/docker-autonomy/ability-<时间戳>.json`（可机器比对）。

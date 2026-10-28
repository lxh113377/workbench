# session-ds-perf-scan-2026-10-28 —— 任务执行偏慢：瓶颈定位与扫描优化（目标轮 1/256）

## 一、任务与产物
- 目标：定位「各种对话的任务执行明显偏慢」的主要瓶颈 → 选收益最大的一处 → 直接实施（不改对外接口与结果）。
- 结论：**主要瓶颈 = 执行器看门狗对同一棵大树的高频全量扫描**（外部模式下 workDir = D:/workdesktop 用户真实项目）。
- 落地：`6108111`（实现）+ `6be1e28`（口径校正）→ 已 push（`HEAD == origin/master == 6be1e28`）；已部署两处根。

## 二、瓶颈成因（实测证据）
- 调用点：`executor/executor.go:463-464` 看门狗每 **5 s** 回调 `IsEarlyStall` + `IsStalled`；两者的产出探测在 `executor/pure.go` 里是**无条件全树 `filepath.Walk`**。
- 树规模（Go 侧口径，2026-10-28）：**295,305 个文件 / 431.5k 个目录项**（本机 D:/workdesktop）。
  ⚠️ python `os.walk` 会跟随 junction，实测多算 18.5 万文件（480,615）——不是本代码看到的树（独立复核者先发现，已采纳）。
- 单次全树成本：`filepath.Walk` **16–21 s**（Lead 16.1/18.0 s，独立复核 18.30/21.30 s）⇒ 每 tick 两次 ≈ 35–40 s。
  ⇒ 看门狗**永远追不上 5 s tick**：每个在跑任务全程把 CPU/磁盘元数据打满，同机所有 DSH 子进程与 GUI 会话一起被拖慢。
- 端到端旁证：`stalled` 的 `elapsedMs` p50 = **149,994 ms**（n=53），而「零进展」预算只有 120 s ——
  多出的 ~30 s 恰好是「预算到点 + 两轮 18 s 扫描」的量化形状（预算期内每 tick 都要走两遍树）。
- 同方向第二/三处：`executor.hasWorkspaceOutput`（找到后仍走完整棵树）、`engine.collectOutputsSince`（每尝试收尾一次全树）。

## 三、改动（同一方向，三处；对外接口与结论不变）
1. `pure.go:latestOutputMtime(dir, watermark)` —— 探针唯一实现：`WalkDir`（不额外 Lstat）＋ watermark 命中即 `SkipAll` ＋ 先扫顶层文件。
   等价性：两个调用方都只与 watermark 比较（命中⇒必不判卡死；未命中⇒返回真实最大值⇒逐位同旧）。
   返回值契约已写死在注释：**只可用于与 watermark 比较**（独立复核异议 3 的处置）。
2. `pure.go:IsStalled` —— 新增**事件信号仍在窗口内即免扫盘**快速路径（`max(事件,产出) ≥ 事件 ≥ watermark` ⇒ 结论已定）。
3. `executor.go:hasWorkspaceOutput` —— `WalkDir` + 命中 `SkipAll`（布尔结论不变）。
4. `engine.go:collectOutputsSince` —— `Walk → WalkDir`（跳过 node_modules/点目录、since 过滤、扩展名白名单全部保持）。
5. `probe_scan_contract_test.go`（新增）—— 用**分配量**钉住快速路径：快速路径 ≈0 分配 vs 过期路径 16,706 分配。

## 四、验证链（都可复跑）
- 门禁：`cd D:/workdesktop/workbench && go vet ./... && go test ./... -count=1` → rc=0，31 包全 ok（含既有 IsStalled/IsEarlyStall/LatestOutputMtimeMs 契约测试）。
- 官方构建链：`pwsh -File setup/build.ps1 -Version 1.0.0` → 覆盖率门禁通过（31 包，avg 73.95%）+ 子系统 GUI OK + VERDICT PASS。
- 基准（合成 6,000 文件树）：`go test ./internal/executor/ -run XXX -bench BenchmarkOutputProbe -benchtime 20x`
  → `old_Walk_full 81.7 ms/op` vs `new_WalkDir_watermark 4.2 ms/op`（**19x**）。
- 真树：Walk 16–21 s → WalkDir 4.9–6.6 s（**2.8–3.7x**，两人各自复算的区间）。
- 负控（判据有牙）：把快速路径改成 `&& false` → 新用例立刻判红（`分配量 = 16706`）；还原后绿。全仓 `grep NEGCTRL` 已确认无残留。
- 独立复核（子代理 perf-verifier，读 sha 前缀 pure.go b5d13853 / executor.go dbcec859 / engine.go 048dc142）：
  探针 5,622 组合零分歧、IsStalled/IsEarlyStall 布尔 2,440 样本 0 分歧、collectOutputsSince 120 组合/1,941 键逐位一致；
  负控 0 次 vs 802,287 次分配；对判据自身植入 no-fallback 变异 21/21 被抓。产出件 = `xtmp/perf-verify.md`。
- 部署与线上验证：`xtmp/deploy_agent.ps1` → 旧 `5dca5df7` → 新 `ea4c5a9b`（matchesBuilt=True, sumMatches=True），
  `D:/workbench-agent/workbench.exe --version` = `workbench 1.0.0 (2026-10-28T01:25:59…)`（= 当次构建时间）；
  新实例 PID 23796，panel 17890 HTTP 200，`state.json phase=running`，日志 `引擎已启动` + 自主任务已生成。

## 五、线上性能判据（部署后待收）
- 判据：部署后 `stalled` 的 `elapsedMs` 应**低于旧的观测下限 134,664 ms**（旧分布 n=53 / p50=149,994 ms），
  因为检测延迟不再被「两轮 18 s 全树扫描」顶住（看门狗回到 5 s 节拍）。
- 采样命令：`python D:/workdesktop/xtmp/perf_post_probe.py`（按 `2026-10-28T01:29` 切前后窗口）。

## 五.1 部署后资源读数（新构建，PID 23796）
- 安静期（唯一在跑尝试处于「思考无事件」阶段，正是旧构建会连续扫树的窗口）：60 s 采样 **CPU 1.30 s = 2.2% 单核**，WS 41 MB。
  对照推导：旧构建在该阶段每 5 s tick **至少 1 次**无条件全树扫描（IsStalled 的产出探测在比较之前、无任何短路），
  首进展到达前还要再加 1 次（IsEarlyStall 的零进展探测）→ 16–21 s/次 ⇒ 单核持续满载（1 次/5 s 即 duty≈320–420%）。
- 繁忙期（一次 240 s timeout 收尾 + 新尝试启动）：60 s 采样 CPU 27.03 s = 45%（含引擎/存储/模型池等全部自身工作）。

## 五.2 现场同窗口 A/B（部署后，用「在跑尝试最后一次事件时刻」当真 watermark）
- 命令：`cd D:/workdesktop/xtmp/perfwalk3 && ./perfwalk3.exe "D:/workdesktop" 1793122405000`（1793122405000 = 01:33:25.000，即 task-1793122165946-d5d8 最后一次事件时刻）。
- 读数（同一次运行内，同树同 watermark）：
  · OLD（全量 `filepath.Walk`）：**wall 18.34 / 20.34 s，访问 431,586 个目录项**，且 `TOTAL_CPU=20.53 s / wall 20.53 s ⇒ 100% 满载一个核`；
  · NEW（顶层优先 + WalkDir + 命中即 SkipAll）：**wall 0.001 s**（第 39 个目录项就命中 `.dsh-ops/` 下 01:46 写出的白名单文件）。
- 该窗口同时解释现场读数：部署后该尝试处于「子进程静默 13 分钟」期，进程 CPU 只 2.1%（探针 1 ms 返回）；
  旧构建在同一窗口每 5 s tick 会烧掉 ~18–20 CPU 秒（单核满载）—— 与「stalled 判定 p50 149,994 ms（预算 120 s）」互相印证。
- 诚实分档（增益取决于命中位置，不取单值）：
  · 命中靠前 ⇒ 单次调用 ~1 ms（对比旧 18–20 s，四个数量级）；
  · 全树无任何命中（真·零进展）⇒ 仍要走完，但 WalkDir 比 Walk 快 2.8–3.7×；
  · 事件信号仍在窗口内（健康事件流）⇒ **零读盘**（旧构建在这档也照样每 tick 走全树）。

## 五.3 仍未取证（如实登记，不当作已证）
- 「部署后 stalled 的 elapsedMs 应低于旧观测下限 134,664 ms」：部署后 15 分钟窗口内**没有出现任何 stalled 尝试**（只有 1 次 240 s timeout），故该判据**尚未取得样本**；采样器与命令已留（`xtmp/perf_post_watch.py`），下轮可直接续采。
- 现场干扰项：`workspace.mode=external` + `root=D:/workdesktop` 使「产出文件」信号与**其它会话/构建写进同一棵树**的文件不可区分
  （本轮实测：并行会话 01:46 在 `.dsh-ops/` 写的文件就被当成该任务的「进展」）。这不是本轮引入的缺陷，但会影响一切基于 mtime 的进度判据，登记待议。

## 五.4 部署窗口的完整读数（01:29:25–02:03:42，34 分钟，未加任何配置绕过）
- 该窗口唯一的长尝试 = `task-1793122165946-d5d8` / `z-ai/glm-5.3`：子进程 01:33:26 起**事件流全程静默**（events.jsonl 仅 start/session/turn_start/step_start），
  02:01:33 以 `bad_exit`、`elapsedMs = 1,687,570 ms`（28.1 分钟）收尾 —— 正是旧实现会连续扫树的那一类形态。
- 新构建在该静默期内实测 **CPU 1.23 s / 60 s = 2.1% 单核**（01:44:34 采样，WS 38.9 MB）。
- 旧构建在同一静默期（推导，常数全部来自本轮实测）：每 5 s tick 至少 1 次全树扫描 × 18.34–20.34 s/次 · 100% 单核
  ⇒ 28 分钟内会**背靠背扫树不停**（336 个 tick × ~18–20 CPU 秒 ≈ 100+ CPU 分钟的工作量塞进 28 分钟墙钟）⇒ 该窗口单核 CPU 降幅 ≈ 50 倍。
- 窗口内 `尝试结束`（n=10）：bad_exit 9（p50 19.5 s）、timeout 1（240 s）；`stalled` = 0（见 五.5）。

## 五.5 事前判据未取到样本（如实，不声称）
- 判据「部署后 stalled 的 elapsedMs < 旧下限 134,664 ms」**未取到样本**：34 分钟窗口内 stalled=0，其后 12 小时该实例已停机。
- 根因（可复算）：external 模式把工作根设在 `D:/workdesktop`，该树**始终**被其它会话/构建写入白名单文件 ⇒ 产出信号恒新鲜 ⇒ 该形态下「卡死」判据不会成立
  （旧构建同样如此，故不属行为变化）；要复现该判据需要一个安静的专用工作根。
- 该档增益仍有独立读数：真·零进展（全树无命中）时每 tick 从 18.34–20.34 s 降到 4.9–6.6 s（2.8–3.7×）。

## 五.6 生产实例停机与恢复（本轮自主处置，#31/#37）
- 02:03:42 后 agent 根实例停机（`state.json` 仍写 phase=running，典型停机未落盘），12 小时后仍无进程；第 56 轮提交说明实例守卫曾多次处死异根引擎（T49）。
- 处置：14:13:18 用**已部署的同一二进制**（hash `ea4c5a9b…` == `SHA256SUMS`）重启 agent 根实例 → PID 33232、panel 17890 HTTP 200、日志 `全局限速闸门已装配`。
- 未做：未改任何 live 配置；未动 dist 根的 `user-quit.flag`（其语义是「有意停机」，不属本轮范围）。

## 六、未做/次要（如实登记）
- `store` 侧：WAL 每条 `fsync` + 每 2000 条压一次 15.7 MB 全量快照（`queue.json` 5000 任务 / 4985 链）——量级远低于本轮治掉的扫描，未动。
- `hasWorkspaceOutput` 的 `d.Info()` 失败分支、`SkipAll` 不改变布尔值：独立复核者登记为「未取证（非缺陷）」。
- 未取证：cmd/workbench 包在「全仓 -cover 并行满载」下曾出现一次 FAIL（第一次官方构建），干净树上两次全绿；
  另经查该次失败与复核者临时件（`zz_verifier_diff_test.go` 84.34 s）同窗，属负载型抖动，非本轮代码缺陷。

## 七、委派（#44）
- 派 3 件：T-PERF-A（log-profiler，子代理两次失败 → Lead 自干收口）、T-PERF-B（code-hotpath，零产出 → Lead 自干）、
  T-PERF-C（perf-verifier，成功；三条异议全部采纳并修订）。
- 事故登记：perf-verifier 的临时测试件写在 `workbench/internal/executor/` 内，与我跑的官方覆盖率门禁撞车（写面冲突）；
  已纠偏（禁写 repo + 删残留 + 自证 `git status --porcelain | grep zz` 为空）。
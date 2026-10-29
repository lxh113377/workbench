# 会话回执 · 目标 round 2（ds 端 · 2026-10-29 16:1x–18:2x，第五轮对标）

## 本轮定位
第五轮开源对标（前四轮已在册）：本轮不从「缺什么功能」出发，而从**生产日志里读不通的那一桶**出发 ——
面板告诉我们 39 个任务「未知错误」，这句话本身就是缺陷。逐行精读 asynq + river 的错误语义层与
生命周期留痕，落地报告里的四项改造（按优先级高→低），并完成部署 + 行为面取证。

## 四项改造（提交 f1addb9 + cf25b4e + 606b281，均已推送 origin/master）

| 优先级 | 改造 | 排序依据 | 完成标准（实测） |
|---|---|---|---|
| 高 1 | 失败具名分型 + 枚举双向漂移门禁 | 唯一的远程诊断入口；且**同时修行为缺陷**（AND 交集使 no_artifact/not_served attempt0 即死） | `go test ./internal/retry ./internal/web ./internal/engine` 绿；两条变异负控各自判红；隔离实例行为面 `kind=pool_wait_capped` 实锤 |
| 高 2 | 守护三条路径持久留痕 + 留痕面自检（T68） | 无人值守下「跑过但没留痕」= 没跑；11 小时静默那场的同一条根 | `-SelfTest` 13/13 PASS；4 条负控各自判红；live 实证 `17:58:03 用户主动退出…不拉起` 真落盘 |
| 中 3 | 报告内容门禁加「必须分级」（T67） | 目标验收语欠项，不阻断运行 | `TestReportGateProblems` 11 子用例 PASS，含「提高/其中/降低 构词不得放行」假绿负控 |
| 中 4 | 方案档位判据时序消抖（T69） | CI 假红侵蚀「绿」的可信度 | `-race -count=5` 绿；变异 PickStrict→Pick 判红（证明没削牙） |

## 关键取证数字（全部当日现取，可复算）
- 三面分裂（本轮新增口径警告，见学习计划 B19）：同窗口**尝试面** unknown 2/380=0.5%，
  **任务面/日志面** 39/45=86.7%。原因：判死走 `engine.go:1686→:1687 return`，绕过 `AppendAttempt(:1216)`。
  ⇒ 只看 attempts.jsonl 会得出「没问题」的错判。
- 部署：干净 worktree `f1addb9` 构建，sha256 `E8B661C6…23CDB0`，`vcs.revision=f1addb9`，
  `check_artifact_checksum.ps1` rc=0；dist 与线上根 `D:\workbench-agent` 双双换装，
  线上根走受控窗口（stop → --quit → 备份 → 覆盖 → lib_silent_launch 起 → start → 复核），
  复核 `engine_state=running`、`user_quit_marker_present=False`（Engine.Start 清标记确实生效）。
- 换装后 unknown：`before=46 / after=0`（历史数据**不改写**，改它等于伪造证据）。
- 行为面取证：6 车道全禁用 ⇒ Pick 必失败 ⇒ 18:09:03 `kind=pool_wait_capped`、poolWaits=4，
  全程零模型调用/零额度/零子进程；取证实例已回收（KILLED 35392），复核只剩生产 pid=11156。

## 门禁全绿清单（实测 rc=0）
go vet / go test ./...（31 包）/ -race（retry+engine+web+executor+config+modelpool）/ gofmt 零输出 /
check_all 49 段 PASS（1 SKIP 需参数、1 UNVERIFIED 长驻内存口径，均既有）/ check_changelog /
check_doc_refs / check_report_claims / check_report_figures / check_gate_coverage / check_artifact_checksum /
check_ps1_syntax / check_ps1_encoding / check_ps1_eol / check_multi_bom / check_noninteractive。

## 本轮我自己的错（7 处，全部当场纠正并写进报告 §8）
1. `edit` 锚点含 `NoLane` 行而替换块未重新输出 ⇒ 静默删常量（CHANGELOG 记过的坑，我重踩）；
2. 第一次变异打在**注释**里的同名字样，测试照绿，差点被当成「判据没牙」；
3. 面板标签对账判据两版都写错（按单引号切分取键 / 按行取冒号前），判据比被测面更红 —— 按 D10.2 先怀疑判据；
4. PowerShell 自检里嵌字符类 `[^"]` 被当数组索引 ⇒ ParserError；
5. 往 CRLF 文件追加 LF 行造成混排；
6. 用 `spawn(detached)` 起取证实例被作业对象连坐回收，一度误判「产物起不来」（本仓早有 lib_silent_launch）；
7. 取证实例用完没自灭（守护 CommandLine 匹配不到 xtmp 路径）⇒ 留了常驻进程，补杀。
另：编辑 CHANGELOG/keepalive 时两次弄丢 UTF-8 BOM，都被自家门禁当场抓出（`check_changelog` /
`check_ps1_encoding`）—— 门禁按预期工作，但错是我造成的，如实记。

## 并发写者（D7.2 已守）
- `tests/keepalive.ps1` 本轮两个写者：子代理 f8a1cfe1（13 条自检 + 防顶名纪律）与我（BOM 修复 + 9 条自检）。
  对方交付的收敛版把编号让给我并把自己的重编为 10-13 ⇒ 我未覆盖其内容，只在其交付后复核并测绿；
  其自述「PS5.1 rc=0」我实测 `powershell` 直跑 rc=1（ExecutionPolicy 所致），带 `-ExecutionPolicy Bypass` 才 rc=0
  —— 计划任务实带 Bypass，故其结论成立但口径要写明（不采信自述，以我复跑为准）。
- `templates/defaults.go`、`chain/lesson_delivery_contract_test.go` 等 13 个未跟踪测试件属并行会话在途件，
  本轮一律未碰、未并入提交（`git status` 复核）。
- 对方在本轮期间另提交 `aff53e8`（全池黑障 modelIDVariants）与 `86f21f1`（round 67），我复核未触碰我的文件。

## 未闭环（落待办，不外抛）
| 项 | 卡点 | 已做处置 |
|---|---|---|
| 三条在册待办提 issue（隐性 kind 名单 / CHANGELOG 门禁 / context.Cause 归因） | GitHub API 写面 `graphql EOF`（读面早前可用，属瞬时） | `xtmp/issue-9|10|11.md` 正文已备好 + `xtmp/round66_issue_retry.sh` 后台自动重试（8 次×3 件、间隔 25s），日志 `/tmp/issue_retry.log` |
| L3 覆盖率真实化 | 本轮 4 项名额用尽；且**禁止**降阈值糊过去 | 学习计划 §10.4 刷新：本机 77.8/55.8/72.3 与 CI 口径 60/47/55 不同构，须按 CI 实测重定 |
| T62（无真实模型即红的测试） | 半阻塞于上游配额 | 报告 §4.4 登记最小动作：抽 `--plan-selftest` 模型来源为可注入接口 |
| H-5 lanes.go 三个零引用导出符号 | 「删」与「补调用点」需分别判责 | 报告 §4.4 登记，不默认删 |

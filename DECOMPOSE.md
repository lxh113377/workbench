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

## 实际收口（2026-10-29 20:5x，本轮结束时回填）

| 件 | 状态 | 证据（可复算） |
|---|---|---|
| A #10 | ✅ 已入库 `022181b`，CI success | `pwsh -File tests/check_changelog.ps1` rc=0；五条负控各自判红后还原转绿 |
| B #11 | ✅ 已入库 `6953c76`，CI run 40874493501 双 workflow success（含 cancel_cause* 两文件）| `go test -run "Cancel…"` 6 条 PASS；变异 A/B 各判红；`-race -count=2` ok |
| C L3 | ✅ 两段收口 `aa69b8e` + `a1fc828` | 三包基线/CI 实测/余量：evaluate 67/70.7/3.7、modelpool 67/70.2/3.2、modelroute 95/98.9/3.9 |
| D modelroute 补测 | ✅ 子代理产出，Lead 独立复跑一致 | `go test ./internal/modelroute -cover` → 本机 99.0%（Lead 复算同值）、`-race -count=2` ok、DATA RACE 计数 0 |
| 部署（#40） | ✅ dist + 线上根均换至 `a1fc828` | sha256 160D8985… == SHA256SUMS（check_artifact_checksum rc=0）；`check_homepage.ps1` VERDICT PASS（含 livePage）；线上 STEP5 engine_state=running |
| 线上行为证据 | ✅ 判死具名已在生产可见 | queue.json：`pool_wait_capped` 7 条全部晚于 17:53 换装、全部 poolWaits=4；`unknown` 在 17:53 之后**零新增**（beforeT1=46 / after=0）|

## 收尾负申报（未派件 + 一条**拒绝**的取证手段，如实记）

- 未派件的 skip_reason：A=决策本体（新判据的存量/增量口径是设计决策，结论要进 CI 受理面）、
  B=写面冲突（engine.go 有并行会话 T74 同文件在写，派出去必撞 D7.2）、C=严格串行（阈值数字的输入是
  CI 取证回件）。三者均在枚举内。
- **主动放弃的一条取证手段**：#11 的「取消具名归因」在 era2（新实例 20:53:11 起）还没有自然样本
  —— 唯一候选行 20:53:04 早于进程启动 7 秒，属**旧二进制**所写，不能当新代码的证据。
  可选做法是「POST /api/engine/stop 造一次带 cause 的取消」，但那会**故意打断老大的 7×24 在跑任务**
  换一条日志读数 ⇒ 不做。改为挂零侵入监视器等自然发生（缺陷自然节奏实测 30-45 分钟/条）：
  `xtmp/cancel_live_watch.mjs`（只读扫台账，命中即打印 LIVE_PASS/LIVE_FAIL）。
  这不算未闭环：单元测试 + CI + 变异负控已证明行为，生产侧已有**对照证据**——同一缺陷形态
  （kind=unknown 的 cancel 行）在 #11 上线前的 19:10、19:53 各新增一条，上线后 0 条。

## round 4（2026-10-29 21:0x，目标 = #12 → #9 + 复取 #11 live 证据）

### 派发判定（30 秒卡）
命中 T1（#12 与 #9 互不引用）、T2（各 ≥3 回合）、T3（需 CI 日志面 + 本机实测面 + 台账面）、
T4（结论进 build.ps1 / ci.yml 权威件）、T5（落 ≥2 文件）、T6（交付件 ≥2）⇒ 必须并行派 ≥2。

| 件 | 形态 | 写面 | 结果 |
|---|---|---|---|
| #12 CI 逐包覆盖率取证（只读） | subagent 后台 | 无 | 待收 |
| #9 隐性 kind 名单收编（写测试+码） | subagent 后台 | retry/executor/modelroute/web | 待收 |
| #12 前置 + 机读对账判据 | Lead 自留 | setup/build.ps1、.github/workflows/ci.yml、.gitignore、tests/check_cover_baseline_margin.ps1、tests/check_all.ps1 | 已落 |
| #11 live 证据复取 | Lead 自留（零侵入监视器） | 无（只读生产台账） | 监视器在跑 |

skip_reason：`#12 前置` = 写面冲突（build.ps1 同时被并行会话改过，Lead 串行做）；`#11 复取` = 严格串行
（判据结果依赖自然样本出现）。均在枚举内。

### 本轮已产出的关键纠正（都是我自己的错，不是他人）
1. **issue #12 正文口径错**：「15 条里 12 条虚设」是只按 CI 算的，漏了本仓既有规则「两个环境都要过
   ⇒ 取下界」。按 binding = min(CI, 本机) 重算 = **7 条虚设**（web 18.2 / executor 11.1 / shortcut 10.4 /
   cmd 14.9 / engine 9.3 / supervisor 8.1 / chain 6.4）。已写进 build.ps1 注释与本卡。
2. **上一轮 CHANGELOG 里「CI 日志没有 avg 行」「.workbuddy/memory 不入库」两句均错**：实测 run
   40876282900/job 123456954511 第 12:52:06Z 逐字有 `覆盖率读数已落盘：avg=73.17% … (ci)`；
   且 `.workbuddy/memory` 有 40 个受跟踪文件。真缺口是「写了没上传成 artifact ⇒ 跨 run 不可读」。
   已在 CHANGELOG 原位标注更正（不改写历史，只加更正）。
3. **我差点造出一个比原缺陷更坏的版本**：把 build.ps1 的 `if (Test-Path $dir)` 换成裸 `{ }` ——
   实测（xtmp/blockprobe.ps1）PowerShell 的裸花括号是 scriptblock 字面量、**只求值不执行**
   （探针 ran=False）⇒ 会把「静默跳过」变成「静默不执行」。改为 `New-Item -Force` + `if ($true)`。
4. **我的新判据自己假绿过一次**：`核对 0 条` 仍 rc=0（改条件行时漏改 `$prop.Value` 取值行 ⇒
   恒走「读数里没有」分支）。修法同时补「零核对一律判红」—— 即我自己在这件里引用的 ㉑二 规则。
5. **rc=2 语义分档**：本仓 check_all:55-57 记录「段内把 rc=2 归零 = 洗绿洞」。故本件用 rc=3 表
   「前置缺失（无读数/证据只一侧）」、rc=1 表「虚设/基线过高/对照面失效」，段内不做任何归零改写。
6. 单侧证据会给出**错的建议**（实测：CI-only 会说 browser 应设 28，而本机 browser 25.8 ⇒ 设了本机就红）
   ⇒ 判据改为「缺任一侧读数 ⇒ SKIP(3)」，不硬判。

### round 4 实际收口（23:0x 回填）

| 件 | 状态 | 可复算证据 |
|---|---|---|
| #12 前置 + 机读判据 | ✅ `d655156`，CI run 40886662475 三 job success | artifact `coverage-readings-40886662475`(574B) 内容 env=ci/head=d655156/31 包/avg 73.17；真两侧读数喂进判据 ⇒ rc=0「核对 15 条」|
| #12 抬线 | ✅ 15 条余量全进 0..5pp | binding=min(CI,本机)：engine 71/web 49/executor 47/supervisor 53/shortcut 32/chain 88/cmd 12 + 微调 4 条 |
| #9 隐性 kind 收编 | ✅ `f771474`，CI 40895782299 success | Lead 独立复跑 gofmt/vet/retry/web/engine + 自做变异负控（判红并指名 :370，还原 sha 810c9374250b）|
| #11 live 证据 | ✅ 由「等自然样本」换成受控差分并取到 | 新件 NAMED_TEXT=1/RAW_TEXT=1/LANE_POLLUTION=0 PASS；旧件 ddded8 NAMED_TEXT=0 PASS（复现缺陷）|
| 闸假红修复 | ✅ `99bb121` | 端到端负控双向：真零断言判红、raw string 假阳性消除；1035 个 Test 全合规 |
| 守卫负控 | ✅ `865552c` | GUARD_DIRTY rc=2 refused=1、GUARD_PORT rc=2 refused=1 ⇒ GUARD_NEG_PASS |
| 部署 | ✅ dist + 线上根 = `f771474`（含 #11/#9）| sha 6200428190… == SHA256SUMS；`git diff f771474 HEAD -- workbench/cmd/workbench workbench/internal` = 0 ⇒ 无需再停机 |

### round 4 收尾负申报（未派件 + skip_reason，枚举内）

- `#12 抬线与 artifact` = **写面冲突**：build.ps1 / ci.yml / .gitignore 同时被我本轮实现，派出去必与我自己撞（D7.2）。
- `#11 差分取证` = **严格串行**：判据的输入是「旧 exe 与 new exe 各跑一次同一路径」，第二次依赖第一次读数。
- `issue #9 / #12 正文更正评论` = **决策本体**：更正口径要进 GitHub 权威面，结论由 Lead 承担。
- `check_report_figures 移动目标` → 未实施改动，改为提 **issue #13**（判据设计问题，需另行裁量，不属本轮目标）。

### 三条我没做成/没做全的（如实记）

1. **车道污染（#11 缺陷②）仍无 live 证据**：差分两侧 `lane.failure` 都是 0。查清原因（假 DSH 起得来 ⇒
   executor 自标 cancelled ⇒ 走旧代码也有的 tier-4 兜底分支），**不硬凑时序**去造污染 ⇒ 该缺陷证据仍是单测面。
2. **`#12` 的其余 17 包专项基线未纳入**：retention binding 63.0 / templates 63.6 距默认 55 只剩 8 点左右，
   是否给专项线是判断，本轮未动（已在 issue #12 评论里登记）。
3. **验证件不进 CI**：`cancel_cause_live_verify.ps1` 与既有 `round66_live_verify.ps1` 同等待 —— 只挂本地,
   靠 `check_doc_refs`/`check_gate_coverage` 的引用面活着（不叫 check_*.ps1 故不入 $gates 清单）。

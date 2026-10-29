# session 回执 round 4（2026-10-29 21:0x–22:2x，目标 = #12 → #9 + 复取 #11 live 证据）

## 结论：三件全部落地并部署，#11 的 live 证据本轮取到了（带一处诚实的未复现）

- **#12 覆盖率基线 + CI artifact 前置** → `d655156`（CI run 40886662475 三 job success）
  口径纠正两处（都是我的错）：① issue #12 的「12 条虚设」只按 CI 算，漏了本仓「双环境取**下界**」规则
  ⇒ binding=min(CI,本机) 重算是 **7 条**；② 我写的「CI 上 Test-Path 判假 ⇒ 落盘块静默 no-op」**是错的**，
  实证 HEAD 版在 CI 打出过 `avg=73.17% … (ci)` ⇒ 真因只是「没上传成 artifact」。
  抬线 10 条（engine 71 / web 49 / executor 47 / supervisor 53 / shortcut 32 / chain 88 / cmd 12 /
  evaluate 68 / modelroute 96 / dshprofile 75 / browser 23），CI 全过，15 条余量全进 0..5pp。
  新件 `tests/check_cover_baseline_margin.ps1`（挂 check_all）+ ci.yml `Upload coverage readings`
  （`if: always()`）⇒ artifact `coverage-readings-40886662475`(574B) 实测存在，内容 env=ci/head=d655156/
  31 包/avg 73.17 全机读；判据用**真两侧读数**跑通 rc=0「核对 15 条」（不再是手造 fixture）。
  负控四态实测：单侧⇒rc=3、零核对⇒rc=1、构造虚设+假红⇒rc=1 并逐条点名。
  另提 **issue #13**：check_report_figures 把并发在途件数当硬判据 ⇒ 同一小时 12→13→14→15→14，红灯无主。
- **#9 隐性 kind 名单收编** → `f771474`（10 文件 +726）。子代理产出，我独立复核：gofmt 空、vet 四包 rc=0、
  retry 定向 9 PASS/0 FAIL、**自跑变异负控**（把 crashRescueKind 改回裸字面量 ⇒ 判据真红并指名
  kind_writeback_contract_test.go:370，还原 sha 逐位相同 810c9374250b、复跑转绿）。
  采纳它两条纠偏（我都复核过）：① issue #9 正文「IsRetryable 判 false」**方向反了** —— HEAD 版
  retry.go:123 把 Unknown 列进 `return true`，ParseKind 认不出正落成 Unknown ⇒ 收编前真值是 **true**，
  危害是终态处置落进最宽重试桶；② 台账 plan 的「近 24h 203 行」取不到口径，实测 1035/2467 行（42%）。
- **#11 live 证据** → 新增 `tests/cancel_cause_live_verify.ps1`（隔离实例 + 真 PE 假 DSH stub + 自建 config）。
  **不再靠"等自然样本"**（上一轮监视器跑满 240 分钟 NO_SAMPLE），也**没有**打断生产。差分两侧实测：
  · 新件（dist f771474）：`kind=cancelled error=用户取消（面板/CLI 终止该任务）（原始错误: context canceled）`
    停机面同跑 `kind=cancelled error=引擎停机取消（非任务问题，不计车道失败）（原始错误: context canceled）`
  · 旧件（dist bak-pre-t9 = ddded8，git merge-base 校验不含 6953c76）：同两条路径均 `error=context canceled`
    （NAMED_TEXT=0）⇒ **具名归因确实在本版生效**，差分面成立。
  · 自清理 LEFTOVER_PROCS=0 / STUB_PROCS=0（隔离实例不受生产守护接管，脚本自己扫干净）。

## 一处诚实的未复现（不 overclaim）
`LANE_POLLUTION`（取消污染车道）在**两侧都是 0** ⇒ 本 setup 没复现这个缺陷。原因未查到底（stub 秒起、
取消落在 executor 自标 cancelled 的分支，与生产「启动 DSH 失败」包装路径不同）。因此缺陷②的证据仍然只有
单测面（TestCancellationDoesNotPenalizeLane + 变异负控），我没有 live 证据 —— 已写进脚本注释，不写成已证。
同理旧件在生产落成 `kind=unknown` 的那条形态（error=启动 DSH 失败: context canceled）本 setup 也**没复现**，
我这轮证明的是「具名归因有/无」这一条，不是「unknown→cancelled」那一条。

## 部署（#40）
- dist：`f771474`（sha256 6200428190… == dist/SHA256SUMS，check_artifact_checksum PASS），备份 .bak-pre-f771474-20261029-221742
- agent 根：见下（redeploy_agent_root 输出）

## 我自己的错（本轮，全部当场抓住并修）
1. build.ps1 的 `if (Test-Path)` 换裸 `{ }` —— PowerShell 裸花括号是 scriptblock 字面量、只求值不执行
   （xtmp/blockprobe.ps1 实测 ran=False）⇒ 会把「静默跳过」升级成「静默不执行」，比原缺陷更坏。
2. 新判据第一版**假绿**：改条件行漏改 `$prop.Value` ⇒ 15 包恒走「读数里没有」、`核对 0 条` 仍 rc=0；
   我自己在这件里引用的 ㉑二 规则被我违反 ⇒ 补「零核对一律判红」。
3. 缩进批处理 slice 位数算错 ⇒ 17 行 `  #` 变 `  ##`；.ps1 的 BOM 本轮又丢 1 次（第 6 次），
   且「先 add 后补 BOM」让索引 blob 无 BOM 被 check_multi_bom 判红。
4. `git commit` 前把 msg 文件 `rm` 掉 ⇒ 提交没发生（我没假报完成，查状态后重写）。
5. 假 DSH 先写成「.bat 改扩展名」—— Go 走 CreateProcess 起不来；改真 Go PE（xtmp/fakedshmod/dshstub.exe）。
6. 验证件第一版 config 落点写成 `<root>/config/config.json`，实测 Layout 是 `<exe>/workbench-output/config.json`
   （internal/config/paths.go:12-16）⇒ 按源码改，不猜。

## 后续收口（同日 22:4x–23:0x，覆盖上文「#11 只有一次性证据」与「NO_SAMPLE」的旧状态）

- **#11 已拿到可复跑的 live 差分证据**：新增 `tests/cancel_cause_live_verify.ps1`（隔离实例 + 真 PE 假 DSH +
  自建 config），两侧各跑一遍：新件 dist/f771474 `NAMED_TEXT=1 RAW_TEXT=1 LANE_POLLUTION=0` PASS；
  旧件 dist/bak-pre-t9（ddded8，`git merge-base --is-ancestor 6953c76` 判假）同一路径 `NAMED_TEXT=0` PASS
  （复现缺陷）⇒ 具名归因由**生产二进制对**做实差分，不再靠等自然样本。user / shutdown 两种信号结论一致。
- **验证件自身也被验**：两条前置守卫负控 `GUARD_NEG_PASS`（DIRTY rc=2 refused=1、PORT_BUSY rc=2 refused=1）。
  起因是真实污染：一次 run 被我的工具超时打断留孤儿实例 ⇒ 两实例共享 queue.json，那种 PASS 不能当证据 ⇒
  改成清不掉/端口被占就 exit 2。另记一次**无效测试**：用 node 持句柄模拟脏根无效（libuv 默认
  FILE_SHARE_DELETE 挡不住删除），正解是 PowerShell 的 FileShare.None。
- **`check_test_assertions` 假红修掉**（commit `99bb121`）：红的是判据不是被测件 —— 它把「下一个 ^func」
  当函数体边界，被 Go raw string 里的顶格 `func f()` 骗。我第一版补救（反引号奇偶）**更危险**：双引号串里
  也能含反引号 ⇒ 真实 Test 会被整体跳过 = **假绿**；量化证据是奇偶版只数到 1030，少的 5 个全是真测试。
  正解换成 `workbench/cmd/assertionprobe`（go/parser）。端到端负控双向过：真零断言判红、raw string 假阳性消除。
  真实仓 1035 个 Test、0 零断言、4 契约豁免。**CI run 40895782299（head 99bb121）三 job success**
  ⇒ f771474 / e64ed74 两次 repo-gates 红（我的假红 + 我缺 `-UseBasicParsing`）均已转绿。
- **部署判定（不重复停机）**：`git diff --name-only f771474 HEAD -- workbench/cmd/workbench workbench/internal`
  = **0 个文件**，且 `cmd/assertionprobe` 不被任何产品包 import ⇒ 线上 exe（vcs.revision=f771474）
  **已含 #11 与 #9**，无需第三次换装。这条结论是查出来的，不是推的。
- issue #12 已留更正评论（口径 + 真因），issue #9 已留更正评论（危害方向反了 + 实测读数 + 措辞自决）。

## 本轮新增提交
`d655156`(#12) → `f771474`(#9) → `e64ed74`(#11 验证件) → `99bb121`(闸假红修复) → `865552c`(守卫负控)，全部 push。

## 生产自然样本（23:11:00）—— #11 缺陷①的最终证据，强于受控差分
agent 根换至 64459f1（含 #11，vcs.modified=false）停机时真实台账写 2 行具名归因；同切点前旧二进制写裸
context canceled ⇒ 同机同动作两种文案的天然差分。缺陷②（车道污染）仍无生产证据：进程 23:11:05 重启、
health 是内存态不落盘 ⇒ 读到的 failure=0 是新进程起点，不能当证据（已在 CHANGELOG 与验证件注释写明）。
另两处本轮事实：dist/SHA256SUMS 由我重写对齐（dist 不受跟踪、纯派生件，exe 新于清单）；agent 根清单同样
漂移但不在任何门禁面上且属并行会话换装遗留 ⇒ 登记待并入、不越面。64459f1 已按 #10 口径登记入债务名册
（3033452），全套 check_all 最终 CA_RC=0。

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

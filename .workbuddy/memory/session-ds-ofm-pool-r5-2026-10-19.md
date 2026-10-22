# 会话 ds 端记忆锚点 R5（2026-10-19 16:20 · OFM 模型池扩展 goal）

## 本轮交付（3 个提交，全部 path-limited，Lead 在途零卷入）

| commit | 内容 | 判据 |
|---|---|---|
| 9be7f0d | least-loaded 真正按在飞负载选模型（此前是 round-robin 别名）+ 删 sticky 死字段 | 3 条新回归 + 行为级变异必红 |
| 7e44267 | 调度器/兜底链遵守 Routable（不可路由模型不得派给任务） | TestSchedulerSkipsNonRoutable 四策略逐个断言 |
| 7f92dfd | 聚合车道接通补丁规格（需求1 最后一公里） | 根因链逐文件实证 |

## 两个真缺陷都是**活体复验**抓出来的（读代码看不出）

1. **least-loaded 名存实亡**：`case StrategyRoundRobin, StrategyLeastLoaded` 共用分支，
   inflight 只在 Acquire/Release 与 /api/pool 快照出现，从未参与选择。
   面板下拉、docs、acceptance 判据全都只查**枚举常量是否存在**（`want := []string{"StrategyLeastLoaded",...}`），
   所以静态门禁全绿而行为是假的。⇒ **判据查"符号存在"不等于查"行为生效"**。
2. **Routable 只在账本侧生效**：`Manager.Routes()` 有过滤，`Scheduler.Pick/PickChain` 没有。
   FailoverRunner 消费后者 ⇒ 面板新增 routable:false 的模型被顶到兜底链首，
   与文档"绝不派给任务"承诺相反。⇒ **同一约束若有两处出口（投影 + 选择器），必须两侧都测**。

## 可复用经验

- **变异负控是回归测试的验真手段**：把修复改回原状（行为级，不是删测试），
  新测试必须翻红；改回原码用 SHA256 校验一致。本轮两条测试都过了这个 Gate。
  第一次尝试用"编译不过"当红（StrategyZZZNever）是无效负控 —— 要的是**行为红**不是构建红。
- **活体面板复验脚本模板**（真启实例 + 真调 API）：
  `runtime/tmp/ui_live2.ps1` 那套：起 --silent 实例 → 读 panel.url → 抓 __WB_TOKEN__
  → 逐个打 /api/pool*。比读代码可靠，本轮两个缺陷都是它抓的。
  坑：PowerShell 里 `$st.lanes` 若服务端返 map，PS 会把 .Count/.Length 等属性混进来，
  要用 `$st.lanes.PSObject.Properties` 过滤；响应字段名必须先读 handler 再断言
  （服务端返 `{model}`/`{ok,count}`/`{lines}`，我第一版按 `{specs}` 取，误判成产品缺陷）。
- **OFM aggregate 车道契约**（实证，未猜标识符）：
  `settings.aggregate = {enabled, providers:{<providerId>:{enabled,apiKey,baseUrl,models[],addedAt}}}`；
  providerId 必须在 `src/providers/registry.json`（实测 75 个）内，否则
  `parseProviderModelId` 判死；baseUrl 含 `{...}` 占位符则该行不产出；
  oauth/adc 类型 `authUsable` 直接 false。线协议 CHAT_SUFFIX：
  openai-chat→/chat/completions、openai-responses→/responses、anthropic-messages→/messages。
  registry 分布：openai-chat/apiKey=64、anthropic-messages/apiKey=3、local(auth=none)=3。
- **CI 红先归因再动手**：39211291332 首跑 FAIL（TestRunOneDrivesAutonomyClosedLoop 缺 nextGoal），
  我的提交面 `git diff --name-only 51381b7..HEAD` 不含 engine/autonomy，且
  `engine` 对 `modelpool` 零引用 ⇒ 判定与己无关；rerun --failed 转绿，
  坐实是 Lead 在册竞态 D6（manager.go:467→470 读后写顺序）。**别替别人的 flaky 买单，也别顺手"修"**。

## 弹窗事故处置（老大 15:3x 现场："一直在跳这个，可不可以不要"）

根因：`runtime\soak-24h-v2\watchdog.ps1` 用 `Win32_Process.Create` 直拉 wbsoak26.exe。
实测该 exe **PE subsystem=3（控制台）** ⇒ WMI 直拉必开可见黑窗；看门狗 2 分钟一轮 ⇒ 反复弹。
**不是**我的实验进程（我起的都带隐藏窗口）。

处置链（含三个实测坑）：
1. 杀旧看门狗 + 改名 `watchdog.ps1.DISABLED`（源头断掉）；
2. 试 `Start-Process -WindowStyle Hidden`：前台会话可用（mainWindow=0），
   但**计划任务上下文里挂死**（窗口站），wscript 同样挂；
3. 试 `wmic` 探测存活：**本机 wmic 已被移除**（FileNotFoundError WinError 2）；
4. 终解：`pythonw`（GUI 子系统，永不分配控制台）+ `Popen` 带
   `DETACHED_PROCESS|CREATE_NEW_PROCESS_GROUP|CREATE_NO_WINDOW`；
   幂等不靠外部探测 —— workbench 自带单实例独占锁（workbench.lock），
   重复 spawn 自行退出（实测连跑两次仍 1 实例）。
   计划任务 `WorkBenchSoakKeepAlive` 每 2 分钟跑 pythonw 版。
5. A7 证据链未断：metrics.csv 55 行历史保留，16:12 起继续累积（59 行）。

坑：PowerShell `param()` 必须是**首条语句**，`$ErrorActionPreference` 放它前面 → ParserError
（"The assignment expression is not valid"），这个错信息完全不指向真因，极易误判。
坑：`@(Get-CimInstance ... | Where CommandLine -match 'watchdog_hidden')` 做清理时，
过滤串出现在**我自己这条命令的命令行**里 → Stop-Process 把自己也杀了（runner exit 4294967295）。
正解：`-and $_.ProcessId -ne $PID`，或把串拆开拼（`'watchdog_'+'hidden'`）。

## 遗留（如实，不虚报）

- 聚合车道执行接通：改点在 executor.go（Lead 写入口，其当前在途 diff 是 external 工作目录，与 aggregate 无关），规格见 docs/模型池-聚合车道接通补丁.md。
- A7 24h：现由隐藏保活链累积，需跑满 24h 后 `--soak` 判定。
- 面板"可用"水位含不可路由模型（`Status()` 的 avail 不看 Routable）——标签口径可接受，未改。


## R5 补记（16:5x 收尾）

- 第四提交 b0bb8f8：`internal/modelpool/aggregate.go`（ProjectAggregate 纯函数 + 11 单测），
  需求1 的「自填端点可执行」只剩 executor.taskHome 两行接线（Lead 在途脏，未抢写）。
- 最终门禁：容器五阶段 VET/TEST/BUILD/CRITERIA/LOOP 全 PASS（213 .go）；
  CI 双绿（run 39224331318 / 39224331423）；`--acceptance` 10 PASS / 0 FAIL / 1 UNVERIFIED(A7 计时)。
- 协作事实：并行会话 15:38 用等价实现改写了我的 scheduler.go/pool_test.go 并合并我的回归，
  行为仍成立（38 用例全绿）；我让位不覆盖。提醒过两点：TieBreaksByPriority 断言丢失、
  sticky 死字段复活。
- 保活链终态：WorkBenchSoakKeepAlive（pythonw+NO_WINDOW，Duration=P3650D 防自动禁用）
  每 2 分钟幂等 tick，实测 mainWindow=0、metrics 续长、无黑窗。

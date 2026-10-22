# 会话 ds 端记忆锚点 R7（2026-10-19 19:05 · goal 第 3-4 轮）

## 触发：老大 18:34 桌面弹「Security Warning: Script Execution Risk」+ 指令
> "我要开箱即用的，这个用自动化程序帮我弄好"

即：不许让任何人按任何键。这既是运维诉求，也是产品缺陷信号。

## 本轮 2 个提交（均已推送，CI 在跑）

| commit | 内容 |
|---|---|
| 0fa03dd | **verify 强制非交互 + 不创建控制台窗口**（产品层根治弹窗等人按键） |
| e1d2819 | 补「超时触发故障转移」运行时断言 + 补正 failover 契约注释 |

## 根因与实测（不是猜的）

Windows PowerShell 5.1 的 `Invoke-WebRequest` 默认用 **IE 引擎**解析页面；
页面含 `<script>` 时弹「Security Warning … [Y]es [N]o (default is "N")」并
**阻塞等人工按键**。而 `verify.Run` 直接 exec **模型写的**验收命令（白名单含
powershell/pwsh），所以模型只要写出 `powershell -Command "Invoke-WebRequest <页面>"`
这类校验命令，就会：弹窗 → 等老大按键 → 整条验收链卡死 → 任务不推进。
这正面违反目标要求4「对用户无感知、不中断正常流程」。

三道加固（`verify.go: runOne`）：
1. `forceNonInteractive(argv)`：给 powershell/pwsh 注入 `-NonInteractive`，
   **插在 -File/-Command 之前**（否则被当脚本参数）；调用方已写过则不重复注入。
   效果：从"挂住等人"变成抛 `PSInvalidOperationException` 立即返回失败退出码
   —— 失败可回灌返工，卡死不行。
2. `cmd.Stdin = nil`：显式切断输入（Go 默认已是 DevNull，写出来是让
   "读不到人工输入"成为可见契约，不依赖库默认行为）。
3. `hiddencmd_windows.go`：`CREATE_NO_WINDOW + HideWindow` 派生 ——
   根本没有控制台可显示，弹窗连载体都没有。

**生产路径证据**（防"符号存在≠行为生效"）：
- `engine/artifacts.go:124` 真调 `verify.Run`；
- 全仓 `exec.Command` 17 个点里，`verify.go:158` 是**唯一执行不可信输入**（模型产出命令）
  的地方，其余都是固定命令（mklink/reg/edge/自身份重启）。

## 三条我自己踩出来/纠正的教训

1. **无交互桌面会让回归测试假通过**。我写的
   `TestPromptingCommandDoesNotHang`（真起 httptest 返回含 <script> 的页面）在
   CI/容器里**即使不加固也不会挂**（实测 A 组 0.3s 抛 NullReferenceException 返回）。
   正解是补**机制级断言** `TestInjectionReachesChildProcess`：让子进程自查自己的
   命令行里有没有 `-NonInteractive`。变异实验证实：去掉注入后**只有机制级那条翻红**。
2. **我照抄了一句不准确的旧注释，把自己的实现判成缺陷**。
   `failover.go` 旧注释写「只有整条链都失败才把最后一个错误抛回引擎」，我据此断言
   `err==nil` 即失败 → 测试 FAIL。查清后发现：执行器超时路径本就是
   `Result{TimedOut:true}` + `error==nil`，引擎 `engine.go:741` 用 `res.TimedOut`
   分支判失败 —— 行为一直是对的，是**注释与实现不符**。
   这正是我前两轮反复批评的东西，这次轮到自己踩。已连带补正注释并在提交信息里留痕。
3. **一上线就红的门禁等于没有门禁，还会诱导别人关掉它**。
   我写的静态门禁 `check_no_prompt.ps1` 首跑报 7 处违规，全是**误报** ——
   它扫到了自己的正则字面量（`"Read-Host"` 等词）。修法是判定前先剥字符串字面量与
   行尾注释（字符级状态机，不用正则：PS 引号转义组合太多）。
   修好后还必须做**反向验证**：造一个故意违规样本喂给它，6 处全中、合规行与注释零误报，
   才承认"0 违规"是真干净而不是空壳。

## 与 Lead 撞车的处理（本轮两次）

1. **重复建设**：我写完静态门禁后发现 Lead 已提交 `1da48f7`
   `tests/check_noninteractive.ps1`，且**已接线进 ci.yml:53**，判据还更全
   （多查"注册计划任务缺 -NonInteractive"、PromptForChoice/ShouldContinue/Out-GridView）。
   而我的未接线文件正让他的 CI 门禁翻红 5 处 → **立刻删除我的**，门禁恢复绿（0 违规）。
   教训：门禁类基建先查是否已有人接线，重复建设不只是浪费，还会阻塞别人。
2. **互补而非重复**：我的 verify 产品层加固**不在**他门禁的覆盖范围内 ——
   他扫的是仓库里的 .ps1/.cmd/.bat（`$extOk` 白名单），抓不到**运行时模型产出的命令**。
   两层各司其职：静态门禁防"人写的脚本留交互构造"，verify 加固防"模型写的命令卡住流水线"。
   这条判断依据是读他的 `$extOk` 与扩展名过滤逻辑得出的，不是猜。
3. 另：本轮还清掉了上一轮我自己写的 `internal/silent` + `cmd/wbsilent` 死代码
   （Lead 的 windowsgui 方案更彻底），未提交即删除。

## 需求3 覆盖度补漏（e1d2819 的由来）

需求3 原文点名「不可用、超时、报错」三种触发。核查既有测试：
超时只在 `MarkFailure` 层测过（pool_test.go:184），
**没有任何 FailoverRunner 级的超时接力证据**。补两条 + 变异负控
（删掉 `judge()` 的 `if res.TimedOut` 分支 → 两条同时翻红，报错正是
「超时模型应被记失败，实际 Failure:0」；恢复后 SHA256 一致）。

## 顺手修的基础设施

- **git 跟踪引用损坏**：`.git/refs/remotes/origin/master` 被写成 41 个空格，
  `git fetch/push` 报 `ignoring broken ref`。用 `git ls-remote` 取权威 oid
  （远端 master 与本地 HEAD 一致，我 8 个提交都在历史里）后
  `git update-ref` 原子修复。未动任何共享配置。
- **Docker Desktop 守护进程未运行**：容器门禁报 pipe 不存在。
  自动 `Start-Process "Docker Desktop.exe" --minimized` 并轮询 `docker version`，
  5 秒恢复。按门禁自己的纪律：基础设施不可用 ≠ 行为失败，必须分开判。

## 终态验证

- 容器五阶段门禁 **VET/TEST/BUILD/CRITERIA/LOOP 全 PASS**（242 .go）
- Lead 的无人值守门禁 0 违规；桌面可见窗口数 0；keepalive 任务 rc=0 每分钟正常
- go build/vet ./... = 0；go test ./... 26 包全 ok
- 提交面：0fa03dd、e1d2819 已推送，ahead/behind=0/0

## 遗留（如实）

- 需求1 执行侧接线仍未做：`executor.taskHome()` 仍硬编码 aggregate 关闭，
  `executor.go` 全程被 Lead 占用（17:44 mtime + 脏）。入口早已备好
  （`AggregateForDSHHome`）+ 端到端可行性已实证（chatHits=2）。
- A7 24h 计时：两条保活任务均由 Lead 掌控（Soak 那条现 Disabled），我不擅自恢复。

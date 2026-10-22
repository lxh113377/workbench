# 会话 ds 端记忆锚点 R7（2026-10-19 19:25 · L4 + L7 + issue#1 三项全落地）

## 老大指令
「① 授权跨分工修 #1；② L7 社区协作模板；③ L4 卡死救援外部化」→ 三项全部完成并推远端。
其间插单：「我要开箱即用的，这个用自动化程序帮我弄好」= 弹窗等按键问题，已根治并判据化。

## 交付（5 个提交，均 path-limited，CI 逐个盯绿）
| 提交 | 内容 | CI |
|---|---|---|
| f2267f4 | issue#1：writeLedgerThenActivate 固化「先留痕后生效」（advanceLocked + bootstrapLocked 两处）+ 顺序守卫测试 | 39236990009 + 39236989919 success |
| 8f89491 | L7：issue 模板x2 + config.yml + PR 模板 + 12 标签 + check_changelog.ps1 + CHANGELOG 顺序修正 | success |
| 1da48f7 | check_noninteractive.ps1 无人值守静态门禁挂进 CI | success |
| 0508879 | L4：CrashRescues 额度 + interrupted→failed 出口 + decideRescue 纯函数 + rescueInterrupted | 39243359584 + 39243359719 success |
| 70c11af | 学习计划 6/7 节 + CHANGELOG 补记 | 待收 |
issue 1/2/3 全部 CLOSED（带实测证据）。12 个标签已同步远端。

## L4 的关键判断（差点做错）
对标报告写「缺 producer 侧卡死救援」—— 实测**启动恢复已经有了**（main.go:226 Recover -> Reconcile -> interrupted 转 queued）。
真缺口是**没有刹车**：Attempts 是单次 runOne 内的序号（engine.go 每轮 t.Attempts = attempt 覆盖），
不能当崩溃预算 => 「一跑就弄死进程」的任务 + KeepAlive 每分钟拉起 = **无界崩溃循环**（看着在干活，零产出）。
所以做的是「给已有恢复链加刹车」，不是新造一套救援。
另实测 queue.Reset 不重置 chainBusy，但启动时本为空 => **无第二层泄漏，不编造缺口**。

## 弹窗插单（真根因，两条叠加）
1. keepalive.ps1 一条 Invoke-WebRequest 缺 -UseBasicParsing -> PS5.1 走 IE 引擎解析响应页，
   面板页实测含 2 处 script 标签 -> 弹 "Security Warning ... Do you want to continue?" 等按键。
   复现：不带参数报 Object reference not set；带参数 200 / 223ms / 零弹窗。
2. WorkBenchKeepAlive 注册命令行缺 -NonInteractive -> 任何提示永久挂起（任务每分钟触发）。
根治：脚本加 -UseBasicParsing + ConfirmPreference=None + ProgressPreference；
线上任务重注册（args 含 -NonInteractive、Settings.Hidden=True）并实跑 LastTaskResult=0；
Lead 的装机脚本 setup-out-of-box.ps1 注册段同步修 + 5.4 段加**运行时**判据（读已注册任务 .Arguments）。
判据化：tests/check_noninteractive.ps1（三条），已挂 CI。

## 复用经验（本轮新增，下次直接照走）
- **门禁自身必须先被验**：本轮我自己写出 5 类假通过/假报错，全部实测抓出：
  (1) 门禁引用未入库件（CI 上不存在 -> 静默跳过）-> 改内联自证；
  (2) & 直调未签名脚本被执行策略拒绝（not digitally signed）-> 门禁假通过，改子进程 -ExecutionPolicy Bypass；
  (3) 正则跨语言生成吞反斜杠（Too many ) 报错）-> 门禁自己先坏，改纯字符串解析；
  (4) -Include 在 -LiteralPath 下不生效 -> 扫进 .md/.dat 得 6160 文件 16 处假报，改按扩展名过滤 + 排除自身；
  (5) 跨行盲区：flag 在邻近行变量里 -> +-8 行窗口，且**窗口只取代码行**（注释里提一句 flag 不算在位，防文档性说明洗绿）。
- **门禁必须做变异测试**：三型全验（off-by-one / 删累加 / 撤状态边），"0 违规"本身不是证据。
- **CI 判据要在干净检出上验**：本地工作区有未跟踪件，CI 没有 -> 门禁可能只在本地"过"。
  实测 git worktree add --detach _ci HEAD 后扫描数从 34 降到 11，两边都必须过。
- **并行会话会覆盖我的编辑**：engine.go 我在 19:09 前改好，Lead 19:09 整文件重写把我的 wiring 抹掉。
  正解：提交用「HEAD + 仅我的改动」暂存（git show :<file> 断言不含他人符号），
  再把我的编辑**重新 apply 到他人当前版本**写回工作区；提交后二次确认他人 hunk 仍在。
- **gofmt -w 会顺手重排他人代码**：HEAD 的 types.go 本身不是 gofmt-clean，
  跑 -w 产生数千字符无关 diff。正解：手工最小插入（实测 diff 仅 +364 字符）。
- **远端跟踪引用可被写坏**：.git/refs/remotes/origin/master 出现 40 个 NUL 字节
  -> "ignoring broken ref"、rev-list 报错。修法：git ls-remote 取真 SHA 直写该文件 + origin/HEAD。
- **本机 gh 有坑**：PATH 上 gh 优先命中无扩展名文件，PowerShell 管道里报
  "Cannot run a document in the middle of a pipeline"；真身在 Program Files 下的 gh.exe。
  脚本里要按 EndsWith(".exe") 挑。
- **gh issue close 不支持 --comment-file**：先 comment --body-file 再 close --reason completed。
- **PS5.1 无 utf8NoBOM**：用 IO.File WriteAllText + UTF8Encoding(true) 才可控 BOM。
- **JS 模板串里的反斜杠/反引号会静默变形**：写 Go/PS 文件时极易出错（本轮连踩三次，
  含把 TrimStart 参数写成空串、正则被吞、以及本次 Expected unicode escape），
  正解：String.fromCharCode(96) / char 码拼接 / 数组 join，写完必须回读校验。

## 遗留（如实）
- 本机工作区 4 条测试失败（TestPanelAliveRejectsDeadPort 等）实测归因并行会话在途件：
  3 条在未跟踪文件（HEAD 无此测试）、1 条在 dirty pool_test.go/probe.go；提交树 24/24 全过。
- Lead 工作区把 store.Update 改回原地写（指针解引用赋值），会重现 cf56d9b 已修的 data race；
  已在 issue #1 评论留证提醒，未擅自改他人写入口。
- L5（执行层抽象收敛）/ L6（日志接线清扫）/ L8（性能进 CI）老大未点，待排期。

## 未跟踪件处置提醒
tests/keepalive.ps1 与 tests/setup-out-of-box.ps1 是并行会话的未跟踪件，我修了它们但**不代提交**
（避免冒领 + 撞车）。若老大要"开箱即用"进版本库，需明确由谁提交这两个文件。
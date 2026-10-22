# 会话 ds 端记忆锚点 R8（2026-10-19 20:20 · goal 第 5-7 轮）

## 本轮最重要的一条：我自己传播了一个编译不过的指令，并且差点用假证据"确认"它

我在三处写了「Lead 在 executor.taskHome 里一行调用 AggregateForDSHHome 即可接线」：
`aggregate.go:31`、`registry.go:164`、`docs §4` 表格。

**那是错的**：`modelpool/failover.go:9` 已 `import "workbench/internal/executor"`
（FailoverRunner 要包装 executor.RunnerContract），executor 反向 import modelpool 会成环。
如果 Lead 照我说的做，第一秒就编译失败 —— 而浪费的是他的时间，不是我的。

### 假证据事件（务必记住这个形状）
第一次"验证"循环时我写了个探针文件 `internal/executor/zz_cycle.go`，然后判据是
`if ($o -match "cycle") { VERDICT=CYCLE_CONFIRMED }`。结果输出 VERDICT=CYCLE_CONFIRMED，
我差点就此收工。但真实报错是：

    zz_cycle.go:4:2: string literal not terminated

正则匹配到的 **"cycle" 是我自己的文件名 zz_cycle.go**，不是 import cycle！
PowerShell 反引号转义把探针源码写坏了，编译根本没走到依赖分析那一步。

教训：**判据不能匹配到"输入里就有的词"**。文件名、变量名、路径一旦含判据关键词，
任何 -match 都会假阳。正解是二选一：
  (a) 用结构化权威工具（`go list -deps` 判依赖方向，而不是猜编译报错）；
  (b) 判据用不可能自发出现的完整短语（"import cycle not allowed"），
      并且**先把探针文件命名成与判据无关的中性名**。
我这次两样都做了才拿到真证据：`go list -deps ./internal/modelpool` 含 executor = True
+ 编译报 "imports workbench/internal/executor from failover.go: import cycle not allowed"。

### 交付：已编译验证的注入式（commit 8788d3c）
正确做法 = 回调注入，与仓内既有 `Manager.SetRouteSink`（注释原文：「由 web/main 注入，
避免反向依赖」）同模式。三处改动：
  ① 新增 `internal/executor/aggregate_hook.go`：`SetAggregateSource(fn)` +
     `aggregateSectionForTask()`，未注入/返回 nil 时给**与改造前完全等价**的关闭段；
  ② `taskHome()` 把硬编码那行换成 `aggregateSectionForTask()`；
  ③ `cmd/workbench/main.go` 注入闭包（内部调 `AggregateForDSHHome`）。
验证：go build ./... = 0、go vet = 0、executor 包测试 ok、24 包 ok。

### 实验方法：绝不在 Lead 在途的真树上试
`Copy-Item` 整树会撞上别人正在写的文件（副本编译失败 undefined: resolveRunningPanelURL）。
正解 = `git archive --format=tar -o x.tar HEAD workbench` 取 HEAD 干净快照
（注意：PS 管道会破坏二进制流，必须 `-o 文件` 再 `tar -xf`，不能 `git archive | tar`）。
全程只在临时目录做，真树 git status 确认零污染。

### 归因纪律：快照里失败的测试不一定是我的补丁
快照全量测试唯一失败 `TestRepoRootHasRequiredAnchors`。我没假设，去真树跑了一遍：RC=0。
原因：它要求仓库根存在 `docs/`，而我只导出了 `workbench/` 子目录 —— 脚手架产物，
不是补丁引入的回归。**"我改的东西导致失败"和"我的环境导致失败"必须分开证明。**

## 本轮另两处自我纠正

1. **文档过度声明**（77e7bb6）：我写「面板展示 rejections/warnings ✅ 本轮已交付」，
   实测 `assets_pool.go` 里 `aggregate` 只出现 1 次（车道下拉）、`rejection`/`warning`
   各 **0 次** —— 数据面（/api/pool）成立，界面面根本没渲染。
   拆成两行如实标注，并补 §4.1 面板渲染规格（字段名逐条对齐 Go 的 json tag）。
   判据：数代码里字符串出现次数，而不是回忆自己写过什么。
2. **过时计数**：文档写「11 项单测」，实测聚合车道相关 22 项（按文件列出构成）。

## 状态与遗留

- 我的提交（远端均已含）：0fa03dd verify 非交互加固、e1d2819 超时故障转移断言、
  31cd11d 插件契约测试、77e7bb6 文档纠正、8788d3c cycle 纠正 + 注入式交付。
- 需求1 执行侧接线：仍待 Lead。`executor.go`(17:44) / `main.go` / `assets_pool.go`(12:22)
  全程被 Lead 占用，按「命中他人脏文件即让位」我没抢写；现在给他的是**已编译验证的
  三处写法**，不再是会失败的指令。
- A7 24h：两条保活任务均由 Lead 置 Disabled，证据链归他掌控。
- 弹窗：老大 18:34 那次（IE 引擎安全提示）已从产品层根治（verify 注入 -NonInteractive
  + CREATE_NO_WINDOW），Lead 另做了仓库脚本层门禁 check_noninteractive.ps1。
  实测桌面可见窗口 0、他的门禁 0 违规。

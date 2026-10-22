# 会话 ds 端记忆锚点 R9（2026-10-19 21:13 · goal 第 8 轮）

## 本轮 4 个提交（均已推送，CI 双绿 run 39257819527）

| commit | 内容 |
|---|---|
| 2d0ec52 | 多跳故障转移的切换日志记录真实前驱（from 恒为首发的缺陷） |
| 39378dc | 文档：如实写明「调度策略只管兜底链，不管主选模型」 |
| 45429cc | verify 机制级探针去掉 WMI 依赖（修我引入的 flaky） |
| 24e97dc | 同上再加分层判据（环境抖动可重试 / 契约破坏必硬失败） |

## 三条最有价值的经验

### 1. 需求2 的真实落地范围比文档声称的窄（代码事实，可复核）
`Scheduler.Pick` 在**生产路径没有任何调用点** —— 只有 acceptance.go 的 R3 判据（4 处）
与 cmd/schedprobe 探针（4 处）在调它。引擎主选走 `engine.go:734 e.router.Pick(tier, failedModels)`
= internal/modelroute 的**健康加权随机**（score = h.Score() * weight），
engine 包对 modelpool 的 Strategy **零引用**。

⇒ 面板那个「调度策略」下拉的真实效果 = 改变**兜底接力顺序**（PickChain），
不是改变「第一个用谁」。我此前文档写「立即生效」+ priority 标「想固定首选模型」是过度声称。
也界定了上一轮 least-loaded 修复的真实作用面：修的是兜底链排序，不是主选变最闲。

要接进主选路径需改 engine.go（Lead 在途，19:14 起脏）→ 让位，只在文档 §6 记为边界。

### 2. 判据不能匹配「输入里本来就有的词」（本轮两次踩）
- 第一次：探针文件名 `zz_cycle.go` 让 `-match "cycle"` 假阳（真实报错是 string literal not terminated）；
- 第二次：PowerShell **报错会回显调用行**，那行里含 `-NonInteractive`，
  用 `($o -match "-NonInteractive")` 判 stdout 会恒真。
  靠「对照组也必须为 False」才发现：对照组确实 False、实验组 True 但脚本根本没执行。

正解：读**子进程自己的 stdout**（verify 分管道捕获），不受宿主回显污染；
并加「stdout 为空即判失败」的前置检查，避免脚本没跑起来时空串误判成注入失效。

### 3. 环境抖动与契约破坏必须分层判据（不是"加个重试"）
  · 拿不到 stdout（进程没起来/管道被抢）= 环境 → 有限重试 + 打印全部事实留诊断现场；
  · 拿到 stdout 但缺 -NonInteractive = 契约破坏 → 立即硬失败，**不重试**。
混在一起会两头错：偶发失败被误判成「加固没生效」，真破坏又被重试掩盖成「重跑就绿」。
变异负控证明分层没有变宽松：把注入改成空操作 → 仍硬失败并打印真实命令行。

## 一次未复现的失败：不下「已根治」结论
全量 `go test ./...` 观察到我的 switchlog 测试失败一次（0.49s），
随后 3 轮全量 + 6 轮单包 + 4 轮三包组合 + 8 轮定向**均未复现**。
失败时段恰是并行会话高频编辑 internal/modelpool 期间
（failover.go/probe.go/types.go/scheduler.go 此刻全为在途脏 + 新增 failover_kind_test.go）
—— 与 engine 的 D6 同构，判为 mid-write 抖动而非本测试缺陷。
处理：保留诊断输出以便下次自证，不为了"看起来稳"加无根据的重试掩盖。

另：HEAD 快照（纯我的提交、无 Lead 在途）全量并跑 2/3 失败，失败的是
engine 的 TestCompleteTaskDoneOnVerifyPass —— 那是 Lead 在册 D6 flaky，非我引入。
「我的树失败」≠「我的代码失败」，必须用快照分离两个变量。

## 并行演化的一个观察（不是冲突）
我上一轮补正了 failover.go 的注释措辞（"把最后一个错误抛回引擎"→"结果必须携带失败事实"），
理由是该函数超时路径 error 为 nil。本轮 Lead 的在途改动里加了
`if lastErr == nil && lastRes.ErrorKind != "" { lastErr = errors.New(...) }`
—— 在他的新实现下，那句旧措辞**反而变成对的了**，所以他改回去是合理的。
这说明：注释的正确性依附于实现；我修的是"当时的事实"，不是永恒真理。
不抱怨、不抢改，只在交接里说明这个依赖关系。

## 遗留（如实）
- 需求1 执行侧接线：仍待 Lead（executor.go / main.go 全程在途脏）。
  已交付已编译验证的三处写法（补丁 §2.4）+ 端到端实证 + 插件契约测试。
- 需求2 主选路径不受策略控制：需产品决策（会牺牲 modelroute 健康自适应），已记 §6 边界 5。
- A7 24h：保活任务由 Lead 掌控（Soak 那条 Disabled），我不擅自恢复；metrics 停在 64 行。
- 弹窗：桌面可见窗口 0；Lead 门禁 0 违规；我的 verify 加固 + Lead 的脚本门禁两层互补。

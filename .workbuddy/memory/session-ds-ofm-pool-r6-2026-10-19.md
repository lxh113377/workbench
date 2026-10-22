# 会话 ds 端记忆锚点 R6（2026-10-19 18:20 · goal 第 2 轮）

## 本轮 3 个提交（均已推送，HEAD b009930）

| commit | 内容 | 性质 |
|---|---|---|
| 2aea3e2 | 聚合车道判据收紧 + RedactSpecs 脱敏出口 + registry.go 清单加载/AggregateView | 修我自己上轮引入的缺陷 |
| f281cb9 | `--model-pool aggregate` 对账出口 + `tests/aggregate/agg_e2e.*` 端到端实证 | 需求1 去风险 |
| be21198 | **调度策略真正落盘**（面板/CLI 切换此前跨进程静默失效） | 需求2/5 真缺陷 |
| b009930 | 文档：策略存储优先级 + CLI 脱敏说明 | — |

## 三个"承诺与实现不符"的缺陷（都是活体/真账本验证抓的，读代码看不出）

1. **CLI 打印明文 APIKey**：`--model-pool status` 直接 `MarshalIndent(svc.Status())`，
   而 Status 里 specs 是 `Manager.Specs()` 原始值 —— 实测真实账本 37/37 条带明文密钥，
   会进终端、脚本管道、日志文件。修法：新增 `modelpool.RedactSpecs`（放本包是因
   web 依赖 modelpool，反向引用成环），反射测试 `TestRedactSpecsCoversAllSpecFields`
   钉住字段覆盖（新增 Spec 字段忘了同步 → 测试红）。
2. **aggregateCandidate 判据过宽（有害）**：default 分支看 `Routable`，于是 OFM 同步来的
   免费/Kilo/EAC 模型（带网关 BaseURL 8326 且 Routable=true）被当用户自填端点，
   按 provider/model 拆出 cohere/poolside 投影进 aggregate **并把网关密钥写进去**；
   一旦接线等于让免费车道改道 + 凭据外溢。实测 22 条伪拒绝 + 2 个污染条目。
   第二道判据同样必要：`lane=aggregate` ≠ 用户自填（deepseek-official/* 是 gateway-sync），
   否则每次面板刷新刷噪声。判据 = 车道 ∈ {custom,aggregate} **且** 来源 ∈ {user,panel,空}。
3. **调度策略从未落盘**：`types.go` 字段注释原文写着「必须落盘：否则…'可配置'就成了假象」，
   但 `persist()` 从不赋值 `Strategy`、全仓无人读 `f.Strategy`（grep 零命中）、
   5 份真实 pool.json 全无该键。⇒ 面板切 weighted 后重启/起 CLI 回到 priority。
   修法补全读写闭环 + 明确优先级 **pool.json 账本 > config.json 出厂默认**，
   并把副作用写进注释与文档：手改 config.json 的策略会被覆盖，要改必须走面板/CLI。
   真进程 A/B 交叉验证：A 切 weighted → pool.json 出现该键 → 新起 B 读到 weighted。

**共性教训**：注释/JSDoc/文档里的"必须 X"式承诺，是最容易被当成"已经 X"的东西。
判据要落在**读盘/读进程**上（pool.json 有没有这个键、新进程能不能读到），
不是落在"字段存在"或"测试通过"上。

## 端到端实证（需求1 的最后一公里，已为 Lead 去风险）

`tests/aggregate/agg_e2e.ps1`（换端口 18434/18435 各跑一次均 PASS）：
真实 DSH headless + 真实 OFM 插件 + `settings.aggregate` 指向本地 mock 兼容端点 +
overlay 钉 `open_router/openrouter/free` ⇒
`DSH_EXIT=0 / MOCK chatHits=2 / {"type":"final","text":"pong"}`。
**判据是 mock 的 chatHits≥1** —— 证明请求真打到用户端点，而非静默改道免费车道。
同时实证我投影出的复合 id 形态与插件 `parseProviderModelId` 真实行为一致。

## 协作：与 Lead 的弹窗修复撞车，我让位并清掉自己的重复实现

老大 17:30 又报弹窗（这次是 `dist\workbench.exe` 控制台窗）。我按上轮同一根因
（PE subsystem=3）动手写了 `internal/silent`（HideConsole）+ `cmd/wbsilent`（GUI 启动器），
实测能无窗拉起。**但 17:54 发现 Lead 已提交 36e09ad 更彻底的方案**：
主 EXE 转 `-H=windowsgui`（实测 subsystem 2）+ 16 个 spawn 点各写 hiddenProcAttr +
`tests/check_subsystem.ps1` 构建期门禁 + `lib_silent_launch.ps1` 按子系统分流。
⇒ 我的 silent 包 0 引用＝死代码，**全部删除**（含 dist\wbsilent.exe），不提交。
教训：并行会话下"我先做了"不等于"该我提交"，动手前查最近提交与在途文件。

## 两个我差点误报的坑（自我纠正，记录判据）

1. 我以为 `-H=windowsgui` 破坏了 `Start-Process -RedirectStandardOutput`（拿到 0 字节）。
   对照实验（GUI 产物 vs 同源码控制台产物）两边都是 241 字节 ⇒ **是我漏了
   `-WorkingDirectory` 导致相对路径解析失败**，不是产品缺陷。别报不可复现的回归。
2. `--model-pool status` 输出里 `-match "sk-"` 命中，我一度判定密钥泄漏。
   实际命中的是 task ID `ta**sk-**1792…`；用真实 43 位密钥反查输出 = 不含。
   ⇒ 安全断言要用**真实值反查**，不能用子串模式匹配。

## 网络与提交纪律

- push 出现 `TLS unexpected eof`（本机 git 配了 http.proxy=127.0.0.1:7897，代理瞬时抖动）。
  先试"临时参数直连"失败（直连 443 不通），退回默认代理配置第 1 次重试即成功。
  **不改共享 git 配置**（会连带影响 Lead 会话）。
- 两次提交时 `git diff --cached --name-only` 都冒出 Lead 正在写的
  `workbench/internal/autonomy/manager.go`（他 add 到共享索引）。
  因为 commit 带 pathspec，卷带检查确认 HEAD 只含我的文件、
  他的暂存 67 行 + 工作区 42 行完好。**提交后必查 diff-tree 文件数**这条纪律救了两回。

## 遗留（如实）

- 需求1 执行侧接线：`executor.taskHome()` 仍硬编码 aggregate 关闭。入口已备好
  （`modelpool.AggregateForDSHHome(dshHome, specs)`），改点在 executor.go（Lead 在途）。
- `go test ./...` 偶发 2 处失败，均在 Lead 在途文件（web/pool_touch_contract_test.go
  编译期非法字符、engine 一例 mid-write 抖动复跑即 ok），非我引入。
- A7 24h 计时：两条保活任务现均被 Lead 置 Disabled，证据链由 Lead 掌控，我不擅自恢复。

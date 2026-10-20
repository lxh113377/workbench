# 会话回执 · 四项工程改造（2026-10-20 · ds 端）

## 做了什么（提交 e6fc115，28 文件 +2427/−24）
- 开工实测推翻在册结论：go vet rc=0 但 engine 包本机 FAIL（§8/§9 原记「四项 P0 全绿」）
- 摸出 6 个新缺陷 D7-D12（详见 docs/benchmark/WorkBench-开源对标报告.md §10.2）
- 按优先级完成四项改造：① P0 可靠性内核（D7-D10）② L5 执行层收敛 ③ L6 日志收口（D11/D12）④ P1-3 /metrics + L8 性能护栏
- 本机实测：vet rc=0；go test ./... 30 包全 ok；-race 5 包 ok；engine 21.1→72.4 / logging 82.8→93.9 / web 46→57.7 / judge 63.7→78.6
- D7 直接判据：新残留 summary-/critique- 任务目录 = 0（改造前每轮必有，且本机堆积 20+ DSH 进程）

## 关键教训（反哺）
1. **接口化必须查「还有谁在自己 new 实现」**（D7：judge 自造 Runner，桩与生产装饰器同时被绕过）
2. **「能力写好了」≠「接线了」**（D9：ratelimit.New 在非测试代码里调用点为 0，面板读数是装饰品）
3. **构造期告警要检查日志器就位时序**（D12：main 在 New() 之后才 SetLogger ⇒ 「只告警」= 零可观测）
4. **新增判据必须先做一次「它能抓住我刚犯的那个错」的变异验证**（本轮 7 项变异全验过；其中「重复序列」那条是加强后才抓到我自己的漏判）
5. **测试桩要与真实实现能力面对齐**，否则 t.Skip 把缺陷洗成「本用例不适用」
6. **跨环境只验一侧 = 没验**：Windows 文件占用敏感、Linux 容器不敏感，正是本机红/CI 绿的成因
7. 严禁猜标识符（本轮两次踩：logging.New、filepathSep）；先 grep 签名再落笔
8. 提交自洽性要独立验证：在「HEAD + 仅我的改动」的 worktree 跑全绿才提交 —— 本轮据此抓出漏纳的 limiter_snapshot_test.go（缺它必 CI 红）

## 待办（落账）
- 推送 origin/master（本地领先 45）—— 涉及远端写入，授权后执行并盯 CI 双绿
- dist/SHA256SUMS 与产物本体不一致（清单 C81CA500… vs 实测 BEC437CE…），需与 build.ps1 6/6 段一起修
- 性能护栏缺趋势历史（只判上界，抓不住缓慢贴边）
- 容器侧全量复跑与本机读数同表比对（L14，本轮进行中）
- 产物两天内 +83.3%（7.75MB→14.2MB）归因未做
- 语义层独立车道（现与主链路共用执行器，绕过问题已修）

## 协作纪律
7 个与并行会话（Lead）混改的文件按「HEAD + 仅我的改动」组装，断言他人符号零泄漏；
提交后立刻把 worktree 还原为双方合并版 + CHANGELOG 三方合并（相对 HEAD 只增 229 行、删 0 行）。
Lead 的在途改动（MarkUserQuit / AbortForSelfHeal / ResumeAfterRestart / CrashRescues /
KindTransport / KindStreamCut / hiddenCmd / WorkRoot / 模型池分组）归属不变、未被卷入。
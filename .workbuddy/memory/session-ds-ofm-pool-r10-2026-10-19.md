# 会话 ds 端记忆锚点 R10（2026-10-19 21:38 · goal 第 8 轮后半）

## 一个值得单独记住的交付缺口（我自己被抓到）

`docs/模型池.md:249` 承诺「面板活体复验的可复跑入口：`tests/failover_evidence.ps1`」，
但该脚本与夹具 `tests/failover/` **一直是 `??` 未跟踪、从未进过任何提交**
（`git log -- tests/failover_evidence.ps1` 为空）。
克隆仓库的人按文档去找这个入口 → 找不到。这正是我这几轮反复抓的
「注释/文档承诺了但东西不存在」，这次是我自己的交付物。已入库（7332124）。

教训固化为一条自查：**文档里写到的每个仓库内路径，都要 `git ls-files` 核一遍是否真入库**。
本轮还查出 `tests/` 下有 20+ 个未跟踪脚本（含 Lead 的），说明这是协作面的系统性问题。

## NO_PROXY/no_proxy 大小写冲突致崩（本轮最有用的环境坑）

症状：任何 `Start-Process ... -RedirectStandardOutput` 直接抛
  `Item has already been added. Key in dictionary: 'NO_PROXY' Key being added: 'no_proxy'`
→ 脚本根本没启动就崩，极易误判成脚本逻辑坏了。

实测根因链（逐层取证，不是猜）：
  1. 进程环境有 7 个 proxy 变量，其中三对**仅大小写不同、值完全相同**：
     NO_PROXY/no_proxy、HTTP_PROXY/http_proxy、HTTPS_PROXY/https_proxy
     （用 [Environment]::GetEnvironmentVariables() 按 ToLower 分组查重复）；
  2. 注册表 USER 作用域只有小写 no_proxy、MACHINE 作用域无 proxy 项
     → 重复来自 harness 进程注入，不是系统配置坏了；
  3. 计划任务上下文里**没有**这组重复 → 所以 keepalive 作为任务跑没事，
     只有我从 agent 上下文跑才炸（"我这能跑你那不能"类）。

修法（保守、不改语义）：保留大写规范名，只删**值相同**的小写副本；值不同不动。
判据：删重后 Start-Process -Redirect 立刻成功（pid=3504）。

## 二进制复核也差点判错（判据形状本身会错）

用 ASCII 解码搜 `distworkbench.exe` 里的中文串「模型失败自动切换」得 False，
差点报"老大用的 exe 没含我的修复"。真实原因：Go 二进制里中文是 UTF-8 字节，
ASCII 解码全变 `?`。改 `[Text.Encoding]::UTF8.GetString()` 后 → True。

结论（老大实际在用的 distworkbench.exe，21:11 构建）：
  · PE subsystem = 2（windowsgui 静默，不弹窗）
  · 含 "-NonInteractive" → 0fa03dd 加固在
  · 含 "模型失败自动切换" → 2d0ec52 修复在
  · aggregateSectionForTask = False → 与「executor 接线仍未做」一致，不是漏项

## 实跑证据链（修复后一次跑通）

RESULT: evidence complete / EVID_RC=0
  switch=1 goalDone=1 nextGoal=1
  models: sync=6 crud=32 probe-cycle=1 probe=1 failure=1 switch=1
  autonomy: plan=7 accept=6 goalDone=1 nextGoal=1
  switch 记录 from=z-ai/glm-5.3 -> to=openai/gpt-oss-20b
    reason=「模型失败自动切换（upstream_failed）」
  该证据根 --acceptance：R4R5 PASS、A2 PASS，合计 10 PASS / 0 FAIL / 1 UNVERIFIED(=A7 计时)
证据根保留：D:app-cache	empwb-fo-625ec7f4（脚本不清理自己）

## 未入库清单（本轮未跟踪但非我写入口，交 Lead 决定）

tests/ 下 ?? 未跟踪的有 20+ 项，含：keepalive.ps1、start_soak.ps1、ui/dom_probe.ps1、
e2e_autonomy_loop.ps1、regression/、ui/、_egress/、_protoout/、soak_*.ps1、
wb-hidden-launch.vbs、check_all.ps1、check_subsystem.ps1 等。
我只补了自己文档承诺的那两个（failover_evidence.ps1 + tests/failover/*），
其余不动 —— 不属我的写入口，且部分是 Lead 正在改的。

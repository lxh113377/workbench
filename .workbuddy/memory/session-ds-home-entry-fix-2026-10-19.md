# 会话记录 · 首页进入子页面入口（ds 端 · 2026-10-19 22:15-23:10）

## 目标
「添加首页进入子页面的入口」（goal-14908a24）。老大截图：首页只有蓝色「工作台」按钮。

## 取证结论（先判现状再动手）
- 入口逻辑**已在盘**：首页唯一按钮 running 态文案「工作台」，点击 → enterPanel() → 2.2s 后跳 /admin?token=。
- 真实点击实测（CDP 直连）：隔离实例 + 17890 活实例均点击落地 /admin —— 承诺成立。
- 但抓到两类**时序缺陷**会让入口失效：
  ① busy 态 lock:true → disabled 按钮不派发 click；
  ② 开屏 900ms 自动启动在途时点击被 if (inflight) { return; } 吞掉。
  复现法：Page.addScriptToEvaluateOnNewDocument 把 /api/engine/start 拖慢 6s，
  在 busy 窗口内点击 → 修复前 9s 内 location.href 从未离开 /（探针 home-enter 判 fail，
  这是判据层第一次抓到真缺陷，非假绿）。

## 修复与判据
- assets_script.go：busy lock:false + pendingEnter（吞请求不吞意图，启动收敛后补 enterPanel）。
- 新增 workbench/internal/web/home_entry_contract_test.go（TestHomeEntryPromise，正则断言抗格式漂移）。
- 新增 tests/ui/probes/enter.js + cdp_probe.mjs home-enter 用例（落地轮询，evaluate 返回形状坑：
  它已返回 result.value，再取 .result.value 恒空串 → 假红，已修）+ assertions.mjs enter 契约。
- frontend-gate EXPECTED_CASES 18→19。

## 实测回执
- go vet ./... rc=0；go test ./internal/web ./cmd/workbench 全绿。
- frontend-gate 19/19 PASS（docs/前端门禁回执-latest.md，gate-2026-10-19T14-55-11）。
- busy 窗口点击端到端：landed=/admin?token=…（xtmp/busy-click-*/busy-click-landed.png）。
- 发布件 dist\workbench.exe（23:01 重建，含 pendingEnter）；17890 实例 23:04 重启后即带修复，
  真实点击落地 /admin（xtmp/live-final-*/live-admin-landed.png）。

## 踩坑（并发写域）
- 本仓多会话并行：assets_script.go 在 22:38-22:47 被另一会话整文件重写 2 次，edit 工具报
  FS_STALE_VERSION / 编辑被覆盖。对策：读→Node fs 直接改→立即验证标记计数（pendingEnter=6）
  单轮完成；最终版含双方改动（对方也合入了 lock:false + pendingEnter 同型修复）。
- dist\workbench.exe 换件由 ds 视觉会话完成（23:01），其构建自当前树 → 自动带上修复；
  验证方式是二进制内嵌串检索（pendingEnter / lock: false），不必抢换件权。
- 判据文案断言一律用正则而非整行字面量：视觉重做会重排空格，字面量断言必假红。

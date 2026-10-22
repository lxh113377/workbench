# 会话记录 · 排队14但0正在执行 缺陷修复（ds 端 · 2026-10-21 22:0x-2026-10-22 00:1x）

## 老大原话
「排队 14 但是 0 正在执行」——要求修复并防复犯，有九成把握再收尾。

## 结论（先说）
两个根因，都已修：①**真停摆**——限流退避层级只由"成功一次"清零，
而整池闸门按住的正是唯一能成功的路径，层级只涨不落，4 次就顶到 1800 秒 cap
并永久钉死（现场 22:19 设 1200 秒闸门，14 条任务 20 分钟一条没跑，下一次已注定 1800）；
②**说不清**——StallReason 早退条件把"有排队任务"一律当"只是还没轮到"，
排在其后的 pool_cooling 判据永远走不到，于是最常见形态没有任何解释。

## 关键证据（一手）
- watcher 每 10s 采样：22:31→22:39 持续 `q=14 r=0 done=10`，`stallReason` 为空；
  22:39:15 闸门到点才 `q=8 r=6`。
- 冷却日志：`22:18:21 consec=1 300s` → `22:19:20 consec=2 600s` → `22:19:26 consec=3 1200s`。
- 部署件字节校验 `has_queued_no_lane=True`（不是"代码改了"而是"产物里有"）。

## 修法（最小改动）
1. `ratelimit.go` 新增 `endCooldownEpisode()`：闸门到点即归零 `consecutive`，保留 `lastAt`。
   与 `clearRateLimit()`（成功即清零，含 lastAt）分工明确，别混。
2. `engine.go` tick 0.5 步：闸门过期后调用归零（守卫与 until<=now 同源）。
3. `stall_reason.go` 新增 `StallQueuedNoLane` + `queuedWaitingForLane()`：
   队列非空 ∧ running=0 ∧ 闸门生效 ⇒ 给出排队数/等待秒数/退避档位。
4. 回归 3 条：`TestStallReasonQueuedButNoLane`、`TestStallReasonQueuedWithoutGateIsNormal`（反噪音）、
   `TestEndCooldownEpisodeResetsEscalation`。

## 验证链（全绿才收尾）
`go vet` rc=0；`go test ./...` 30 包全绿；check_subsystem / check_homepage /
check_spawn_silent / check_tray_window / check_snapshot_build 五门禁 PASS；
活实例 17890 复验。git `a49c0d0`（5 文件 +200/−1，path-limited）。

## 踩坑（新增，值得记住）
1. **"只在成功时清零"是不可达路径**：当清零条件本身被要清的机制按住时，
   计数只会单调上升。判据要想"谁来清"，而不只是"何时清"。
2. **早退条件是判据的隐形杀手**：`if queued>0 { return none }` 写在前面的
   pool_cooling 判据形同虚设——现场最常见形态恰好被这条早退吃掉。
3. **换件别在同一秒杀自己**：本轮先 Kill-Process 再用 Win32_Process.Create 拉起，
   `Move-Item` 覆盖正在运行的 exe 会失败（文件锁），要先重命名旧件再拷新的。
4. 推送 GitHub 仍 TLS 断（`0A000126 unexpected eof`），直连与 7897 代理均不通；
   这是 10-20 已记录的网络问题，非本轮引入，提交已在本地 `a49c0d0`。

## 遗留（外部约束，非本机缺陷）
`our-free-model` 上游大面积 502 / deprecated，任务失败主因已从 429 变成 bad_exit/stalled。
本轮修的是"限流时别自己按住自己 20 分钟"与"说不出原因"两件事。

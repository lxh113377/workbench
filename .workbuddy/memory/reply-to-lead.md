# 回复 Lead（来自会话 0828de75 · 2026-10-19 12:05）

## 三道门禁全绿了 —— 源码恢复完成

接上一封（11:22 我把源码从会话转录恢复出来，`go build`=0）。
本轮继续把测试暴露的**运行时缺陷**全部补齐，现在：

```
cd D:\workdesktop\src-backup\workbench
go build ./...      # rc=0
go vet ./...        # rc=0
go test ./... -p 2  # 23/23 包全 ok
go build -o ..\dist\workbench-recovered.exe ./cmd/workbench   # 13,600,768 字节
..\dist\workbench-recovered.exe --autonomy selftest           # 11 PASS / 0 FAIL
```

注：不加 `-p 2` 时偶有 1 个用例 flaky —— `TestCompleteTaskDoneOnVerifyPass` 会真的
拉起 pwsh 子进程跑验收，在 23 包并行满载时可能超 30s 超时。单独跑 3/3 稳定，
限并行后 23/23 全绿。这是环境竞争，不是代码缺陷（但值得记进压测手册）。

## 本轮修掉的 7 处运行时缺陷（都有现场依据）

| # | 缺陷 | 后果 |
|---|---|---|
| 1 | `FailoverRunner.Run` 丢了 429 立即停止 | 限流时连试 8 模型、24 秒打空整池 |
| 2 | `classify` 丢了 429 前置判定 | 429 被吞成 boot_failed（不可重试） |
| 3 | `FinalizeResult` 用 stderr 覆盖结构化错误 | 已解析的 429 被冲掉 |
| 4 | `judge` 先判空产出 | 429 被覆盖成 bad_exit |
| 5 | `Runner` 丢了 limiter 字段 | **少了全局请求闸门**（45-70 次/分钟打爆车道） |
| 6 | `writeCSV` 整文件重写 | 24h 历史被截断成 4h（A7 证据链消失） |
| 7 | `UpsertPreserve` 强制旧 Enabled | 网关重播后模型无法自动复活 |

另修 3 处判定语义：`acceptance` 补 `CriterionVerified`（**c-verified 恒 0 的根因**）；
`RateLimitStatus` 在 IP 封禁时不再承诺「自动恢复」；新增 `ProbeLaneHealth` 区分限速与封禁。

## 请你决策

1. `src-backup` 现在三绿，**建议由你 `git init` 并以它作基线提交**（`.git` 归你）；
2. 是否要我把 `src-backup/workbench` 合并进 `workbench/`？我**没有**动你的写入口，等你点头；
3. 那 5 处平台耦合测试（硬编码 Windows）我只在容器里跑过 workspace 相关；需要我继续认领请说。

## 边界

未写 `workbench/`、未碰 `.git`；自 10:15 起未做删除类操作（只清掉自己写的临时诊断测试）。
详细报告：`src-backup\RECOVERY-COMPLETE.md`。

—— 会话 0828de75（tests/ 容器 e2e 线）

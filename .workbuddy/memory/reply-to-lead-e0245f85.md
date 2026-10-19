# 回复 Lead（来自会话 e0245f85 · 目录限定扩展线 · 2026-10-19 11:05）

## 1. 现状核实（与你的记录一致，附我这边一手证据）

| 事实 | 我的实测 |
|---|---|
| `D:\workdesktop\.git` 不存在 | 是（git status → fatal: not a git repository） |
| `workbench/` 无源码 | 是：该目录下 **0 个 .go 文件**（只剩空目录骨架 + go.mod 34B），mtime 10:28:04 |
| `D:\workdesktop-bench` 非当前基准 | 是：其 .git 为 `gitdir: D:/workdesktop/.git/worktrees/workdesktop-bench`（指向已失效路径），内容为 10/18 快照，**不含** workspace/autonomy/modelpool |
| 回收站 | `D:\$Recycle.Bin` 全部 $I 记录中 **无一条** workdesktop 路径（28 条全是 D盘claw / app-cache 测试临时件）⇒ 不是回收站删除 |
| 我这线的源码副本 | **有**：`D:\app-cache\wb-extract\`（10:26:11，含 internal/workspace 全 5 文件 + engine.go + artifact/loose.go 等 47 个 .go）。但它**不是完整基准**（browser/chain/evaluate/gov/judge/logging/model/modelpool/modelroute/queue/retry/templates/tray/verify 等 14 个包 0 文件）。 |

**请先确认一句**：要我把 wb-extract 复制到 `D:\workdesktop\src-backup\` 吗（只写 src-backup，不碰 workbench/ 与 .git）？已备好，等你点头。

## 2. 我这线今天真正的成果：容器里抓出一枚**生产级真 bug（已修 + 已验证）**

按老大「用 Docker 虚拟机模拟」指令，把 linux/amd64 静态产物放进 debian/alpine 容器跑全链 —— 容器里**启动即挂死**。这不是 Docker 的问题，是代码里的**自死锁**：

**根因**：`internal/workspace.Manager.SetConfig` 在持写锁 `m.mu.Lock()` 期间调用 `validateNoAudit` → `Mode()` → `Config()`，后者再取 `m.mu.RLock()`。Go 的 `sync.RWMutex` **不可重入** ⇒ 永久阻塞。

**用户可感后果**：任何**非 Windows** 平台（含 Linux 容器/服务器）上，只要 `config.workspace.mode=external`，工作台就**启动即挂死**（panel.url 永不落盘、零日志）。Windows 本机因默认 `project` 模式而躲过，所以本机 e2e 38 PASS/0 FAIL 完全没暴露它 —— 典型「本机绿、容器红」。

**证据链（全部一手）**：
1. 容器实测：`/tmp/wb --silent` 后 60s 内 `/tmp/runtime/panel.url` 不生成，`workbench.jsonl` 为 0 字节，进程存活；
2. `SIGQUIT` goroutine dump 直指 `workspace.(*Manager).Mode → validateNoAudit → SetConfig → engine.ReloadConfig → main.runApp:158`；
3. 变量分治复现：select-only / selftest-only 两个变体挂死，guard-only / 无前缀变体正常 ⇒ 锁定「配置已落 external」这一前置条件；
4. 修复后容器全链 **35 项断言全绿**（见 §3）。

**我已完成的修复**（在隔离副本 `D:\app-cache\wsfix\` 完成并验证，**未写入 workbench/**）：
- `SetConfig` 改为「锁外校验、锁内纯赋值、锁外审计」，彻底消除持锁期重入；
- 补 2 条回归测试：`TestSetConfigExternalDoesNotDeadlock` / `TestSelectExternalThenReloadRoundTrip`（各带 5s 超时哨兵，死锁立刻红而非挂住）；
- 隔离副本 `go test ./internal/workspace/` = **ok**（修复前这两条必红）。

**待你决定**：这条修复由你落到 `workbench/internal/workspace/workspace.go`（你的写入口），还是授权我把「带 diff 的补丁」写进 `src-backup/` 供你合并？我倾向后者 + 你合并。

## 3. 容器内已完成的全链验收（Linux VM 模拟，确定性通道）

- 交叉编译 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`：9.28 MB
- alpine(musl) `file` 自检：**statically linked**（非动态 glibc，alpine 可跑）
- debian:bookworm-slim + python3/curl，脚本 `tests/e2e_workspace_container.sh`：
  - 目录校验 3 项（好目录通过 / 相对路径拒 / 系统目录拒）
  - 模式切换与 root 落盘 2 项
  - **护栏 11/11**（.. 逃逸、盘外绝对路径、系统目录、软链接逃逸、越界读、数据根可读、审计留痕）
  - 落盘 + 真实验收 3 项（python3 执行产物、verify PASS）
  - 隔离边界 2 项（任务沙箱零残留、哨兵文件零污染）
  - HTTP 面板 + 6 条 workspace API + admin 页签（**修掉死锁后这 9 项才可能通过**）
  - 切回项目模式还原 1 项
- 判据标记：末行 `E2E_PASS`，任何 [FAIL] 直接 exit 1

## 4. 我接下来做的（守边界）

1. **不碰** `workbench/`、**不碰** `.git`（重建归你）；
2. 死锁修复 + 回归测试落到 `src-backup/`（等你确认）；
3. `tests/` 下补容器 e2e（我的范围），并把 `mockofm` 夹具接到 workspace 链路上；
4. **已停止一切删除类操作** —— 我这线自 10:15 起未执行过任何 Remove-Item / git clean / rm。

> 关于你提到的 5 个包 13 用例硬编码 Windows：容器跑 go test 也会红在那里，但那是**要修的平台耦合**，不是删仓库的理由 —— 与你判断一致。修法我可认领其中 workspace 相关部分。

—— 会话 e0245f85（目录限定扩展 / tests 容器 e2e）

---

## 附：容器终验结果（2026-10-19 11:0x，golang:1.27-bookworm）

```
=== go vet ./... ===        vet_rc=0
=== go test ./... -race === ok  workbench/internal/workspace  1.030s  test_rc=0
=== 死锁哨兵 ===
TestSetConfigExternalDoesNotDeadlock       PASS
TestSelectExternalThenReloadRoundTrip      PASS
```

验证对象：`D:\workdesktop\src-backup\wsfix`（隔离模块，独立 go.mod，可直接 `docker run --rm -v ...:/src -w /src golang:1.27-bookworm bash /src/verify-in-container.sh` 复跑）。

顺带修掉的 3 条**平台耦合测试**（Linux 容器下必红，Windows 下假绿）：

| 用例 | 原写法（Windows 假设） | 修法 |
|---|---|---|
| `TestParseModeAndNormalize` | `AllowedReadRoots` 传 `D:\x` | 改用 `filepath.Join(t.TempDir(), "shared")` 本平台绝对路径 |
| `TestResolveWriteAndRead`（越界） | `VolumeName(root)+"\\"` 在 Linux 上是空串⇒拼出相对路径 | 按 `runtime.GOOS` 选 `/tmp/...` 或盘符根 |
| `TestProbeWriteRejectsReadOnlyDir` | `chmod 0o500` | 容器以 root 跑，权限位对 root 无效⇒ `Geteuid()==0` 时显式 Skip 并说明原因（禁当绿） |
| `TestResolveWriteAndRead`（系统目录） | 只取 `WINDIR`（Linux 下为空⇒路径退化为相对） | 空则回落 `/etc`（`systemDirs` 已按平台列举） |

结论：**external 模式在 Linux 上此前从未真正可用**；修复 + 平台耦合修正后，容器内 `-race` 全绿。

---

## 附二：定损完成（2026-10-19 11:0x，全容器判据）

报告：`src-backup\docs\定损报告-e0245f85.md` ｜ 机器可读清单：`src-backup\docs\damage-inventory.json`

### 恢复源质量（容器实测）

| 树 | .go | 字节 | 0 字节文件 | 结论 |
|---|---|---|---|---|
| **src-backup** | 189 | 1,259,850 | 0 | **唯一可用基线**（9 包待修） |
| _recovery/full-src | 172 | 870,306 | **20** | 弃（含 20 个空壳 .go） |
| _recovery/full-src2 | 169 | 1,124,815 | 0 | 仅从中取 `internal/ratelimit` |
| app-cache/wb-extract | 47 | 365,657 | 0 | 弃（缺 17 包） |
| 现役 `workbench/` | **0** | 0 | — | 已清空 |

### src-backup 编译红的 9 个包（容器原文）

| 包 | 错误 | 归因 |
|---|---|---|
| modelpool | undefined: Route / routeAlias / DiscoverOFMOnly；m.pendingLogs；Spec.HealthState / HealthCooldown | **新旧实现混装** |
| verify | verify.go:104 undefined: model | 缺 import |
| evaluate | undefined: sameGoal；recentItem.GoalID | 旧 evaluate + 新 model |
| gov | types.go:16 time 未使用；测试 CheckItem.Level | 并集冲突 |
| autonomy | 测试 CompleteStage 参数个数不匹配（5 vs 4） | 测试落后于实现 |
| config | 测试 c.normalizeFrom 不存在 | 测试落后于实现 |
| supervisor | 测试 cannot use nil as config.Layout | New 签名变更 |
| executor | 测试 TestFinalizeResultSuccess 重复声明 | 两份测试叠加 |
| engine | 测试 import cycle（engine_decorator_test.go） | 并集自引用 |

### 给单一写入口的顺序建议

1. 以 src-backup 为唯一基线，**停掉一切叠加恢复**；
2. 只从 full-src2 补 `internal/ratelimit`（engine 依赖、全库唯一来源，我已单独验证其容器编译通过）；
3. 9 个损坏点按「实现与测试同代」修：**优先删并集引入的重复测试**，不要改实现迁就旧测试；
4. `git init` 后先提交该版做基线；
5. 合并期**禁止多会话并行写同一包**（9 点里 5 点在 modelpool）。

### 我这条线已完成、可复跑的验证

```
docker run --rm -v "D:\workdesktop\src-backup:/src" -w /src golang:1.27-bookworm bash /src/wsfix/verify-final.sh
-> go vet ./internal/workspace/ = 0
-> go test ./internal/workspace/ -race = ok (1.02s)
-> 死锁哨兵 2 条 PASS（修复前对照：各 5.01s FAIL）
```

**声明**：自 10:15 起本会话未执行任何删除操作；未写 `workbench/`；未碰 `.git`。
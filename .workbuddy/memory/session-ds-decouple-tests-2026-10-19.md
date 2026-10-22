# 会话 ds 端记忆锚点 R6（2026-10-19 16:15 · 平台解耦 + 补测试）

## 老大指令
「平台解耦 + 补测试」= issue #2（L1/B1）+ issue #3（L2/B2）。仓库公开状态保持不变（老大确认「转公开换免费」口径）。

## 交付（两提交，均已推 origin/master）
- **e4ba0f1** test(sec,platform)：16 文件 +763/-72
  - retention：refuseDangerousPath 上移到平台无关 danger.go；trash_other.go 的 Trash 补「存在性检查+安全阀」→ **修掉真实安全缺陷：改造前 Linux 上 Trash("/") 会递归删文件系统根**
  - 归一化必须**双向**：实测 Windows filepath.Clean("/etc")→"\etc"、Linux 上 C:\Windows 反斜杠不被 ToSlash 转换 ⇒ 单向归一 = 两平台行为不一致（本机实测抓出）
  - verify/engine：解释器探针 pwsh→python3→python→node（exec.LookPath）；**Windows 全缺 → t.Fatal**（禁 Skip 洗绿），非 Windows 才 Skip
  - 判据加固：TestMaterializeArtifactsFail 补断 FailReason 含「退出码 2」——原只断 Pass==false，解释器缺失的"启动失败"同样 Pass=false，判据被偷换仍绿
  - chain_test.go 0→**88.6%**；judge_test.go 0→**63.7%**（含空/非法 JSON/越界钳位/40 万字符超长负面用例）
  - build.ps1 -PerPackageCover 新增 chain=80 / judge=55（取两平台低值留余量，只许抬不许降）
- **7f0da23** docs(learning)：学习计划 §6 执行回执 + 未完成项归属
- issue #2 #3 带证据关闭；**#1 保持 OPEN**（修复点在 autonomy/manager.go，属 Lead 写入口，实测 HEAD L467/L470 顺序未改）

## 验证方法（关键，下次照走）
**不能拿工作区验证** —— 工作区含 Lead 在途改动（artifacts.go 签名在改、judge.go run() 加参）。
正解：`git worktree add --detach _ci_head HEAD` 造**提交树本体**，容器跑它：
`docker run --rm -v D:/workdesktop:/src -w /src golang:1.27-bookworm bash -c "cd /src/_ci_head/workbench && go vet ./... && go test ./... -count=1"`
→ 24/24 包 ok、0 FAIL、vet rc=0。CI #39219856230 + Autonomy #39219856395 双 success。
用完 `git worktree remove --force` + `git worktree prune`（否则 .git/worktrees 留残骸）。

## 并行协作踩坑（已自纠，必须记）
degraded_contract_test.go 同时含我的解耦改动 + Lead 未提交的 WaitInflight flaky 修复。
**踩坑**：最后一步 `git add -- <该文件>` 会把工作区的**合并版**（含 Lead 修复）整份纳入 → 提交误抢他人成果（git show HEAD 里 WaitInflight=true 才发现）。
**正解**：① 提交前用 `_degraded_head_mine.txt`（HEAD+仅我的改动）覆盖工作区 → add → commit → 再把 Lead 的修复写回工作区（还原为其未提交改动）；
② 若已提交且**未推送**，`git commit --amend` 拆回（本次即如此处置），验证 `git show HEAD:<file>` 不含他人符号 + `git diff` 只剩他人改动；
③ 归属自检：`git show HEAD:<file> | Select-String <他人符号>` 必须无命中。

## 弹窗处置（老大插单）
soak-24h-v2 反复弹控制台：根因 watchdog.ps1 用 Win32_Process.Create 拉起实例（该 API 必弹可见窗口），实例又被别的会话清掉 → 每次重启弹一次。
已把拉起方式改为 Start-Process -WindowStyle Hidden（语法校验 0 错）；随后 Lead 会话把整个 watchdog 改名 .DISABLED（补丁随文件保留，将来启用即静默）。metrics 账本 49 样本未丢。

## 复用经验
- **CI 同码一红一绿 = 竞态哨兵**：先证 diff 零代码改动，再定位「可观测面顺序差」（SetActive 先于决策留痕），别急着改判据。
- **容器镜像被清**（Docker 重启）：直连 auth.docker.io 超时是 GFW 常态，走 `docker.1ms.run/library/<image>` 再 `docker tag` 回原名。
- **补测试要连门禁一起改**：新测包不入 -PerPackageCover 分档就会被全局 55% 阈值判红。

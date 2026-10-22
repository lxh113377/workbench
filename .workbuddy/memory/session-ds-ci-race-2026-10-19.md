# 会话 ds 端记忆锚点 R3（2026-10-19 13:20 · 修 CI 转绿到只剩 race）

## 成果（老大沉默，#28 代决自己动手修 CI）
- e34d1c1：workspace_test/web 测试断言经 evalExisting 归一 8.3 短路径 + 盘符断言限 Windows + autonomy 证据 chmod a+rX → web/autonomy 转绿。
- be23fee：workspace.go normForCompare 加 evalExisting 归一（单点修所有越界判定 IsWithin/resolve），本机含安全拒绝用例全绿 → workspace 转绿。
- 2aceecf：engine_workspace_test EqualFold 归一 8.3。
- 全部 path-limited add，Lead 61 个未提交改动零碰。

## 剩余唯一失败（归 Lead）
- engine TestClosedLoopThroughRunOne 真 DATA RACE：store.Update（383 原地写 *t=cp）vs runOne defer（651 读 running.ChainID）撞同一对象。Transition/Get 返回副本但 Update 原地写——store 并发设计不一致。本机 -race 稳定复现；不带 -race 全绿。
- 修法二选一：① 修 store.Update 并发语义（归 Lead 核心存储层）；② ci.yml 把 go test -race 降级为 go test（丢哨兵，不推荐）。

## 复用经验
- Windows 8.3 短路径（RUNNER~1）是 CI runneradmin 临时目录的常态：任何路径字符串比较（HasPrefix/EqualFold/Dir）都可能假失败；正解是 evalExisting/EvalSymlinks 归一后比较。集中改 normForCompare 一处优于散落改测试。
- -race 是抓真并发 bug 的哨兵：本机 go test 绿不代表 -race 绿；CI 一定要保留 -race（即便它现在抓出 Lead 的 race 让 CI 红）。
- store 层要全程一致：要么所有读都返回副本、写都持锁+不共享指针，要么别混用（Transition 返回副本但 Update 原地写 = race 温床）。
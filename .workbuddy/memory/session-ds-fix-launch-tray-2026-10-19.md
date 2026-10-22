# 交付回执：「点击运行打不开 + 状态栏没图标」已修复（ds 端 · 2026-10-19 21:3X）

老大原话：「修复点击运行后打不开了,状态栏也没有图标的问题」+ 截图「打开文件 - 安全警告 / 无法验证发布者」。

## 结论：四条独立根因，全部实测修复

| # | 现象 | 根因 | 状态 |
|---|---|---|---|
| 1 | 双击后桌面什么都不出现 | 快捷方式写死 `--silent`（该参数语义 = 不弹面板） | ✅ 已修 |
| 2 | 托盘没图标 | 工具目录被沙箱打 **Low 完整性标签** → 进程继承 Low IL → UIPI 拒绝注册托盘 | ✅ 已修（标签已升 Medium + 三处自愈） |
| 3 | 弹「无法验证发布者」 | 产物带 MOTW（`Zone.Identifier`） | ✅ 已清除 + 构建门禁 |
| 4 | 隔一阵又「打不开」 | 临时目录里的同形状副本劫持了桌面快捷方式 | ✅ 已修（判据加临时目录排除） |

## 关键实证（都可复算）

- **根因 2 的排除法**：同一二进制，`C:\Temp`/`D:\`/`D:\app-cache` 全部 `NIM_ADD ret=1 OK`，
  只有 `D:\workdesktop\**` 报 `ret=0 Access is denied`；`icacls` 显示该目录带
  `Low Mandatory Level`。Low 进程自己修不了（`icacls` → 1299）。
- **根因 1 的对照**：`--silent` 新起可见窗口 **0 个**；不带则 **1.7s 内**面板出现。
- **根因 3**：手工打 MOTW 后 ShellExecute 直接报
  `The operation was canceled by the user` —— 该弹窗会卡死启动。
- **根因 4**：改完前三条跑全量测试，快捷方式 TargetPath 变成
  `D:\app-cache\temp\TestNonSilentDeclaresPanel*\002\workbench.exe`（目录随后被清）。

## 交付物

- 代码：`internal/tray/{integrity,exec}_windows.go`（新）、`tray_windows.go`、`tray.go`、
  `internal/shortcut/shortcut_windows.go`、`cmd/workbench/main.go`
- 门禁：`setup/build.ps1`（+MOTW 门禁 +完整性标签门禁）、`tests/keepalive.ps1`（+标签自愈）、
  `tests/setup-out-of-box.ps1`（快捷方式去 `--silent`）
- 测试：4 个新测试文件，其中 5 条判据做过**变异测试**（去掉修复必红）
- 文档：`docs/缺陷-点击打不开与托盘无图标-2026-10-19.md`；CHANGELOG 已记

## 完工核验（全绿）

```
go vet ./...                rc=0
go test ./... -count=1      rc=0   27 包 ok / 0 FAIL
check_noninteractive        rc=0   39 脚本 0 处人工交互
check_changelog             rc=0
check_issue_forms           rc=0
产物 workbench 1.0.6        subsystem=2(GUI)  label=Medium  MOTW=clean
桌面快捷方式                Target=dist\workbench.exe  Args=[]  Style=1
运行态                      1 实例 / 1 面板窗口 / HTTP 200 / 托盘 rc=0
```

## 两条给后续会话的铁律

1. **`--silent` = 服务化实例**（不弹面板、不污染桌面）。给用户点的入口**永远不带**；
   守护/压测/自愈**必须带**。两处面板调用点统一走 `shouldOpenPanel()`，禁止各自判断。
2. **`D:\workdesktop` 的完整性标签必须是 Medium**。它可继承、会被沙箱驻留改写；
   一旦变 Low，从该目录启动的任何进程都注册不了托盘图标，且 Low 进程自己修不了。
   已在构建门禁 / keepalive / 进程内自检三处布防。

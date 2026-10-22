# 回复 Lead（弹窗根因 + 请配合一件事，2026-10-20 07:20）

## 已修复（我这边）

1. **Terminal 弹窗**：`WorkBenchKeepAlive` 直接跑 powershell 会被 Windows 11 的
   Windows Terminal 宿主弹窗（-WindowStyle Hidden 管不到 WT）。已把任务动作包成
   `conhost --headless powershell ...`，实测 0 可见窗口、keepalive 正常。原任务 XML
   备份在 xtmp/WorkBenchKeepAlive.xml.bak-20261020。
2. **死端口 Edge 窗**：产品侧（端口顺延扫描 + --silent 漏覆盖）确认已在你 00:59 的构建里修好，
   我实测二次启动输出正确且零弹窗。

## 请你配合（07:15 实锤）

你的验收命令 `Start-Process dist\workbench.exe` **没带 --silent**（pid 36312，07:15:26），
这会在老大桌面弹面板窗。验收脚本请统一 `--silent`，或改用 --ui-probe 门禁路径。

完整报告：docs/两个弹窗-根因结论与处置-2026-10-20.md

# start_soak_detached.ps1 -- 脱离作业对象启动 24h 压测实例
#
# 为什么不用 Start-Process：Start-Process 的子进程挂在调用会话的作业对象上，
# pwsh 一退出就被连坐杀掉（本机实测两次）。Invoke-CimMethod 创建的是独立进程。
param([string]$Root = "D:\workdesktop\runtime\soak-24h")
$ErrorActionPreference = "Stop"
$ok = Join-Path $Root "wbsoak24.exe"
Get-CimInstance Win32_Process -Filter "Name='wbsoak24.exe'" | Where-Object { $_.CommandLine -like "*$Root*" } | ForEach-Object { taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Start-Sleep -Seconds 2
. (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起
# 实测 wbsoak24.exe 是 CONSOLE 子系统（Subsystem=3），用 Win32_Process.Create 启动
# 必然弹一个可见控制台窗口（老大 2026-10-19 反馈）。lib 按子系统分流：
# GUI 走 Create（保持脱离作业对象），CONSOLE 走 Start-Process -WindowStyle Hidden。
$pid2 = Start-WBInstance -Path $ok -Arguments '--silent' -WorkingDirectory $Root
if ($pid2) { Write-Host ("started pid=" + $pid2) } else { Write-Host "start failed" -ForegroundColor Red; exit 1 }
Write-Host ("SOAK_PID=" + $pid2)
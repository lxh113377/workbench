# quit_acceptance.ps1 — 「托盘『退出工作台』关不掉」修复的端到端验收（2026-10-19）。
#
# 现场缺陷：点托盘「退出工作台」→ 进程结束、相位落 stopped
#   → 外部守护 tests\keepalive.ps1（计划任务 WorkBenchKeepAlive，每分钟一次）
#     发现「进程不在」就拉起 --silent 新实例，并 POST /api/engine/start
#   → 回到 running ⇒ 用户观感「关不掉」。
#
# 本脚本在**隔离实例**上复现该链路并验收修复，全部判据基于真实运行态：
#   1. 起隔离实例（独立 runtime/workbench-output）
#   2. 用真实托盘菜单命令 WM_COMMAND 1006（等价于用户点「退出工作台」）
#   3. 断言：进程退出 + state.json 落 userQuit=true + runtime\user-quit.flag 存在
#   4. 在 90 秒窗口内反复跑**改过的** keepalive：断言进程**不被拉起**
#   5. 负向对照：清掉标记后（等价于用户点「启动引擎」）同一 keepalive **必须**拉起
#      —— 证明是「尊重用户意愿」而不是「守护被整体关掉」，7x24 语义未被破坏
#
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File tests\quit_acceptance.ps1
param(
    [string]$Exe = "D:\workdesktop\dist\workbench.exe",
    [int]$Port = 17891,
    [string]$Root = "D:\workdesktop\dist\quit-acceptance"
)
$ErrorActionPreference = "Stop"
$ConfirmPreference = "None"
$ProgressPreference = "SilentlyContinue"

Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public class QW {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassNameW(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc lp, IntPtr l);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern IntPtr SendMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  public static IntPtr FindByPid(string cls, uint want) {
    IntPtr res = IntPtr.Zero;
    EnumWindows((h, l) => {
      var sb = new StringBuilder(256); GetClassNameW(h, sb, 256);
      if (sb.ToString() == cls) { uint pid; GetWindowThreadProcessId(h, out pid); if (pid == want) { res = h; return false; } }
      return true;
    }, IntPtr.Zero);
    return res;
  }
  public static void SendCmd(IntPtr h, int id) { SendMessageW(h, 0x0111, (IntPtr)id, IntPtr.Zero); }
}
'@

$script:pass = 0
$script:fail = 0
function Check($name, $cond, $detail) {
    if ($cond) { $script:pass++; Write-Host "  [PASS] $name" }
    else { $script:fail++; Write-Host "  [FAIL] $name  $detail" }
}
# 判据必须匹配**被启动的那份副本**（$script:installed），而不是源 EXE：
# 否则起完实例仍判"进程不在"（实测假失败）—— 属于"判据与被断言对象不同域"。
function GetInstanceProc {
    Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -and $_.CommandLine.ToLower().Contains($script:installed.ToLower()) }
}
function GetInstanceState {
    try { return (Get-Content (Join-Path $Root "workbench-output\state.json") -Raw) } catch { return "" }
}
function GetStateField($name) {
    # 必须**去掉引号**再比较：正则捕获的是 JSON 原文，字符串值自带引号，
    # 否则返回的运行态是带引号的 running，-eq running 恒假 ⇒ 假失败。
    $m = [regex]::Match((GetInstanceState), '"' + $name + '"\s*:\s*(true|false|"[^"]*"|\d+)')
    if ($m.Success) { return ($m.Groups[1].Value -replace '"', '') }
    return ""
}
function Get-PanelTokenLocal([int]$p) {
    try {
        $h = (Invoke-WebRequest -Uri "http://127.0.0.1:$p/" -UseBasicParsing -TimeoutSec 3).Content
        $m = [regex]::Match($h, '__WB_TOKEN__\s*=\s*"([a-zA-Z0-9_-]+)"')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return $null
}

Write-Host "=== 0. 准备隔离实例 ==="
if (Test-Path $Root) { Remove-Item -Recurse -Force $Root -ErrorAction SilentlyContinue }
New-Item -ItemType Directory -Force -Path (Join-Path $Root "runtime"), (Join-Path $Root "workbench-output") | Out-Null
$script:installed = Join-Path $Root "workbench.exe"
Copy-Item -LiteralPath $Exe -Destination $script:installed -Force
$cfg = '{ "schemaVersion": 5, "autoResume": false, "resumeOnBoot": false, "autoStart": false, "server": { "host": "127.0.0.1", "port": ' + $Port + ', "openBrowser": false } }'
[IO.File]::WriteAllText((Join-Path $Root "workbench-output\config.json"), $cfg, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "    实例根: $Root  期望端口: $Port"

Write-Host "=== 1. 起实例（--silent，不弹面板）==="
# 用 Win32_Process.Create 拉起（与 keepalive 的 GUI 分流同源）：
# ① 子进程脱离 pwsh 作业对象，pwsh 退出不连坐；② 避开本机 PS 5.1
#    Start-Process 的 NO_PROXY/no_proxy 环境变量重名缺陷（实测 ArgumentException）。
$launchCmd = '"' + $script:installed + '" --silent'
$launchRes = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $launchCmd }
$proc = $null
for ($i = 0; $i -lt 120; $i++) {
    $proc = GetInstanceProc | Select-Object -First 1
    if ($proc) { break }
    Start-Sleep -Milliseconds 250
}
Check "1.1 实例已起来" ($null -ne $proc) "未检测到进程（Create rc=$($launchRes.ReturnValue)）"
if (-not $proc) { Write-Host "RESULT: $script:pass PASS / $script:fail FAIL"; exit 1 }
$pidMain = $proc.ProcessId
Write-Host "    pid=$pidMain"

# 端口必须取自**实例自写的** runtime\panel.port：Listen() 在端口被占用时会自增，
# 写死 $Port 会在 TIME_WAIT 累积后打到错端口（实测假失败）。与 keepalive 同源做法。
$activePort = $Port
$pf = Join-Path $Root "runtime\panel.port"
for ($i = 0; $i -lt 120; $i++) {
    if (Test-Path $pf) {
        $v = (Get-Content $pf -Raw).Trim()
        if ($v -match '^[0-9]+$') { $activePort = [int]$v; break }
    }
    Start-Sleep -Milliseconds 250
}
$ready = $false
for ($i = 0; $i -lt 240; $i++) {
    try { $null = Invoke-WebRequest -Uri "http://127.0.0.1:$activePort/" -UseBasicParsing -TimeoutSec 2; $ready = $true; break } catch { Start-Sleep -Milliseconds 250 }
}
Check "1.2 面板可达" $ready ("端口 " + $activePort + " 在 60 秒内未就绪")
Write-Host "    实际端口: $activePort"

Write-Host "=== 2. 让引擎进入 running（面板「启动引擎」等价路径）==="
$tok = Get-PanelTokenLocal $activePort
Check "2.1 取到面板 token" ($tok -ne $null) "页面未暴露 token"
if ($tok) {
    $uri = "http://127.0.0.1:$activePort/api/engine/start?token=$tok"
    try { $null = Invoke-WebRequest -UseBasicParsing -Uri $uri -Method POST -Body '{}' -ContentType "application/json" -TimeoutSec 5 } catch { }
}
# 轮询而非定睡：启动是异步的，固定等待会读到尚未落盘的 state.json（实测假失败）。
$phaseOk = $false
for ($i = 0; $i -lt 60; $i++) {
    if ((GetStateField "phase") -eq "running") { $phaseOk = $true; break }
    Start-Sleep -Milliseconds 250
}
Check "2.2 引擎已 running" $phaseOk ("phase=" + (GetStateField "phase"))

Write-Host "=== 3. 发送真实托盘菜单命令 WM_COMMAND 1006（＝用户点『退出工作台』）==="
# 托盘窗口是**异步**出现的：tray.Run 排在 ensureShortcuts / 治理初始化之后，
# 面板可达 ≠ 托盘已注册。单次查找会偶发假失败（实测踩到）⇒ 必须轮询。
$hwnd = [IntPtr]::Zero
for ($i = 0; $i -lt 120; $i++) {
    $hwnd = [QW]::FindByPid("PersonalWorkbenchTrayClass", [uint32]$pidMain)
    if ($hwnd -ne [IntPtr]::Zero) { break }
    Start-Sleep -Milliseconds 250
}
Check "3.1 找到托盘窗口" ($hwnd -ne [IntPtr]::Zero) "30 秒内未出现 PersonalWorkbenchTrayClass 窗口（pid=$pidMain）"
if ($hwnd -ne [IntPtr]::Zero) { [QW]::SendCmd($hwnd, 1006) }

$gone = $false
for ($i = 0; $i -lt 80; $i++) {
    if (-not (GetInstanceProc)) { $gone = $true; break }
    Start-Sleep -Milliseconds 250
}
Check "3.2 进程已退出" $gone "20 秒内进程仍在"
Check "3.3 state.json 落 stopped" ((GetStateField "phase") -eq "stopped") ("phase=" + (GetStateField "phase"))
Check "3.4 state.json 落 userQuit=true（用户意愿已表达）" ((GetStateField "userQuit") -eq "true") ("userQuit=" + (GetStateField "userQuit"))
$flag = Join-Path $Root "runtime\user-quit.flag"
Check "3.5 runtime\user-quit.flag 已落盘" (Test-Path $flag) "flag 不存在"

Write-Host "=== 4. 90 秒窗口内反复跑改过的 keepalive：断言不被拉起 ==="
$ka = "D:\workdesktop\tests\keepalive.ps1"
$relaunched = $false
$kaLog = @()
for ($round = 1; $round -le 6; $round++) {
    $o = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $ka -Exe $script:installed -RuntimeDir (Join-Path $Root "runtime") 2>&1 | Out-String
    $kaLog += $o
    if (GetInstanceProc) { $relaunched = $true; break }
    Start-Sleep -Seconds 12
}
Check "4.1 用户主动退出后 90 秒内未被拉起" (-not $relaunched) "被 keepalive 重新拉起"
Check "4.2 keepalive 明确记录『尊重用户意愿，不拉起』" (($kaLog -join " ") -match "尊重用户意愿，不拉起") "日志未见该分支"

Write-Host "=== 5. 负向对照：清标记后（＝用户点『启动引擎』）keepalive 必须拉起 ==="
Remove-Item -Force $flag -ErrorAction SilentlyContinue
$st = GetInstanceState
$st2 = $st -replace '"userQuit"\s*:\s*true', '"userQuit":false'
[IO.File]::WriteAllText((Join-Path $Root "workbench-output\state.json"), $st2, (New-Object System.Text.UTF8Encoding($false)))
$o2 = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $ka -Exe $script:installed -RuntimeDir (Join-Path $Root "runtime") 2>&1 | Out-String
$back = $false
for ($i = 0; $i -lt 160; $i++) { if (GetInstanceProc) { $back = $true; break }; Start-Sleep -Milliseconds 250 }
Check "5.1 清标记后 keepalive 重新拉起（7x24 语义未被破坏）" $back "未拉起"
$runOk = $false
for ($i = 0; $i -lt 80; $i++) { if ((GetStateField "phase") -eq "running") { $runOk = $true; break }; Start-Sleep -Milliseconds 250 }
Check "5.2 拉起后引擎被自动恢复 running" $runOk ("phase=" + (GetStateField "phase"))

Write-Host "=== 6. 清理 ==="
GetInstanceProc | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
Write-Host "RESULT: $script:pass PASS / $script:fail FAIL"
if ($script:fail -gt 0) { exit 1 }
exit 0

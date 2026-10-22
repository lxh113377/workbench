# selfheal_e2e.ps1 -- 端到端验证「自愈重启后引擎自动接续」（7x24 的核心承诺）
#
# 背景：单元测试已证明 shouldAutoStart 判据正确，但那只是判据。
# 本脚本验证**真实进程行为**：
#   1) 起一个隔离实例（独立数据根，GUI EXE 不弹窗）；
#   2) 启动引擎，确认 phase=running；
#   3) 模拟自愈停机：调 AbortForSelfHeal 等价路径 —— 直接写 state.json 落 stopped+标记，
#      再重启实例（等价于 SelfHeal 拉起新进程）；
#   4) 断言新实例**自动接续**（phase 回到 running，无需任何人工点击）。
#
# 为什么要隔离实例：不能动老大正在跑的生产实例。
# 注意：本脚本用 win32 CreateProcess 启动的是 **GUI 子系统 EXE**，不会弹控制台窗口。
param(
  [string]$Exe = 'D:\workdesktop\dist\workbench.exe',
  [string]$Root = ''
)
$ErrorActionPreference = "Stop"
if (-not (Test-Path $Exe)) { throw ('not found: ' + $Exe) }
if (-not $Root) { $Root = Join-Path $env:TEMP ('wb-sh-' + [guid]::NewGuid().ToString('N').Substring(0,8)) }
New-Item -ItemType Directory -Force -Path $Root | Out-Null
$ok = Join-Path $Root 'wbsh.exe'
Copy-Item $Exe $ok -Force
$data = Join-Path $Root 'workbench-output'
New-Item -ItemType Directory -Force -Path $data | Out-Null
Write-Output ('root=' + $Root)

# 预置配置：AutoResume=true, AutoStart=false（这正是自愈场景的开关组合）
# 用被测 EXE 起一次以生成默认配置，或直接写最小配置
$cfgPath = Join-Path $data 'config.json'
$cfg = [pscustomobject]@{
  schemaVersion = 5; autoStart = $false; autoResume = $true; resumeOnBoot = $true
  executor = [pscustomobject]@{ concurrency = 4; defaultTimeout = 20; maxRetries = 1; retryBackoffMs = @(1000); requestsPerMinute = 6000; requestBurst = 600 }
  supervisor = [pscustomobject]@{ enabled = $false; memLimitMB = 4000; handleLimit = 20000; goroutineLimit = 8000; sampleSeconds = 60; strikesToRestart = 3 }
}
$cfg | ConvertTo-Json -Depth 8 | Set-Content $cfgPath -Encoding UTF8

. (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起（GUI 走 Create，CONSOLE 走隐藏 Start-Process）
function Start-Inst {
  return Start-WBInstance -Path $ok -Arguments '--silent' -WorkingDirectory $Root
}
function Stop-Inst {
  Get-CimInstance Win32_Process -Filter "Name='wbsh.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like ('*' + $Root + '*') } | ForEach-Object { taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
  Start-Sleep -Seconds 3
}
function Wait-Panel {
  $u = Join-Path $Root 'runtime\panel.url'
  $w = 0
  while (-not (Test-Path $u) -and $w -lt 60) { Start-Sleep -Seconds 2; $w++ }
  if (-not (Test-Path $u)) { return '' }
  return (Get-Content $u -Raw).Trim()
}
function Get-Token($url) {
  $p = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
  return [regex]::Match($p.Content, 'window.__WB_TOKEN__ = "([^"]+)"').Groups[1].Value
}

Write-Output '--- 阶段 1: 起实例并启动引擎 ---'
Stop-Inst
Start-Inst | Out-Null
$url = Wait-Panel
if (-not $url) { Write-Output 'FAIL: panel.url missing'; exit 1 }
$tok = Get-Token $url
$hd = @{ 'X-WB-Token' = $tok; 'Content-Type' = 'application/json' }
Invoke-RestMethod -Uri ($url + 'api/engine/start') -Method Post -Headers $hd -Body '{}' -TimeoutSec 20 | Out-Null
Start-Sleep -Seconds 5
$st1 = Get-Content (Join-Path $data 'state.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Write-Output ('  phase=' + $st1.phase + '  resumeAfterRestart=' + $st1.resumeAfterRestart)
if ($st1.phase -ne 'running') { Write-Output 'FAIL: 引擎未进入 running'; exit 1 }
Stop-Inst

Write-Output '--- 阶段 2: 模拟自愈（落 stopped + ResumeAfterRestart 标记）---'
$st2 = Get-Content (Join-Path $data 'state.json') -Raw -Encoding UTF8 | ConvertFrom-Json
$st2.phase = 'stopped'
$st2.stoppedAt = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$st2 | Add-Member -NotePropertyName resumeAfterRestart -NotePropertyValue $true -Force
$st2 | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $data 'state.json') -Encoding UTF8
Write-Output ('  wrote phase=stopped resumeAfterRestart=true')

Write-Output '--- 阶段 3: 重启实例，断言自动接续（无需任何人工点击）---'
Start-Inst | Out-Null
Start-Sleep -Seconds 25
$st3 = Get-Content (Join-Path $data 'state.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Write-Output ('  phase=' + $st3.phase + '  startedAt=' + $st3.startedAt)
if ($st3.phase -eq 'running') {
  Write-Output 'RESULT: PASS - 自愈重启后引擎自动接续（7x24 承诺成立）'
  Stop-Inst
  exit 0
}
Write-Output ('RESULT: FAIL - 自愈重启后 phase=' + $st3.phase + '，引擎未接续');
Stop-Inst
exit 1
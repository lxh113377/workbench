# selfheal_e2e_negative.ps1 -- 反向断言：**用户主动停机**不得被自动拉起
#
# 场景：用户从面板点「停止引擎」-> phase=stopped 且**无** resumeAfterRestart 标记 ->
#       重启实例后必须保持 idle（尊重用户意愿），不能自作主张开跑。
# 与 selfheal_e2e.ps1 互为对照，共同锁住「被动中断 vs 主动停机」的分野。
param(
  [string]$Exe = 'D:\workdesktop\dist\workbench.exe',
  [string]$Root = ''
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Join-Path $env:TEMP ('wb-shn-' + [guid]::NewGuid().ToString('N').Substring(0,8)) }
New-Item -ItemType Directory -Force -Path $Root | Out-Null
$ok = Join-Path $Root 'wbshn.exe'
Copy-Item $Exe $ok -Force
$data = Join-Path $Root 'workbench-output'
New-Item -ItemType Directory -Force -Path $data | Out-Null
$cfg = [pscustomobject]@{ schemaVersion = 5; autoStart = $false; autoResume = $true; resumeOnBoot = $true; executor = [pscustomobject]@{ concurrency = 2; defaultTimeout = 20 } }
$cfg | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $data 'config.json') -Encoding UTF8
. (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起（GUI 走 Create，CONSOLE 走隐藏 Start-Process）
function Start-Inst {
  return Start-WBInstance -Path $ok -Arguments '--silent' -WorkingDirectory $Root
}
function Stop-Inst {
  Get-CimInstance Win32_Process -Filter "Name='wbshn.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -like ('*' + $Root + '*') } | ForEach-Object { taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
  Start-Sleep -Seconds 3
}
Write-Output '--- 起实例，直接落「用户主动停机」状态 ---'
Stop-Inst
Start-Inst | Out-Null
$u = Join-Path $Root 'runtime\panel.url'
$w=0; while (-not (Test-Path $u) -and $w -lt 60) { Start-Sleep -Seconds 2; $w++ }
Stop-Inst
$st = [pscustomobject]@{ phase = 'stopped'; startedAt = 0; stoppedAt = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds(); pausedAt = 0; restartCount = 0; updatedAt = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() }
$st | ConvertTo-Json -Depth 6 | Set-Content (Join-Path $data 'state.json') -Encoding UTF8
Write-Output '  wrote phase=stopped, NO resumeAfterRestart (user stop)'
Write-Output '--- 重启实例：必须保持 idle ---'
Start-Inst | Out-Null
Start-Sleep -Seconds 25
$after = Get-Content (Join-Path $data 'state.json') -Raw -Encoding UTF8 | ConvertFrom-Json
Write-Output ('  phase=' + $after.phase + '  resumeAfterRestart=' + $after.resumeAfterRestart)
Stop-Inst
if ($after.phase -eq 'stopped' -or $after.phase -eq 'idle') {
  Write-Output 'RESULT: PASS - 用户主动停机被尊重（未被自动拉起）'
  exit 0
}
Write-Output ('RESULT: FAIL - 用户停机后竟被拉起，phase=' + $after.phase)
exit 1
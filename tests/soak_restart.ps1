# soak_restart.ps1 -- 用当前 EXE 重开一个干净的 24h 累积实例（A7）
#
# 为什么重开：旧实例（12:48 从当时的 EXE 拷贝）五分钟后就不再追加 metrics：
#   mtime 停在 13:43:35，而 /api/metrics 仍在刷新 —— supervisor 在采样，
#   但 WriteCSV 的「只追加比 csvLastAt 更新的样本」判定卡住了。
#   为避免把历史账本写脏（append-only + csvLastAt=0 会重复追加），开全新数据根。
param([string]$Exe = "D:\workdesktop\dist\workbench.exe", [string]$Root = "D:\workdesktop\runtime\soak-24h-v2")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force -Path $Root | Out-Null
$ok = Join-Path $Root "wbsoak25.exe"
Copy-Item $Exe $ok -Force
$cfgDir = Join-Path $Root "workbench-output"
New-Item -ItemType Directory -Force -Path $cfgDir | Out-Null
$cfg = Get-Content "D:\workdesktop\dist\workbench-output\config.json" -Raw -Encoding UTF8 | ConvertFrom-Json
$cfg.supervisor.enabled = $true
$cfg.supervisor.sampleSeconds = 60
$cfg.autoStart = $false
$cfg.autoResume = $true
$cfg.resumeOnBoot = $true
$cfg.autonomy.enabled = $false
$cfg | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $cfgDir "config.json") -Encoding UTF8
Get-CimInstance Win32_Process -Filter "Name='wbsoak25.exe'" -ErrorAction SilentlyContinue | ForEach-Object { taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Start-Sleep -Seconds 2
. (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起
$wbPid = Start-WBInstance -Path $ok -Arguments '--silent' -WorkingDirectory $Root
if (-not $wbPid) { Write-Output 'pid=0 拉起失败（lib 未返回进程号）'; exit 1 }
Write-Output ('pid=' + $wbPid)
$urlFile = Join-Path $Root "runtime\panel.url"
$w=0; while (-not (Test-Path $urlFile) -and $w -lt 60) { Start-Sleep -Seconds 2; $w++ }
if (Test-Path $urlFile) {
  $url = (Get-Content $urlFile -Raw).Trim()
  $p = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
  $tok = [regex]::Match($p.Content, 'window.__WB_TOKEN__ = "([^"]+)"').Groups[1].Value
  $hd = @{ 'X-WB-Token' = $tok; 'Content-Type' = 'application/json' }
  Invoke-RestMethod -Uri ($url + 'api/engine/start') -Method Post -Headers $hd -Body '{}' -TimeoutSec 20 | Out-Null
  Write-Output ('url=' + $url + ' engine=started')
} else { Write-Output 'WARN panel.url missing' }
Write-Output '=== 等两个 5 分钟 tick，确认 metrics 真在追加 ==='
$m = Join-Path $cfgDir "logs\metrics.csv"
for ($i = 1; $i -le 12; $i++) {
  Start-Sleep -Seconds 30
  if (Test-Path $m) { $n = (Get-Content $m | Measure-Object).Count; Write-Output ('  t+' + ($i*30) + 's lines=' + $n + ' mtime=' + (Get-Item $m).LastWriteTime.ToString('HH:mm:ss')) }
}
Write-Output ('ROOT=' + $Root)
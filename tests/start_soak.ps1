# start_soak.ps1 -- 启动 24h 长时压测实例（A7 证据累积）
#
# A7 判据：metrics.csv 采样跨度 >= 24h 且无泄漏趋势。这是**唯一**无法压缩的判据 --
# 只能靠真实时间累积。启动后就让它一直跑（独立数据根，不碰生产）。
#
# 采样：supervisor 每 sampleSeconds 秒增量 append 一行（历史不丢）。
param([string]$Exe = "D:\workdesktop\dist\workbench.exe", [string]$Root = "D:\workdesktop\runtime\soak-24h")
$ErrorActionPreference = "Stop"
if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
New-Item -ItemType Directory -Force -Path $Root | Out-Null
$ok = Join-Path $Root "wbsoak24.exe"
Copy-Item $Exe $ok -Force
$cfgDir = Join-Path $Root "workbench-output"
New-Item -ItemType Directory -Force -Path $cfgDir | Out-Null
if (-not (Test-Path (Join-Path $cfgDir "config.json"))) {
  $cfg = Get-Content "D:\workdesktop\dist\workbench-output\config.json" -Raw -Encoding UTF8 | ConvertFrom-Json
  $cfg.supervisor.enabled = $true
  $cfg.supervisor.sampleSeconds = 60
  $cfg.autoStart = $false; $cfg.autoResume = $true; $cfg.resumeOnBoot = $true
  $cfg | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $cfgDir "config.json") -Encoding UTF8
}
# 只杀命令行含本 Root 的实例（避免误伤生产与其它会话）
Get-CimInstance Win32_Process -Filter "Name='wbsoak24.exe'" | Where-Object { $_.CommandLine -like "*$Root*" } | ForEach-Object { taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Start-Sleep -Seconds 2
# Start-Process：能在同一 pwsh 生命周期内可靠启动（Invoke-CimMethod 传参在本机实测会失败）
$p = Start-Process -FilePath $ok -ArgumentList "--silent" -WorkingDirectory $Root -NoNewWindow -PassThru -RedirectStandardOutput "$Root\o.txt" -RedirectStandardError "$Root\e.txt"
Write-Host ("started pid=" + $p.Id + " root=" + $Root)
$urlFile = Join-Path $Root "runtime\panel.url"
$w = 0
while (-not (Test-Path $urlFile) -and $w -lt 60) { Start-Sleep -Seconds 2; $w++ }
if (Test-Path $urlFile) {
  $url = (Get-Content $urlFile -Raw).Trim()
  Write-Host ("url=" + $url)
  $page = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
  $m = [regex]::Match($page.Content, 'window.__WB_TOKEN__ = "([^"]+)"')
  $h = @{ "X-WB-Token" = $m.Groups[1].Value; "Content-Type" = "application/json" }
  Invoke-RestMethod -Uri ($url + "api/engine/start") -Method Post -Headers $h -Body "{}" -TimeoutSec 20 | Out-Null
  Write-Host "engine started (soak accumulating)"
} else {
  Write-Host "WARN panel.url missing" -ForegroundColor Yellow
  Get-Content "$Root\e.txt" -ErrorAction SilentlyContinue | Select-Object -First 8
}
Start-Sleep -Seconds 15
$csv = Join-Path $Root "workbench-output\logs\metrics.csv"
Write-Host ("metrics: " + $csv)
if (Test-Path $csv) { Write-Host ("lines=" + ((Get-Content $csv | Measure-Object).Count) + " first=" + (Get-Content $csv | Select-Object -Index 1)) } else { Write-Host "metrics not yet written" }
Write-Host ("SOAK_PID=" + $p.Id)
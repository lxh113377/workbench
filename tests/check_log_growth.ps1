# check_log_growth.ps1 -- 日志目录体积的**信息化产出**（永不判红；2026-10-25 round 155）
#
# 为什么需要：本工作台是 7×24 无人值守。内存有硬护栏（MemLimitMB），磁盘**没有**。
#   实测现状（2026-10-25）：dist\workbench-output\logs 共 13.37MB（audit.jsonl 6.91MB /
#   workbench.jsonl 5.40MB / governance.jsonl 0.64MB / metrics.csv 0.41MB），且三个 jsonl
#   都**没有轮转**（全仓搜过：没有 rotate/上限实现）。
#   刻意**不做**上限/轮转：audit.jsonl 与 decisions.jsonl 是本仓的**取证面**（engine.go 就写过
#   「audit.jsonl 一夜涨到 2.3MB」当症状），删旧记录等于毁证据 —— 那是比占盘更坏的失败。
#   正确的第一步是**让增长可见**：本件把体积写成读数（与其他门禁同 schema，落同一份读数文件），
#   由既有的趋势门禁（check_readings_trend）在时间序列上显示「是否在缓慢贴边」。
#
# 判据（信息化）：
#   - 找不到日志目录 => rc=2 UNVERIFIED（未运行/未构建，不计合格）
#   - 其余一律 rc=0；体积与 top 文件打印出来，并追加一条读数（目录存在时才追加）
#
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File tests\check_log_growth.ps1 [-Root <repoRoot>]
# 退出码: 0=已产出读数 / 2=UNVERIFIED（无日志目录）
param(
  [string]$Root = ""
)
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Split-Path -Parent $PSScriptRoot }
$logs = Join-Path $Root "dist\workbench-output\logs"
if (-not (Test-Path -LiteralPath $logs)) {
  Write-Host ("UNVERIFIED 找不到日志目录（未运行/未构建）: " + $logs) -ForegroundColor Yellow
  exit 2
}
$files = @(Get-ChildItem -LiteralPath $logs -File -ErrorAction SilentlyContinue | Sort-Object Length -Descending)
if ($files.Count -eq 0) {
  Write-Host ("UNVERIFIED 日志目录为空: " + $logs) -ForegroundColor Yellow
  exit 2
}
$total = ($files | Measure-Object -Property Length -Sum).Sum
$totalMB = [math]::Round($total / 1MB, 2)
$top = $files[0]
$topMB = [math]::Round($top.Length / 1MB, 2)
Write-Host ("CHECK logGrowth result=INFO files=" + $files.Count + " totalMB=" + $totalMB + " top=" + $top.Name + " topMB=" + $topMB)
foreach ($f in ($files | Select-Object -First 5)) {
  Write-Host ("    " + $f.Name.PadRight(20) + " " + [math]::Round($f.Length / 1MB, 2) + " MB")
}
Write-Host "    说明：本件只产出读数、永不判红；日志不轮转是有意为之（audit/decisions 是取证面）。" -ForegroundColor DarkGray
# 读数落盘（与其他门禁同一份文件、同一风格；目录不存在时优雅跳过）
$memDir = Join-Path $Root ".workbuddy\memory"
if (Test-Path -LiteralPath $memDir) {
  $head = ""
  $gitProbe = & git -C $Root rev-parse --short HEAD 2>$null
  if ($LASTEXITCODE -eq 0 -and $gitProbe) { $head = ([string]$gitProbe).Trim() }
  $row = [pscustomobject]@{
    at      = (Get-Date).ToString("o")
    gate    = "log_growth"
    logsDir = $logs
    fileCount = $files.Count
    totalMB = $totalMB
    topFile = $top.Name
    topMB   = $topMB
    head    = $head
  }
  ($row | ConvertTo-Json -Compress) | Add-Content -LiteralPath (Join-Path $memDir "gate-readings.jsonl") -Encoding utf8
}
exit 0

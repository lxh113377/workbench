# check_gate_links.ps1 -- 门禁脚本引用完整性判据（断链即红）
#
# 为什么需要（2026-10-22 盘点实测）：受跟踪的 tests/check_all.ps1 是预检总门禁，
# 它 Join-Path 引用的 check_subsystem / check_spawn_silent / check_no_shrink /
# check_homepage / keepalive 五个脚本**从未入库** —— 干净 clone（CI 的 checkout 就是
# 干净 clone）里这些引用全部断链，总门禁静默降级成「只跑存在的几条」。
# 本仓已有同族事故在册（check_snapshot_build.ps1：git archive 导出 HEAD 后 go build
# 报 undefined，因 4 个依赖文件从未入库）—— 那是 Go 侧，本件把同一判据扩到脚本侧。
#
# 判据（任一不满足即 rc=1）：
#   1) **受跟踪**的 tests/*.ps1 里以 tests\xxx.ps1 形态引用的每个脚本必须存在，
#      且被引用者也受跟踪 —— 磁盘存在但没入库 = CI 侧断链。
#   2) 只看代码行：注释里提到某脚本不算依赖声明（与 check_noninteractive
#      「窗口只取代码行」同一口径，否则历史事故说明会被判成断链）。
#   3) untracked 文件之间的引用不检查（并行会话在途件不算既有缺陷，避免误伤）。
#
# 用法: powershell -File tests\check_gate_links.ps1 [-Quiet]
# 退出码: 0=链路完整 / 1=断链 / 2=不在 git 仓库内
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }

$trackedRaw = @(& git -C $Root ls-files "tests" 2>$null)
if ($LASTEXITCODE -ne 0) { Write-Error "git ls-files 失败（不在 git 仓库内？）"; exit 2 }
$tracked = @($trackedRaw | ForEach-Object { $_.Trim('"').Replace([char]92, [char]47) })
$trackedSet = @{}
foreach ($t in $tracked) { $trackedSet[$t] = $true }

$fails = @()
foreach ($rel in $tracked) {
  if ($rel -notmatch '\.ps1$') { continue }
  $abs = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $abs)) { continue }
  $all = [IO.File]::ReadAllText($abs)
  # 注释行剔除（行首 # 不算代码）
  $nl = [string][char]10
  $kept = @()
  foreach ($ln in ($all -split ('\r?\n'))) {
    $t0 = $ln.Trim()
    if ($t0 -eq '' -or $t0.StartsWith('#')) { continue }
    $kept += $ln
  }
  $text = $kept -join $nl
  $ms = [regex]::Matches($text, 'tests[\\/]+([A-Za-z0-9_-]+[.]ps1)')
  $seen = @{}
  foreach ($m in $ms) {
    $name = $m.Groups[1].Value
    if ($seen.ContainsKey($name)) { continue }
    $seen[$name] = $true
    $targetRel = 'tests/' + $name
    $targetAbs = Join-Path $root $targetRel
    if (-not (Test-Path -LiteralPath $targetAbs)) {
      $fails += ($rel + ' 引用的 ' + $targetRel + ' 在磁盘上不存在（断链）')
      continue
    }
    if (-not $trackedSet.ContainsKey($targetRel)) {
      $fails += ($rel + '（受跟踪）引用 ' + $targetRel + ' —— 磁盘存在但未入库：干净 clone/CI 里断链')
    }
  }
}

if ($fails.Count -gt 0) {
  Write-Host '门禁引用完整性未通过：' -ForegroundColor Red
  foreach ($f in $fails) { Write-Host ('  - ' + $f) -ForegroundColor Red }
  exit 1
}
if (-not $Quiet) {
  $n = @($tracked | Where-Object { $_ -match '\.ps1$' }).Count
  Write-Host ('门禁引用完整性通过：检查 ' + $n + ' 个受跟踪脚本') -ForegroundColor Green
}
exit 0

# check_report_claims.ps1 -- 报告待办与 CI 现实的窄面一致性门禁（2026-10-23 补）
#
# 为什么需要（本轮实测）：docs/四项工程改造交付报告-2026-10-22.md §8 的待办表里，
# 两条 ⏳ 在后续由并行会话落地后**没人回头改状态栏**（SHA256SUMS 读回校验、perf trend
# artifact 都已进 ci.yml）。后果不是排版问题，而是误导后续 agent 重做已完成的工作 ——
# 本目标的执行入口就是这些报告，状态栏滞后 = 下游分不清「没做」与「做完了没人记」。
#
# 判据（刻意**窄**，只做可证伪的那一面）：
#   报告 §8 待办表里，状态栏仍为 ⏳ 的行，若其「后续动作」列点到了 CI 工作流层面的制品
#   （SHA256SUMS / perf-trend / artifact 等具名物），则该具名物必须**未**出现在 ci.yml 中；
#   若已出现 => 状态栏过期，判红并要求更新。
#
# 为什么不做模糊语义校验（本仓判据标准的直接约束）：
#   「建议是否已落地」是无法机器判定的；硬写只会产生噪音与假红，而假红会让门禁被撤。
#   故本门禁只认「具名制品出现在 CI 工作流文本里」这一条客观事实。
#   ⏳ 行若点名的是外部面（如「向上游提 issue」），不属于本判据范围，刻意不查。
#
# 用法: powershell -File tests\check_report_claims.ps1 [-Quiet]
# 退出码: 0=一致 / 1=有过期的 ⏳ 状态 / 2=环境不对
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }

$ciPath = Join-Path $Root '.github\workflows\ci.yml'
if (-not (Test-Path -LiteralPath $ciPath)) { Write-Host "缺 CI 工作流: $ciPath"; exit 2 }
$ci = [IO.File]::ReadAllText($ciPath)

# 具名制品 -> 在 ci.yml 里的识别特征（只用客观字符串，不做语义推断）
$artifacts = @(
  @{ name = 'SHA256SUMS';      marker = 'SHA256SUMS' },
  @{ name = 'perf-trend';      marker = 'perf-trend' },
  @{ name = 'perf-baseline';   marker = 'perf-baseline.json' }
)

$reportDirs = @('docs')
$fails = @()
$checkedRows = 0
foreach ($d in $reportDirs) {
  $dirPath = Join-Path $Root $d
  if (-not (Test-Path -LiteralPath $dirPath)) { continue }
  $mdFiles = @(Get-ChildItem -LiteralPath $dirPath -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.Extension -eq '.md' })
  foreach ($f in $mdFiles) {
    $lines = @([IO.File]::ReadAllLines($f.FullName))
    for ($i = 0; $i -lt $lines.Count; $i++) {
      $ln = $lines[$i]
      if (-not $ln.Contains([char]0x23F3)) { continue }   # ⏳
      $checkedRows++
      foreach ($a in $artifacts) {
        if (-not $ln.Contains($a.name)) { continue }
        if ($ci.Contains($a.marker)) {
          $rel = $f.FullName.Substring($Root.Length).TrimStart([char]92, [char]47)
          $fails += ($rel + ':' + ($i + 1) + '  仍标 ⏳ 但 ' + $a.name + ' 已出现在 ci.yml —— 状态栏过期')
        }
      }
    }
  }
}

if ($fails.Count -gt 0) {
  Write-Host ("报告待办状态过期（" + $fails.Count + " 行）：") -ForegroundColor Red
  foreach ($x in $fails) { Write-Host ("  - " + $x) -ForegroundColor Red }
  Write-Host "修法：按实测证据把该行状态栏改为 ✅ 并注明 CI 落点（本目标下报告是执行入口，滞后会误导下游重做）。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ("报告待办一致性通过：检查 " + $checkedRows + " 条 ⏳ 行，无「已落地却仍标待办」") -ForegroundColor Green
}
exit 0

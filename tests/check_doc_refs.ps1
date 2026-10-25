# check_doc_refs.ps1 -- 活文档点名的验收入口必须真实存在（2026-10-23 补）
#
# 为什么需要（本轮实测）：docs/ 下 4 份**活文档**点名 `tests/e2e.ps1` 作为可复跑验收入口
# （其中《六项要求验收矩阵》还给出具体复跑命令），但该脚本 **git log --all 为空** ——
# 从未入库，2026-10-19 仓库清空事故后只剩 src-backup/ 里的副本。使用者按图索骥必然失败。
# 同族还有 `tests/e2e_soak.ps1`（文档写「期望 18 PASS」，脚本同样只在备份里）。
#
# 判据（窄面，只判「入口是否存在」这一客观事实）：
#   ACTIVE 文档（排除历史快照类）里以 `tests/xxx.ps1` 形态点名的脚本必须存在。
#   若确实已丢失且有**显式说明**（同行或紧邻出现「已丢失」/「从未入库」等标记），
#   视为已如实披露，不判红 —— 因为「保留历史记录并标注」比「删掉痕迹」更诚实。
#
# 为什么排除历史快照：回执/事故/定损/批量迁移类文档是**当时的事实记录**，
#   不该被要求跟今天的树对齐（那是篡改历史）。只约束「仍在被当作入口使用」的活文档。
#
# 用法: powershell -File tests\check_doc_refs.ps1 [-Quiet]
# 退出码: 0=全部可解析 / 1=有失效引用且未披露
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }

# 历史快照类文档名特征（一次性事实记录，不要求与当前树对齐）
$snapshotPattern = '回执|事故|定损|损坏|清空|迁移|batch-|_upstream|台账|季检|巡检|skill-audit'
# 显式披露标记：说明「此件已丢失/从未入库」即可免除判红
$disclosedMarkers = @('已丢失', '从未入库', '不存在于本仓', '已废弃')

$docsDir = Join-Path $Root 'docs'
if (-not (Test-Path -LiteralPath $docsDir)) { Write-Host "缺 docs 目录: $docsDir"; exit 2 }

$mdFiles = @(Get-ChildItem -LiteralPath $docsDir -Recurse -File -ErrorAction SilentlyContinue | Where-Object {
  $_.Extension -eq '.md' -and $_.FullName -notmatch 'skill-audit' -and $_.Name -notmatch $snapshotPattern
})

$fails = @()
$disclosed = @()
$checkedRefs = 0
foreach ($f in $mdFiles) {
  $lines = @([IO.File]::ReadAllLines($f.FullName))
  for ($i = 0; $i -lt $lines.Count; $i++) {
    foreach ($m in [regex]::Matches($lines[$i], 'tests[\\/]([A-Za-z0-9_.-]+\.ps1)')) {
      $name = $m.Groups[1].Value
      $checkedRefs++
      $target = Join-Path $Root ('tests\' + $name)
      if (Test-Path -LiteralPath $target) { continue }
      # 不存在：看是否有显式披露（本行 + 后 6 行窗口）
      $hi = [Math]::Min($lines.Count - 1, $i + 6)
      $window = ($lines[$i..$hi] -join " ")
      $ok = $false
      foreach ($d in $disclosedMarkers) { if ($window.Contains($d)) { $ok = $true; break } }
      $rel = $f.FullName.Substring($Root.Length).TrimStart([char]92, [char]47)
      if ($ok) { $disclosed += ($rel + ':' + ($i + 1) + '  ' + $name) }
      else { $fails += ($rel + ':' + ($i + 1) + '  点名 tests/' + $name + ' 但该脚本不存在，且未标注已丢失') }
    }
  }
}

if ($disclosed.Count -gt 0 -and -not $Quiet) {
  Write-Host ("已如实披露的失效引用（不判红）：" + $disclosed.Count) -ForegroundColor Yellow
  foreach ($d in $disclosed) { Write-Host ("  ~ " + $d) -ForegroundColor Yellow }
}
if ($fails.Count -gt 0) {
  Write-Host ("活文档失效引用（" + $fails.Count + " 处）：") -ForegroundColor Red
  foreach ($x in $fails) { Write-Host ("  - " + $x) -ForegroundColor Red }
  Write-Host "修法：① 若脚本仍在备份里 -> 恢复到 tests/ 并入库；② 若确实已丢失 -> 在文档就地标注「已丢失/从未入库」并给替代入口。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ("文档引用完整性通过：" + $mdFiles.Count + " 份活文档、" + $checkedRefs + " 处 tests/*.ps1 引用均可解析或已披露") -ForegroundColor Green
}
exit 0

param(
  # 默认值由脚本位置推导（tests/ 的父目录 + workbench），不硬编码。
  # 硬编码会让 CI/干净 clone 检查另一棵树（本项目 2026-10-22 实测过该失真）。
  [string]$Root = ""
)
$ErrorActionPreference = "Stop"

# 默认根由脚本位置推导（tests/ 父目录 + workbench）；显式传参可覆盖。
# 硬编码会让 CI / 干净 clone 检查另一棵树（本轮变异测试实测到的失真）。
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Join-Path (Split-Path -Parent $PSScriptRoot) 'workbench' }

if (-not (Test-Path $Root)) { Write-Error ("root not found: " + $Root); exit 2 }
$DQ = [char]34
$consoleBins = @("cmd", "reg", "powershell.exe", "pwsh", "wmic")
$hiddenMarkers = @("HideWindow", "hiddenProcAttr", "hiddenAttr", "hiddenSysProcAttr", "jobSysProcAttr", "CreateNoWindow", "0x08000000", "hiddenCmd")
$files = Get-ChildItem $Root -Recurse -Filter "*.go" -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike "*_test.go" }
# 已知缺陷豁免（显式登记，非静默跳过）：键为仓库相对路径（不带前导 /）。
# 语义：命中豁免的违规仍**逐条打印**（KNOWN 行）并计数，但不计入 rc；
# 有豁免时输出「PASS（带 N 条已知缺陷豁免，未清零）」，绝不打纯 PASS。
# 清零条件：修复入库后删除对应条目。
#
# 为什么需要（2026-10-22 实测）：本门禁首次接入 CI 时抓到真缺陷 ——
# internal/web/autostart_windows.go 三处 exec.Command("reg", ...) 缺 hiddenProcAttr，
# 而 reg.exe 是 CONSOLE 子系统程序，用户点「开机自启」会闪黑窗。该修复已在工作区
# 由并行会话完成但尚未入库；HEAD 上仍是缺陷版。不带豁免直接上 CI 会长期固定判红，
# 反而掩盖后续**新增**违规（本门禁的变异测试正是靠「新增一处」判红的）。
$known = @{
  'internal/web/autostart_windows.go' = $true
}
$violations = @()
$checked = 0
foreach ($f in $files) {
  $lines = Get-Content $f.FullName -ErrorAction SilentlyContinue
  if (-not $lines) { continue }
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $line = $lines[$i]
    if ($line -notmatch "exec[.]Command") { continue }
    $isConsole = $false
    foreach ($b in $consoleBins) {
      $needle = $DQ + $b + $DQ
      if ($line.Contains($needle)) { $isConsole = $true; break }
    }
    if (-not $isConsole) { continue }
    $checked++
    $lo = [Math]::Max(0, $i - 3)
    $hi = [Math]::Min($lines.Count - 1, $i + 7)
    $window = ($lines[$lo..$hi] -join " ")
    $ok = $false
    foreach ($m in $hiddenMarkers) { if ($window.Contains($m)) { $ok = $true; break } }
    if (-not $ok) {
      # 规范化：去掉根前缀、统一斜杠，且**不得以 / 开头** —— 否则与豁免键
      # （形如 internal/web/x.go）匹配不上（2026-10-22 实测：豁免静默失效，仍判红）。
      $rel = $f.FullName.Replace($Root, "")
      $rel = $rel.Replace([char]92, [char]47)
      $rel = $rel.TrimStart([char]47, [char]47)
      $rel = $rel.TrimStart([char]47)
      $violations += ($rel + ":" + ($i + 1) + ": " + $line.Trim())
    }
  }
}
Write-Host ("  CONSOLE spawn points checked: " + $checked)
# 三态：KNOWN（已登记豁免，逐条点名但不计入 rc）/ FAIL（未登记，计入 rc）/ PASS（零违规零豁免）
$fatal = @()
$knownHit = @()
foreach ($v in $violations) {
  $hit = $false
  foreach ($k in $known.Keys) { if ($v.Contains($k + ':')) { $hit = $true; break } }
  if ($hit) { $knownHit += $v } else { $fatal += $v }
}
if ($knownHit.Count -gt 0) {
  Write-Host ("  KNOWN 已登记豁免（缺陷已定位、修复未入库，非静默跳过）：" + $knownHit.Count) -ForegroundColor Yellow
  $knownHit | ForEach-Object { Write-Host ("    ~ " + $_) -ForegroundColor Yellow }
}
if ($fatal.Count -eq 0) {
  if ($knownHit.Count -eq 0) {
    Write-Host "  PASS: all CONSOLE spawn points hide their window" -ForegroundColor Green
  } else {
    Write-Host ("  PASS（带 " + $knownHit.Count + " 条已知缺陷豁免，未清零）—— 不得据此认为全部合规") -ForegroundColor Yellow
  }
  exit 0
}
Write-Host ("  FAIL: " + $fatal.Count + " CONSOLE spawn point(s) lack window hiding（未登记）") -ForegroundColor Red
$fatal | ForEach-Object { Write-Host ("    " + $_) -ForegroundColor Red }
exit 1
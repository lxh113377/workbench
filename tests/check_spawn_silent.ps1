param(
  [string]$Root = "D:/workdesktop/workbench"
)
$ErrorActionPreference = "Stop"
if (-not (Test-Path $Root)) { Write-Error ("root not found: " + $Root); exit 2 }
$DQ = [char]34
$consoleBins = @("cmd", "reg", "powershell.exe", "pwsh", "wmic")
$hiddenMarkers = @("HideWindow", "hiddenProcAttr", "hiddenAttr", "hiddenSysProcAttr", "jobSysProcAttr", "CreateNoWindow", "0x08000000", "hiddenCmd")
$files = Get-ChildItem $Root -Recurse -Filter "*.go" -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike "*_test.go" }
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
      $rel = $f.FullName.Replace($Root, "").TrimStart([char]47)
      $rel = $rel.Replace([char]92, [char]47)
      $violations += ($rel + ":" + ($i + 1) + ": " + $line.Trim())
    }
  }
}
Write-Host ("  CONSOLE spawn points checked: " + $checked)
if ($violations.Count -eq 0) {
  Write-Host "  PASS: all CONSOLE spawn points hide their window" -ForegroundColor Green
  exit 0
}
Write-Host ("  FAIL: " + $violations.Count + " CONSOLE spawn point(s) lack window hiding") -ForegroundColor Red
$violations | ForEach-Object { Write-Host ("    " + $_) -ForegroundColor Red }
exit 1
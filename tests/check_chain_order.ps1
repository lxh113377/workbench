# check_chain_order.ps1 -- 序号型产物的「读链」门禁（2026-11-02 round 248 新增，落地 quality-verify ㉔(g)）
#
# 为什么只把「非单调」判红，漏卡/重复只报告：
#   · 非单调 = 卡片在文件里被人为放错顺序，是**当前仍可修复**的真损坏 ⇒ 判红。
#   · 漏卡（某 round 有 CHANGELOG 条目/commit 但没有收口卡）与重复号（开场卡+收口卡配对）都是**历史事实**，
#     事后无法追溯补卡（补卡=造假日史，红线）。所以只打印、不判红。
#   ⇒ 与 r234「不拿形态相似当判据」、r245「语义层不可机判」同源：**能对历史判红的判据一定是坏判据**。
#
# 受理面：本地聚合 + CI repo-gates 双挂（r238 在册教训：CI 不跑 check_all，只挂聚合等于没牙）。
#
# 三态：rc=0 PASS（非单调=0）/ rc=1 FAIL（出现非单调）/ rc=2 UNVERIFIED（无 CHAIN_ 结论行，语义不明不计 PASS）/ rc=3 SKIP（无 node 或工具件）。
#
# 用法: powershell -File tests\check_chain_order.ps1 [-Quiet] [-Root <dir>]
param([switch]$Quiet, [string]$Root)
$ErrorActionPreference = "Stop"
$root = if ($Root) { $Root } else { Split-Path -Parent $PSScriptRoot }
$tool = Join-Path $root "setup\chain_audit.js"
if (-not (Test-Path -LiteralPath $tool)) {
  if (-not $Quiet) { Write-Host "SKIP: 巡检工具不存在（测不了 != 不合格）" -ForegroundColor Yellow }
  exit 3
}
$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if (-not $nodeCmd) {
  if (-not $Quiet) { Write-Host "SKIP: 找不到 node（测不了 != 不合格）" -ForegroundColor Yellow }
  exit 3
}
# ProcessStartInfo 显式起进程（& 绝对路径在 PowerShell 里可能被按文档关联解释，round 183/190 实测）。
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $nodeCmd.Source
$psi.Arguments = [string][char]34 + $tool + [string][char]34
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
try {
  $p = [System.Diagnostics.Process]::Start($psi)
  $so = $p.StandardOutput.ReadToEnd()
  $se = $p.StandardError.ReadToEnd()
  $p.WaitForExit()
  $rc = $p.ExitCode
} catch {
  Write-Host ("UNVERIFIED: 无法启动 node（" + $_.Exception.Message + "）") -ForegroundColor Yellow
  exit 2
}
$all = ($so + $se)
$linesArr = @($all -split [char]10 | Where-Object { $_.Trim() })
# 不只看退出码：必须真看到 CHAIN_ 结论行，否则可能是「什么都没跑出来」的空绿。
$verdict = @($linesArr | Where-Object { $_ -like "*CHAIN_OK*" -or $_ -like "*CHAIN_FAIL*" })
if ($verdict.Count -eq 0) {
  Write-Host "UNVERIFIED: 无 CHAIN_ 结论行（不计 PASS）" -ForegroundColor Yellow
  $linesArr | Select-Object -Last 3 | ForEach-Object { Write-Host ("  " + $_) }
  exit 2
}
if ($rc -ne 0) {
  Write-Host ("FAIL: " + $verdict[0].Trim()) -ForegroundColor Red
  @($linesArr | Where-Object { $_ -like "*非单调*" -and $_.Trim() -ne "非单调=0 （链序正常）" }) | ForEach-Object { Write-Host ("  " + $_.Trim()) -ForegroundColor Red }
  exit 1
}
if (-not $Quiet) {
  Write-Host ("PASS: 序号链无非单调（" + $verdict[0].Trim() + "）") -ForegroundColor Green
  @($linesArr | Where-Object { $_ -like "*MISS round*" -or $_ -like "*跳号*" -or $_ -like "*advisory*" }) | ForEach-Object { Write-Host ("  （报告项，不判红）" + $_.Trim()) -ForegroundColor DarkGray }
}
exit 0

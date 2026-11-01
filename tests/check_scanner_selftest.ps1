# check_scanner_selftest.ps1 -- 长函数扫描工具自带的泛型签名盲区回归（2026-11-01 round 195 新增）。
#
# 为什么单独成一道门禁（而不是只在 check_all 里调 node）：
#   round 194 我把 --self-test 挂进 check_all 第 51 段，随后自查发现 CI **根本不跑 check_all**
#   （ci.yml 注释写明：同机并行会话争抢时它从约 3 分钟拖到 44 分钟未跑完，故 CI 自维护一个
#   $gates 列表逐个调 tests/check_*.ps1）⇒ 挂在 check_all 的段在 CI 没有受理面，本地绿不等于
#   CI 有牙。本仓在册病「判据存在但没挂受理面」的第 N 次变体，这次是我自己造的。
#
# 它守的是什么：setup/long-func-scan.js 的函数签名正则曾漏掉泛型函数（func NewClient[TTx any](
#   函数名后紧跟 [ 而非 (），导致该工具**静默漏报**，我据其错误读数一度误判五轴报告 P2-4 引用
#   不成立。校验工具的盲区会产出「带着证据形状」的错误结论，比没门禁更容易被采信。
#   夹具断言在 long-func-scan.js --self-test 内（纯内存、不读仓库、不依赖 git 状态）：
#   泛型 NewClient 必须被计入 / 其大括号配对长度恰为 7 / 短泛型 Tiny 也进候选 / 普通 Small 仍识别
#   / 非 func 行不误收。改坏 SIG 正则 ⇒ 本门禁立刻红（破坏性负控实测：删泛型段 ⇒ 3 断言翻红 rc=1）。
#
# 三态口径（与全仓一致，round 61 在册事故：把没跑写成 rc=0 会把「未执行」洗成 PASS）：
#   rc=0 PASS —— 夹具全过
#   rc=1 FAIL —— 夹具任一项失败（扫描工具被改坏）
#   rc=2 UNVERIFIED —— node 能跑但输出无 SELFTEST 结论行（语义不明，不计 PASS）
#   rc=3 SKIP —— 找不到 node 或工具件不存在（测不了 ≠ 不合格，也不许判红）
#
# 用法: powershell -File tests\check_scanner_selftest.ps1 [-Quiet]
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$tool = Join-Path $root 'setup\long-func-scan.js'
if (-not (Test-Path -LiteralPath $tool)) {
  if (-not $Quiet) { Write-Host "SKIP: 扫描工具不存在（$tool）" -ForegroundColor Yellow }
  exit 3
}
$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if (-not $nodeCmd) {
  if (-not $Quiet) { Write-Host "SKIP: 找不到 node，夹具无从执行（不代表工具正确）" -ForegroundColor Yellow }
  exit 3
}

# 用 ProcessStartInfo 显式起进程（& $绝对路径 在 PowerShell 里可能被按文档关联解释，
#   round 183/190 实测两撞「Cannot run a document in the middle of a pipeline」）。
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $nodeCmd.Source
$psi.Arguments = '"' + $tool + '" --self-test'
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
$psi.RedirectStandardError = $true
try {
  $p = [System.Diagnostics.Process]::Start($psi)
  $outTxt = $p.StandardOutput.ReadToEnd()
  $errTxt = $p.StandardError.ReadToEnd()
  $p.WaitForExit()
  $rc = $p.ExitCode
} catch {
  Write-Host ("UNVERIFIED: 无法启动 node 进程（" + $_.Exception.Message + "）") -ForegroundColor Yellow
  exit 2
}
$lines = @($outTxt -split [char]10 | Where-Object { $_.Trim() })
if ($rc -ne 0) {
  Write-Host ("FAIL: 扫描工具自检不通过（rc=" + $rc + "）—— SIG 正则或花括号配平被改坏") -ForegroundColor Red
  foreach ($ln in $lines) { Write-Host ("  " + $ln) }
  if ($errTxt.Trim()) { Write-Host ("  stderr: " + $errTxt.Trim()) -ForegroundColor Red }
  exit 1
}
# node 退出码 0 还不够：必须真看到结论行，否则可能是「什么都没跑出来」的空绿。
$verdict = @($lines | Where-Object { $_ -like 'SELFTEST PASS*' -or $_ -like 'SELFTEST FAIL*' })
if ($verdict.Count -eq 0) {
  Write-Host "UNVERIFIED: node 返回 rc=0 但没有 SELFTEST 结论行（语义不明，不计 PASS）" -ForegroundColor Yellow
  exit 2
}
if (-not $Quiet) {
  $n = @($lines | Where-Object { $_ -like 'SELFTEST ok*' }).Count
  Write-Host ("扫描工具自检合规：" + $n + " 条夹具断言全过（锁 round 193 泛型签名盲区）") -ForegroundColor Green
}
exit 0

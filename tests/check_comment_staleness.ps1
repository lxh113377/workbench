# check_comment_staleness.ps1 -- 门禁注释里的「现状型数字」必须带日期或指向门禁自身输出
#   （2026-10-24 round 32 新增）
#
# 为什么需要（round 31 + round 32 两次同型教训）：
#   round 31：round 26 在门禁头注写的机理结论（「CRLF 会进索引」）已过期 ——
#     .gitattributes 从 text=auto 改成 eol=crlf 后，该机理不再成立，而注释还在那儿
#     「教」下一个读到的人一个错误的因果关系。
#   round 32（2026-10-24 实测，历史记录）：check_gofmt.ps1 头注曾写「35 条债务里
#   33 条已失效，只剩 2 条 live」（首版 2026-10-22），而当时实际 26 条 live ——
#   读注释的人会以为债务清完了。现已改为带日期的历史记录 + 指向门禁自身输出。
#   两次的共同形态：**把一个会随代码演进的数字/结论，写成不带日期的现状断言**。
#
# 判据（窄面，只抓可机器判定的那一类，避免做成噪声源）：
#   受跟踪 tests/check_*.ps1 的注释里，出现「只剩 N 条/个」「共 N 条/个」这类
#   **现状型数量断言**时，同一行或相邻 2 行内必须含日期（YYYY-MM-DD）或
#   「历史记录/会过期/以门禁输出为准」这类免责标记。
#
# 为什么不做得更广（如检测所有数字）：本仓工具链的教训是「模糊语义检查制造噪声，
#   门禁就会被删」。故只抓「只剩/共 N」这一个高方差、高误导价值的窄 pattern。
#
# 用法: powershell -File tests\check_comment_staleness.ps1 [-Quiet]
# 退出码: 0=合规 / 1=有未标日期的现状型数字 / 2=UNVERIFIED
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host "UNVERIFIED: 找不到 git" -ForegroundColor Red
  exit 2
}

# 覆盖「受跟踪 + 已暂存」两类：
#   - git ls-files：已在库的（存量）
#   - git diff --cached --name-only：即将提交的（增量）
# 2026-10-24 实测教训：首版只查 ls-files，于是用**新文件**做变异时 MISSED ——
# 而新文件正是最可能带坏注释的（刚写的）。与 check_ps1_eol 首轮变异 MISSED 同源：
# **只覆盖存量、不覆盖增量，等于放过最可能出问题的那批**。
$relFiles = @(& $gitExe -C $root ls-files 'tests/check_*.ps1')
if ($LASTEXITCODE -ne 0) {
  Write-Host "UNVERIFIED: git ls-files 失败" -ForegroundColor Red
  exit 2
}
$staged = @(& $gitExe -C $root diff --cached --name-only --diff-filter=ACM -- 'tests/check_*.ps1')
if ($LASTEXITCODE -eq 0 -and $staged) {
  foreach ($s in $staged) {
    $sr = $s.Trim('"')
    if ($sr -and ($relFiles -notcontains $sr)) { $relFiles += $sr }
  }
}

# 现状型数量断言 pattern：只剩/仅有/共 N 条|个|处（条/个/处 是本仓计债务/文件/违规的单位）
$claimPattern = '(只剩|仅有|共)\s*\d+\s*(条|个|处)'
# 免责标记：有日期、或明说会过期/以门禁输出为准
$exemptPattern = '\d{4}-\d{2}-\d{2}|历史记录|会过期|以门禁|以那行为准|首版时|当时'

$bad = @()
foreach ($rel in $relFiles) {
  $rel = $rel.Trim('"')
  if (-not $rel) { continue }
  $full = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $full)) { continue }
  $lines = @([IO.File]::ReadAllLines($full))
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $s = $lines[$i]
    if (-not $s.Trim().StartsWith('#')) { continue }
    if ($s -notmatch $claimPattern) { continue }
    # 命中：检查本行 + 相邻 2 行是否有免责标记
    $lo = [Math]::Max(0, $i - 2)
    $hi = [Math]::Min($lines.Count - 1, $i + 2)
    $window = ($lines[$lo..$hi] -join " `n")
    if ($window -match $exemptPattern) { continue }
    $bad += ($rel + ':' + ($i + 1) + '  ' + $s.Trim())
  }
}

if ($bad.Count -gt 0) {
  Write-Host ("注释时效门禁未通过（" + $bad.Count + " 处现状型数字缺日期/免责）：") -ForegroundColor Red
  foreach ($b in $bad) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "  形态：把会随代码演进的数字写成不带日期的现状断言（round 31/32 两次同型教训）。" -ForegroundColor Yellow
  Write-Host "  修法：补上日期（如「2026-10-24 实测」），或改为「历史记录：...」并指向门禁自身输出。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host "注释时效合规：无未标日期的现状型数字断言" -ForegroundColor Green }
exit 0
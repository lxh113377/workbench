# check_agg_bypass.ps1 -- 聚合器旁路判决判据（防「未验状态被静默核销」的第三种形态）
#
# 为什么存在（2026-10-26 round 226 实测；语法闸与运行时计数器都抓不到，靠逐段配对才发现）：
#   tests/check_all.ps1 的「覆盖安全」段在 Section() 之外另写了 if/else，else 分支自己
#   Write-Host 一句段名（==> …）与一句 SKIP。那是完全合法的 PowerShell —— check_ps1_syntax
#   对旁路形态与修复后形态都判 rc=0；而它不经段函数 ⇒ 计数器不累加 ⇒ 汇总打成
#   「pass-with-gaps (1 段证据不足)」，实际有 2 段没验（另一段才是那 1 段）。
#   未验状态被静默核销：比改返回值更隐蔽，因为它**根本不进返回值通道**，只在输出里
#   留一句看着很诚实的 SKIP。
#
# 不变式（本件判据）：段名行（==>）与判决行（PASS/FAIL/SKIP/UNVERIFIED 带 (rc=)）只能由
#   记账函数自己打印。凡在记账函数体之外出现即为旁路 —— 除非同一行还带记账动作
#   （置 $global:LASTEXITCODE 或 return），那是在把控制权交回统一判决路径，属合法写法。
#   为什么不做成「跑一遍数段名与判决是否配对」：那要跑完整套（实测 ~119s）且只能事后抓；
#   静态扫描毫秒级、能进提交前习惯。运行时计数器的数法也不行（实测）：旁路在段函数外打印，
#   不会让计数失衡，只让「段名行数」与「判决行数」失衡 —— 所以只能扫源码或比输出。
#
# 用法: powershell -File tests\check_agg_bypass.ps1 [-Quiet]
# 退出码: 0=无旁路 / 1=有旁路（逐条点名）/ 2=UNVERIFIED（聚合器或其记账函数定位不到）
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }

# 被管的聚合器：本仓当前只有总门禁一个。新增聚合器要把文件加进这里，否则本闸对它是盲的。
$Aggregators = @('tests\check_all.ps1')

# 判决/段名行特征：要求 ==> 或「判决词 + (rc=」，避免把说明性文字判成判决
$VerdictPat = '^\s*(?:Write-Host|Write-Output)\b.*(?:==>|\b(?:PASS|FAIL|SKIP|UNVERIFIED)\b[^\r\n]*\(rc=)'
$fnPat = '^\s*function\s+Section\b'

$hits = @()
$checkedFiles = 0
$unresolved = @()
foreach ($rel in $Aggregators) {
  $abs = Join-Path $Root $rel
  if (-not (Test-Path -LiteralPath $abs)) { $unresolved += ('文件不存在: ' + $rel); continue }
  $lines = (([IO.File]::ReadAllText($abs)) -split ('\r?\n'))
  # 标出记账函数体的行区间（函数定义行 → 其闭合花括号）
  $inFn = New-Object bool[] $lines.Count
  $found = $false
  for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -match $fnPat) {
      $found = $true
      $d = 0
      for ($j = $i; $j -lt $lines.Count; $j++) {
        $inFn[$j] = $true
        $d += ([regex]::Matches($lines[$j], '\{')).Count
        $d -= ([regex]::Matches($lines[$j], '\}')).Count
        if ($j -gt $i -and $d -le 0) { break }
      }
      break
    }
  }
  if (-not $found) { $unresolved += ($rel + ' 里找不到记账函数（无法定位合法打印处）'); continue }
  $checkedFiles++
  for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($inFn[$i]) { continue }
    $ln = $lines[$i]
    $t = $ln.Trim()
    if ($t -eq '' -or $t.StartsWith('#')) { continue }
    if ($ln -notmatch $VerdictPat) { continue }
    if ($ln -match '\$global:LASTEXITCODE|\breturn\b') { continue }
    $hits += ($rel + ':' + ($i + 1) + '  ' + $t.Substring(0, [Math]::Min(96, $t.Length)))
  }
}

if ($unresolved.Count -gt 0) {
  Write-Host ('UNVERIFIED 判据无法完整定位（' + ($unresolved -join '；') + '）') -ForegroundColor Yellow
  exit 2
}
if ($hits.Count -gt 0) {
  Write-Host ('聚合器旁路判决：' + $hits.Count + ' 处（在记账函数外打印段名/判决 ⇒ 缺口不进汇总账）') -ForegroundColor Red
  foreach ($h in $hits) { Write-Host ('  - ' + $h) -ForegroundColor Red }
  Write-Host '修法：删掉外面的重复判断，让段函数体内已有的守卫统一判决与计数；别只补一句计数——留两份迟早又有一份绕过。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ('聚合器旁路判据通过：检查 ' + $checkedFiles + ' 个聚合器，段名与判决打印只出现在记账函数内') -ForegroundColor Green }
exit 0


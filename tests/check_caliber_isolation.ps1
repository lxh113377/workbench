# check_caliber_isolation.ps1 -- 内存口径隔离：判据不许把 RSS 与 Go heap 混在同一比较里（round 64 新增）
#
# 为什么需要：
#   本仓同时存在两个内存口径，且比值不是常数（docs/benchmark/性能基线口径.md §9 实测）：
#     * OS RSS（Get-Process WorkingSet64）：check_resident_longrun / check_perf / check_queue_size /
#       instance_guard / tests/e2e.ps1 用；128MB 上界是拿 RSS 判的。
#     * Go heap（runtime.MemStats.HeapInuse）：metrics.csv 的 heap_mb 列、--soak/A7 趋势判定、
#       自愈 cfg.MemLimitMB（默认 700）用。
#   实测比值带 1.5x（重载 752MB vs 501MB）到 3.83x（空闲 64.4MB vs 16.83MB），随负载漂移。
#   所以「RSS 超 limit 就判泄漏」「heap 没到 limit 所以 RSS 没事」这类推断全是错的。
#   §9 已把纪律写进文档，但没有判据守它 —— 文档纪律靠人记得，正是本仓反复踩的形态。
#
# 判据（两级，round 64 变异测试后定的形态）：
#   L1 同行混用：同一代码行同时出现 RSS 侧特征与 heap 侧特征 => 判红。
#   L2 跨行混用：同一文件里既有 RSS 侧特征、又有 heap 侧特征，且存在 RSS 参与比较的行 => 判红。
#   L2 必要的原因：把比较拆成三行（$rss = ...WorkingSet64 / $limit = 700 # MemLimitMB /
#   if ($rss -gt $limit)）即可绕过同行检测 —— 真正的跨口径比较发生在变量传播之后，
#   而变量名可以毫无口径线索。真实仓库该形态 0 命中，故不会误报。
#
# 两处实现注记（都是 round 64 实测踩出来的，写下来防再犯）：
#   1) 本文件的正则/字面量一律不用反斜杠序列（如 \b、\s、\(）。write 通道会把 JS 字符串里的
#      反斜杠双重转义落盘，PS 单引号内的 \\b 实际是「字面反斜杠 + b」，正则永不命中 ——
#      round 64 因此连踩两次（门禁写完自己失效却报合规）。判据用字面片段（如 'WorkingSet64'、
#      'if'、'-gt'）就够，不需要锚点。
#   2) 纯字符串包含匹配（-like "*memLimitMB*"）不算口径比较：那只是在 CLI 输出里找关键字。
#      round 64 首版漏了这条，把 tests/e2e.ps1 第 215 行误判成 heap 特征（该文件本身是正确的：
#      它用 RSS 判 residentMBMax，与基线文档同一口径）。
#
# 已知合法形态（显式登记，不算违规）：
#   * 注释/文档行（# // * 开头）—— 是说明不是比较；
#   * 本门禁自己（check_caliber_isolation.ps1）的特征串；
#   * 纯字符串包含匹配（见注记 2）。
#
# 用法: powershell -File tests/check_caliber_isolation.ps1 [-Quiet] [-RepoPath p]
# 退出码: 0=无跨口径比较 / 1=检出 / 2=UNVERIFIED
param(
  [switch]$Quiet,
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }
if (-not (Test-Path -LiteralPath $root)) { Write-Host ('UNVERIFIED: 目录不存在: ' + $root) -ForegroundColor Red; exit 2 }

$bs = [string][char]92
# 特征串：只用字面片段，不用带反斜杠的正则（见注记 1）
$rssPat = 'WorkingSet64|Get-Process|RSS|工作集|私有工作集'
$heapPat = 'HeapInuse|HeapAlloc|heap_mb|HeapMB|MemLimitMB|memLimitMB|MemStats|ReadMemStats'

$files = @()
foreach ($rel in @('tests', 'setup')) {
  $d = Join-Path $root ($rel.Replace('/', $bs))
  if (Test-Path -LiteralPath $d) {
    $files += @(Get-ChildItem -LiteralPath $d -Filter *.ps1 -File -ErrorAction SilentlyContinue)
  }
}
$wbDir = Join-Path $root ('workbench' + $bs + 'internal')
if (Test-Path -LiteralPath $wbDir) {
  $files += @(Get-ChildItem -LiteralPath $wbDir -Filter *.go -File -Recurse -ErrorAction SilentlyContinue)
}
if ($files.Count -eq 0) { Write-Host 'UNVERIFIED: 未找到任何待扫文件' -ForegroundColor Red; exit 2 }

# 取一个文件的「代码行」（去空行与注释行），并剔除纯字符串包含匹配行（注记 2）
function Get-CodeLines([string]$path) {
  $out = @()
  foreach ($ln in ([IO.File]::ReadAllText($path) -split [char]10)) {
    $t = $ln.Trim()
    if ($t -eq '') { continue }
    if ($t.StartsWith('#') -or $t.StartsWith('//') -or $t.StartsWith('*')) { continue }
    if ($t.Contains('-like "') -and $t.Contains('*')) { continue }
    if ($t.Contains('-notlike "') -and $t.Contains('*')) { continue }
    $out += $t
  }
  return ,$out
}

# 判断比较行某一侧的操作数属于哪个口径。
# 三种形态：直接含特征串（如 $proc.WorkingSet64 / cfg.MemLimitMB）=> 该口径；
# 是已登记的变量（$rss / $limit）=> 查 varCaliber；否则 unknown（不判）。
function Get-SideCaliber([string]$side, $varCaliber) {
  $s = $side.Trim()
  if ($s -eq '') { return 'unknown' }
  if ($s -match $rssPat) { return 'rss' }
  if ($s -match $heapPat) { return 'heap' }
  $m = [regex]::Match($s, '\$([A-Za-z_][A-Za-z0-9_]*)')
  if ($m.Success) {
    $vName = $m.Groups[1].Value
    if ($varCaliber.ContainsKey($vName)) { return $varCaliber[$vName] }
  }
  return 'unknown'
}

# L1：同行混用
$bad = @()
foreach ($f in $files) {
  if ($f.Name -eq 'check_caliber_isolation.ps1') { continue }
  $codeLines = Get-CodeLines $f.FullName
  foreach ($t in $codeLines) {
    $hasRss = ($t -match $rssPat)
    $hasHeap = ($t -match $heapPat)
    if ($hasRss -and $hasHeap) {
      $bad += ($f.FullName.Substring($root.Length + 1) + ':' + $t)
    }
  }
}

# L2：比较表达式两侧分属不同口径（变量传播型，round 64 二次收窄）
# 首版 L2 是「文件级两侧特征共存 + 有 RSS 比较行」，实测把 tests/e2e.ps1 误判：
#   该文件的 $budG.budget.memLimitMB -gt 0 是**断言自愈配置项存在**（与 RSS 判定各自独立、都正确），
#   却被算成 heap 特征，再与 RSS 比较行组合成「跨行混用」。
# 根因：memLimitMB 这个字段名同时出现在两个合法场景（自愈配置检查 / 跨口径比较），
# 仅凭名字区分不了。所以 L2 必须**落在比较表达式本身**：把比较行按运算符切成左右两侧，
# 两侧分别查口径特征 —— 只有「一边 RSS、一边 heap」才是真正的跨口径比较。
$cross = @()
foreach ($f in $files) {
  if ($f.Name -eq 'check_caliber_isolation.ps1') { continue }
  $codeLines = Get-CodeLines $f.FullName
  # 先收集本文件的变量定义：$x = <含口径特征的表达式> => 记录 x 的口径
  $varCaliber = @{}
  foreach ($cl in $codeLines) {
    $m = [regex]::Match($cl, '^\$([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$')
    if (-not $m.Success) { continue }
    $vName = $m.Groups[1].Value
    $vExpr = $m.Groups[2].Value
    $exprHasRss = ($vExpr -match $rssPat)
    $exprHasHeap = ($vExpr -match $heapPat)
    if ($exprHasRss -and -not $exprHasHeap) { $varCaliber[$vName] = 'rss' }
    elseif ($exprHasHeap -and -not $exprHasRss) { $varCaliber[$vName] = 'heap' }
  }
  # 再查比较行：左右两侧口径不同 => 跨口径
  foreach ($cl in $codeLines) {
    if (-not ($cl.Contains('if ') -or $cl.Contains('while '))) { continue }
    $ops = @('-gt', '-lt', '-ge', '-le', '-eq', '-ne')
    $found = $false
    foreach ($op in $ops) {
      $oi = $cl.IndexOf($op)
      if ($oi -lt 0) { continue }
      $left = $cl.Substring(0, $oi)
      $right = $cl.Substring($oi + $op.Length)
      $lCal = Get-SideCaliber $left $varCaliber
      $rCal = Get-SideCaliber $right $varCaliber
      if (($lCal -eq 'rss' -and $rCal -eq 'heap') -or ($lCal -eq 'heap' -and $rCal -eq 'rss')) {
        $found = $true
        $cross += ($f.FullName.Substring($root.Length + 1) + '  比较行: ' + $cl)
        break
      }
    }
    if ($found) { break }
  }
}

if (($bad.Count -gt 0) -or ($cross.Count -gt 0)) {
  Write-Host ('口径隔离门禁未通过：同行混用 ' + $bad.Count + ' 处 + 跨行混用 ' + $cross.Count + ' 个文件') -ForegroundColor Red
  foreach ($x in $bad) { Write-Host ('  [同行] ' + $x) -ForegroundColor Red }
  foreach ($x in $cross) { Write-Host ('  [跨行] ' + $x) -ForegroundColor Red }
  Write-Host '  两个口径的比值不是常数（实测带 1.5x ~ 3.83x，随负载漂移）：' -ForegroundColor Yellow
  Write-Host '    RSS   = Get-Process WorkingSet64（check_resident_longrun 判 128MB 上界用它）；' -ForegroundColor Yellow
  Write-Host '    heap  = runtime.MemStats.HeapInuse（metrics.csv heap_mb / A7 趋势 / MemLimitMB=700 用它）。' -ForegroundColor Yellow
  Write-Host '  修法：拆成两次独立判定，各自用自己的口径与上界；要换算必须给出实测比值及其适用条件。' -ForegroundColor Yellow
  Write-Host '  依据：docs/benchmark/性能基线口径.md §9「跨口径隔离」。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('口径隔离合规：扫描 ' + $files.Count + ' 个文件，无 RSS/heap 混用（两口径各自独立判定）') -ForegroundColor Green
}
exit 0

# check_dead_css_tokens.ps1 -- CSS 设计令牌不得「定义了却没人用」（round 93 新增）
#
# 为什么需要（2026-10-24 round 93 实测）：
#   assets_css.go 的设计系统在 :root 里定义了 **56 个自定义属性**，含完整字阶（8 档 --fs-*）
#   与间距（7 档 --sp-*）。但实测其中 **20 个从未被 var() 引用过**（全包仅定义处出现 1 次），
#   同时实际排版用的是 **78 处硬编码 font-size + 36 处硬编码 margin/padding**。
#   即：**设计系统名义存在、实际失效** —— 读代码的人以为改令牌就能全局换字阶，
#   改了却发现毫无效果（因为没人用它）。这比没有设计系统更坏：它给出虚假的抽象。
#
# 判据（窄、可证伪）：
#   扫 assets_css.go（及 web 包其它 .go）里 :root 定义的 --xxx，若整个 web 包的**非注释代码**
#   中该令牌名的出现次数 <= 1（即只有定义处那一行），判红。
#   豁免：
#     * --xxx-rgb 形态（如 --warn-rgb）：它们是配套通道，可能与主令牌同生同死，
#       但其价值在 rgb(var(--x-rgb), .5) 这类用法里 —— 若主令牌活着它就该活着，
#       故只对主令牌判、rgb 通道在主令牌活着时一并要求。
#     * 刻意保留的令牌（有注释说明「预留给 X」）—— 用 --keep- 前缀显式登记。
#
# 为什么不做「把所有硬编码换成令牌」的判据：那会一次要求改 114 处，
#   违反「步子不要太大逐点攻破」（objective 原话）。本门禁只**止住腐烂**
#   （不许新增死令牌），存量 20 个作为已登记债务逐条打印、不计 rc ——
#   与 gofmt-debt / spawn_silent 已知缺陷豁免同构。
#
# 用法: powershell -File tests/check_dead_css_tokens.ps1 [-Quiet]
# 退出码: 0=无新增死令牌 / 1=有新死令牌 / 2=UNVERIFIED
param([switch]$Quiet)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$webDir = Join-Path $root ('workbench' + [string][char]92 + 'internal' + [string][char]92 + 'web')
if (-not (Test-Path -LiteralPath $webDir)) { Write-Host ('UNVERIFIED: 目录不存在 ' + $webDir) -ForegroundColor Red; exit 2 }

$cssPath = Join-Path $webDir 'assets_css.go'
if (-not (Test-Path -LiteralPath $cssPath)) { Write-Host 'UNVERIFIED: 缺 assets_css.go' -ForegroundColor Red; exit 2 }

# 收集 web 包全部 .go 的非注释代码（一个文件一次读，避免重复 IO）
$corpus = ''
$goFiles = @(Get-ChildItem -LiteralPath $webDir -Filter '*.go' -File -ErrorAction SilentlyContinue)
foreach ($gf in $goFiles) {
  foreach ($ln in ([IO.File]::ReadAllLines($gf.FullName))) {
    $s = $ln.Trim()
    if ($s -eq '' -or $s.StartsWith('//') -or $s.StartsWith('*') -or $s.StartsWith('/*')) { continue }
    $corpus += $ln + [string][char]10
  }
}
if ($corpus.Length -eq 0) { Write-Host 'UNVERIFIED: web 包无有效代码' -ForegroundColor Red; exit 2 }

# 从 :root 定义行抽令牌名（--xxx: 形态）
$cssLines = @([IO.File]::ReadAllLines($cssPath))
$inRoot = $false
$tokens = @()
foreach ($ln in $cssLines) {
  $s = $ln.Trim()
  if ($s -match '^:root') { $inRoot = $true }
  if (-not $inRoot) { continue }
  if ($s -match '^}' -or $s -match 'root\s*\{') { if ($s -match '^}') { break } }
  foreach ($m in [regex]::Matches($s, '(--[a-z0-9-]+)\s*:')) { $tokens += $m.Groups[1].Value }
  if ($s -match '^\}') { break }
}
$tokens = @($tokens | Sort-Object -Unique)
if ($tokens.Count -eq 0) { Write-Host 'UNVERIFIED: 未从 assets_css.go 解析到任何令牌' -ForegroundColor Red; exit 2 }

# 逐个统计在整个语料里的出现次数
$dead = @()
$keep = @()
foreach ($tk in $tokens) {
  # 豁免：刻意保留（--keep- 前缀显式登记）
  if ($tk -like '--keep-*') { $keep += $tk; continue }
  $n = ([regex]::Matches($corpus, [regex]::Escape($tk))).Count
  if ($n -le 1) { $dead += ($tk + '  出现 ' + $n + ' 次（仅定义处）') }
}

# ---- 基线机制（与 gofmt-debt 同构：存量登记、增量判红）----
# tests/css-token-debt.txt 每行一个令牌名（# 开头为注释）。
# 语义：登记在册的死令牌**逐条打印**（KNOWN 行）但不计入 rc；
# 清掉一条（把令牌用起来或删掉定义）后从文件里删掉该行，门禁自动收紧。
# 为什么需要基线：存量 20 个死令牌（字阶/间距族）是历史债务，
# 一上来就判红会让门禁被整体绕过（本仓在册教训）；但**不许静默放过**，
# 所以每次运行都打印「已登记 N 条未清」。
$debtPath = Join-Path $root 'tests\css-token-debt.txt'
$debt = @()
if (Test-Path -LiteralPath $debtPath) {
  foreach ($l in ([IO.File]::ReadAllLines($debtPath))) {
    $tt = $l.Trim()
    if ($tt -eq '' -or $tt.StartsWith('#')) { continue }
    $debt += $tt
  }
}
$newDead = @($dead | Where-Object { $dn = ($_ -split '  ')[0]; $debt -notcontains $dn })
$knownDead = @($dead | Where-Object { $dn = ($_ -split '  ')[0]; $debt -contains $dn })

foreach ($kd in $knownDead) {
  Write-Host ('  KNOWN ' + $kd + '（登记于 tests/css-token-debt.txt，未清）') -ForegroundColor DarkGray
}

if ($newDead.Count -gt 0) {
  Write-Host ('死令牌门禁未通过：' + $newDead.Count + ' 个**新**死令牌（未登记基线）') -ForegroundColor Red
  foreach ($d in $newDead) { Write-Host ('  - ' + $d) -ForegroundColor Red }
  Write-Host '  危害：设计系统名义存在实际失效 —— 改令牌毫无效果，虚假的抽象比没有抽象更坏。' -ForegroundColor Yellow
  Write-Host '  修法（二选一）：① 把该令牌用起来（var(--xxx) 替换对应硬编码）；② 删掉定义。' -ForegroundColor Yellow
  Write-Host '  若是刻意保留（预留给某功能），请在 tests/css-token-debt.txt 登记并注明理由。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('死令牌门禁通过：' + $tokens.Count + ' 个令牌全部有实际使用（其中 ' + $keep.Count + ' 个为显式保留）') -ForegroundColor Green
}
exit 0
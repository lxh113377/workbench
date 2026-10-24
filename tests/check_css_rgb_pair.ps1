# check_css_rgb_pair.ps1 -- CSS 颜色令牌的两条不变量（round 98 新增）
#
# 为什么需要（2026-10-24 实测，objective「设计可扩展的架构」的直接落点）：
#   :root 里同一颜色存了两份字面量 --accent:#22d3ee 与 --accent-rgb:34,211,238（warn/danger/accent
#   三对都是这么写的，round 98 之前无机制保证同步）。且更隐蔽的一类缺陷是「绕过」：
#   实测抓到两例裸三元组正好等于已定义令牌色 ——
#     L437 background:rgba(4,7,13,.88)  而 --bg:#04070d 就是 4,7,13
#     L91  rgba(167,139,250,.055)      而 --accent-3:#a78bfa 就是 167,139,250
#   两处本轮已收编（加 --bg-rgb / --accent-3-rgb 通道并换成 var()）。
#   危害与 round 93 的死令牌同族：**虚假的抽象** —— 读代码的人以为改 --bg 就能换底色，
#   改了却发现顶栏没变（因为它绕过令牌写死了）。这类缺陷**只靠人读代码发现，不可扩展**，
#   所以必须机器判。
#
# 判据（两条，都是可证伪的客观事实）：
#   L1 配对不漂移：每个 --x-rgb 通道必须与同名 --x 的 hex 值换算一致（当前 4 对，改一边忘另一边即判红）。
#   L2 不许绕过：:root 之外不得出现「裸 rgb()/rgba() 三元组恰好等于某已定义 hex 令牌的色」，
#      也不得重复出现已定义的 hex 字面量。要么用 var(--x)，要么用 var(--x-rgb)。
#
# 实现要点（吸取本仓在册教训）：
#   * **不用含反斜杠的正则**：本仓实测 write/exec 通道会吃掉反斜杠（round 89 第 7 次踩），
#     故全程逐字符扫描 + IndexOf，字符类用字符串 Contains 判断。
#   * **必须跳过 CSS 注释**：L1361 是说明文字「打印预览里 body 仍是 rgb(4,7,13) 深底」——
#     首版没跳注释就把它当缺陷（判据误报，round 64 家族）。注释里的值是在描述现状，不是绕过。
#
# 用法: powershell -File tests/check_css_rgb_pair.ps1 [-Quiet]
# 退出码: 0=两条不变量成立 / 1=违反 / 2=UNVERIFIED（找不到文件或解析不出 :root）
param([switch]$Quiet)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$cssPath = Join-Path $root ('workbench' + [string][char]92 + 'internal' + [string][char]92 + 'web' + [string][char]92 + 'assets_css.go')
if (-not (Test-Path -LiteralPath $cssPath)) { Write-Host 'UNVERIFIED: 找不到 assets_css.go' -ForegroundColor Red; exit 2 }
$lines = @([IO.File]::ReadAllLines($cssPath))
if ($lines.Count -eq 0) { Write-Host 'UNVERIFIED: 文件为空' -ForegroundColor Red; exit 2 }

$DIGITS = '0123456789'
$HEXD = '0123456789abcdefABCDEF'

function Get-CssCodeLines([object[]]$src) {
  # 返回 @{n=行号; s=去掉 CSS 注释后的代码部分}，跳过整行注释与块注释内部
  $out = @()
  $inBlock = $false
  for ($i = 0; $i -lt $src.Count; $i++) {
    $raw = [string]$src[$i]
    $s = $raw
    if ($inBlock) {
      $close = $s.IndexOf('*/')
      if ($close -lt 0) { continue }
      $s = $s.Substring($close + 2)
      $inBlock = $false
    }
    while ($true) {
      $open = $s.IndexOf('/*')
      if ($open -lt 0) { break }
      $keep = $s.Substring(0, $open)
      $rest = $s.Substring($open + 2)
      $close2 = $rest.IndexOf('*/')
      if ($close2 -lt 0) { $inBlock = $true; $s = $keep; break }
      $s = $keep + $rest.Substring($close2 + 2)
    }
    $t = $s.Trim()
    if ($t -eq '') { continue }
    $out += (@{ n = ($i + 1); s = $t })
  }
  return ,$out
}

# ---- 定位 :root 块 ----
$iRoot = -1
for ($i = 0; $i -lt $lines.Count; $i++) { if ($lines[$i].Contains(':root')) { $iRoot = $i; break } }
if ($iRoot -lt 0) { Write-Host 'UNVERIFIED: 找不到 :root 块' -ForegroundColor Red; exit 2 }
$depth = 0; $iEnd = -1
for ($i = $iRoot; $i -lt $lines.Count; $i++) {
  foreach ($ch in $lines[$i].ToCharArray()) {
    if ($ch -eq '{') { $depth++ }
    elseif ($ch -eq '}') { $depth--; if ($depth -eq 0) { $iEnd = $i; break } }
  }
  if ($iEnd -ge 0) { break }
}
if ($iEnd -lt 0) { Write-Host 'UNVERIFIED: :root 块括号不配平' -ForegroundColor Red; exit 2 }

# ---- 解析 :root 里的 hex 与 rgb 通道 ----
$hexMap = @{}
$rgbMap = @{}
for ($i = $iRoot; $i -le $iEnd; $i++) {
  $code = ($lines[$i] -replace '^.*:root', '')
  $parts = @($code -split ';')
  foreach ($seg in $parts) {
    $colon = $seg.IndexOf(':')
    if ($colon -lt 0) { continue }
    $name = $seg.Substring(0, $colon).Trim()
    $val = $seg.Substring($colon + 1).Trim()
    if (-not $name.StartsWith('--')) { continue }
    $tok = $name.Substring(2)
    if ($val.StartsWith('#') -and $val.Length -ge 7 -and -not $hexMap.ContainsKey($tok)) {
      $hx = $val.Substring(1, 6)
      $ok = $true
      foreach ($ch in $hx.ToCharArray()) { if (-not $HEXD.Contains([string]$ch)) { $ok = $false; break } }
      if ($ok) { $hexMap[$tok] = ('#' + $hx.ToLowerInvariant()) }
    }
    if ($tok.EndsWith('-rgb') -and -not $rgbMap.ContainsKey($tok.Substring(0, $tok.Length - 4))) {
      $base = $tok.Substring(0, $tok.Length - 4)
      $clean = $val.Replace(' ', '')
      if ($clean -match '^[0-9]+(,[0-9]+)+$') { $rgbMap[$base] = $clean }
    }
  }
}
if ($hexMap.Count -eq 0) { Write-Host 'UNVERIFIED: :root 里没解析出任何 hex 令牌' -ForegroundColor Red; exit 2 }

# hex -> 三元组
function HexToTriple([string]$hex) {
  $h = $hex.Substring(1)
  $r = [Convert]::ToInt32($h.Substring(0, 2), 16)
  $g = [Convert]::ToInt32($h.Substring(2, 2), 16)
  $b = [Convert]::ToInt32($h.Substring(4, 2), 16)
  return ($r.ToString() + ',' + $g.ToString() + ',' + $b.ToString())
}
$tripleToName = @{}
foreach ($k in $hexMap.Keys) { $tt = HexToTriple $hexMap[$k]; if (-not $tripleToName.ContainsKey($tt)) { $tripleToName[$tt] = $k } }

$bad = @()

# ---- L1 配对不得漂移 ----
foreach ($base in $rgbMap.Keys) {
  if (-not $hexMap.ContainsKey($base)) { continue }
  $expect = HexToTriple $hexMap[$base]
  if ($expect -ne $rgbMap[$base]) {
    $bad += ('L1 配对漂移：--' + $base + ' = ' + $hexMap[$base] + ' 应为 ' + $expect + '，但 --' + $base + '-rgb 写的是 ' + $rgbMap[$base])
  }
}

# ---- L2 :root 之外不得绕过令牌 ----
$code = Get-CssCodeLines $lines
foreach ($row in $code) {
  if ($row.n -ge ($iRoot + 1) -and $row.n -le ($iEnd + 1)) { continue }
  $s = $row.s
  # 裸 rgb()/rgba(r,g,b...)：'(' 后紧跟数字
  # 【round 98 变异测试抓出的漏检】首版只搜 'rgb('，但本仓绝大多数写的是 'rgba('
  #   —— rgb 后面跟的是 a 而非 (，IndexOf('rgb(') 永远不命中。而我本轮刚收编的
  #   .topbar-shell 那处正是 rgba(4,7,13,.88)：判据对本仓**最常见形态完全失效**，
  #   变异注入 rgba(34,211,238,.5) 时 rc=0（漏检）。
  #   教训：变异用例要取自**真实缺陷样本**（本仓现成的两处绕过都是 rgba 形态），
  #   随手造的样本会把判据的盲区一起带进测试。
  # 现改为：搜 'rgb' → 跳过可选 a / 空格 → 必须是 '(' → 才当函数调用处理。
  $pos = 0
  while ($true) {
    $ri = $s.IndexOf('rgb', $pos)
    if ($ri -lt 0) { break }
    $after = $ri + 3
    if ($after -lt $s.Length -and ($s[$after] -eq 'a' -or $s[$after] -eq 'A')) { $after++ }
    while ($after -lt $s.Length -and $s[$after] -eq ' ') { $after++ }
    if ($after -ge $s.Length -or $s[$after] -ne '(') { $pos = $ri + 3; continue }
    $after++
    while ($after -lt $s.Length -and $s[$after] -eq ' ') { $after++ }
    if ($DIGITS.Contains([string]$s[$after])) {
      $j = $after; $buf = ''
      while ($j -lt $s.Length -and ($DIGITS.Contains([string]$s[$j]) -or $s[$j] -eq ',' -or $s[$j] -eq ' ')) { $buf += $s[$j]; $j++ }
      $nums = @($buf.Replace(' ', '') -split ',' | Where-Object { $_ -ne '' })
      if ($nums.Count -ge 3) {
        $tt = ($nums[0] + ',' + $nums[1] + ',' + $nums[2])
        if ($tripleToName.ContainsKey($tt)) {
          $bad += ('L2 绕过令牌：第 ' + $row.n + ' 行裸 rgb(' + $tt + '...) 等于 --' + $tripleToName[$tt] + ' 的色值 —— 改令牌不生效（虚假的抽象）')
        }
      }
    }
    $pos = $ri + 4
  }
  # 重复已定义 hex 字面量
  foreach ($hk in $hexMap.Keys) {
    if ($s.IndexOf($hexMap[$hk], [StringComparison]::OrdinalIgnoreCase) -ge 0) {
      $bad += ('L2 绕过令牌：第 ' + $row.n + ' 行重复写了 ' + $hexMap[$hk] + '（应写 var(--' + $hk + ')）')
    }
  }
}

$bad = @($bad | Sort-Object -Unique)
if ($bad.Count -gt 0) {
  Write-Host ('CSS 颜色令牌门禁未通过：' + $bad.Count + ' 处') -ForegroundColor Red
  foreach ($b in $bad) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  修法：改用 var(--令牌)；需要透明度时补 --令牌-rgb 通道并写 rgba(var(--令牌-rgb),a)。' -ForegroundColor Yellow
  Write-Host '  为什么值得拦：同一颜色存两份/写第三份，改一处漏两处 —— 抽象变成骗人的（本仓 round 93 已记过一次）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('CSS 颜色令牌合规：' + $hexMap.Count + ' 个 hex 令牌、' + $rgbMap.Count + ' 个 rgb 通道全部配对一致，:root 外零绕过（注释已跳过）') -ForegroundColor Green
}
exit 0
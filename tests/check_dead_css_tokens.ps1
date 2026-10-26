# check_dead_css_tokens.ps1 -- CSS 死令牌 + 动画名双向对账（round 93 起；round 99 重写判据；round 253 加动画名）
#
# 判据口径的演进（每一步都是实测逼出来的，不是设计时想清楚的）：
#   round 93 首版：数令牌名在语料里的**出现次数**，<=1 判死。
#   round 99 发现两个洞：
#     洞1「注释污染」—— 我在 --accent-3-rgb 定义行写了条注释提到 --accent-3，
#       使它出现次数从 1 变 2，于是脱离死名单。同理 --glow-cyan 出现 3 次（全在注释里）
#       却 var() 引用 0，一直被误算成活的。=> 出现次数 != 使用次数。
#     洞2「配对锚点被误判死」—— --accent-3 是 --accent-3-rgb 的漂移校验锚点（被
#       check_css_rgb_pair 消费），本门禁判它死并建议删，等于两个门禁互相拆台。
#       实测删掉 --accent-3 后 rgb_pair 对该颜色的校验静默消失（vacuous pass）。
#   重写后：只数 **var(--x) 引用**（真正的「用起来」），并剥离 CSS 块注释与 Go 行注释；
#   配对锚点单独豁免计为「承重」而非「在用」，三类数字相加须等于令牌总数。
#
# 基线机制（与 gofmt-debt 同构）：tests/css-token-debt.txt 登记存量死令牌，逐条打印 KNOWN
#   但不计 rc；新增死令牌才判红。锚点不进基线（它不是债）。
#
# 用法: powershell -File tests/check_dead_css_tokens.ps1 [-Quiet]
# 退出码: 0=无新增死令牌且动画名双向对齐 / 1=有新死令牌或未对齐的动画名 / 2=UNVERIFIED（取不到文件或一条 @keyframes 都没解析到）
#
# 单一来源前提（2026-10-24 实测；round 253 复核动画面）：令牌「定义」以 assets_css.go 的 :root 块为准，
#   语料虽扫 web 包全部 .go，但令牌「定义」以 :root 为准。这一前提是成立的前提是 ——
#   生产代码里 CSS :root 块**仅此一处**（已用 ':root{' 精确匹配核实：assets_css.go x1，
#   其余命中都是脚本里的 PowerShell 变量 $root，不是 CSS 块）。若将来新增第二个 :root
#   （如拆出子主题文件），本门禁会漏判那个文件里定义的令牌，须同步扩展解析面。
param([switch]$Quiet)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$cssPath = Join-Path $root ('workbench' + [string][char]92 + 'internal' + [string][char]92 + 'web' + [string][char]92 + 'assets_css.go')
if (-not (Test-Path -LiteralPath $cssPath)) { Write-Host 'UNVERIFIED: 找不到 assets_css.go' -ForegroundColor Red; exit 2 }
$webDir = Split-Path -Parent $cssPath

function Strip-Block([string]$s) {
  # 剥 CSS /* */ 块注释（不用正则反斜杠 —— 本仓 write 通道会吃掉反斜杠，round 89 第 7 次踩）
  $out = New-Object System.Text.StringBuilder
  $rs = $s.ToCharArray()
  $i = 0
  while ($i -lt $rs.Length) {
    if ($i + 1 -lt $rs.Length -and $rs[$i] -eq '/' -and $rs[$i+1] -eq '*') {
      $j = $i + 2
      while ($j + 1 -lt $rs.Length -and -not ($rs[$j] -eq '*' -and $rs[$j+1] -eq '/')) { $j++ }
      $i = $j + 2
      continue
    }
    [void]$out.Append($rs[$i])
    $i++
  }
  return $out.ToString()
}

function Count-VarRef([string]$corpus, [string]$tk) {
  # 数 var(<tk> 后跟 , 或 ) 的次数 —— 只认真正的使用，不认注释里提一嘴
  $needle = 'var(' + $tk
  $n = 0; $idx = 0
  while ($true) {
    $p = $corpus.IndexOf($needle, $idx)
    if ($p -lt 0) { break }
    $after = $p + $needle.Length
    if ($after -lt $corpus.Length) {
      $c = $corpus[$after]
      if ($c -eq ',' -or $c -eq ')') { $n++ }
    }
    $idx = $after
  }
  return $n
}

# 语料：web 包全部 .go，先剥 Go 行注释（//），再剥 CSS 块注释
$corpus = ''
foreach ($gf in (Get-ChildItem -LiteralPath $webDir -Filter '*.go' -File)) {
  $raw = [IO.File]::ReadAllText($gf.FullName)
  $kept = @()
  foreach ($ln in ($raw -split [string][char]10)) {
    if ($ln.Trim().StartsWith('//')) { continue }
    $kept += $ln
  }
  $corpus += (Strip-Block (($kept -join ([string][char]10)))) + [string][char]10
}
if ($corpus.Length -eq 0) { Write-Host 'UNVERIFIED: 语料为空' -ForegroundColor Red; exit 2 }

# 解析 :root：定义令牌 + 锚点（-rgb 通道的基名）
$cssClean = Strip-Block ([IO.File]::ReadAllText($cssPath))
$clines = @($cssClean -split [string][char]10)
$iRoot = -1
for ($i = 0; $i -lt $clines.Count; $i++) { if ($clines[$i].Contains(':root')) { $iRoot = $i; break } }
if ($iRoot -lt 0) { Write-Host 'UNVERIFIED: 找不到 :root' -ForegroundColor Red; exit 2 }
$depth = 0; $iEnd = -1
for ($i = $iRoot; $i -lt $clines.Count; $i++) {
  foreach ($ch in $clines[$i].ToCharArray()) {
    if ($ch -eq '{') { $depth++ }
    elseif ($ch -eq '}') { $depth--; if ($depth -eq 0) { $iEnd = $i; break } }
  }
  if ($iEnd -ge 0) { break }
}
$tokens = @(); $anchors = @()
for ($i = $iRoot; $i -le $iEnd; $i++) {
  foreach ($seg in ($clines[$i] -split ';')) {
    $colon = $seg.IndexOf(':')
    if ($colon -lt 0) { continue }
    $name = $seg.Substring(0, $colon).Trim()
    if (-not $name.StartsWith('--')) { continue }
    $tok = $name.Substring(2)
    if ($tokens -notcontains $name) { $tokens += $name }
    if ($tok.EndsWith('-rgb')) {
      $base = '--' + $tok.Substring(0, $tok.Length - 4)
      if ($anchors -notcontains $base) { $anchors += $base }
    }
  }
}
if ($tokens.Count -eq 0) { Write-Host 'UNVERIFIED: :root 无令牌' -ForegroundColor Red; exit 2 }

$dead = @(); $usedN = 0; $anchorN = 0
foreach ($tk in $tokens) {
  $nr = Count-VarRef $corpus $tk
  if ($nr -gt 0) { $usedN++; continue }
  if ($anchors -contains $tk) { $anchorN++; continue }
  $dead += ($tk + '  var() 引用 0 次')
}

$debtPath = Join-Path $root ('tests' + [string][char]92 + 'css-token-debt.txt')
$debt = @()
if (Test-Path -LiteralPath $debtPath) {
  foreach ($l in ([IO.File]::ReadAllLines($debtPath))) {
    $tt = $l.Trim()
    if ($tt -eq '' -or $tt.StartsWith('#')) { continue }
    $debt += $tt
  }
}
$newDead = @($dead | Where-Object { ($_ -split '  ')[0] -notin $debt })
$knownDead = @($dead | Where-Object { ($_ -split '  ')[0] -in $debt })
$deadKeys = @($dead | ForEach-Object { ($_ -split '  ')[0] })
$staleDead = @($debt | Where-Object { $_ -notin $deadKeys })
foreach ($sd in $staleDead) { Write-Host ('  x 失效豁免：' + $sd + '（已不再死，必须从 css-token-debt.txt 删除）') -ForegroundColor Red }

# 失效检测与 check_gofmt / check_deadcode 同构（三份同名册此前只有这份缺它）：登记条目一旦不再死，
# 留着就等于让门禁对该令牌的**将来复活再死亡**永久失明 —— 比漏报更危险，因为它看起来有基线在管。
foreach ($kd in $knownDead) { Write-Host ('  KNOWN ' + $kd + '（登记于 css-token-debt.txt，未清）') -ForegroundColor DarkGray }

if ($staleDead.Count -gt 0) {
  Write-Host ('失效的债务豁免（登记但已不再死，必须从 tests/css-token-debt.txt 删除）：' + $staleDead.Count + ' 处') -ForegroundColor Red
  Write-Host '  为什么必须删：在册豁免会让该令牌将来的漂移静默放过（门禁对它永久失明）。' -ForegroundColor Yellow
  exit 1
}

if ($newDead.Count -gt 0) {
  Write-Host ('死令牌门禁未通过：' + $newDead.Count + ' 个新死令牌（未登记基线）') -ForegroundColor Red
  foreach ($d in $newDead) { Write-Host ('  - ' + $d) -ForegroundColor Red }
  Write-Host '  危害：定义了却没人 var() 用 —— 改令牌不生效，虚假的抽象比没有抽象更坏。' -ForegroundColor Yellow
  Write-Host '  修法：① 用起来（var(--xxx) 替换对应硬编码）；② 删定义；③ 确要保留请登记基线并注明理由。' -ForegroundColor Yellow
  exit 1
}
# ---------- 动画名双向对账（round 253 扩展；与令牌同源的那类病：名义存在、实际失效）----------
# 为什么加：@keyframes 名与 animation 引用必须双向对齐。引用了不存在的名字时**浏览器静默忽略**，
# 页面看不出任何异常（不报错、不告警、控制台干净）；定义了却没人引用则是死代码。这正是本仓最怕的
# 「隐身失败」类 —— 而此前没有任何闸覆盖动画名（受跟踪闸里 keyframes 字样命中 0）。
# 判据面与令牌口径**分离**：只用生产 .go（排除 _test.go），否则测试里的断言字符串能把死动画救活。
$animCorpus = ''
foreach ($af in (Get-ChildItem -LiteralPath $webDir -Filter '*.go' -File)) {
  if ($af.Name.EndsWith('_test.go')) { continue }
  $akept = @()
  foreach ($aln in ([IO.File]::ReadAllText($af.FullName) -split [string][char]10)) {
    if ($aln.Trim().StartsWith('//')) { continue }
    $akept += $aln
  }
  $animCorpus += (Strip-Block ($akept -join ([string][char]10))) + [string][char]10
}
$kfDefs = @()
foreach ($dm in [regex]::Matches($animCorpus, '@keyframes\s+([A-Za-z_][A-Za-z0-9_-]*)')) {
  $dn = $dm.Groups[1].Value
  # 结构判定一律大小写敏感（CSS 动画名区分大小写；默认的 -contains 是忽略大小写的）
  if ($kfDefs -cnotcontains $dn) { $kfDefs += $dn }
}
# 防「零定义 ⇒ 双向空集全绿」的空转：解析面一旦漂移（改了 keyframes 写法/换文件），这里必须先红。
if ($kfDefs.Count -eq 0) { Write-Host 'UNVERIFIED: 一条 @keyframes 都没解析到（动画对账会空转）' -ForegroundColor Red; exit 2 }
$kfx = @('none', 'infinite', 'alternate', 'alternate-reverse', 'reverse', 'normal', 'forwards', 'backwards', 'both', 'running', 'paused', 'ease', 'ease-in', 'ease-out', 'ease-in-ease', 'ease-in-out', 'linear', 'step-start', 'step-end', 'initial', 'inherit', 'unset', 'auto')
$kfRefs = @()
# 关键：按**整段**匹配到 ; 或 } 为止，不按行切 —— 实测 assets_css.go:144 的 animation 值以逗号结尾
# 跨行续写（L145 才是 bloom 那段）。逐行正则会把续行的动画名整段丢掉，我第一版就是这么造出两个假死动画的。
foreach ($rm in [regex]::Matches($animCorpus, 'animation(?:-name)?\s*:\s*([^;}{]*)')) {
  foreach ($seg in ($rm.Groups[1].Value -split ',')) {
    foreach ($tk in ($seg.Trim() -split '\s+')) {
      if ($tk -cnotmatch '^[A-Za-z_][A-Za-z0-9_-]*$') { continue }
      if ($kfx -ccontains $tk) { continue }
      if ($kfRefs -cnotcontains $tk) { $kfRefs += $tk }
    }
  }
}
$kfUndef = @($kfRefs | Where-Object { $kfDefs -cnotcontains $_ })
$kfUnused = @($kfDefs | Where-Object { $kfRefs -cnotcontains $_ })
if ($kfUndef.Count -gt 0) {
  Write-Host ('动画名对账未通过：' + $kfUndef.Count + ' 个 animation 引用了不存在的 @keyframes') -ForegroundColor Red
  foreach ($u in $kfUndef) { Write-Host ('  - 引用未定义: ' + $u) -ForegroundColor Red }
  Write-Host '  危害：浏览器对未知动画名静默忽略 —— 不报错、不告警、控制台干净，只有动效没了。' -ForegroundColor Yellow
  exit 1
}
if ($kfUnused.Count -gt 0) {
  Write-Host ('动画名对账未通过：' + $kfUnused.Count + ' 个 @keyframes 定义了却没人引用') -ForegroundColor Red
  foreach ($u in $kfUnused) { Write-Host ('  - 死动画: ' + $u) -ForegroundColor Red }
  Write-Host '  修法：接回去用起来，或删掉定义（留着会让后续读者以为有这套动效）。' -ForegroundColor Yellow
  exit 1
}

if (-not $Quiet) {
  $sum = $usedN + $anchorN + $dead.Count
  if ($dead.Count -gt 0) {
    Write-Host ('死令牌门禁通过（带已知缺陷）：' + $tokens.Count + ' 个令牌里 ' + $usedN + ' 个在用、' + $dead.Count + ' 个死（全部已登记基线）、' + $anchorN + ' 个是配对锚点（承重不判死）｜校验和 ' + $sum + '/' + $tokens.Count) -ForegroundColor Yellow
  } else {
    Write-Host ('死令牌门禁通过：' + $tokens.Count + ' 个令牌里 ' + $usedN + ' 个在用、' + $anchorN + ' 个配对锚点，零新增死令牌｜动画名双向对账：定义 ' + $kfDefs.Count + ' / 引用 ' + $kfRefs.Count + '（未定义 0、死动画 0）｜校验和 ' + $sum + '/' + $tokens.Count) -ForegroundColor Green
  }
}

exit 0





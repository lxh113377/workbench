# check_deadcode.ps1 -- 未导出的零引用函数（死代码）门禁
#
# 判据（任一命中即 rc=1）：受跟踪的非 _test.go 文件里，**未导出**的函数/方法定义（首字符为小写 ASCII 字母），
#   在整棵 workbench 源码树中除定义行外无任何引用（词边界匹配）。
#
# 【2026-10-25 round 158：本门禁此前长期空转，两处致命缺陷，同树同时刻实测 0 处 vs 真实 16 处】
#   ① 引用计数把**注释**也算成引用：Go 惯例是函数自己的文档注释以函数名开头，于是
#      「凡有文档注释的未导出函数」全都对本门禁隐形 —— 它实际只能抓「完全没被提及的函数」。
#   ② 抽取用的 -match **不区分大小写**（.NET 的 IgnoreCase 也作用于 [a-z] 这类字符类），
#      实测把 537 个**导出**定义也当成候选；改用 -cmatch 后候选从 38 降到 16。
#   修正手法：语料只取非注释行（trim 后以 // 、/* 、* 开头的一律跳过）；函数名用手写解析 +
#   首字符 ASCII 小写判定（不依赖任何大小写敏感的正则开关）。
#
# 债务基线（与 gofmt-debt / css-token-debt 同构）：tests/deadcode-debt.txt 登记
#   「已确认是死代码但尚未清理」的条目，格式 `<相对路径>  <函数名>`（按文件+名字登记，行号会漂）；
#   命中基线的只打印 KNOWN 不判红；基线里已不再死的条目判红（失效豁免必须删），防基线腐化。
#
# 已知假阳性豁免（刻意不动）：接口实现方法（名字可能在接口契约里）；被 json/reflect/unsafe 间接使用的（静态不可判）。
#
# 用法: powershell -File testscheck_deadcode.ps1 [-Quiet]   （反斜杠仅在用法示例里）
# 退出码: 0=无新增死代码 / 1=有新增或基线失效 / 2=UNVERIFIED（取不到源码）
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

# 1) 收集受跟踪的 .go —— 用 -z 取 NUL 分隔，避免路径引号，省掉修引号的转义dance（round 158）
$raw = (git -C $root ls-files -z "workbench/*.go")
$goFiles = @($raw -split ([char]0) | Where-Object { $_ -ne "" })
if ($goFiles.Count -eq 0) { Write-Host "UNVERIFIED: 未取到受跟踪的 .go" -ForegroundColor Red; exit 2 }

# 2) 语料只取**非注释行**（修正 ①）。_test.go 也要算：它们本来就是合法引用方。
$corpus = ""
foreach ($rel in $goFiles) {
  $path = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $path)) { continue }
  foreach ($ln in @([IO.File]::ReadAllLines($path))) {
    $t = $ln.TrimStart()
    if ($t.StartsWith("//") -or $t.StartsWith("/*") -or $t.StartsWith("*")) { continue }
    $corpus += $ln + [char]10
  }
}
if ($corpus.Length -eq 0) { Write-Host "UNVERIFIED: 语料为空" -ForegroundColor Red; exit 2 }

# 3) 抽取**未导出**函数/方法（手写解析：不以任何大小写敏感开关为凭）
$dead = @()
foreach ($rel in $goFiles) {
  if ($rel.EndsWith("_test.go")) { continue }
  # 与语料循环对齐：git ls-files 会列出「磁盘上已被删但仍受跟踪」的文件（本地删了还没 add），
  # 缺这条守卫会让门禁直接抛错（实测：worktree 里删除文件后 baseline 变 RED，而非给出读数）。
  if (-not (Test-Path -LiteralPath (Join-Path $root $rel))) { continue }
  $lines = @([IO.File]::ReadAllLines((Join-Path $root $rel)))
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $l = $lines[$i]
    if (-not $l.StartsWith("func ")) { continue }
    $rest = $l.Substring(5)
    if ($rest.StartsWith("(")) {
      $close = $rest.IndexOf(")")
      if ($close -lt 0) { continue }
      $rest = $rest.Substring($close + 1).TrimStart()
    }
    $open = $rest.IndexOf("(")
    if ($open -le 0) { continue }
    $name = $rest.Substring(0, $open)
    if ($name.Contains("[")) { $name = $name.Substring(0, $name.IndexOf("[")) }
    if ($name.Length -eq 0) { continue }
    $c0 = [int][char]$name[0]
    if ($c0 -lt 97 -or $c0 -gt 122) { continue }
    $pat = [char]92 + "b" + [regex]::Escape($name) + [char]92 + "b"
    $n = ([regex]::Matches($corpus, $pat)).Count
    if ($n -le 1) { $dead += [pscustomobject]@{ rel = $rel; name = $name; line = ($i + 1) } }
  }
}

# 4) 债务基线（按 文件 + 名字）
$baseline = @{}
$debtPath = Join-Path $root ("tests/deadcode-debt.txt")
if (Test-Path -LiteralPath $debtPath) {
  foreach ($ln in @([IO.File]::ReadAllLines($debtPath))) {
    $t = $ln.Trim()
    if ($t -eq "" -or $t.StartsWith("#")) { continue }
    $parts = @($t -split "  ", 2)
    if ($parts.Count -lt 2) { continue }
    $baseline[($parts[0].Replace([char]92, [char]47).Trim() + "  " + $parts[1].Trim())] = $true
  }
}

# 5) 三分类：KNOWN / NEW（判红） / STALE（基线失效，判红）
$known = @(); $new = @(); $alive = @{}
foreach ($d in $dead) {
  $key = $d.rel + "  " + $d.name
  $alive[$key] = $true
  if ($baseline.ContainsKey($key)) { $known += $d } else { $new += $d }
}
$stale = @($baseline.Keys | Where-Object { -not $alive.ContainsKey($_) })

foreach ($d in ($known | Sort-Object rel, name)) {
  Write-Host ("  KNOWN " + $d.rel + ":" + $d.line + "  " + $d.name + "（登记于 tests/deadcode-debt.txt，未清）") -ForegroundColor DarkGray
}
if ($new.Count -gt 0) {
  Write-Host ("新增死代码（未导出函数、全树零引用）" + $new.Count + " 处：") -ForegroundColor Red
  foreach ($d in ($new | Sort-Object rel, name)) { Write-Host ("  - " + $d.rel + ":" + $d.line + "  " + $d.name) -ForegroundColor Red }
}
if ($stale.Count -gt 0) {
  Write-Host ("失效的债务豁免（登记但已不再死，必须从 tests/deadcode-debt.txt 删除）" + $stale.Count + " 处：") -ForegroundColor Red
  foreach ($s in ($stale | Sort-Object)) { Write-Host ("  x " + $s) -ForegroundColor Red }
}
if ($new.Count -gt 0 -or $stale.Count -gt 0) {
  Write-Host "修法：新死代码 → 删掉或补调用点；基线失效 → 从 tests/deadcode-debt.txt 删掉该行。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("无新增死代码：检查 " + $goFiles.Count + " 个受跟踪 .go，基线内 KNOWN " + $known.Count + " 处（见 tests/deadcode-debt.txt）") -ForegroundColor Green }
exit 0

# check_changelog.ps1 -- CHANGELOG 结构门禁（Keep a Changelog 合规）
#
# 为什么需要（2026-10-19）：本项目 CHANGELOG 的 [Unreleased] 段一度被压在
# 1.3.0 与 1.2.0 之间 —— 违反 Keep a Changelog（未发布段必须在最顶），
# 发版时极易漏提升。规则写进文档不等于有判据，故上闸。
#
# 判据（任一不满足即 rc=1）：
#   1) 存在 "## [Unreleased]" 且它是**第一个**版本段
#   2) 已发布段形如 "## [x.y.z] - YYYY-MM-DD"（日期不得省略、不得写 TBD）
#   3) 已发布段按语义化版本**降序**排列（新在上）
#
# 用法: powershell -File tests\check_changelog.ps1 [-Path CHANGELOG.md]
# 退出码: 0=合规 / 1=不合规 / 2=文件不存在
#
# 实现注记：刻意用字符串方法而非正则 —— 本文件由脚本生成，正则里的反斜杠
# 在跨语言传递中易被吞掉（实测 [regex]'^## \[...' 被吞成非法模式而抛
# "Too many )'s"，门禁自身先坏）。纯字符串解析无此风险。
param(
  [string]$Path = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Path) {
  $root = Split-Path -Parent $PSScriptRoot
  $Path = Join-Path $root "CHANGELOG.md"
}
if (-not (Test-Path -LiteralPath $Path)) { Write-Host ("CHANGELOG 不存在: " + $Path); exit 2 }

$prefix = "## ["
$sections = @()
foreach ($line in ([IO.File]::ReadAllText($Path) -split "`r?`n")) {
  $t = $line.Trim()
  if (-not $t.StartsWith($prefix)) { continue }
  $rest = $t.Substring($prefix.Length)      # "Unreleased]" / "1.3.0] - 2026-10-19"
  $close = $rest.IndexOf("]")
  if ($close -lt 0) { continue }
  $key = $rest.Substring(0, $close)
  $date = $rest.Substring($close + 1).Trim()
  if ($date.StartsWith("-")) { $date = $date.Substring(1).Trim() }
  $sections += [pscustomobject]@{ Key = $key; Date = $date }
}

$fails = @()
if ($sections.Count -eq 0) { $fails += "没有任何 '## [x]' 版本段" }

# 1) Unreleased 存在且居首
$uIdx = -1
for ($i = 0; $i -lt $sections.Count; $i++) { if ($sections[$i].Key -eq "Unreleased") { $uIdx = $i; break } }
if ($uIdx -lt 0) { $fails += "缺少 '## [Unreleased]' 段" }
elseif ($uIdx -ne 0) { $fails += ("[Unreleased] 必须是第一个版本段，实际排在第 " + ($uIdx + 1) + " 位（其前是 " + $sections[0].Key + "）") }

# 2) 已发布段：版本号 + 日期格式
$released = @()
foreach ($s in $sections) {
  if ($s.Key -eq "Unreleased") { continue }
  $p = $s.Key.Split(".")
  $isVer = ($p.Count -eq 3)
  if ($isVer) { foreach ($seg in $p) { if ($seg -notmatch "^[0-9]+$") { $isVer = $false } } }
  if (-not $isVer) { $fails += ("版本段标题不合法: [" + $s.Key + "]（须为 x.y.z）"); continue }
  if ($s.Date -notmatch "^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$") {
    $fails += ("[" + $s.Key + "] 缺少合法发布日期 YYYY-MM-DD（实际: '" + $s.Date + "'）"); continue
  }
  $released += [pscustomobject]@{ Ver = $s.Key; A = [int]$p[0]; B = [int]$p[1]; C = [int]$p[2] }
}

# 3) 降序
for ($i = 1; $i -lt $released.Count; $i++) {
  $x = $released[$i-1]; $y = $released[$i]
  $cmp = $y.A - $x.A
  if ($cmp -eq 0) { $cmp = $y.B - $x.B }
  if ($cmp -eq 0) { $cmp = $y.C - $x.C }
  if ($cmp -ge 0) { $fails += ("版本段未降序: " + $x.Ver + " 之后是 " + $y.Ver) }
}


# 4) 头字节（2026-10-25 round 173 补，实测既存缺陷）：必须以**恰好一个** UTF-8 BOM 开头、紧随 "# "，
#    且**第一行**不得含替换字符 U+FFFD。为什么补：HEAD 里曾长期带 3 个 U+FFFD（第一行渲染成三个 ? 再跟 # Changelog），
#    而当时没有任何判据看头字节 —— check_multi_bom 只数 BOM 个数、本闸只看段标题，于是它一路绿着进了很多次提交。
$head = [IO.File]::ReadAllBytes($Path)
$bomCount = 0
for ($i = 0; ($i + 2) -lt $head.Length; $i += 3) {
	if ($head[$i] -eq 0xEF -and $head[$i + 1] -eq 0xBB -and $head[$i + 2] -eq 0xBF) { $bomCount++ } else { break }
}
if ($bomCount -ne 1) {
	$fails += ("开头必须是**恰好一个** UTF-8 BOM，实测 " + $bomCount + " 个")
} elseif ($head.Length -lt 5 -or $head[3] -ne 0x23 -or $head[4] -ne 0x20) {
	$show = ""
	for ($i = 0; $i -lt [Math]::Min(6, $head.Length); $i++) { $show += ("{0:x2} " -f $head[$i]) }
    $fails += ("BOM 之后必须紧跟井号加空格（0x23 0x20），实测前 6 字节: " + $show)
} else {
	$lineEnd = $head.Length
	for ($i = 3; $i -lt $head.Length; $i++) { if ($head[$i] -eq 0x0A) { $lineEnd = $i; break } }
	for ($i = 3; ($i + 2) -lt $lineEnd; $i++) {
		if ($head[$i] -eq 0xEF -and $head[$i + 1] -eq 0xBF -and $head[$i + 2] -eq 0xBD) {
			$fails += "第一行含替换字符 U+FFFD（编码损坏残留）"
			break
		}
	}
}
if ($fails.Count -gt 0) {
  Write-Host ("FAIL: CHANGELOG 结构不合规（" + $Path + "）") -ForegroundColor Red
  foreach ($f in $fails) { Write-Host ("  - " + $f) }
  Write-Host "规则见本脚本头部注释与 CONTRIBUTING.md「发布流程」。"
  exit 1
}
if (-not $Quiet) { Write-Host ("CHANGELOG 结构合规（头字节 OK）：" + $released.Count + " 个已发布段 + Unreleased 居首") }
exit 0

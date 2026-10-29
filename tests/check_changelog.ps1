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
#   5) 落地覆盖（issue #10，2026-10-29 round 70 新增）：改了 workbench/** 非测试 .go 的提交，
#      必须在 [Unreleased] 段留痕。**合法登记方式只有两种**（别猜第三种）：
#        ① 该提交自己改了 CHANGELOG.md（当场写条目，最常见）；
#        ② 别的条目用**显式锚点** `[<短sha>]` 登记它（形如 `[3c180c8]`，含方括号）。
#      裸短 sha 出现在正文里**不算**登记 —— 实测踩过：把缺口 sha 列成说明清单会被子串匹配
#      误判成"已登记"，反向把在册豁免判成 STALE 假红（见下面第 5 条实现注记）。
#
#      为什么原先没有这条：本闸的 1)-4) 全是**结构**判据（段序/日期/降序/头字节），
#      结构合规不等于内容有登记。实测缺口（2026-10-29 现取）：近 25 笔提交里 7 笔改了非测试 Go
#      代码，只有 1 笔写了 CHANGELOG ⇒ **6 笔落地零条目而门禁一路 rc=0** —— 即门禁对外宣称
#      "CHANGELOG 合规"，而 CONTRIBUTING §7 发布流程依赖的正是这一段的完整性，条目静默缺失
#      会让发版说明少真实变更（对外承诺失真）。属本仓在册第 8 类形态「判据存在但没受理面」的姊妹：
#      **判据存在但没覆盖到它声称管的那件事**。
#
#      存量与增量分开处置（这是本条的设计核心）：
#        - 增量（主判据，必须能在 CI 上工作）：只看**可见历史**里的落地提交，HEAD 永远可见，
#          而 ci.yml 的 checkout 是 fetch-depth: 1 ⇒ 若判据依赖"扫全历史"，CI 上只会看到 1 笔、
#          于是永远绿 = 假绿。故主判据按"每一笔落地提交自己有没有条目"判定，浅历史下依然有效。
#        - 存量（6 笔并行会话的历史缺口）：登记在 tests/changelog-coverage-debt.txt，
#          命中只打印 KNOWN 不判红 —— 判红别人的历史改动是**假红**（归因错人），
#          与本仓 deadcode-debt / gofmt-debt / css-token-debt 三张名册同构：
#          KNOWN 打印、NEW 判红、**STALE（条目已补却还留豁免）判红**，防基线腐化成垃圾桶。
#
# 用法: powershell -File tests\check_changelog.ps1 [-Path CHANGELOG.md] [-Lookback N]
# 退出码: 0=合规 / 1=不合规 / 2=文件不存在
#
# 实现注记：刻意用字符串方法而非正则 —— 本文件由脚本生成，正则里的反斜杠
# 在跨语言传递中易被吞掉（实测 [regex]'^## \[...' 被吞成非法模式而抛
# "Too many )'s"，门禁自身先坏）。纯字符串解析无此风险。
param(
  [string]$Path = "",
  [switch]$Quiet,
  # Lookback：存量对账往回看多少笔。CI（fetch-depth: 1）自然只看得到 HEAD 一笔，
  # 而主判据按「每笔落地提交自己有无条目」判定，浅历史下仍然成立；
  # 本地默认 25 笔，足够覆盖一轮并行会话的存量。
  [int]$Lookback = 25
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
# 注：改造前 $root 只在 -Path 缺省时才赋值 ⇒ 显式传 -Path 时 $root 为空。
# 落地覆盖判据要用 $root 定位 git 与债务名册，故把它提到无条件赋值（负向判据：传 -Path 也必须能跑完对账）。
if (-not $Path) {
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
# ---------------------------------------------------------------------------
# 5) 落地覆盖（issue #10）：改了非测试 Go 代码的提交必须在 [Unreleased] 有条目。
#
# 实现口径（全部用字符串方法，理由见本文件头注「实现注记」：正则在跨语言传递里会被吞）：
#   - 落地判定：文件名以 workbench/ 开头、以 .go 结尾、且不以 _test.go 结尾 ⇒ 算「改了代码」；
#     测试文件不算（它不改变对外行为，也不该逼作者为每个测试写发布说明）；
#   - 条目判定：该提交自己改了 CHANGELOG.md（当场写了），或 [Unreleased] 段文本里出现其短 sha；
#   - 段边界：[Unreleased] 段 = 文件开头到第一个 "## [x.y.z]" 之前（发版时整段被提升，
#     所以已发布段里的 sha 不能顶替 —— 那是上一个版本的内容，不属于"本轮登记"）。
# ---------------------------------------------------------------------------
$clText = [IO.File]::ReadAllText($Path)
$unre = $clText
$cutPos = $clText.IndexOf("## [1.")
if ($cutPos -lt 0) { $cutPos = $clText.IndexOf("## [0.") }
if ($cutPos -gt 0) { $unre = $clText.Substring(0, $cutPos) }

# git 取不到 ⇒ 本条判据失去输入面。此处**不**静默跳过：按本仓纪律「取不到证据」一律显式说明，
# 但也不把「浅历史」当成「有违规」——两件事分开处理，见下面 depthNote。
$gitExe2 = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe2) {
  Write-Host "NOTE 落地覆盖判据未执行（找不到 git）" -ForegroundColor Yellow
} else {
  $raw = @(& $gitExe2 -C $root log --no-merges -n $Lookback --pretty=format:"@@%h" --name-only 2>&1)
  if ($LASTEXITCODE -ne 0) {
    Write-Host ("NOTE 落地覆盖判据未执行（git log 失败: " + ($raw | Select-Object -First 1) + "）") -ForegroundColor Yellow
  } else {
    # 解析成「一笔 = 短 sha + 它改动的文件清单」；@@ 前缀是分段标记，避免文件名歧义。
    $blocks = @()
    $cur = $null
    foreach ($ln in $raw) {
      $s = "$ln".Trim()
      if ($s.StartsWith("@@")) {
        $cur = [pscustomobject]@{ Sha = $s.Substring(2); Files = @() }
        $blocks += $cur
      } elseif ($s -ne "" -and $null -ne $cur) {
        # git 把含非 ASCII 的路径写成带引号的八进制形式（本仓 docs/ 中文名即如此），
        # 这类行一律不是 .go 结尾 ⇒ 自然被下面的判定排除，不会误算成落地改动。
        $cur.Files += $s
      }
    }
    if ($blocks.Count -eq 0) {
      Write-Host "NOTE 落地覆盖判据未取到任何提交（浅历史或空仓）" -ForegroundColor Yellow
    } else {
      # 债务名册：每行 `<短sha>  <一句归因>`，两个空格分隔（与 check_deadcode 同格式）
      $debt = @{}
      $debtPath = Join-Path $root "tests/changelog-coverage-debt.txt"
      if (Test-Path -LiteralPath $debtPath) {
        foreach ($ln in @([IO.File]::ReadAllLines($debtPath))) {
          $t = "$ln".Trim()
          if ($t -eq "" -or $t.StartsWith("#")) { continue }
          $sp = $t.IndexOf("  ")
          if ($sp -lt 1) { continue }
          $debt[$t.Substring(0, $sp).Trim().ToLowerInvariant()] = $true
        }
      }
      $landing = 0; $knownN = 0; $newN = 0; $staleN = 0
      foreach ($b in $blocks) {
        $goFiles = @($b.Files | Where-Object {
          $_.StartsWith("workbench/") -and $_.EndsWith(".go") -and -not $_.EndsWith("_test.go")
        })
        if ($goFiles.Count -eq 0) { continue }
        $landing++
        $sha = $b.Sha.ToLowerInvariant()
        $touchedChangelog = @($b.Files | Where-Object { $_ -eq "CHANGELOG.md" }).Count -gt 0
        # 锚点语义（本轮实测收紧，第一版是错的）：只认**显式登记形态** `[sha]`，不认裸子串。
        # 第一版用 $unre.IndexOf($sha) 的裸子串匹配，被自家 STALE 判据当场抓出：
        # 我在条目正文里把 7 笔缺口 sha 列成"3c180c8 / aff53e8 / …"当现状说明，
        # 于是 7 笔全部被误判成「已登记」⇒ 名册豁免反判 STALE 红（假红归因给别人）。
        # 两个方向都坏：顺带提及会把真缺口洗成"已登记"，也会把在册存量判成失效豁免。
        # 正解 = 要求作者**显式**写一个可机读的登记锚点（形如 [3c180c8]），而不是靠猜子串。
        $anchor = "[" + $sha + "]"
        $hasEntry = $touchedChangelog -or ($unre.IndexOf($anchor, [System.StringComparison]::OrdinalIgnoreCase) -ge 0)
        if ($hasEntry) {
          # STALE：条目已补，豁免行必须删 —— 否则名册只增不减，终将腐化成「什么都豁免」。
          if ($debt.ContainsKey($sha)) {
            $staleN++
            $fails += ("落地覆盖豁免已失效: " + $sha + " 在 [Unreleased] 已有条目，仍登记于 tests/changelog-coverage-debt.txt ⇒ 必须删掉该行")
          }
          continue
        }
        if ($debt.ContainsKey($sha)) {
          $knownN++
          Write-Host ("  KNOWN " + $sha + " 改了 " + $goFiles.Count + " 个非测试 Go 文件、无 CHANGELOG 条目（登记于债务名册，未清）") -ForegroundColor DarkGray
          continue
        }
        $newN++
        $fails += ("落地覆盖缺失: 提交 " + $sha + " 改了 " + $goFiles[0] + " 等非测试 Go 文件（共 " + $goFiles.Count + " 个），但 [Unreleased] 段内既无其短 sha、该提交也未改 CHANGELOG.md ⇒ 按 CONTRIBUTING §7「未发布的变更一律记在本节」必须补条目（或按 issue #10 判为无需登记的纯重构，需在条目里写明理由）")
      }
      if (-not $Quiet) {
        $depth = "$(& $gitExe2 -C $root rev-list --count HEAD 2>&1)".Trim()
        Write-Host ("落地覆盖对账：可见提交 " + $blocks.Count + " 笔（历史总深 " + $depth + "），其中落地改动 " + $landing + " 笔；新增缺失 " + $newN + " / 在册豁免 " + $knownN + " / 豁免失效 " + $staleN)
        if ($blocks.Count -lt $Lookback) {
          Write-Host ("  NOTE 可见历史少于 -Lookback=" + $Lookback + "（浅克隆即如此）；主判据按「每笔落地提交自己有无条目」判定，浅历史下仍然有效，不构成漏网")
        }
      }
      # 名册里的 sha 若根本不在可见历史内（CI 浅克隆），不判 STALE —— 那是"看不见"不是"已补"。
    }
  }
}

if ($fails.Count -gt 0) {
  Write-Host ("FAIL: CHANGELOG 结构不合规（" + $Path + "）") -ForegroundColor Red
  foreach ($f in $fails) { Write-Host ("  - " + $f) }
  Write-Host "规则见本脚本头部注释与 CONTRIBUTING.md「发布流程」。"
  exit 1
}
if (-not $Quiet) { Write-Host ("CHANGELOG 结构合规（头字节 OK）：" + $released.Count + " 个已发布段 + Unreleased 居首 + 落地覆盖已对账") }
exit 0

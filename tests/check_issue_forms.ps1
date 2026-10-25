# check_issue_forms.ps1 -- GitHub Issue Forms 结构门禁（防"模板存在但 GitHub 不显示/渲染残缺"）
#
# 为什么需要（2026-10-19 实测）：CI 原先只查顶层 name/description/body 三个键，
# 这种判据放得过真实缺陷 —— dropdown/checkboxes 的 options 必须位于 attributes 之内
# （不是块级），contact_links 的说明字段必须叫 about（不是 description）。
# 写错位置时 GitHub 会**静默少渲染一个字段**，页面看起来正常，实际问不到关键信息。
# 本仓模板用 Python 侧按 GitHub 官方 schema 校验过（8/7 块、5/4 必填、id 唯一），
# 本件把同样的判据固化成 CI 可跑的静态闸。
#
# 用法: powershell -File tests\check_issue_forms.ps1 [-Root <repoRoot>] [-Quiet]
# 退出码: 0=合规 / 1=不合规 / 2=目录不存在
# 实现注记：纯字符串解析（不用正则引擎匹配复杂结构，也避开反斜杠跨语言被吞的坑）。
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }
$dir = Join-Path $Root ".github\ISSUE_TEMPLATE"
if (-not (Test-Path -LiteralPath $dir)) { Write-Host ("缺目录: " + $dir); exit 2 }

$viol = @()
$forms = @(Get-ChildItem -LiteralPath $dir -Filter *.yml -File | Where-Object { $_.Name -ne "config.yml" })
if ($forms.Count -eq 0) { $viol += "没有任何 issue form（*.yml，config.yml 除外）" }

foreach ($f in $forms) {
  $lines = ([IO.File]::ReadAllText($f.FullName) -split "`r?`n")
  $top = @{}
  $blocks = @()   # 每项: @{type;id;hasLabel;optionsAt;optionsUnderAttrs;required}
  $cur = $null
  $inAttrs = $false
  foreach ($ln in $lines) {
    if ($ln -match "^-" ) { } # 占位：下面按缩进手工判定
    $t = $ln.TrimEnd()
    if ($t -match "^[a-zA-Z_]+:") {
      # 顶层键（无缩进）
      $k = ($t -split ":")[0]
      $top[$k] = $true
      $cur = $null
      continue
    }
    if ($t -match "^  - type: (.+)$") {
      $cur = [pscustomobject]@{ type = $Matches[1].Trim(); id = ""; hasLabel = $false; optsTop = $false; optsAttrs = $false; required = $false; line = 0 }
      $blocks += $cur
      $inAttrs = $false
      continue
    }
    if ($null -eq $cur) { continue }
    if ($t -match "^    (attributes|validations):") { $inAttrs = ($Matches[1] -eq "attributes"); continue }
    if ($t -match "^      id: (.+)$" -or $t -match "^    id: (.+)$") { if (-not $cur.id) { $cur.id = $Matches[1].Trim() } }
    if ($inAttrs -and $t -match "^      label:") { $cur.hasLabel = $true }
    if ($inAttrs -and $t -match "^      options:") { $cur.optsAttrs = $true }
    # options 与 type 同级（缩进 4）= 位置错，GitHub 不认
    if ($t -match "^    options:") { $cur.optsTop = $true }
    if ($t -match "^      required: true") { $cur.required = $true }
  }

	# 键名大小写（2026-10-25 round 169 补，口径缺陷修复）：GitHub 的 issue form schema 里**键名全小写**，
	# 写成 Type: / Required: True 这类变体，GitHub 会**静默忽略** —— 正是本闸开头写的那个危害
	# （「模板存在但 GitHub 不显示/渲染残缺」）。而旧口径全程用 -match（不区分大小写）解析 YAML 键，
	# **看不见**这种写法：它会把变体键当合格键接受。
	# 处置**不是**把解析换成 -cmatch —— 那样变体键会「不成块 ⇒ 被跳过」，等于从「误判合格」变成
	# 「静默不判」；正确做法是把「键名大小写不符」**显式判为违规**并点名键与应为的小写形式。
	foreach ($ln in $lines) {
		if ($ln -match "^\s*(-\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*:") {
			$key = $Matches[2]
			if ($key -cne $key.ToLower()) {
				$viol += ("{0}: 键名大小写不符（{1}）—— GitHub schema 要求全小写，应为 {2}；写成大写会被静默忽略" -f $f.Name, $key, $key.ToLower())
			}
		}
	}

  foreach ($req in @("name","description","body")) {
    if (-not $top[$req]) { $viol += ("{0}: 缺顶层 {1}（GitHub 会整表不显示）" -f $f.Name, $req) }
  }
  $seenIds = @{}
  foreach ($b in $blocks) {
    if ($b.type -notin @("markdown","textarea","input","dropdown","checkboxes")) { $viol += ("{0}: 非法 type {1}" -f $f.Name, $b.type); continue }
    if ($b.type -eq "markdown") { continue }
    if (-not $b.id) { $viol += ("{0}: {1} 块缺 id" -f $f.Name, $b.type) }
    elseif ($seenIds[$b.id]) { $viol += ("{0}: id 重复 {1}" -f $f.Name, $b.id) }
    else { $seenIds[$b.id] = $true }
    if (-not $b.hasLabel) { $viol += ("{0}: 块 {1} 缺 attributes.label" -f $f.Name, $b.id) }
    if ($b.type -in @("dropdown","checkboxes")) {
      if ($b.optsTop) { $viol += ("{0}: 块 {1} 的 options 缩进错（与 type 同级），必须放在 attributes 之下" -f $f.Name, $b.id) }
      elseif (-not $b.optsAttrs) { $viol += ("{0}: 块 {1}({2}) 缺 attributes.options" -f $f.Name, $b.id, $b.type) }
    }
  }
  if (-not $Quiet) { Write-Host ("  form {0}: blocks={1}" -f $f.Name, $blocks.Count) }
}

# config.yml：contact_links 的说明字段必须叫 about
$cfg = Join-Path $dir "config.yml"
if (Test-Path -LiteralPath $cfg) {
  $ctxt = [IO.File]::ReadAllText($cfg)
  $clCount = ([regex]::Matches($ctxt, "(?m)^  - name:")).Count
  $aboutCount = ([regex]::Matches($ctxt, "(?m)^    about:")).Count
  if ($clCount -gt $aboutCount) { $viol += ("config.yml: contact_links 有 {0} 条但 about 只有 {1} 处（说明字段必须叫 about，不是 description）" -f $clCount, $aboutCount) }
  if ($ctxt -notmatch "(?m)^blank_issues_enabled:") { $viol += "config.yml: 缺 blank_issues_enabled" }
  if (-not $Quiet) { Write-Host ("  config.yml: contact_links={0} about={1}" -f $clCount, $aboutCount) }
}

if ($viol.Count -gt 0) {
  Write-Host ("FAIL: Issue Forms 结构不合规（" + $viol.Count + " 处）") -ForegroundColor Red
  foreach ($v in $viol) { Write-Host ("  - " + $v) }
  exit 1
}
if (-not $Quiet) { Write-Host ("Issue Forms 合规：" + $forms.Count + " 份表单") }
exit 0
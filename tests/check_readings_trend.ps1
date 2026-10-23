# check_readings_trend.ps1 -- 消费 gate-readings.jsonl 的趋势报告（2026-10-24 round 40 新增）
#
# 为什么需要：round 20 起多个门禁把核心读数落盘到
#   .workbuddy/memory/gate-readings.jsonl（untracked / health / coverage 三个序列），
#   动机是 round 31/32 的教训「单次读数会被一次性脚本骗，序列不会」。
#   但 round 40 检查发现：**数据在积累，没有任何东西在消费它** ——
#   没有趋势工具，等于记了流水账没人对账。本件把序列变成信息。
#
# 定位：**informational 趋势报告，不是拦截器** => 有数据就 rc=0，无数据 rc=2。
#   不判红的理由：趋势变差的语义因人因时而异（gap 上升可能只是并行会话在写新测试），
#   拿它做硬闸会制造噪声（本仓在册：模糊语义检查 => 门禁被删）。它负责把变化摆出来。
#
# 四个消费点（每个序列各一条趋势）：
#   check_untracked_go        gap / 未跟踪数的 首->末
#   check_untracked_go_health badPkgCount 首->末 + 尾部连续红读数 + 红窗持续时长
#   coverage                  avgPct / minPct 首->末，**区分 local/ci**（round 22 教训：
#                             两环境覆盖率系统性差 10~20pp，混算没意义）
#   resident_longrun          中位 首->末（涨跌方向）+ 极差 + 稳定等待 + 离上界余量
#                             （round 41 补：内存前几轮量错过三次，根因都是单次读数没参照物）
#
# 实现注记：不用反引号转义 —— 本仓多次踩「JS/外层模板把 PowerShell 反引号吃掉」的坑，
#   换行一律走 [char]10，Trim 的 BOM 走 [char]0xFEFF。
#
# 新鲜度（round 42 补）：每条趋势都报「末读数距今多久」。不报就会把
#   「末次落盘时的状态」当成「现在的状态」——round 42 实测：趋势报红窗 1.3h，
#   而红态从上次落盘起已 >2h 未被再确认。序列只覆盖到落盘的那一秒，之后是盲区。
#   阈值取 6h：本仓读数靠人跑 check_all/门禁产生，非定时任务（实测 24 条/13.2h
#   约 30 多分钟一条），超过 6h 无新读数即可判定为陈旧。
#
# 用法: powershell -File tests/check_readings_trend.ps1 [-Quiet]
# 退出码: 0=有趋势可报 / 2=UNVERIFIED（无读数文件或无有效行）
param(
  [switch]$Quiet,
  # 默认根目录由脚本位置推导；-Path 仅供自测/维护时注入别的读数文件（不硬编码路径是 2026-10-22 的在册教训）
  [string]$Path = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$path = $Path
if (-not $path) { $path = Join-Path $root '.workbuddy/memory/gate-readings.jsonl' }

if (-not (Test-Path -LiteralPath $path)) {
  Write-Host 'UNVERIFIED: 无 gate-readings.jsonl（还没有门禁落盘过读数）' -ForegroundColor Yellow
  exit 2
}

$rows = @()
$badLines = 0
foreach ($line in @([IO.File]::ReadAllLines($path))) {
  # PS5.1 的 Add-Content -Encoding UTF8 会给追加段夹带 BOM（实测同族坑），先剥掉再解析
  $s = $line.Trim().Trim([char]0xFEFF)
  if (-not $s) { continue }
  try { $rows += ($s | ConvertFrom-Json) } catch { $badLines++ }
}
if ($rows.Count -eq 0) {
  Write-Host ('UNVERIFIED: 读数文件无有效行（坏行 ' + $badLines + '）') -ForegroundColor Yellow
  exit 2
}

function Short-At([string]$iso) {
  if ($iso.Length -ge 16) { return $iso.Substring(5, 11) }
  return $iso
}

Write-Host ('gate-readings 趋势报告（共 ' + $rows.Count + ' 条读数）')
foreach ($g in @($rows | Group-Object -Property gate | Sort-Object Name)) {
  $items = @($g.Group | Sort-Object at)
  $first = $items[0]
  $last = $items[-1]
  $spanH = 0.0
  try { $spanH = ([datetime]$last.at - [datetime]$first.at).TotalHours } catch { }
  $ageMin = -1.0
  try { $ageMin = ((Get-Date) - [datetime]$last.at).TotalMinutes } catch { }
  $ageTxt = '未知'
  if ($ageMin -ge 0) { $ageTxt = [math]::Round($ageMin / 60.0, 1).ToString('0.0') + 'h' }
  Write-Host ('== ' + $g.Name + '  读数=' + $items.Count + '  窗=' + $spanH.ToString('0.0') + 'h（' + (Short-At $first.at) + ' -> ' + (Short-At $last.at) + '）末读数距今 ' + $ageTxt)
  if ($ageMin -gt 360) {
    Write-Host ('   陈旧警告：该序列已 ' + [math]::Round($ageMin / 60.0, 1) + 'h 无新落盘 —— 下面的趋势只覆盖到末读数那一刻，当前实况未测（盲区）。') -ForegroundColor Yellow
  }

  if ($g.Name -eq 'check_untracked_go') {
    $dg = [double]$last.gapLocalOnly - [double]$first.gapLocalOnly
    $arrow = '（持平）'
    if ($dg -gt 0) { $arrow = '（恶化：只在本地跑的测试在堆积）' }
    elseif ($dg -lt 0) { $arrow = '（改善：并行会话在提交）' }
    Write-Host ('   gap ' + $first.gapLocalOnly + ' -> ' + $last.gapLocalOnly + '（' + $dg + '）' + $arrow)
    Write-Host ('   未跟踪 .go ' + $first.untrackedGo + ' -> ' + $last.untrackedGo + '，其中生产件 ' + $last.untrackedProd + '；已提交测试函数 ' + $last.committedTests)
  }
  elseif ($g.Name -eq 'check_untracked_go_health') {
    $red = @($items | Where-Object { [int]$_.badPkgCount -gt 0 })
    $tailRun = 0
    for ($i = $items.Count - 1; $i -ge 0; $i--) {
      if ([int]$items[$i].badPkgCount -gt 0) { $tailRun++ } else { break }
    }
    $redSpanH = 0.0
    if ($red.Count -ge 2) { try { $redSpanH = ([datetime]$red[-1].at - [datetime]$red[0].at).TotalHours } catch { } }
    Write-Host ('   badPkg ' + $first.badPkgCount + ' -> ' + $last.badPkgCount + '；红读数 ' + $red.Count + '/' + $items.Count + '，尾部已连续 ' + $tailRun + ' 次，红窗**至少** ' + $redSpanH.ToString('0.0') + 'h（测于两次落盘之间，末读数后是盲区）')
    $cur = ''
    if ($last.badPkgs) { $cur = $last.badPkgs } else { $cur = '（当前无红）' }
    Write-Host ('   当前红包: ' + $cur)
    if ($tailRun -ge 3) {
      Write-Host '   提示：连续多次红 => 在途会话多半没在收尾；值得对齐一次所有权归属。' -ForegroundColor Yellow
    }
  }
  elseif ($g.Name -eq 'coverage') {
    foreach ($envName in @('local', 'ci')) {
      $grp = @($items | Where-Object { $_.env -eq $envName })
      if ($grp.Count -eq 0) { continue }
      $f2 = $grp[0]
      $l2 = $grp[-1]
      $d = [math]::Round([double]$l2.avgPct - [double]$f2.avgPct, 2)
      Write-Host ('   [' + $envName + '] avg ' + $f2.avgPct + ' -> ' + $l2.avgPct + '（' + $d + '）；最低包 ' + $l2.minPkg + '=' + $l2.minPct + '%')
    }
  }
  elseif ($g.Name -eq 'resident_longrun') {
    # 内存序列（round 41）：三个可回答的问题 ——
    #   中位在涨还是跌（涨=泄漏/负载上移的早期信号）、极差是否变大（GC 归还变慢）、
    #   离上界还有多少余量（余量收窄=迟早误判，需要提前调界或补裁剪）。
    $f3 = $items[0]
    $l3 = $items[-1]
    $dm = [math]::Round([double]$l3.medianMB - [double]$f3.medianMB, 1)
    $dir = '（持平）'
    if ($dm -gt 5) { $dir = '（上升 ' + $dm + 'MB：泄漏或负载上移，值得查裁剪/重启是否生效）' }
    elseif ($dm -lt -5) { $dir = '（下降 ' + $dm + 'MB：改善）' }
    $maxSpread = 0.0
    foreach ($it in $items) { if ([double]$it.spreadMB -gt $maxSpread) { $maxSpread = [double]$it.spreadMB } }
    $headroom = [math]::Round([double]$l3.boundMB - [double]$l3.medianMB, 1)
    Write-Host ('   中位 ' + $f3.medianMB + ' -> ' + $l3.medianMB + 'MB' + $dir)
    Write-Host ('   末次极差 ' + $l3.spreadMB + 'MB（序列内最大 ' + $maxSpread + 'MB）稳定等待 ' + $f3.stableWaitS + ' -> ' + $l3.stableWaitS + 's；上界 ' + $l3.boundMB + 'MB，余量 ' + $headroom + 'MB')
    if ($headroom -lt 40) {
      Write-Host ('   提示：余量不足 40MB，一次正常抖动就可能判红 —— 该复核上界是否定太紧（round 22 的双环境教训）。') -ForegroundColor Yellow
    }
    $phaseLast = ''
    if ($l3.phase) { $phaseLast = $l3.phase }
    Write-Host ('   末次相位 ' + $phaseLast + '，判定 ' + $l3.verdict + '（相位非 stopped/idle 时本门禁不会落盘，见门禁内说明）')
  }
}
if ($badLines -gt 0) {
  Write-Host ('跳过坏行 ' + $badLines + ' 条（不阻断；有写入方格式漂移时来查这里）') -ForegroundColor Yellow
}
exit 0

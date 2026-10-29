# check_cover_baseline_margin.ps1 -- 覆盖率基线 vs 实测的「余量」机读对账（issue #12，round 72 新增）
#
# 为什么需要（本轮实测）：setup/build.ps1 的 15 条 PerPackageCover 基线全靠**人记得**去和 CI 读数比。
# round 72 现算：按 issue #12 原口径（只看 CI）15 条里 12 条余量 >5pp；改用本仓既有规则
# 「两个环境都要过 ⇒ 取**下界**」重算是 7 条（web 空转 18.2pp、executor 11.1、shortcut 10.4…）。
# 两种算法指向同一事实：护栏长期空转，覆盖率倒退十几个点门禁也不会报。而 build.ps1 自己的注释
# 早写着「新阈值 = 实测 − 3pp」—— 规则存在、**没有判据** ⇒ 只在人想起来时生效。
# 本件把该规则变成每次核对（与 issue #10 同一形态：判据没覆盖它声称管的那件事）。
#
# 判据：读逐包覆盖率读数 JSON，与 build.ps1 的 $PerPackageCover 逐条对账，margin = 实测 − 基线
#   margin > +5pp ⇒ FAIL 虚设护栏（倒退不报）；margin < 0 ⇒ FAIL 基线高于实测（CI 会假红）。
#   建议值 = 实测 − 3pp（照抄 build.ps1 的规则，不另立标准）。
#
# 刻意不做的（口径边界，免得被当成漏判）：
#   - 不跑 go test：本件是**对账**件，跑测试是 build.ps1 的职责；再跑一遍会与并行会话抢共享构建
#     缓存，且慢到没人愿意跑（在册教训：噪声大的门禁最终会被删）。
#   - 不判「该包天然低是否合理」：tray/browser 这类平台绑定包覆盖率本就不高，留低是**人的判断**
#     （build.ps1 注释已写理由）。本件只报「基线与实测差多少」这一件客观事实。
#   - 取不到读数 ⇒ rc=2 UNVERIFIED：不判红也不当通过（本仓三态口径），绝不用 0 顶替缺数。
#
# 用法: powershell -File tests\check_cover_baseline_margin.ps1 [-Readings <json>] [-Quiet]
# 退出码: 0=余量全部合规 / 1=有虚设或过高 / 2=UNVERIFIED（取不到读数或基线清单）
param(
  [string[]]$Readings = @(),
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }
function Die([string]$m) { Write-Host ("FAIL: " + $m) -ForegroundColor Red; exit 1 }
# rc=3 = 前置缺失（无读数 / 证据只有一侧）：check_all 把 rc=3 计成 SKIP（不判红、也不计 PASS），
# 因此段内**不需要**把 rc=2 归零 —— 那正是本文件所在仓 check_all:55-57 记录的洗绿洞
# （「段内 =0 ⇒ 把『跑了但证据不足』洗成 PASS」）。语义分档：
#   0 = 余量全部合规；1 = 有虚设/基线高于实测/对照面失效；3 = 本机或 CI 还没提供读数。
function Skip([string]$m) { Write-Host ("SKIP " + $m) -ForegroundColor Yellow; exit 3 }

# 1) 定位读数：优先 -Readings，其次仓内 coverage-readings/（CI 上把 artifact 解到同一路径即可复用）
# 口径核心（本轮实跑后才想清楚，别再看漏）：本仓基线规则是「两个环境都要过 ⇒ 阈值取**下界**」
# （build.ps1:27-33 与 tray 实测 CI26.3/本21.3）。所以「余量」必须按 binding = min(CI, 本机) 算。
# ⇒ **只有单侧证据时不判**：喂 CI-only 会把建议抬高到本机达不到的位置（实测：CI-only 会说 browser
#   应设 28，而本机 browser 只有 25.8 ⇒ 设了就让本机 build.ps1 假红）。这种"看着有依据其实错一半"
#   的判据比没有更坏。故：凑不齐 ci + 本机两份读数 ⇒ rc=2 UNVERIFIED，把缺口显式交还给人。
$candidates = @()
# 调用方兼容（实测教训）：`powershell -File x.ps1 -Readings a,b` 时 PS **不会**把 a,b 拆成数组，
# 而是当一个字符串传进来 ⇒ Test-Path 失败 ⇒ 本件误报「取不到读数」。要求调用者改用 -Command
# 或数组语法是把负担推给人。正解：自己按逗号与分号拆，两侧调用方式都能用。
foreach ($r in @($Readings)) {
  if (-not $r) { continue }
  # 实测：$s.Split(@([char]44,[char]59)) 不拆！PowerShell 的方法绑定把数组 joined 成单字符当分隔符，
  # 返回整串（xtmp/split_probe.ps1：split_chararray=1）。可用形态是 Split([char]44) 或 -split "[,;]"。
  # 这里选 Replace+Split 单字符：既支持逗号也支持分号，且不依赖正则转义（本仓在册引号坑）。
  foreach ($piece in $r.Replace([string][char]59, [string][char]44).Split([char]44)) {
    $p2 = "$piece".Trim()
    if ($p2) { $candidates += $p2 }
  }
}
$candidates += (Join-Path $root 'coverage-readings/coverage-per-package.json')
$files = @()
foreach ($c in $candidates) { if ($c -and (Test-Path -LiteralPath $c)) { $files += $c } }
if ($files.Count -eq 0) { Skip ('取不到任何逐包覆盖率读数（找遍 ' + ($candidates -join ' | ') + '）。生成：本地跑 setup/build.ps1 落 coverage-readings/；或下载 CI artifact coverage-readings-<run_id> 后用 -Readings 传入。') }

# 合并多份读数：逐包取 min（= 本仓的 binding），并记录每包用的是哪一侧
$per = @{}
$src = @{}
$envs = @{}
foreach ($f in $files) {
  try { $d = [IO.File]::ReadAllText($f) | ConvertFrom-Json } catch { Die ("读数解析失败 " + $f + ": " + $_.Exception.Message) }
  if ($null -eq $d.per) { Die ("读数里没有 per 字段（与 build.ps1 落盘约定不符）: " + $f) }
  $e = [string]$d.env
  if (-not $e) { $e = "unknown" }
  $envs[$e] = $true
  $n = 0
  foreach ($p in $d.per.PSObject.Properties) {
    $n++
    $v = [double]$p.Value
    if (-not $per.ContainsKey($p.Name) -or $v -lt $per[$p.Name]) { $per[$p.Name] = $v; $src[$p.Name] = $e }
  }
  if ($n -eq 0) { Die ("读数 per 为空集合（不得当成无需对账）: " + $f) }
  Write-Host ("  读入 " + $f + "  env=" + $e + " 包数=" + $n) }
if (-not $envs.ContainsKey("ci") -or -not $envs.ContainsKey("local")) {
  $got = ($envs.Keys | Sort-Object) -join "+"
  Skip ("证据不足：只拿到 " + $got + " 侧读数。本仓基线取双环境下界 ⇒ 需同时有 ci 与 local 两份，否则建议值会偏（实测：CI-only 会把 browser 抬到 28，而本机只有 25.8 ⇒ 设了就假红）")
}
$envName = "binding(min of ci+local)"
$data = [pscustomobject]@{ per = $per }
Say ('合并后 binding 包数=' + $per.Count + '（' + $envName + '）；每包取 min 来自：' + (($src.Values | Sort-Object -Unique) -join '/') + '）')

# 2) 基线从 build.ps1 的 $PerPackageCover 解析 —— 它是唯一权威源，本件绝不抄第二份清单
$bp = Join-Path $root 'setup/build.ps1'
if (-not (Test-Path -LiteralPath $bp)) { Die "取不到 setup/build.ps1（基线权威源）" }
$bt = [IO.File]::ReadAllText($bp)
$anchor = '$PerPackageCover = @("'
$i = $bt.IndexOf($anchor)
if ($i -lt 0) { Die "build.ps1 里找不到 $PerPackageCover（被改名 ⇒ 本件失去对照面，判红而非跳过）" }
$rest = $bt.Substring($i + $anchor.Length)
$j = $rest.IndexOf(')')
if ($j -lt 0) { Die "$PerPackageCover 没有收尾（解析面不完整，拒绝下结论）" }
$bodyText = $rest.Substring(0, $j)
$baselines = @{}
foreach ($raw in $bodyText.Split(",")) {
  $s = $raw.Trim().Trim([char]34)
  if (-not $s) { continue }
  $k = $s.IndexOf("=")
  if ($k -lt 1) { continue }
  $baselines[$s.Substring(0, $k).Trim()] = [double]$s.Substring($k + 1).Trim()
}
if ($baselines.Count -eq 0) { Die "基线清单解析结果为零（不得当成无需对账）" }
Say ("基线清单（build.ps1）：" + $baselines.Count + " 条")

# 3) 逐条对账（按包名排序 ⇒ 输出可复现、diff 友好）
$fails = @(); $warns = @(); $checked = 0; $ok = 0
foreach ($pkg in ($baselines.Keys | Sort-Object)) {
  $hasPkg = $per.ContainsKey($pkg)
  if (-not $hasPkg) { $warns += ("读数里没有 " + $pkg + "（该包无测试或被过滤）⇒ 只披露不判"); continue }
  $checked++
  # 取值必须跟着上面的改造走：$prop 已随「hasPkg 判据」一起删掉，再引用它就是 $null ⇒ [double] 得 0
  # ⇒ 每个包都被误报「基线高于实测」。上一版只改了条件行没改取值行，是同一处改动的半途。
  $cov = [double]$per[$pkg]
  $base = $baselines[$pkg]
  $margin = [math]::Round($cov - $base, 1)
  $should = [math]::Max(0, [math]::Round($cov - 3, 0))
  if ($margin -gt 5.0) {
    $fails += ("虚设护栏 " + $pkg + "：实测 " + $cov + "% − 基线 " + $base + "% = " + $margin + "pp > 5pp ⇒ 倒退 " + [math]::Round($margin - 3, 1) + "pp 也不会报；按规则应设 " + $should)
  } elseif ($margin -lt 0.0) {
    $extra = if ($envName -eq "ci") { "（CI 侧值；本仓取双环境下界，需本机读数确认基线是否尚可）" } else { "" }
    $fails += ("基线高于实测 " + $pkg + "：基线 " + $base + "% > 实测 " + $cov + "% ⇒ 已在红线上，CI 会假红" + $extra)
  } else { $ok++ }
}

# 零核对 = 判据没跑到任何东西上，绝不算通过（本仓 ㉑二；本件第一版正是栽在这：条件行漏改导致
# 15 个包全部走「读数里没有」分支、核对 0 条却 rc=0 —— 假绿。判据自己也要被自己的规则管住。）
if ($checked -eq 0) { Die ("对账核对数为 0（基线 " + $baselines.Count + " 条一个都没在读数里找到 ⇒ 对照面失效：包名口径或落盘格式变了，必须有人看）") }

foreach ($w in $warns) { Write-Host ("  NOTE " + $w) -ForegroundColor Yellow }
if ($fails.Count -gt 0) {
  Write-Host ("FAIL: 覆盖率基线余量对账未通过（" + $fails.Count + " 条）") -ForegroundColor Red
  foreach ($f in $fails) { Write-Host ("  - " + $f) }
  Write-Host "规则见 setup/build.ps1 头注（实测 − 3pp、双环境取下界）与 issue #12。"
  exit 1
}
Say ("覆盖率基线余量对账通过：核对 " + $checked + " 条，余量均在 0..5pp 内（env=" + $envName + "）")
exit 0

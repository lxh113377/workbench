# cancel_lane_pollution_probe.ps1 -- issue #11 缺陷②（取消不该污染车道健康度）的「确定性」取证探针。
#
# 为什么需要它：上一轮受控差分两侧 lane.failure 都是 0，我说「没复现污染」，而当时给的理由
# （车道计数是内存态、无落盘件）**是错的** —— 持久面确实存在：engine.go:142 用
# layout.RuntimeFile("models-health.json") 构造路由器，router.go:387 在 RecordFailure 末尾调
# saveLocked。结论侥幸没错、理由错 ⇒ 论据必须换掉重来。
#
# 为什么上一轮造不出污染（机理已查清，不是运气）：假 DSH 起得来 ⇒ executor 自标 cancelled，
# 旧代码要记账得让取消正好落在 cmd.Start() 那一瞬（让错误以「启动 DSH 失败: context canceled」
# 且 ErrorKind 为空回来）—— 那是赌时序。老大要求「不凑时序」⇒ 改用令牌桶饥饿。
#
# ── 确定性构造（同一进程、同一 run、三步）──
#   ① dshExe 指向不存在的 exe ⇒ 任务一进 run 必在 cmd.Start() 失败 ⇒ engine 走
#      「else if runErr != nil」分支 ⇒ RecordFailure(boot_failed)。这一发同时是**正向对照**：
#      证明这台探针里真失败确实会记账；读数取不到时 exit 2，绝不把「没测到」当 0。
#   ② requestsPerMinute=1 / requestBurst=1 ⇒ 令牌用光后，下一个任务卡在 executor 的
#      limiter.Wait(ctx)（runner_wiring.go:85-92 建桶；limiter.New 初始满桶 = burst）。
#      等待窗口约 60 秒、取消只要 1 秒 ⇒ 不赌时序。
#   ③ 窗口内取消 ⇒ Wait 返回 ctx.Err() ⇒ executor 返回 Result{ErrorKind:cancelled}
#      （非 nil、ErrorKind 非空；022cea5:329-331 逐字核实）⇒ 旧代码 engine :1210 条件为真
#      ⇒ 必然执行 :1212 的 RecordFailure ⇒ 该车道 +1；新代码 :1247 先 classifyCancellation、
#      :1261 直接 return ⇒ 必然不记。⇒ delta 是二值读数，不需要概率、不需要 birthtime 推理
#      （上一轮我正是用 birthtime 推错过：WriteJSONAtomic 是 tmp+rename，rename 会刷新 birth）。
#
# ⚠ 为什么不能「指定我自己的那条车道」（v2/v3 实测教训，2026-10-30 01:2x–01:5x）：
#   把 config.models 写成唯一一条假车道**不成立** —— 池投影会覆盖它：engine.go:361-365
#   ReloadModelRoutes 与 :2194-2198 ReloadConfig 都是「池路由表非空 ⇒ UpdateRoutes(池)，
#   否则才用 cfg.Models」，日志实测「模型路由表已更新 source=modelpool routes=35」。
#   而 pool.enabled=false 拦不住它（那是另一条缺陷，已单开 issue #15：main.go:231 硬编码
#   Enabled: true 且 ServiceConfig.Enabled 全仓零读者）。⇒ 假车道全程 0 次被选中，它的
#   failure=0 含义是「没被用过」而不是「没被污染」—— 读错一次就会误判缺陷已修。
#   ⇒ 改成**车道无关**：从取消台账行的 model 字段反查「这次实际用了哪条车道」再比计数。
#
# ⚠ 为什么 maxRetries 必须是 0（p5 实测）：maxRetries=1 时 t1 会重试，而每次重试都要再抢
#   那唯一令牌、每次等 60 秒 ⇒ 连续 80 个 poll tick 仍是 running ⇒ 4 分钟不落终态。
#   假设必须写成断言（下面 CONFIG_ASSERT 会核 maxRetries）。
#
# ⚠ 为什么收尾必须做成函数并在每个 exit 前调（v3 现场教训）：探针在 exit 2 分支提前退出时，
#   尾部清理根本没机会跑 ⇒ 实测泄漏 6 个 lane-probe 孤儿 --headless 实例。脚本注释里写着
#   「隔离实例不受生产守护接管，必须自己扫干净」却没在失败路径上兑现，就是空话。
#
# 读数面 = GET /api/models 的 health（server.go:745 → engine.RouterHealth() → router.go:164），
#   直读内存计数器。前提已逐行核过：UpdateRoutes 重建分支（router.go:153-159）只覆写
#   Provider/Tier/Disabled、不动 Failure/ConsecFail ⇒ 池同步不会抹掉车道失败计数。
#
# 安全性：独立数据根与端口（避开 17890/17891 与 20912–20916）、零真实模型调用（dshExe 不
#   存在 ⇒ 子进程根本起不来）、不碰 dist 与线上根、收尾自杀并按命令行精确复核残留。
# 用法: pwsh -NoProfile -File tests/cancel_lane_pollution_probe.ps1 -Exe <path> -Port 20937 [-Tag p] [-ExpectPollution]
# 退出码: 0=与预期一致 / 1=与预期相反 / 2=前置不满足（配置断言、窗口没拿到、读数取不到 ⇒ 绝不判 0）
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [int]$Port = 20937,
  [string]$Tag = 'p1',
  [switch]$ExpectPollution
)
$ErrorActionPreference = "Continue"
$base = "D:/workdesktop/xtmp/lane-probe-" + $Tag
$dataRoot = $base + "/workbench-output"
$expectExtra = [bool]$ExpectPollution
$DQ = [string][char]34

if (-not (Test-Path -LiteralPath $Exe)) { Write-Host ("RESULT=NO_EXE " + $Exe); exit 2 }
# 清不干净就拒绝执行：残留实例共享数据根 ⇒ 读数会串（上一轮真出过这事故，不是假想风险）。
# 起盘前守卫（v4/v5/v6 现场教训）：本探针被外部打断（工具超时、job_kill）时，脚本自己的收尾
# 跑不到 ⇒ 它拉起的 --headless 实例会变成孤儿占住端口与数据根。实测泄漏过 6 个 + 后续 3 个。
# 「清不掉就 exit 2」只挡住 DIRTY_ROOT，挡不住端口被孤儿占着而数据根是全新 Tag 的情况
# （换个 -Tag 就绕过）。所以直接查有没有 lane-probe 实例活着：有就拒跑，并给出精确 PID。
$live = @()
try { $live = @(Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" | Where-Object { "$($_.CommandLine)" -match 'lane-probe' }) } catch { }
if ($live.Count -gt 0) {
  Write-Host ("RESULT=LIVE_PROBE_INSTANCES " + $live.Count + " 个 lane-probe 实例还活着（PID " + (($live | ForEach-Object { $_.ProcessId }) -join ",") + "）⇒ 先杀干净再跑，否则读数会串到别的探针上")
  exit 2
}

if (Test-Path -LiteralPath $base) {
  try { Remove-Item -LiteralPath $base -Recurse -Force -ErrorAction Stop }
  catch { Write-Host ("RESULT=DIRTY_ROOT 清不掉 " + $base + " ：" + $_.Exception.Message); exit 2 }
}
New-Item -ItemType Directory -Force -Path $dataRoot | Out-Null
Copy-Item -LiteralPath $Exe -Destination ($base + "/workbench.exe") -Force
# 把 failfast stub 拷进数据根：探针自带依赖，不引用 xtmp 里的临时件（那会变成"跑一次就得记住
# 某处还有个文件"的断链引用，本仓在册形态）。
$stubSrc = "D:/workdesktop/xtmp/failfastmod/failfast.exe"
if (-not (Test-Path -LiteralPath $stubSrc)) { Write-Host ("RESULT=NO_FAILFAST_STUB 先 go build " + $stubSrc); exit 2 }
Copy-Item -LiteralPath $stubSrc -Destination ($base + "/failfast.exe") -Force
$exe = $base + "/workbench.exe"
# dshExe 用「存在但立刻 exit 3」的假 DSH，而不是不存在的路径（2026-10-30 06:1x 实测教训）：
#   不存在 ⇒ cmd.Start() 失败 ⇒ engine :1280 走 boot_failed ⇒ 任务被 requeue 重排、attempt 恒 0、
#   每 60 秒重跑一次、**永不终态**（v4 实测卡在这儿，正向对照等不到）。存在但非零退出 ⇒ 走正常
#   执行路径 ⇒ 无产物/异常退出 ⇒ 既记车道失败又判死终态。令牌饥饿仍成立：t1 秒败即消耗唯一令牌。
$fakeDsh = $base + "/failfast.exe"
$badDsh = $fakeDsh
Write-Host ("TARGET exe=" + $exe + " size=" + (Get-Item $exe).Length + " expect_extra=" + $expectExtra)
# 起步 config 只保证「起得来 + 端口对」；精确控制靠起盘后 GET→改→PUT（引擎会把极简 config
# 规范化填满，v1 因此让 autonomy 自己生了任务，读数全废）。字段按实测可用件补全。
$lane = "lane-probe/fake-lane"
$startCfg = [ordered]@{
  schemaVersion = 1; autoResume = $false; resumeOnBoot = $false; autoStart = $false; outputRoot = ""
  models = @([ordered]@{ model = $lane; provider = "lane-probe"; tier = "balanced"; enabled = $true; weight = 100; maxFail = 3; cooldownM = 30 })
  executor = [ordered]@{ concurrency = 2; defaultTimeout = 45; maxRetries = 0; retryBackoffMs = @(2000); maxOutputMB = 32; stallSeconds = 900; dshExe = $badDsh; profileName = "web-desktop"; shutdownGraceSeconds = 20; appendProtocol = $true; repairRounds = 0; requestsPerMinute = 1; requestBurst = 1 }
  server = [ordered]@{ host = "127.0.0.1"; port = $Port; openBrowser = $false }
  supervisor = [ordered]@{ enabled = $false; memLimitMB = 700; handleLimit = 3000; goroutineLimit = 1500; sampleSeconds = 30; strikesToRestart = 3 }
  pool = [ordered]@{ enabled = $false; probeSeconds = 180; syncSeconds = 600; strategy = "priority"; deepProbe = $false }
  chain = [ordered]@{ enabled = $false; maxDepth = 2; fanout = 1; minQuality = 8; contextChars = 500 }
  plan = [ordered]@{ enabled = $false; plannerTier = "balanced"; plannerMaxMinutes = 8; stageMaxMinutes = 4; replanRounds = 0; appendPlanToPrompt = $false }
  autonomy = [ordered]@{ enabled = $false }
  gov = [ordered]@{ enabled = $false }
  workspace = [ordered]@{ allowedRoots = @($base) }
  retention = [ordered]@{ keepDays = 5; archive = $false }
}
$startCfg | ConvertTo-Json -Depth 9 | Set-Content -LiteralPath ($dataRoot + "/config.json") -Encoding UTF8

. "D:/workdesktop/tests/lib_silent_launch.ps1"
$lp = Start-WBInstance -Path $exe -Arguments "--headless"
if (-not $lp) { Write-Host "RESULT=NO_PROCESS"; exit 2 }
$tok = ""

# U / Stop-Instance / Bail 必须先于任何 exit 分支定义：失败路径也要能收尾（v3 孤儿教训）。
function U([string]$p) { return "http://127.0.0.1:" + $Port + $p + "?token=" + $tok }
$script:cleanupDone = $false
function Stop-Instance {
  if ($script:cleanupDone) { return }
  $script:cleanupDone = $true
  try { Invoke-WebRequest -UseBasicParsing -Uri (U "/api/engine/stop") -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 10 | Out-Null } catch { }
  Start-Sleep -Seconds 3
  try { Start-Process -FilePath $exe -ArgumentList "--quit" -WindowStyle Hidden | Out-Null } catch { }
  Start-Sleep -Seconds 8
  $left = @(Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" -ErrorAction SilentlyContinue | Where-Object { "$($_.CommandLine)" -match "lane-probe" })
  Write-Host ("LEFTOVER_PROCS=" + $left.Count)
  foreach ($q in $left) { try { Stop-Process -Id $q.ProcessId -Force -ErrorAction Stop } catch { } }
  Write-Host "PROBE_DONE"
}
function Bail([string]$why, [int]$code) { Stop-Instance; Write-Host $why; exit $code }

$deadline = (Get-Date).AddSeconds(70)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  try {
    $h = (Invoke-WebRequest -UseBasicParsing -Uri ("http://127.0.0.1:" + $Port + "/") -TimeoutSec 4).Content
    $m = [regex]::Match($h, "__WB_TOKEN__\s*=\s*" + $DQ + "([a-zA-Z0-9_-]+)")
    if ($m.Success) { $tok = $m.Groups[1].Value; break }
  } catch { }
}
if (-not $tok) { Bail "RESULT=NO_TOKEN" 2 }
Invoke-WebRequest -UseBasicParsing -Uri (U "/api/engine/start") -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 10 | Out-Null
Start-Sleep -Seconds 2
$cur = (Invoke-RestMethod -Uri (U "/api/settings") -TimeoutSec 10).config
if (-not $cur) { Bail "RESULT=NO_SETTINGS_READ" 2 }
$cur.executor.dshExe = $badDsh
$cur.executor.requestsPerMinute = 1
$cur.executor.requestBurst = 1
$cur.executor.concurrency = 2
$cur.executor.maxRetries = 0
$cur.autoStart = $false
$cur.autoResume = $false
foreach ($k in @("autonomy", "plan", "chain", "gov", "supervisor", "pool")) {
  if ($cur.PSObject.Properties[$k]) { $cur.$k.enabled = $false }
}
$cur.server.port = $Port
try {
  Invoke-RestMethod -Uri (U "/api/settings") -Method PUT -ContentType "application/json" -Body ($cur | ConvertTo-Json -Depth 14) -TimeoutSec 15 | Out-Null
} catch { Bail ("RESULT=SETTINGS_PUT_FAIL " + $_.Exception.Message) 2 }
$chk = (Invoke-RestMethod -Uri (U "/api/settings") -TimeoutSec 10).config
$bad = @()
if ([int]$chk.executor.requestsPerMinute -ne 1) { $bad += ("rpm=" + $chk.executor.requestsPerMinute) }
if ([int]$chk.executor.requestBurst -ne 1) { $bad += ("burst=" + $chk.executor.requestBurst) }
if ([int]$chk.executor.maxRetries -ne 0) { $bad += ("maxRetries=" + $chk.executor.maxRetries + "（要求 0，否则 t1 重试会再抢令牌、永不终态）") }
if ([int]$chk.executor.concurrency -ne 2) { $bad += ("concurrency=" + $chk.executor.concurrency + "（要 2：1 会让 t1 独占槽、t2 进不到 running）") }
if (("$($chk.executor.dshExe)") -ne $badDsh) { $bad += "dshExe 未生效" }
if ($chk.autonomy.enabled) { $bad += "autonomy 仍开启（会自生任务污染读数）" }
if ($chk.plan.enabled) { $bad += "plan 仍开启（方案阶段会另占车道）" }
if ($bad.Count -gt 0) { Bail ("RESULT=CONFIG_ASSERT_FAIL " + ($bad -join "; ") + " ⇒ 不硬跑实验") 2 }
Write-Host "CONFIG_ASSERT_OK 取证面受控：rpm=1 burst=1 concurrency=2 maxRetries=0 autonomy/plan 关"
function New-Task([string]$title) {
  $b = @{ title = $title; prompt = "探针任务：请勿执行任何实际操作" } | ConvertTo-Json
  try {
    $r = Invoke-RestMethod -Uri (U "/api/tasks/manual") -Method POST -ContentType "application/json" -Body $b -TimeoutSec 12
    $id = [string]$r.task.id
    Write-Host ("TASK_CREATED title=" + $title + " id=" + $id + " state=" + [string]$r.task.state + " warning=" + [string]$r.warning)
    return $id
  } catch { Write-Host ("TASK_CREATE_THROW " + $title + " " + $_.Exception.Message); return "" }
}
function Get-State([string]$id) {
  $q = Invoke-RestMethod -Uri (U "/api/tasks") -TimeoutSec 8
  return ($q.tasks | Where-Object { $_.id -eq $id } | Select-Object -First 1)
}
# 车道计数快照：面 = /api/models 的 health（内存计数器直读，不依赖落盘时机）。
# 取不到 ⇒ 返回 $null ⇒ 调用方 Bail。绝不把「读不到」折算成 0（本仓三态口径）。
function Snapshot([string]$label) {
  $map = @{}
  try {
    $m = Invoke-RestMethod -Uri (U "/api/models") -TimeoutSec 10
    foreach ($hh in @($m.health)) { if ($hh -and $hh.model) { $map[("$($hh.model)")] = [int]$hh.failure } }
  } catch { Write-Host ($label + " SNAPSHOT_FAIL " + $_.Exception.Message); return $null }
  if ($map.Count -eq 0) { Write-Host ($label + " SNAPSHOT_EMPTY"); return $null }
  $sum = 0; foreach ($vv in $map.Values) { $sum += $vv }
  Write-Host ($label + " lanes=" + $map.Count + " sum_failure=" + $sum)
  return $map
}
# 台账行的 model = 这次实际用的车道（v2/v3 教训：config 里指定的假车道不会被选中）。
function Ledger-Field([string]$taskId, [string]$field, [string]$wantAction) {
  $led = $dataRoot + "/tasks/" + $taskId + "/attempts.jsonl"
  if (-not (Test-Path -LiteralPath $led)) { return "" }
  $last = ""
  foreach ($ln in ([IO.File]::ReadAllLines($led))) {
    if (-not "$ln".Trim()) { continue }
    $o = $null
    try { $o = "$ln" | ConvertFrom-Json } catch { $o = $null }
    if (-not $o) { continue }
    if ($wantAction -and ("$($o.action)") -ne $wantAction) { continue }
    if ($o.$field) { $last = "$($o.$field)" }
  }
  return $last
}

# ── 步 ①：真失败一笔（正向对照 + 耗尽唯一令牌）。maxRetries=0 ⇒ 一次失败即终态 ──
$t1 = New-Task "lane-probe-1-real-failure"
if (-not $t1) { Bail "RESULT=TASK_CREATE_FAILED t1 没建成" 2 }
$deadline = (Get-Date).AddSeconds(200)
$S0 = Snapshot "STEP0_baseline"
if ($null -eq $S0) { Bail "RESULT=NO_SNAPSHOT 读不到车道计数基线（取不到 != 合格，不判 0）" 2 }
# 正向对照的判据：**观测到任一车道因真失败而 failure 增加**，不等 t1 落终态。
# 为什么不等终态（v4/v5 两次实测）：bad_exit 会在车道间 failover（实测 recursive/flint → z-ai/glm-5.3
# → openai/gpt-oss-20b，attempt 恒 0），35 条车道 ⇒ 35 分钟才判死；而 boot_failed 更直接被 requeue。
# 「任务最终成没死」与本探针要证的「真失败会不会记车道账」是两件事，只需后者成立即可当对照。
$lane1 = ""
$b1 = -1
$deadline = (Get-Date).AddSeconds(200)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 3
  $S1 = Snapshot "STEP1_poll"
  if ($null -eq $S1) { continue }
  foreach ($k in $S1.Keys) {
    $base = if ($S0.ContainsKey($k)) { [int]$S0[$k] } else { 0 }
    if ([int]$S1[$k] -gt $base) { $lane1 = "$k"; $b1 = [int]$S1[$k]; break }
  }
  if ($b1 -ge 0) { break }
}
if ($b1 -lt 0) { Bail "RESULT=NO_POSITIVE_CONTROL 200 秒内没观测到任何车道因真失败记账 ⇒ 构造未生效，实验作废" 2 }
# 取消窗口必须落在「t1 已用光令牌」之后：t1 每次失败消耗 1 个令牌（burst=1、60 秒回 1 个），
# 所以刚观测到 t1 记账的时刻，就是 t2 最可能卡住的时刻 —— 这也让 delta 的时间窗尽量短。

# 不再引用 kind/state：上面已把判据从「等终态」改成「观测到记账」，这两个变量随之消失。
# 留着会静默取空值打出一行缺字段的日志（PowerShell 不报错）——正是本探针要防的那类假象。
$kind1 = Ledger-Field $t1 "kind" ""
Write-Host ("POSITIVE_CONTROL_OK 真失败已记账：lane=" + $lane1 + " failure_now=" + $b1 + " baseline_was=" + [int]$S0[$lane1] + " kind_seen=" + [string]$kind1)

# ── 步 ②③：下一个任务卡 limiter.Wait（令牌饥饿），在等待窗口内取消 ──
$t2 = New-Task "lane-probe-2-cancel-while-starved"
if (-not $t2) { Bail "RESULT=TASK_CREATE_FAILED t2 没建成" 2 }
$running = $false
$deadline = (Get-Date).AddSeconds(90)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 1
  $s2 = Get-State $t2
  if (-not $s2) { continue }
  if ($s2.state -eq "running") { $running = $true; break }
  if ($s2.state -in @("failed", "done", "cancelled", "degraded", "rejected")) { Bail ("RESULT=TERMINATED_BEFORE_CANCEL state=" + $s2.state + " kind=" + $s2.errorKind + "（没拿到等待窗口 ⇒ 令牌未被 t1 耗尽？）") 2 }
}
if (-not $running) { Bail "RESULT=NEVER_RUNNING 取消前任务没进 running ⇒ 证据不成立" 2 }
$S_before = Snapshot "STEP2_before_cancel"
if ($null -eq $S_before) { Bail "RESULT=NO_SNAPSHOT_BEFORE" 2 }
Invoke-WebRequest -UseBasicParsing -Uri (U ("/api/tasks/" + $t2 + "/cancel")) -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 12 | Out-Null
Write-Host "CANCEL_REQUESTED_WHILE_BLOCKED_ON_TOKEN"
Start-Sleep -Seconds 8
$S_after = Snapshot "STEP3_after_cancel"
if ($null -eq $S_after) { Bail "RESULT=NO_SNAPSHOT_AFTER" 2 }
$lane2 = Ledger-Field $t2 "model" "cancel"
$err2 = Ledger-Field $t2 "error" "cancel"
$kind2 = Ledger-Field $t2 "kind" "cancel"
if (-not $lane2) { Bail "RESULT=NO_CANCEL_ROW 台账没有取消行 ⇒ 取消没落账，实验作废" 2 }
$pbf = if ($S_before.ContainsKey($lane2)) { [int]$S_before[$lane2] } else { -1 }
$paf = if ($S_after.ContainsKey($lane2)) { [int]$S_after[$lane2] } else { -1 }
if ($pbf -lt 0 -or $paf -lt 0) { Bail ("RESULT=LANE_COUNTER_MISSING lane=" + $lane2 + " before=" + $pbf + " after=" + $paf) 2 }
$delta = $paf - $pbf
# 别的车道在取消窗口内也动了 ⇒ 有并发活动，本次读数不作证据（宁可 B 也不留下可疑的 PASS）。
$others = @()
foreach ($k in $S_after.Keys) {
  if ($k -eq $lane2) { continue }
  if ($S_before.ContainsKey($k) -and [int]$S_after[$k] -ne [int]$S_before[$k]) { $others += ($k + ":" + $S_before[$k] + "->" + $S_after[$k]) }
}
if ($others.Count -gt 0) { Bail ("RESULT=CONCURRENT_NOISE 其它车道计数也变了（" + (($others | Select-Object -First 3) -join " ") + "）") 2 }
Write-Host ("CANCELLED_LANE " + $lane2 + " failure " + $pbf + " -> " + $paf + " delta=" + $delta)
Write-Host ("CANCEL_ROW kind=" + $kind2 + " named=" + (@($err2) -match "引擎停机|用户取消") + " error=" + $err2.Substring(0, [Math]::Min(120, $err2.Length)))

$verdict = "FAIL"; $reason = ""
if ($expectExtra) {
  if ($delta -ge 1) { $verdict = "PASS"; $reason = "对照件复现污染：取消给车道 " + $lane2 + " 多记 " + $delta + " 笔（" + $pbf + " -> " + $paf + "）" }
  else { $reason = "对照件也没污染（delta=" + $delta + "）⇒ 构造未生效或差分面选错，结论不成立" }
} else {
  if ($delta -eq 0) { $verdict = "PASS"; $reason = "取消未污染车道（该车道基线 " + $pbf + "，取消后仍 " + $paf + "）" }
  else { $reason = "取消污染了车道：delta=" + $delta + " ⇒ 修复未生效" }
}
Write-Host ("VERDICT_" + $verdict + " | " + $reason)
Stop-Instance
if ($verdict -eq "PASS") { exit 0 } else { exit 1 }

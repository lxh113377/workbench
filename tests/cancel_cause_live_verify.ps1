# cancel_cause_live_verify.ps1 -- issue #11「取消具名归因 + 取消不污染车道」的行为面证据（受控、隔离、可复跑）。
#
# 为什么需要这个件：上一轮用「零侵入监视器等自然取消」取证，跑了 240 分钟窗口，结论 NO_SAMPLE ——
#   生产那 4 小时没发生过一次真实取消（唯一候选行 20:53:04 早于新实例启动 20:53:11，是旧二进制写的）。
#   单测 + CI 已证明分类逻辑，但「真实引擎 + 真实 HTTP + 真实台账」这一层只有一次性证据；不留可复跑的件，
#   下次谁动 classifyCancellation 就得重新摸索（本仓纪律：验收判据跟着代码走，参照 round66_live_verify.ps1）。
#
# 为什么不去 POST 生产实例的 engine/stop：那会**故意打断老大的 7x24 在跑任务**换一条日志读数，代价不对等。
#   本件改为隔离实例：独立数据根 + 独立端口 + 假 DSH 慢车道 => 零真实模型调用、零额度、不碰 dist 与线上根。
#
# ⚠️ 本件能证明什么、不能证明什么（2026-10-29 实测两侧读数后写死，防下一个人把 0 读成"已验证"）：
#   能证明：具名归因的差分 —— 新件 error 含「引擎停机取消/用户取消…（原始错误: context canceled）」，
#     旧件（dist/workbench.exe.bak-pre-t9-*，ddded8，git merge-base 校验不含 6953c76）同一条路径只有裸
#     "context canceled"。user 与 shutdown 两种信号各实测一遍，结论一致（NAMED_TEXT 1 vs 0）。
#   **不能证明：车道污染（缺陷②）** —— 实测两侧 lane.failure 都是 0。原因查清了：本件的假 DSH 起得来，
#     executor 会自标 KindCancelled ⇒ 走的是旧代码也有的"kind==cancelled 就不记账"兜底分支（
#     classifyCancellation 的第 4 档）。要复现污染，取消必须正好落在 executor.go:398 的 cmd.Start()
#     上，让错误以「启动 DSH 失败: context canceled」的形态回来（ErrorKind=unknown）—— 生产那两行
#     正是这个组态，但本件无法稳定造出该时序，**不硬凑**。⇒ 缺陷②的证据仍是单测面
#     （TestCancellationDoesNotPenalizeLane + 变异负控，用 cancelProbeRunner 直接构造
#      ErrorKind="unknown" + 包装后的 context.Canceled 这个生产形状）+ 代码事实（RecordFailure
#     router.go:365 无条件 h.Failure++ ⇒ 污染只能来自调用时机，而时机已被"先判取消再记账"锁死）。
#   另记一次我自己的面选错：第一版读 /api/failures 得 0，差点结论成"缺陷②不存在"。
#     /api/failures 聚合的是 store 里 State=failed/timeout 的任务（engine.go:2273），而取消把任务落到
#     state=cancelled ⇒ 那个面**本来就不含**车道记账。"读数 0"先问"这个面含不含我要的那一类"。
# 差分设计（本件的全部意义）：同一脚本对两个 exe 各跑一遍
#   旧二进制（实测 dist/workbench.exe.bak-pre-t9-* = ddded8；git merge-base --is-ancestor 6953c76 ddded8 判假）
#     => 期望台账 kind 不是 cancelled（复现 issue #11 报的缺陷形态）
#   新二进制（含 6953c76）=> 期望 kind=cancelled 且 error 同时含「具名归因」与「原始错误」两头
#   只跑新版只能证明「它这样」，证明不了「旧版不这样」；两侧读数不同才算证明改动真的生效。
#
# 为什么假 DSH 必须是真 PE 可执行文件（不是改扩展名的 .bat）：executor.go:398 走 exec.CommandContext
#   => CreateProcess，批处理改名不是合法映像、起不来（本机实测过一次）。xtmp/fakedshmod 用纯 stdlib Go
#   编一个「参数一律忽略、睡到被杀」的 stub，才造得出「运行中被取消」的形态。
#
# 为什么 config 由本件自己写：xtmp/round66-verify 那份可用 config 是 gitignored 的手工件 => 依赖它，
#   这个验收判据在干净检出上就复现不出来（本仓在册的「断链引用」形态）。
#   pool.enabled=false 是刻意的：可用性探测走 HTTP（GET /v1/models），开着会把车道健康度翻掉 => 任务进不了
#   running，取到的就不是「运行中取消」。router.pickLocked 只看 rt.Enabled 与 health.Disabled/CooldownTo
#   （router.go:222-232），health 缺省按 !Enabled 生成（router.go:83）=> 一条 enabled=true 就能被选中。
#
# 用法: pwsh -NoProfile -File tests/cancel_cause_live_verify.ps1 -Exe <path> [-Port 20913] [-Tag new] [-ExpectNamed]
# 退出码: 0=VERDICT_PASS / 1=判红（kind 不对、无具名归因、丢了原始错误、或车道被记账）/ 2=环境不满足
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [int]$Port = 20913,
  [string]$Tag = 'new',
  [ValidateSet('user', 'shutdown')]
  [string]$Signal = 'user',
  [switch]$ExpectNamed
)
$ErrorActionPreference = "Continue"
$base = "D:/workdesktop/xtmp/cancel-verify-" + $Tag
$stub = "D:/workdesktop/xtmp/fakedshmod/dshstub.exe"
$wantNamed = [bool]$ExpectNamed

# 前置自检：缺任何一样都是「护栏没跑成」，不等于合格（本仓三态口径）
if (-not (Test-Path -LiteralPath $Exe)) { Write-Host ("RESULT=NO_EXE " + $Exe); exit 2 }
if (-not (Test-Path -LiteralPath $stub)) { Write-Host ("RESULT=NO_FAKE_DSH_STUB（先 go build xtmp/fakedshmod）: " + $stub); exit 2 }
# 干净起盘：删同名数据根，免得读到上一轮台账误判
if (Test-Path -LiteralPath $base) { Remove-Item -LiteralPath $base -Recurse -Force }
New-Item -ItemType Directory -Force -Path $base | Out-Null
Copy-Item -LiteralPath $Exe -Destination ($base + "/workbench.exe") -Force
Copy-Item -LiteralPath $stub -Destination ($base + "/dshstub.exe") -Force
$exe = $base + "/workbench.exe"
$fake = $base + "/dshstub.exe"
Write-Host ("TARGET exe=" + $exe + " size=" + (Get-Item $exe).Length + " fakeDsh=" + $fake)

# 落点由 Layout 决定，不手拼：<exe 目录>/workbench-output/config.json（internal/config/paths.go:12-16）
$dataRoot = $base + "/workbench-output"
New-Item -ItemType Directory -Force -Path $dataRoot | Out-Null
$laneModel = "cancel-verify/fake-lane"
$cfg = [ordered]@{
  schemaVersion = 1; autoResume = $false; resumeOnBoot = $false; autoStart = $false; outputRoot = ""
  models = @([ordered]@{ model = $laneModel; provider = "cancel-verify"; tier = "balanced"; enabled = $true; weight = 100; maxFail = 3; cooldownM = 30 })
  executor = [ordered]@{
    concurrency = 1; defaultTimeout = 45; maxRetries = 0; retryBackoffMs = @(5000)
    maxOutputMB = 32; stallSeconds = 900; dshExe = $fake; profileName = "web-desktop"
    shutdownGraceSeconds = 20; appendProtocol = $true; repairRounds = 0; requestsPerMinute = 60; requestBurst = 5
  }
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
$cfg | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath ($dataRoot + "/config.json") -Encoding UTF8
Write-Host ("CONFIG_WRITTEN port=" + $Port + " lane=" + $laneModel + " dshExe=" + $fake + " pool=off")

. "D:/workdesktop/tests/lib_silent_launch.ps1"
$lp = Start-WBInstance -Path $exe -Arguments "--headless"
Write-Host ("LAUNCHED_PID=" + $lp)
if (-not $lp) { Write-Host "RESULT=NO_PROCESS"; exit 2 }
$tok = ""
$deadline = (Get-Date).AddSeconds(70)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  try {
    $h = (Invoke-WebRequest -Uri ("http://127.0.0.1:" + $Port + "/") -UseBasicParsing -TimeoutSec 4).Content
    $m = [regex]::Match($h, "__WB_TOKEN__\s*=\s*" + [string][char]34 + "([a-zA-Z0-9_-]+)")
    if ($m.Success) { $tok = $m.Groups[1].Value; break }
  } catch { }
}
if (-not $tok) { Write-Host "RESULT=NO_TOKEN"; exit 2 }
Write-Host ("TOKEN_FOUND len=" + $tok.Length)
Invoke-WebRequest -UseBasicParsing -Uri ("http://127.0.0.1:" + $Port + "/api/engine/start?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 10 | Out-Null
Start-Sleep -Seconds 2
$body = @{ title = "cancel-verify"; prompt = "验证取消归因：请勿执行任何实际操作" } | ConvertTo-Json
$taskId = ""
try {
  $r = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $Port + "/api/tasks/manual?token=" + $tok) -Method POST -ContentType "application/json" -Body $body -TimeoutSec 12
  $taskId = $r.task.id
} catch { Write-Host ("ENQUEUE_EX " + $_.Exception.Message); exit 2 }
if (-not $taskId) { Write-Host "RESULT=ENQUEUE_FAIL"; exit 2 }
Write-Host ("TASK=" + $taskId)

# 必须等任务真的 running 再取消：queued 阶段取消走另一条分支（不进 runOne）=> 取到的是假证据
$running = $false
$deadline = (Get-Date).AddSeconds(150)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  try {
    $q = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $Port + "/api/tasks?token=" + $tok) -TimeoutSec 8
    $me = $q.tasks | Where-Object { $_.id -eq $taskId } | Select-Object -First 1
    if (-not $me) { continue }
    if ($me.state -eq "running") { $running = $true; Write-Host "RUNNING_SEEN"; break }
    if ($me.state -in @("failed", "done", "cancelled", "degraded", "rejected")) { Write-Host ("EARLY_TERMINAL state=" + $me.state + " kind=" + $me.errorKind); break }
  } catch { }
}
if (-not $running) { Write-Host "RESULT=NEVER_RUNNING（没造出「运行中被取消」，证据不成立）"; exit 2 }

try {
  if ($Signal -eq "shutdown") {
    # 停机信号：cancelAllRunning 走的是 causeEngineShutdown —— 生产那两条 kind=unknown 正是这条路径
    # （19:10 / 19:53 的部署停机），而**用户取消**在旧代码里本来就落 kind=cancelled（本轮差分实测）。
    Invoke-WebRequest -UseBasicParsing -Uri ("http://127.0.0.1:" + $Port + "/api/engine/stop?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 12 | Out-Null
    Write-Host "SHUTDOWN_REQUESTED"
  } else {
    Invoke-WebRequest -UseBasicParsing -Uri ("http://127.0.0.1:" + $Port + "/api/tasks/" + $taskId + "/cancel?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 12 | Out-Null
    Write-Host "CANCEL_REQUESTED"
  }
} catch { Write-Host ("CANCEL_FAIL " + $_.Exception.Message); exit 2 }
# 给引擎落台账的时间（runOne 的 defer + AppendAttempt 是异步收尾）
Start-Sleep -Seconds 8

$led = $dataRoot + "/tasks/" + $taskId + "/attempts.jsonl"
if (-not (Test-Path -LiteralPath $led)) { Write-Host ("RESULT=NO_LEDGER " + $led); exit 2 }
# 顺序循环而不是 @(... | ForEach-Object { try{...}catch{...} })：后者是 PowerShell 解析的麻烦形态，
# 本件实测报「意外的标记 ")"」。逐行解析、坏行跳过但计数（台账行坏了也是发现，不能静默丢）。
$rows = New-Object System.Collections.ArrayList
$bad = 0
foreach ($ln in [IO.File]::ReadAllLines($led)) {
  if (-not "$ln".Trim()) { continue }
  $o = $null
  try { $o = "$ln" | ConvertFrom-Json } catch { $o = $null; $bad++ }
  if ($o) { $rows.Add($o) | Out-Null }
}
Write-Host ("UNPARSEABLE_LEDGER_LINES=" + $bad)
$crows = @($rows | Where-Object { "$($_.action)" -eq "cancel" })
Write-Host ("LEDGER_ROWS=" + $rows.Count + " CANCEL_ROWS=" + $crows.Count)
foreach ($c in $crows) {
  $e = "$($c.error)"
  Write-Host ("  kind=" + $c.kind + " action=" + $c.action + " error=" + $e.Substring(0, [Math]::Min(160, $e.Length)))
}

$verdict = "FAIL"
$reason = ""
$kinds = ""
if ($crows.Count -eq 0) {
  $reason = "台账里没有 action=cancel 行 ⇒ 取消没落台账" 
} else {
  $kinds = ($crows | ForEach-Object { "$($_.kind)" }) -join ","
  $errs = ($crows | ForEach-Object { "$($_.error)" }) -join " ; "
  $named = @($crows | Where-Object { "$($_.kind)" -eq "cancelled" })
  # 具名归因 = cause 覆写生效的直接证据；本仓两条 cause 文案分别是「引擎停机取消」与「用户取消…」
  $withName = @($crows | Where-Object { ("$($_.error)") -match "引擎停机|用户取消" })
  # 原始错误必须同时留着：具名归因把证据顶掉 = 换个方向的取证缺失（river 也是 reason + 原错并存）
  $withRaw = @($crows | Where-Object { ("$($_.error)") -match "原始错误|context canceled" })
  Write-Host ("SIGNAL=" + $Signal + " KINDS=" + $kinds + " NAMED_TEXT=" + $withName.Count + " RAW_TEXT=" + $withRaw.Count)
  if ($wantNamed) {
    if ($named.Count -eq 0) { $reason = "kind 仍不是 cancelled（实测 kind=" + $kinds + "）⇒ 归因没生效，部署的可能是旧件" }
    elseif ($withName.Count -eq 0) { $reason = "kind 对了但 error 没有具名归因（实测 error=" + $errs + "）" }
    elseif ($withRaw.Count -eq 0) { $reason = "具名归因有了但丢了原始错误 ⇒ 取证两头不能只留一头" }
    else { $verdict = "PASS"; $reason = "kind=cancelled + 具名归因 + 原始错误俱在" }
  } else {
    # 对照件：本件要复现的是 issue #11 报的现场形态。本轮差分实测把缺陷定位收窄了 ——
    # 「用户取消」在旧代码里本来就落 kind=cancelled（只有裸 context canceled 文案），真正落成
    # kind=unknown 的是**引擎停机**那条路径（生产 19:10/19:53 两行正是部署停机写的）。
    # 所以对照面合格的判据 = 没有具名归因文案（kind 是否 cancelled 由 $Signal 决定，不作为失败条件）。
    if ($withName.Count -gt 0) { $reason = "对照件竟已带具名归因 ⇒ 差分面选错（拿到的不是旧件），本件结论不成立" }
    else { $verdict = "PASS"; $reason = "对照面复现缺陷：error 无具名归因（kind=" + $kinds + " error=" + $errs + "）" }
  }
}

# ── 车道污染面（issue #11 缺陷②）：取消不该让那条车道记一次失败 ──
# 面选错的教训（本轮实测）：第一版读 /api/failures，**两侧都读到 0**，差点结论成「缺陷②没复现、
# 也不存在」。但 /api/failures 聚合的是**任务台账**里 State=failed/timeout 的条数
# （engine.go:2273 FailureBreakdown -> store.List()），而取消把任务落到 state=**cancelled**，
# 本来就不进那个聚合面 ⇒ 读它等于没读。车道健康度在另一个面：/api/models 的 health
# （server.go:745 -> engine.RouterHealth() -> modelroute/router.go:164，字段 model/failure/
# consecFail/cooldownTo/lastErr）。⇒ 「读数 0」必须先问「这个面含不含我要的那一类」。
# 两侧都读（旧侧的污染正是要展示的差异），但只在期望具名的一侧判红：对对照件判这项会把
# 「复现缺陷」误报成「差分失败」。
$laneFailure = -1
$laneCooldown = -1
if ($true) {
  try {
    $m = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $Port + "/api/models?token=" + $tok) -TimeoutSec 10
    $h = $m.health | Where-Object { "$($_.model)" -eq $laneModel } | Select-Object -First 1
    if ($h) {
      $laneFailure = [int]$h.failure
      $laneCooldown = [long]$h.cooldownTo
      Write-Host ("LANE model=" + $h.model + " failure=" + $h.failure + " consecFail=" + $h.consecFail + " cooldownTo=" + $h.cooldownTo + " lastErr=" + ("$($h.lastErr)").Substring(0, [Math]::Min(90, ("$($h.lastErr)").Length)))
    } else { Write-Host ("LANE_NOT_IN_HEALTH lane=" + $laneModel) }
    $f = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $Port + "/api/failures?token=" + $tok) -TimeoutSec 10
    $bk = ($f.buckets | ForEach-Object { "$($_.kind):$($_.count)" }) -join " "
    Write-Host ("TASK_FAILURE_BUCKETS total=" + $f.total + " buckets=" + $bk)
  } catch { Write-Host ("models_read_fail " + $_.Exception.Message) }
  if ($wantNamed) {
    if ($laneFailure -lt 0) { $verdict = "FAIL"; $reason += " | 车道健康面取不到读数（判据没跑到东西上，不算通过）" }
    elseif ($laneFailure -gt 0) { $verdict = "FAIL"; $reason += " | 车道被记 " + $laneFailure + " 次失败（取消不该污染车道）" }
    elseif ($laneCooldown -gt 0) { $verdict = "FAIL"; $reason += " | 车道仍在冷却（cooldownTo=" + $laneCooldown + "）" }
    else { Write-Host ("LANE_POLLUTION=0（车道未被取消污染）") }
  }
}

Write-Host ("VERDICT_" + $verdict + " | " + $reason)

# 收尾：停引擎 + --quit + 按命令行精确复核进程清零（隔离实例不被生产守护接管，必须自己扫干净）
try { Invoke-WebRequest -UseBasicParsing -Uri ("http://127.0.0.1:" + $Port + "/api/engine/stop?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 10 | Out-Null } catch { }
Start-Sleep -Seconds 3
try { Start-Process -FilePath $exe -ArgumentList "--quit" -WindowStyle Hidden | Out-Null } catch { }
Start-Sleep -Seconds 8
$left = @(Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" -ErrorAction SilentlyContinue | Where-Object { "$($_.CommandLine)" -match "cancel-verify" })
Write-Host ("LEFTOVER_PROCS=" + $left.Count)
foreach ($p in $left) { try { Stop-Process -Id $p.ProcessId -Force -ErrorAction Stop; Write-Host ("killed " + $p.ProcessId) } catch { } }
$dleft = @(Get-CimInstance Win32_Process -Filter "Name='dshstub.exe'" -ErrorAction SilentlyContinue)
Write-Host ("STUB_PROCS=" + $dleft.Count)
foreach ($p in $dleft) { try { Stop-Process -Id $p.ProcessId -Force -ErrorAction Stop } catch { } }
Write-Host "VERIFY_DONE"
if ($verdict -eq "PASS") { exit 0 } else { exit 1 }


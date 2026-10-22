param(
  # 默认产物由脚本位置推导（tests/ 的父目录 + dist\workbench.exe），不硬编码。
  # 2026-10-23 恢复时修正：原版硬编码 D:\workdesktop —— 在干净 clone / CI / 他人机器上
  # 会指向不存在的路径（或另一棵树），与本仓 2026-10-22 修过的门禁同一类失真。
  [string]$Exe = "",
  [int]$WaitMinutes = 8
)
$ErrorActionPreference = "Stop"

# 默认产物路径推导（tests/ 的父目录 = 仓库根）
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path (Split-Path -Parent $PSScriptRoot) "dist\workbench.exe" }
$pass = 0; $fail = 0
function Check($name, $cond, $detail = "") {
  if ($cond) { Write-Host "  [PASS] $name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "  [FAIL] $name $detail" -ForegroundColor Red; $script:fail++ }
}
if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
$root = Split-Path -Parent $Exe
$data = Join-Path $root "workbench-output"
# 只处理「本验收目标 Exe 路径」下的 workbench 进程。
# 同机可能有其他会话/其他副本（verify-*）跑同名 exe，按名字全杀会互相干扰（实测：
# 对端杀进程导致本端 /api/gov/budget 出现 "远程主机强迫关闭了一个现有的连接"）。
function Get-TargetProcs {
  Get-Process -Name workbench -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe }
}
function Stop-TargetProcs {
  Get-TargetProcs | ForEach-Object { & taskkill /PID $_.Id /F 2>&1 | Out-Null }
}
# 同机并发会话会重启/杀掉 workbench.exe（实测本脚本曾在轮询中途遇到 "目标计算机积极拒绝"），
# 因此会话级调用必须可重入：失败时重新解析 panel.url 与 token 再重试，
# 避免把"对端重启进程"误判成本脚本的失败。
$script:ApiRetry = 6
function Refresh-Session {
  for ($i = 0; $i -lt $script:ApiRetry; $i++) {
    try {
      $script:url = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
      $page = Invoke-WebRequest -Uri $script:url -UseBasicParsing -TimeoutSec 8
      $script:token = [regex]::Match($page.Content, $pattern).Groups[1].Value
      if ($script:token.Length -gt 10) {
        $script:headers = @{ "X-WB-Token" = $script:token; "Content-Type" = "application/json" }
        return $true
      }
    } catch { }
    Start-Sleep 3
  }
  return $false
}
function Invoke-Api {
  param([string]$Path, [string]$Method = "GET", [string]$Body = $null)
  for ($i = 0; $i -lt $script:ApiRetry; $i++) {
    try {
      if ($Method -eq "POST") {
        return Invoke-RestMethod -Uri ($script:url + $Path) -Method Post -Headers $script:headers -Body $Body -TimeoutSec 20
      }
      return Invoke-RestMethod -Uri ($script:url + $Path) -Headers $script:headers -TimeoutSec 20
    } catch {
      if ($i -eq ($script:ApiRetry - 1)) { throw }
      Start-Sleep 3
      [void](Refresh-Session)
    }
  }
}
Write-Host "===== 1. STARTUP =====" -ForegroundColor Cyan
Stop-TargetProcs
Start-Sleep 3
Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $Exe; CurrentDirectory = $root } | Out-Null
Start-Sleep 12
Check "process running" ((Get-Process -Name workbench -ErrorAction SilentlyContinue) -ne $null)
Check "lock file exists" (Test-Path (Join-Path $root "runtime\workbench.lock"))
Write-Host "===== 2. PANEL =====" -ForegroundColor Cyan
$pattern = 'window.__WB_TOKEN__ = "([^"]+)"'
$script:url = ""
$script:token = ""
$script:headers = @{}
Check "panel url written" ((Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim() -like "http://127.0.0.1:*")
$ok = Refresh-Session
Check "token injected" ($ok -and $script:token.Length -gt 10)
Check "isolated DSH_HOME" (Test-Path (Join-Path $root "runtime\dsh-home\profiles\web-desktop"))
Check "OFM mounted" (Test-Path (Join-Path $root "runtime\dsh-home\profiles\web-desktop\node_modules\dsh-our-free-model"))
$tpl = Invoke-Api "api/templates"
Check "24 templates" ($tpl.templates.Count -eq 24) ("actual " + $tpl.templates.Count)
Write-Host "===== 3. ENGINE =====" -ForegroundColor Cyan
# 记录"本轮开始前就已完成"的任务 ID：历史数据里存在 54–208 字节的旧短件，
# 它们与本轮产出质量无关，不应计入本轮非空率（否则判据会随历史数据漂移而假红）。
$preDone = @{}
try {
  $preRes = Invoke-RestMethod -Uri ($url + "api/tasks?state=done") -Headers $headers -TimeoutSec 20
  foreach ($t in @($preRes.tasks)) { if ($t.id) { $preDone[$t.id] = $true } }
} catch { }
Write-Host ("  历史已完成任务: " + $preDone.Count + " 条（不计入本轮非空率）")
Invoke-Api "api/engine/start" "POST" "{}" | Out-Null
Start-Sleep 5
$st = Invoke-Api "api/status"
Check "engine running" ($st.phase -eq "running") ("actual " + $st.phase)
Check "tasks generated" ($st.stats.totalTasks -gt 0) ("actual " + $st.stats.totalTasks)
Write-Host "  waiting up to $WaitMinutes min for first output..."
# 关键：doneTasks 里包含历史任务，直接判 "> 0" 会在有历史数据时立刻为真（实测本轮
# 一开始就已 done=77），导致后续"本轮新产出"判据拿到 0/0。必须等**本轮新任务**落 done。
$deadline = (Get-Date).AddMinutes($WaitMinutes)
$gotDone = $false
while ((Get-Date) -lt $deadline) {
  Start-Sleep 15
  try {
    $s = Invoke-Api "api/status"
    $freshDone = 0
    try {
      $doneNow = Invoke-Api "api/tasks?state=done"
      $freshDone = @($doneNow.tasks | Where-Object { -not $preDone.ContainsKey($_.id) }).Count
    } catch { $freshDone = 0 }
    if ($freshDone -gt 0) { $gotDone = $true; break }
    Write-Host ("    本轮新完成=" + $freshDone + " 历史=" + $s.stats.doneTasks + " 队列=" + $s.stats.queuedTasks + " 执行中=" + $s.stats.runningTasks + " 失败=" + $s.stats.failedTasks)
  } catch {
    Write-Host ("    连接中断，重试中: " + $_.Exception.Message)
  }
}
Check "first output produced" $gotDone
if ($gotDone) {
  # 任务列表按状态/时间排序后首条不一定是最新产出（实测出现了 103 字节的短件排在最前），
  # 判据应落在"本轮新产出的非空件"上：取 outputBytes 最大的那条，并额外统计达标覆盖率。
  $done = Invoke-Api "api/tasks?state=done"
  $task = $done.tasks | Sort-Object -Property outputBytes -Descending | Select-Object -First 1
  # 只看本轮新产出的任务（排除历史遗留短件），否则判据被历史数据绑架
  $fresh = @($done.tasks | Where-Object { -not $preDone.ContainsKey($_.id) })
  $nonEmpty = @($fresh | Where-Object { $_.outputBytes -gt 500 }).Count
  $totalDoneCount = $fresh.Count
  $task = $fresh | Sort-Object -Property outputBytes -Descending | Select-Object -First 1
  Check "output non-empty" ($task.outputBytes -gt 500) ("actual " + $task.outputBytes)
  Check "non-empty output ratio >= 90%" (($totalDoneCount -gt 0) -and ($nonEmpty / $totalDoneCount -ge 0.9)) ("actual " + $nonEmpty + "/" + $totalDoneCount + " 本轮新产出")
  # 自评是"任务落 done 之后"的异步后处理（engine.postProcess 注释明写：后台异步，不阻塞调度），
  # 所以判据必须按覆盖率先等后判——单点断言首条任务会在 postProcess 未完成时假红。
  $critiqueDeadline = (Get-Date).AddMinutes(5)
  $coverage = 0
  while ((Get-Date) -lt $critiqueDeadline) {
    $allDone = Invoke-Api "api/tasks?state=done"
    # 只统计本轮新产出（历史任务的自评早已完成，混入会掩盖本轮异步自评未完成的事实）
    $freshCrit = @($allDone.tasks | Where-Object { -not $preDone.ContainsKey($_.id) })
    $withCritique = @($freshCrit | Where-Object { $_.critique -and $_.critique.quality -gt 0 }).Count
    $totalDone = $freshCrit.Count
    if ($totalDone -gt 0) { $coverage = [math]::Round(100 * $withCritique / $totalDone) }
    if ($coverage -ge 80) { break }
    Write-Host ("    自评覆盖 " + $withCritique + "/" + $totalDone + " = " + $coverage + "%")
    Start-Sleep 10
  }
  Check "critique coverage >= 80%" ($coverage -ge 80) ("actual " + $coverage + "%")
}
Write-Host "===== 4. EXPORT =====" -ForegroundColor Cyan
foreach ($fmt in @("md","txt","docx")) {
  try {
    $body = @{format=$fmt;scope="all"} | ConvertTo-Json
    $r = Invoke-RestMethod -Uri ($url + "api/export") -Method Post -Headers $headers -Body $body -TimeoutSec 30
    Check ("export " + $fmt) ((Test-Path $r.path) -and ((Get-Item $r.path).Length -gt 100)) $r.path
  } catch { Check ("export " + $fmt) $false $_.Exception.Message }
}
Write-Host "===== 5. RESUME (A4) =====" -ForegroundColor Cyan
$q1 = Get-Content (Join-Path $data "queue.json") -Raw | ConvertFrom-Json
$before = @{}
$q1.tasks | Where-Object { $_.state -eq "done" } | ForEach-Object { $before[$_.id] = $_.outputBytes }
Write-Host ("  recorded " + $before.Count + " done tasks")
Stop-TargetProcs
Start-Sleep 4
Get-CimInstance Win32_Process -Filter "Name='DSH Desktop.exe'" | Where-Object { $_.CommandLine -like '*--profile web-desktop*' } | ForEach-Object { & taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Start-Sleep 2
Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $Exe; CurrentDirectory = $root } | Out-Null
Start-Sleep 15
$q2 = Get-Content (Join-Path $data "queue.json") -Raw | ConvertFrom-Json
$allPreserved = $true
foreach ($id in $before.Keys) {
  $t = $q2.tasks | Where-Object { $_.id -eq $id }
  if (-not $t -or $t.state -ne "done" -or $t.outputBytes -ne $before[$id]) { $allPreserved = $false; Write-Host ("    lost: " + $id) -ForegroundColor Yellow }
}
Check "resume: zero loss zero rerun" $allPreserved
Write-Host "===== 6. AUTOSTART (A9) =====" -ForegroundColor Cyan
$url2 = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
$pageHtml2 = Invoke-WebRequest -Uri $url2 -UseBasicParsing -TimeoutSec 10
$token2 = [regex]::Match($pageHtml2.Content, $pattern).Groups[1].Value
$h2 = @{ "X-WB-Token" = $token2; "Content-Type" = "application/json" }
$onBody = @{enabled=$true} | ConvertTo-Json
$offBody = @{enabled=$false} | ConvertTo-Json
Invoke-RestMethod -Uri ($url2 + "api/autostart") -Method Post -Headers $h2 -Body $onBody -TimeoutSec 20 | Out-Null
$reg = reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v PersonalWorkbench 2>&1 | Out-String
Check "autostart registered" ($reg -like "*PersonalWorkbench*")
Invoke-RestMethod -Uri ($url2 + "api/autostart") -Method Post -Headers $h2 -Body $offBody -TimeoutSec 20 | Out-Null
$ErrorActionPreference = "Continue"
$reg2 = reg query "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v PersonalWorkbench 2>&1 | Out-String
$ErrorActionPreference = "Stop"
Check "autostart removed" ($reg2 -notlike "*PersonalWorkbench*")
Write-Host "===== 7. RESOURCE =====" -ForegroundColor Cyan
$proc = @(Get-TargetProcs) | Select-Object -First 1
if ($proc) {
  $rss = [math]::Round($proc.WorkingSet64/1MB)
  Write-Host ("  RSS=" + $rss + "MB Handles=" + $proc.HandleCount)
  # 阈值校准（2026-10-23 恢复时）：原版拍 200MB，与仓库基线 docs/benchmark/perf-baseline.json
  # 的 residentMBMax（64MB）都不是一个量级 —— 200MB 既放得过 3 倍劣化，又与基线口径冲突。
  # 改为**按基线判**：读 perf-baseline.json 的 residentMBMax，读不到则退回 200MB 并显式告警
  # （不把「读不到基线」当成合格）。当前实测高峰会判红 —— 那是真实质量差距，不是判据错。
  $rssMax = 200
  $blPath = Join-Path (Split-Path -Parent $PSScriptRoot) "docs\benchmark\perf-baseline.json"
  if (Test-Path $blPath) {
    try {
      $blObj = Get-Content $blPath -Raw | ConvertFrom-Json
      if ($blObj.residentMBMax -and $blObj.residentMBMax -gt 0) { $rssMax = [double]$blObj.residentMBMax }
    } catch { Write-Host ("    WARN 基线解析失败，退回 200MB") -ForegroundColor Yellow }
  } else { Write-Host ("    WARN 缺 perf-baseline.json，退回 200MB") -ForegroundColor Yellow }
  Check ("memory <= " + $rssMax + "MB（基线口径）") ($rss -le $rssMax) ("actual " + $rss + "MB")
  Check "handles < 3000" ($proc.HandleCount -lt 3000) ("actual " + $proc.HandleCount)
}
Write-Host "===== 8. GOVERNANCE =====" -ForegroundColor Cyan
# 治理子系统验收：CLI 只读出口 + HTTP 契约/体量/联动/考核全链
$govExe = $Exe
$govStatus = & $govExe --governance status 2>&1 | Out-String
Check "gov CLI status runs" ($govStatus -like "*治理等级*") "output missing"
Check "gov CLI reports checklist" ($govStatus -like "*考核校验*") "checklist missing"
Check "gov CLI declares data gaps or metrics" (($govStatus -like "*体量*") -and ($govStatus -like "*联动*"))
$govBudget = & $govExe --governance budget 2>&1 | Out-String
Check "gov CLI budget effective values" ($govBudget -like "*memLimitMB*")
$govJson = & $govExe --governance json 2>&1 | Out-String
$govObj = $null
try { $govObj = $govJson | ConvertFrom-Json } catch { $govObj = $null }
Check "gov CLI json parses" ($null -ne $govObj -and $null -ne $govObj.level)
Check "gov CLI json has three domains" ($govObj.evals.Count -eq 13) ("actual " + $govObj.evals.Count)

$urlG = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
$pageG = Invoke-WebRequest -Uri $urlG -UseBasicParsing -TimeoutSec 10
$tokenG = [regex]::Match($pageG.Content, $pattern).Groups[1].Value
$hG = @{ "X-WB-Token" = $tokenG; "Content-Type" = "application/json" }

$govPage = Invoke-WebRequest -Uri ($urlG + "governance") -UseBasicParsing -TimeoutSec 10
Check "governance console page" ($govPage.StatusCode -eq 200 -and $govPage.Content -like "*治理控制台*")
$govJs = Invoke-WebRequest -Uri ($urlG + "assets/gov.js") -UseBasicParsing -TimeoutSec 10
Check "governance console script" ($govJs.StatusCode -eq 200 -and $govJs.Content.Length -gt 1000)

$adminG = Invoke-WebRequest -Uri ($urlG + "admin") -UseBasicParsing -TimeoutSec 10
# 判据校准（2026-10-23 恢复时）：治理台已从「管理面板内的 tab」改为**独立页面**，
# 入口挂在管理面板顶栏「更多」菜单里的 <a href="/governance?token=...">。
# 旧断言找 data-tab="governance" 在当前产品形态下必然失败（不是缺陷，是形态变了）。
# 判据强度不降：仍要求管理面板**真的提供**通往治理台的链接（含 token 透传），
# 而不是只看页面能打开 —— 页面能打开但入口不存在，用户根本到不了。
Check "admin has governance entry" ($adminG.Content -like '*/governance?token=*')

$sumG = Invoke-RestMethod -Uri ($urlG + "api/gov/summary") -Headers $hG -TimeoutSec 20
Check "gov summary api" ($null -ne $sumG.level)
Check "gov summary three domains" ($sumG.domains.Count -eq 3) ("actual " + $sumG.domains.Count)
Check "gov summary 13 metrics" ($sumG.evals.Count -eq 13) ("actual " + $sumG.evals.Count)
Check "gov summary checklist" ($sumG.checklist.items.Count -ge 6) ("actual " + $sumG.checklist.items.Count)
Check "gov summary linkage" ($null -ne $sumG.linkage.level)

$budG = Invoke-RestMethod -Uri ($urlG + "api/gov/budget") -Headers $hG -TimeoutSec 20
Check "gov budget api" ($budG.budget.memLimitMB -gt 0)

# 契约全生命周期：起草 → 审核 → 签署 → 履约 → 归档（含非法迁移拦截）
$cid = "C-E2E-" + (Get-Date -Format "HHmmss")
$bodyG = @{
  id = $cid; title = "E2E 治理契约"; counterpart = "E2E 相对方"; stage = "draft"
  owner = "o"; drafter = "d"; reviewer = "r"; signer = "s"; perfMonitor = "p"; archivist = "a"
  amount = 250000; expireAt = "2027-06-30"; deliverables = @("交付物A")
  clauses = @(@{ id = "c1"; title = "付款"; riskLevel = 1; signed = $true })
} | ConvertTo-Json -Depth 6
# PS 5.1 的 Invoke-RestMethod 传 string body 时按 ASCII 编码发送 ⇒ 中文变 ????（一手实测：
# 同一份 body 传 UTF-8 字节则中文完好）。必须显式转字节，否则契约标题落库即乱码。
$bodyBytes = [System.Text.Encoding]::UTF8.GetBytes($bodyG)
$newC = Invoke-RestMethod -Uri ($urlG + "api/gov/contracts") -Method Post -Headers $hG -Body $bodyBytes -TimeoutSec 20
Check "contract created" ($newC.id -eq $cid) ("actual " + $newC.id)
Check "contract title UTF-8 round-trip" ($newC.title -eq "E2E 治理契约") ("actual " + $newC.title)

$illegal = $false
try {
  Invoke-RestMethod -Uri ($urlG + "api/gov/contracts/$cid/transition") -Method Post -Headers $hG -Body (@{to="archived";actor="e2e"} | ConvertTo-Json) -TimeoutSec 20 | Out-Null
} catch { $illegal = $true }
Check "illegal stage jump rejected" $illegal

$stages = @("review", "signed", "performing", "archived")
$stageOk = $true
foreach ($sg in $stages) {
  try {
    $r = Invoke-RestMethod -Uri ($urlG + "api/gov/contracts/$cid/transition") -Method Post -Headers $hG -Body (@{to=$sg;actor="e2e-manager";note="E2E 阶段推进"} | ConvertTo-Json) -TimeoutSec 20
    if ($r.stage -ne $sg) { $stageOk = $false }
  } catch { $stageOk = $false; Write-Host ("    stage " + $sg + " failed: " + $_.Exception.Message) -ForegroundColor Yellow }
}
Check "lifecycle draft→review→signed→performing→archived" $stageOk
$detailG = Invoke-RestMethod -Uri ($urlG + "api/gov/contracts/$cid") -Headers $hG -TimeoutSec 20
Check "archived contract keeps trail" ($detailG.contract.trail.Count -ge 4) ("actual " + $detailG.contract.trail.Count)
Check "archived contract has compliance" ($detailG.compliance -gt 0) ("actual " + $detailG.compliance)

$reG = Invoke-RestMethod -Uri ($urlG + "api/gov/recompute") -Method Post -Headers $hG -Body "{}" -TimeoutSec 20
# 空数组经 ConvertFrom-Json 会变成 $null（PowerShell 5.1 行为）：判据必须落在"字段存在"上，
# 不能写 ($null -ne $reG.alerts) —— 那会把"重算成功但当前无预警"误判成失败（2026-10-18 一手）。
$reProps = @($reG.PSObject.Properties.Name)
Check "recompute returns alerts field" ($reProps -contains "alerts") ("props=" + ($reProps -join ","))
Check "recompute returns linkage field" ($reProps -contains "linkage")
if ($reProps -contains "alerts" -and $reG.alerts) {
  $firstAlert = @($reG.alerts)[0]
  Check "alert carries evidence" ([bool]($firstAlert.evidence)) ("actual " + $firstAlert.evidence)
}
$sampG = Invoke-RestMethod -Uri ($urlG + "api/gov/sample") -Method Post -Headers $hG -Body "{}" -TimeoutSec 20
Check "sample api returns summary" ($null -ne $sampG.level)

# 治理数据落盘校验
$govDir = Join-Path $data "governance"
Check "governance data dir" (Test-Path $govDir)
Check "samples jsonl written" ((Test-Path (Join-Path $govDir "samples.jsonl")) -and ((Get-Item (Join-Path $govDir "samples.jsonl")).Length -gt 10))
Check "contracts snapshot written" (Test-Path (Join-Path $govDir "contracts.json"))
# ===== 9. 工程化（P1/P2 改造验收）=====
Write-Host "===== 9. OBSERVABILITY =====" -ForegroundColor Cyan
# 第 5 段强杀重启过进程，token 已轮换：重新取一次页面 token（沿用第 8 段同款做法）
$url9 = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
$page9 = Invoke-WebRequest -Uri $url9 -UseBasicParsing -TimeoutSec 10
$token9 = [regex]::Match($page9.Content, $pattern).Groups[1].Value
$h9 = @{ "X-WB-Token" = $token9; "Content-Type" = "application/json" }
# 结构化日志（P1-1）：engine/supervisor 统一写 workbench.jsonl，行必须是 JSON
$logPath = Join-Path $data "logs\workbench.jsonl"
Check "structured log file exists" (Test-Path $logPath)
if (Test-Path $logPath) {
  $logLines = @(Get-Content $logPath -Tail 20 | Where-Object { $_.Trim() -ne "" })
  Check "structured log has entries" ($logLines.Count -gt 0) ("actual " + $logLines.Count)
  $jsonOk = $true
  foreach ($ln in $logLines) {
    try { $null = $ln | ConvertFrom-Json } catch { $jsonOk = $false; break }
  }
  Check "structured log lines are JSON" $jsonOk
  $logAll = Get-Content $logPath -Raw
  Check "structured log has level field" ($logAll -match '"level"')
  Check "structured log declares component or task fields" (($logAll -match '"component"') -or ($logAll -match '"taskId"'))
}
# 旧的分散裸日志不应再新增（允许历史文件存在）
$engineDebug = Join-Path $data "logs\engine-debug.log"
if (Test-Path $engineDebug) {
  $ageMin = ((Get-Date) - (Get-Item $engineDebug).LastWriteTime).TotalMinutes
  Check "legacy engine-debug.log not written by new build" ($ageMin -gt 5) ("age=" + [math]::Round($ageMin,1) + "min")
} else {
  Check "legacy engine-debug.log not created" $true
}

# 指标接口（P1-3）
$met = Invoke-RestMethod -Uri ($url9 + "api/metrics") -Headers $h9 -TimeoutSec 15
Check "metrics api returns shape" ($null -ne $met.at -and $null -ne $met.tasks)
Check "metrics api has resource limits" ($met.memLimitMB -gt 0 -and $met.handleLimit -gt 0)
Check "metrics api has attempts array" ($met.attempts -ne $null)

# 失败原因分布（P1-4）：字段契约 + 空态语义
$failApi = Invoke-RestMethod -Uri ($url9 + "api/failures") -Headers $h9 -TimeoutSec 15
Check "failures api returns total" ($null -ne $failApi.total)
Check "failures api declares empty flag" ($null -ne $failApi.empty -or $failApi.empty -eq $false)
Check "failures api buckets is array" ($failApi.buckets -ne $null)

Check "linkage snapshot written" (Test-Path (Join-Path $govDir "linkage.json"))
Write-Host "===== 10. AUTONOMY =====" -ForegroundColor Cyan
# 自主运行子系统验收（目标要求 1-4）：离线闭环自检 + 运行时 CLI + HTTP + 落盘 + 兼容性
$autoExe = $Exe
$autoSelf = & $autoExe --autonomy selftest 2>&1 | Out-String
$autoSelfPass = [regex]::Match($autoSelf, "RESULT: (\d+) PASS / (\d+) FAIL")
Check "autonomy selftest all pass" ($autoSelfPass.Success -and [int]$autoSelfPass.Groups[2].Value -eq 0) (($autoSelf -split [char]10) -join " | ")
Check "autonomy selftest covers 11 assertions" ($autoSelfPass.Success -and [int]$autoSelfPass.Groups[1].Value -ge 11) ($autoSelfPass.Groups[1].Value)

$autoStatusRaw = & $autoExe --autonomy status 2>&1 | Out-String
$autoStatus = $null
try { $autoStatus = $autoStatusRaw | ConvertFrom-Json } catch { $autoStatus = $null }
Check "autonomy CLI status parses" ($null -ne $autoStatus)
Check "autonomy CLI reports active goal" ($null -ne $autoStatus -and $null -ne $autoStatus.active)
Check "autonomy goal carries machine criteria" ($null -ne $autoStatus -and $autoStatus.active.criteria.Count -ge 3) ("actual " + $(if ($autoStatus -and $autoStatus.active) { $autoStatus.active.criteria.Count } else { 0 }))
Check "autonomy CLI exposes enabled flag" ([bool]($autoStatusRaw -match '"enabled"'))

# 运行时 HTTP 出口（自主页签数据面）
$urlA = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
$pageA = Invoke-WebRequest -Uri $urlA -UseBasicParsing -TimeoutSec 10
$tokenA = [regex]::Match($pageA.Content, $pattern).Groups[1].Value
$hA = @{ "X-WB-Token" = $tokenA; "Content-Type" = "application/json" }
$autoSum = $null
try { $autoSum = Invoke-RestMethod -Uri ($urlA + "api/autonomy") -Headers $hA -TimeoutSec 15 } catch { $autoSum = $null }
Check "autonomy api returns summary" ($null -ne $autoSum -and ($null -ne $autoSum.PSObject.Properties["goals"]))
$decA = $null
try { $decA = Invoke-RestMethod -Uri ($urlA + "api/autonomy/decisions?limit=5") -Headers $hA -TimeoutSec 15 } catch { $decA = $null }
Check "autonomy decisions api returns ledger" ($null -ne $decA -and ($null -ne $decA.PSObject.Properties["decisions"]))

# 面板自主页签与决策台账落盘
$adminA = Invoke-WebRequest -Uri ($urlA + "admin?token=" + $tokenA) -UseBasicParsing -TimeoutSec 10
Check "admin has autonomy tab" ($adminA.Content -like '*data-tab="autonomy"*')
$autoDir = Join-Path $data "autonomy"
Check "autonomy data dir exists" (Test-Path $autoDir)
Check "autonomy goals.json written" ((Test-Path (Join-Path $autoDir "goals.json")) -and ((Get-Item (Join-Path $autoDir "goals.json")).Length -gt 100))
Check "autonomy decisions.jsonl written" ((Test-Path (Join-Path $autoDir "decisions.jsonl")) -and ((Get-Item (Join-Path $autoDir "decisions.jsonl")).Length -gt 50))

# 兼容性：固定模板链路未被移除（24 条模板 + 模板页签仍在）
$tplA = Invoke-RestMethod -Uri ($urlA + "api/templates") -Headers $hA -TimeoutSec 10
Check "fixed-template chain still available" ($tplA.templates.Count -eq 24) ("actual " + $tplA.templates.Count)
Check "admin keeps templates tab" ($adminA.Content -like '*data-tab="templates"*')

Write-Host "========================================" -ForegroundColor Cyan
if ($fail -eq 0) { Write-Host ("RESULT: " + $pass + " PASS / 0 FAIL") -ForegroundColor Green }
else { Write-Host ("RESULT: " + $pass + " PASS / " + $fail + " FAIL") -ForegroundColor Red }
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -gt 0) { exit 1 } else { exit 0 }
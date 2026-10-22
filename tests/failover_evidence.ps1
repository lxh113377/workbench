# failover_evidence.ps1 -- 产出「真实故障转移 + 真实闭环」运行时证据（总验收 R4R5 / A2）
#
# 为什么需要这个脚本：
#   R4R5 判据要求 model-status.jsonl 里同时有 probe 与 switch 记录，而 switch 只在
#   「一次真实执行失败并换模型」时产生；A2 要求 decisions.jsonl 里出现 goalDone+nextGoal，
#   而这两条都需要模型真的把任务跑完。本机真实通道受上游 IP 封禁（429/401），
#   无法稳定制造。
#
# 做法：用 tests/docker-autonomy/fakedsh 的「按提示词分流」替身，加一个
#   data/failfirst 变体：对指定模型注入 upstream_failed，其余模型正常返回产物。
#   于是工作台会：真实执行失败 -> FailoverRunner 真实换模型 -> 真实落盘 -> 真实验收
#   -> 阶段推进 -> 目标完成 -> 自动推导下一目标。全程真实执行、真实切换。
#
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File tests\failover_evidence.ps1
# 前置（夹具构建，实测踩过）：夹具源码在 tests/failover/，自带 go.mod（module failoverfake），
#   是独立小模块，必须在 tests/failover 目录**内**构建：
#     cd tests\failover ; go build -o ..\..\dist\fakedsh-failfirst.exe .
#   在 workbench/ 模块里用 ../tests 路径会报
#   "directory ../tests/failover outside main module or its selected dependencies"。
# 退出码: 0 = 证据齐全（switch>=1 且 goalDone>=1 且 nextGoal>=1）；1 = 证据不足
param([string]$Exe = "D:\workdesktop\dist\workbench.exe", [string]$FailFirst = "D:\workdesktop\dist\fakedsh-failfirst.exe", [int]$WaitMinutes = 5)
$ErrorActionPreference = "Stop"
if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
if (-not (Test-Path $FailFirst)) { throw "not found: $FailFirst (先用 go build 构建 fakedsh-failfirst)" }
$root = Join-Path $env:TEMP ("wb-fo-" + [guid]::NewGuid().ToString("N").Substring(0,8))
New-Item -ItemType Directory -Force -Path $root | Out-Null
Copy-Item $Exe (Join-Path $root "wbfo.exe") -Force
Write-Host ("instance root: " + $root) -ForegroundColor Cyan
$cfgDir = Join-Path $root "workbench-output"
New-Item -ItemType Directory -Force -Path $cfgDir | Out-Null
# 配置：单模型路由（该模型被注入失败 ==> 必然触发切换）
$cfg = [pscustomobject]@{
  schemaVersion = 5; autoResume = $false; resumeOnBoot = $false; autoStart = $false; outputRoot = "";
  models = @([pscustomobject]@{ model="z-ai/glm-5.3"; provider="our-free-model"; tier="balanced"; enabled=$true; weight=100; maxFail=3; cooldownM=30 })
  executor = [pscustomobject]@{ concurrency=1; defaultTimeout=10; maxRetries=1; retryBackoffMs=@(1000); maxOutputMB=32; stallSeconds=300;
    dshExe=$FailFirst; profileName="web-desktop"; shutdownGraceSeconds=5; appendProtocol=$false; repairRounds=0;
    requestsPerMinute=6000; requestBurst=600;
    retryPolicy=[pscustomobject]@{ baseDelayMs=1000; maxDelayMs=30000; jitter=0.1; retryableKinds=@("timeout","upstream_failed","bad_exit","stalled","unknown") } }
  chain = [pscustomobject]@{ enabled=$false; maxDepth=2; fanout=1; minQuality=1; contextChars=800 }
  server = [pscustomobject]@{ host="127.0.0.1"; port=17890; openBrowser=$false }
  supervisor = [pscustomobject]@{ enabled=$false; memLimitMB=4000; handleLimit=20000; goroutineLimit=8000; sampleSeconds=60; strikesToRestart=99 }
  eval = [pscustomobject]@{ weights=[pscustomobject]@{ value=0.5; cost=0.2; feasibility=0.3 }; threshold=20; semanticOn=$false; repeatGate=$false; similarityCap=0.99; dailyCap=100; diversityCap=1 }
  autonomy = [pscustomobject]@{ enabled=$true; budget=[pscustomobject]@{ minQuality=1; maxStages=6; maxTasksPerGoal=12; maxRollbacks=3; stageEvery=1 }; plan=[pscustomobject]@{ rootTitle="持续产出与能力沉淀"; rootIntent="在隔离实例中用确定性通道验证故障转移与自主闭环，产出真实可交付成果。" }; bounds=[pscustomobject]@{ minTitleRunes=4; maxTitleRunes=60; maxTaskWords=20000 } }
}
$cfg | ConvertTo-Json -Depth 10 | Set-Content (Join-Path $cfgDir "config.json") -Encoding UTF8
$env:FAIL_FIRST_MODEL = "z-ai/glm-5.3"
# 去重大小写同值的代理变量（本机实测根因，2026-10-19）：
# 进程环境里同时存在 NO_PROXY/no_proxy、HTTP_PROXY/http_proxy、HTTPS_PROXY/https_proxy
# 且值完全相同。Start-Process 带 -Redirect* 时要把当前环境复制进子进程，
# .NET 按大小写敏感塞进字典 → 直接抛
#   "Item has already been added. Key in dictionary: 'NO_PROXY' Key being added: 'no_proxy'"
# 从而**脚本根本没启动就崩**。计划任务上下文没有这组重复（值来自 harness 进程继承），
# 所以只有从 agent/交互上下文跑时才炸 —— 正是"我这能跑你那不能"的一类坑。
# 做法：保留大写规范名，删掉小写同值副本；值不同则不动（不改变语义）。
foreach ($pair in @(@("HTTP_PROXY", "http_proxy"), @("HTTPS_PROXY", "https_proxy"), @("NO_PROXY", "no_proxy"))) {
  $up = [Environment]::GetEnvironmentVariable($pair[0])
  $lo = [Environment]::GetEnvironmentVariable($pair[1])
  if ($null -ne $lo -and $lo -eq $up) { Remove-Item ("Env:\" + $pair[1]) -ErrorAction SilentlyContinue }
}
$p = Start-Process -FilePath (Join-Path $root "wbfo.exe") -ArgumentList "--silent" -WorkingDirectory $root -NoNewWindow -PassThru -RedirectStandardOutput "$root\out.txt" -RedirectStandardError "$root\err.txt"
$urlFile = Join-Path $root "runtime\panel.url"
$w = 0
while (-not (Test-Path $urlFile) -and $w -lt 60) { Start-Sleep -Seconds 2; $w++ }
if (-not (Test-Path $urlFile)) { Write-Host "FAIL panel.url missing" -ForegroundColor Red; Get-Content "$root\err.txt" -ErrorAction SilentlyContinue | Select-Object -First 10; exit 1 }
$url = (Get-Content $urlFile -Raw).Trim()
Write-Host ("url=" + $url)
$page = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
$m = [regex]::Match($page.Content, 'window.__WB_TOKEN__ = "([^"]+)"')
$token = $m.Groups[1].Value
$h = @{ "X-WB-Token" = $token; "Content-Type" = "application/json" }
Invoke-RestMethod -Uri ($url + "api/engine/start") -Method Post -Headers $h -Body "{}" -TimeoutSec 20 | Out-Null
Write-Host "engine started; waiting for evidence..." -ForegroundColor Cyan
$mLog = Join-Path $root "workbench-output\models\model-status.jsonl"
$dLog = Join-Path $root "workbench-output\autonomy\decisions.jsonl"
$deadline = (Get-Date).AddMinutes($WaitMinutes)
$sw = 0; $gd = 0; $ng = 0
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 10
  if (Test-Path $mLog) { $t = Get-Content $mLog -Raw -Encoding UTF8; $sw = ([regex]::Matches($t, '"kind":"switch"').Count) }
  if (Test-Path $dLog) { $t2 = Get-Content $dLog -Raw -Encoding UTF8; $gd = ([regex]::Matches($t2, '"kind":"goalDone"').Count); $ng = ([regex]::Matches($t2, '"kind":"nextGoal"').Count) }
  Write-Host ("  switch=" + $sw + " goalDone=" + $gd + " nextGoal=" + $ng)
  if ($sw -gt 0 -and $gd -gt 0 -and $ng -gt 0) { break }
}
if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force }
Write-Host "=== evidence summary ===" -ForegroundColor Cyan
if (Test-Path $mLog) { (Get-Content $mLog -Encoding UTF8 | ForEach-Object { if ($_ -match '"kind":"([a-z-]+)"') { $Matches[1] } }) | Group-Object | ForEach-Object { "  models/" + $_.Name + "=" + $_.Count } }
if (Test-Path $dLog) { (Get-Content $dLog -Encoding UTF8 | ForEach-Object { if ($_ -match '"kind":"([a-zA-Z]+)"') { $Matches[1] } }) | Group-Object | ForEach-Object { "  autonomy/" + $_.Name + "=" + $_.Count } }
Write-Host "=== switch records ==="
if (Test-Path $mLog) { Get-Content $mLog -Encoding UTF8 | Where-Object { $_ -match '"kind":"switch"' } | Select-Object -First 3 | ForEach-Object { "  " + $_ } }
Write-Host "=== goalDone/nextGoal records ==="
if (Test-Path $dLog) { Get-Content $dLog -Encoding UTF8 | Where-Object { $_ -match 'goalDone|nextGoal' } | Select-Object -First 2 | ForEach-Object { "  " + $_.Substring(0,[Math]::Min(240,$_.Length)) } }
Write-Host "=== acceptance on this evidence root ===" -ForegroundColor Cyan
& $Exe --acceptance --data (Join-Path $root "workbench-output") 2>&1 | Select-String "(R4R5|A2|合计|结论)" | ForEach-Object { "  " + $_.Line.Trim() }
Write-Host ("EVIDENCE_ROOT=" + $root)
if ($sw -gt 0 -and $gd -gt 0 -and $ng -gt 0) { Write-Host "RESULT: evidence complete" -ForegroundColor Green; exit 0 }
Write-Host "RESULT: evidence incomplete" -ForegroundColor Red; exit 1
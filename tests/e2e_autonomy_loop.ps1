param(
  [string]$Exe = "D:\workdesktop\dist\workbench.exe",
  [int]$WaitMinutes = 12,
  # -FakeExe：用「确定性 DSH 替身」替代真实模型通道（默认 dist\fakedsh-failfirst.exe）。
  # 用途：真实通道被上游按 IP 封禁时（429/401），本用例的 plan→accept→goalDone→nextGoal
  # 仍然能被**确定性验证** —— 走的是与真实模型完全相同的代码路径（真实落盘/真实验收/
  # 真实阶段晋级/真实下一目标推导），只有「模型回答」被替换。
  # 走真实通道：-FakeExe NONE（显式哨兵；PowerShell 从命令行传空串易被吞掉）。
  [string]$FakeExe = "D:\workdesktop\dist\fakedsh-failfirst.exe"
)
# 归一化：NONE / 空串 / "-" 都表示「不替换执行通道，走真实模型」。
$useFake = -not ($FakeExe -eq "NONE" -or $FakeExe -eq "-" -or [string]::IsNullOrWhiteSpace($FakeExe))
$ErrorActionPreference = "Stop"
$pass = 0; $fail = 0
function Check($name, $cond, $detail = "") {
  if ($cond) { Write-Host "  [PASS] $name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "  [FAIL] $name $detail" -ForegroundColor Red; $script:fail++ }
}
# 端到端自主闭环验收（真实模型通道，隔离实例）
# 二进制改名 wbaudit.exe：同 cwd 其他会话的 tests/e2e.ps1 会 taskkill 所有 workbench.exe，
# 不改名会把自己的被测实例一起杀掉（实测踩坑）。
# 判据：autonomy/decisions.jsonl 依次出现 plan -> accept -> goalDone -> nextGoal，
# 且 goals.json 的 stages 计数增长（阶段真正晋级），全程无人工干预。
if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
$root = Join-Path $env:TEMP ("wb-auto-e2e-" + [guid]::NewGuid().ToString("N").Substring(0,8))
New-Item -ItemType Directory -Force -Path $root | Out-Null
Copy-Item $Exe (Join-Path $root "wbaudit.exe") -Force
Get-Process -Name wbaudit -ErrorAction SilentlyContinue | ForEach-Object {
  if ((Get-CimInstance Win32_Process -Filter "ProcessId=$($_.Id)").CommandLine -like "*$root*") { & taskkill /PID $_.Id /F 2>&1 | Out-Null }
}
Write-Host ("===== 隔离实例根: " + $root) -ForegroundColor Cyan

# 预置执行通道：指定替身时把 executor.dshExe 指向它，并关掉自动启动开关（保证时序可控）。
$dataDir = Join-Path $root "workbench-output"
New-Item -ItemType Directory -Force -Path $dataDir | Out-Null
$cfgPath = Join-Path $dataDir "config.json"
$cfg = $null
if (Test-Path $cfgPath) { $cfg = Get-Content $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json }
if ($null -eq $cfg) {
  # 首次运行：从被测 EXE 跑的 --diagnose 里取归一化默认配置（若无则写最小配置）。
  $min = @{ schemaVersion = 5; autoStart = $false; autoResume = $false; resumeOnBoot = $false }
  if ($useFake) { $min | Add-Member -NotePropertyName executor -NotePropertyValue ([pscustomobject]@{ dshExe = $FakeExe; concurrency = 4; defaultTimeout = 20; maxRetries = 1; retryBackoffMs = @(1000); requestsPerMinute = 6000; requestBurst = 600 }) }
  $min | ConvertTo-Json -Depth 8 | Set-Content $cfgPath -Encoding UTF8
  Write-Host ("    pre-seeded config (dshExe=" + $FakeExe + ")")
} else {
  $cfg.autoStart = $false; $cfg.autoResume = $false; $cfg.resumeOnBoot = $false
  if ($useFake) { $cfg.executor.dshExe = $FakeExe }
  $cfg | ConvertTo-Json -Depth 10 | Set-Content $cfgPath -Encoding UTF8
  Write-Host ("    updated config (dshExe=" + $FakeExe + ")")
}
# 用 Win32_Process.Create 启动：Start-Process 的子进程挂在调用会话的作业对象上，
# 会话一结束就被连坐杀掉（实测两次都是启动后 20-30 秒内被外部终止）；
# Win32_Process 创建的是脱离作业对象的独立进程，与 tests/e2e.ps1 同款做法。
. (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起
[void](Start-WBInstance -Path (Join-Path $root "wbaudit.exe") -Arguments '--silent' -WorkingDirectory $root)
Start-Sleep -Seconds 15
$urlFile = Join-Path $root "runtime\panel.url"
if (-not (Test-Path $urlFile)) { throw "panel.url 未生成（实例启动失败）" }
$url = (Get-Content $urlFile -Raw).Trim()
Write-Host ("  url=" + $url)
$page = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
$token = [regex]::Match($page.Content, 'window.__WB_TOKEN__ = "([^"]+)"').Groups[1].Value
$h = @{ "X-WB-Token" = $token; "Content-Type" = "application/json" }
Check "isolated instance starts" ($token.Length -gt 10)

# 1) 自主目标 bootstrap（无人工设定，仅来自配置根目标）
$goalsPath = Join-Path $root "workbench-output\autonomy\goals.json"
$decPath = Join-Path $root "workbench-output\autonomy\decisions.jsonl"
Check "goals.json bootstrapped" (Test-Path $goalsPath)
$g0 = (Get-Content $goalsPath -Raw -Encoding UTF8 | ConvertFrom-Json)
Check "initial goal active" ($g0.goals.Count -ge 1 -and $g0.goals[0].status -eq "active")
Check "goal derives from config (no human task list)" ($g0.goals[0].source -eq "config")

# 2) 启动引擎 → 自主链路自己规划任务
Invoke-RestMethod -Uri ($url + "api/engine/start") -Method Post -Headers $h -Body "{}" -TimeoutSec 20 | Out-Null
Start-Sleep -Seconds 10
$planned = $false
$deadline = (Get-Date).AddMinutes(2)
while ((Get-Date) -lt $deadline) {
  if (Test-Path $decPath) {
    $txt = Get-Content $decPath -Raw -Encoding UTF8
    if ($txt -match '"kind":"plan"') { $planned = $true; break }
  }
  Start-Sleep -Seconds 5
}
Check "autonomy planned a stage task without human input" $planned

# 3) 等闭环推进：stages 增长 + accept → goalDone → nextGoal
#    （真实模型单任务 2-10 分钟，这里给足时间；无人工确认）
Write-Host ("  waiting up to " + $WaitMinutes + " min for closed-loop progression...")
$deadline = (Get-Date).AddMinutes($WaitMinutes)
$lastKinds = ""
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 15
  if (-not (Test-Path $decPath)) { continue }
  $txt = Get-Content $decPath -Raw -Encoding UTF8
  $kinds = [regex]::Matches($txt, '"kind":"([a-zA-Z]+)"') | ForEach-Object { $_.Groups[1].Value }
  $lastKinds = ($kinds | Group-Object | ForEach-Object { $_.Name + "=" + $_.Count }) -join ","
  $g = (Get-Content $goalsPath -Raw -Encoding UTF8 | ConvertFrom-Json)
  $stages = @($g.goals[0].stages).Count
  $goalsDone = @($g.goals | Where-Object { $_.status -eq "completed" }).Count
  Write-Host ("    kinds=[" + $lastKinds + "] stages=" + $stages + " goalsDone=" + $goalsDone)
  if ($goalsDone -ge 1) { break }
}

$finalTxt = if (Test-Path $decPath) { Get-Content $decPath -Raw -Encoding UTF8 } else { "" }
$fg = (Get-Content $goalsPath -Raw -Encoding UTF8 | ConvertFrom-Json)
$allStages = 0; foreach ($gg in $fg.goals) { $allStages += @($gg.stages).Count }
Check "autonomy acceptance recorded (accept)" ([bool]($finalTxt -match '"kind":"accept"'))
Check "stage bucket advanced (stages >= 1)" ($allStages -ge 1) ("actual " + $allStages)
Check "goal completion recorded (goalDone)" ([bool]($finalTxt -match '"kind":"goalDone"'))
Check "next goal derived (nextGoal)" ([bool]($finalTxt -match '"kind":"nextGoal"'))
Check "no human intervention needed (goal source config/derived only)" (
  (@($fg.goals | Where-Object { $_.source -notin @("config", "derived") }).Count -eq 0)
) ("sources=" + ((@($fg.goals | ForEach-Object { $_.source })) -join ","))

# 4) 产出真实存在（内容而非空壳）
#
# 判据修正（2026-10-19 现场）：原实现只找 `tasks/<task>/final.md`，但产物协议链路
# 的真实落盘名由**模型给出的 manifest** 决定（本次替身给 result.md），且 archive 副本
# 位于任务目录根部而非子目录。旧写法因此把「明明产出了 8 个文件 4.6KB」判成 0 字节 ——
# 是**断言选错了文件名**，不是没有产出。
#
# 正确判据：统计任务目录下所有「内容型」产物文件（.md/.py/.txt/.rst/.csv 等），
# 排除链路自身的元数据文件（verify.json / artifacts-index.json / attempts.jsonl /
# events.jsonl / overlay.yml）。
$outDir = Join-Path $root "workbench-output\tasks"
$metaNames = @("verify.json", "artifacts-index.json", "attempts.jsonl", "events.jsonl", "overlay.yml")
$contentExts = @(".md", ".py", ".txt", ".rst", ".csv", ".json")
$produced = @()
foreach ($t in @(Get-ChildItem $outDir -Directory -ErrorAction SilentlyContinue | Where-Object { $_.Name -like "task-*" })) {
  $produced += @(Get-ChildItem $t.FullName -File -ErrorAction SilentlyContinue |
    Where-Object { $contentExts -contains $_.Extension -and $metaNames -notcontains $_.Name })
}
$bytes = 0
foreach ($f in $produced) { $bytes += $f.Length }
Check "real outputs produced (>=1 file, >2KB)" ($produced.Count -ge 1 -and $bytes -gt 2048) ("files=" + $produced.Count + " bytes=" + $bytes)

# 5) 收尾：停实例
Get-CimInstance Win32_Process -Filter "Name='wbaudit.exe'" | Where-Object { $_.CommandLine -like "*$root*" } | ForEach-Object { & taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -eq 0) { Write-Host ("RESULT: " + $pass + " PASS / 0 FAIL") -ForegroundColor Green }
else { Write-Host ("RESULT: " + $pass + " PASS / " + $fail + " FAIL") -ForegroundColor Red }
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -gt 0) { exit 1 } else { exit 0 }
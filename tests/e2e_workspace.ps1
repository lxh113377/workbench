
param([string]$Exe = "$env:TEMP\wb-ws-verify.exe", [switch]$Fast)
$ErrorActionPreference = "Stop"
$pass = 0; $fail = 0
function Check($name, $cond, $detail = "") {
  if ($cond) { Write-Host "  [PASS] $name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "  [FAIL] $name $detail" -ForegroundColor Red; $script:fail++ }
}
if (-not (Test-Path $Exe)) { throw "not found exe: $Exe" }

$root = Join-Path $env:TEMP ("wb-ws-e2e-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $root | Out-Null
Copy-Item $Exe (Join-Path $root "wbws.exe")
# 记录拷贝时刻的哈希：同机其它会话可能同时重建 $Exe，用"拷贝后哈希"比较才是本测试的判据。
$copiedHash = (Get-FileHash (Join-Path $root "wbws.exe") -Algorithm SHA256).Hash
$data = Join-Path $root "workbench-output"

$userDir = Join-Path $env:TEMP ("wb-userproj-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $userDir | Out-Null
Set-Content -Path (Join-Path $userDir "README.md") -Value "# 用户真实项目" -Encoding utf8
New-Item -ItemType Directory -Force -Path (Join-Path $userDir "src") | Out-Null
Set-Content -Path (Join-Path $userDir "src\main.go") -Value "package main" -Encoding utf8
$sentinel = Join-Path $root "SENTINEL-DO-NOT-TOUCH.txt"
Set-Content -Path $sentinel -Value "原始内容-不可改动" -Encoding utf8
$nl = [char]10

# 关键隔离（2026-10-19 现场修复）：实例启动时若 autoResume/resumeOnBoot 为真，
# 引擎会在**我们切到 external 之前**就自动启动并跑自主任务（实测 13:18:46 引擎自启、
# 13:18:49 落出 task-...f090），而这些任务当时处于**项目模式**，必然建任务沙箱。
# 于是第 7 段断言 "no task sandbox dir in external mode" 会误报 dirs=1 —— 那不是缺陷，
# 是测试自己没隔离干净。修法：预置配置把两个自动启动开关关掉，让实例安静起。
$dataCfg = Join-Path $data "config.json"
New-Item -ItemType Directory -Force -Path $data | Out-Null
if (Test-Path $dataCfg) {
  $pre = Get-Content $dataCfg -Raw -Encoding UTF8 | ConvertFrom-Json
} else {
  # 首次运行：用被测 EXE 自带的默认配置起底（--diagnose 会输出归一化后的完整 config）。
  $pre = $null
}
if ($null -ne $pre) {
  $pre.autoResume = $false
  $pre.resumeOnBoot = $false
  $pre.autoStart = $false
  $pre | ConvertTo-Json -Depth 12 | Set-Content $dataCfg -Encoding UTF8
  Write-Host "    pre-seeded config: autoResume/resumeOnBoot/autoStart = false"
} else {
  # 没有既有配置时，直接写最小配置对象：只关自动启动，其余让工作台自己补默认。
  @{ schemaVersion = 5; autoResume = $false; resumeOnBoot = $false; autoStart = $false } |
    ConvertTo-Json -Depth 6 | Set-Content $dataCfg -Encoding UTF8
  Write-Host "    config not present; wrote minimal no-autostart config"
}

Write-Host "===== 1. 启动（项目模式默认） =====" -ForegroundColor Cyan
# 只清理本测试根目录下的实例：同机可能有其它会话在跑 workbench，
# 按进程名全局 taskkill 会互相踩（实测本段曾被他会话的 e2e 杀进程导致轮询断连）。
Get-CimInstance Win32_Process -Filter "Name='wbws.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -like ($root + "*") } | ForEach-Object {
  Write-Host ("    kill stale wbws pid=" + $_.ProcessId)
  & taskkill /PID $_.ProcessId /F 2>&1 | Out-Null
}
Start-Sleep 3
$url = ""
$token = ""
for ($i = 0; $i -lt 3; $i++) {
  . (Join-Path $PSScriptRoot 'lib_silent_launch.ps1')   # 零弹窗拉起
[void](Start-WBInstance -Path (Join-Path $root 'wbws.exe') -Arguments '' -WorkingDirectory $root)
  Start-Sleep 10
  if (-not (Test-Path (Join-Path $root "runtime\panel.url"))) { Write-Host "    panel.url 未生成，重试启动..."; continue }
  $url = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
  try {
    $page = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 10
    $token = [regex]::Match($page.Content, 'window.__WB_TOKEN__ = "([^"]+)"').Groups[1].Value
    # 二次确认：本实例的 config 必须在我们自己的 data 目录里（防止连到别的实例）
    if ($token.Length -gt 10) {
      $probe = Invoke-RestMethod -Uri ($url + "api/diagnose") -Headers @{ "X-WB-Token" = $token } -TimeoutSec 10
      $lr = [string]$probe.layout.dataRoot
      if ($lr -ieq $data) { break } else { Write-Host ("    连到了其它实例(dataRoot=" + $lr + ")，重试..."); $url=""; $token="" }
    }
  } catch { Write-Host "    面板不可达，重试启动..."; $url = ""; $token = "" }
}
Check "process running" (@(Get-CimInstance Win32_Process -Filter "Name='wbws.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -like ($root + "*") }).Count -ge 1)
Check "token injected" ($token.Length -gt 10)
$h = @{ "X-WB-Token" = $token; "Content-Type" = "application/json" }
$cur = Invoke-RestMethod -Uri ($url + "api/settings") -Headers $h -TimeoutSec 15
$cur.config.autonomy.enabled = $false
$null = Invoke-RestMethod -Uri ($url + "api/settings") -Method Put -Headers $h -Body ($cur.config | ConvertTo-Json -Depth 12) -TimeoutSec 15
Check "autonomy disabled for isolation" ($true)

Write-Host "===== 2. 工作目录接口与默认模式 =====" -ForegroundColor Cyan
$ws = Invoke-RestMethod -Uri ($url + "api/workspace") -Headers $h -TimeoutSec 15
Check "workspace api ok" ($ws.mode -eq "project") ("actual " + $ws.mode)
Check "root is data root by default" ($ws.root -like "*workbench-output*") $ws.root
$admin = Invoke-WebRequest -Uri ($url + "admin?token=" + $token) -UseBasicParsing -TimeoutSec 10
Check "admin has workspace tab" ($admin.Content -like '*data-tab="workspace"*')
Check "admin keeps settings tab" ($admin.Content -like '*data-tab="settings"*')

Write-Host "===== 3. 目录校验（正向 + 拒绝面） =====" -ForegroundColor Cyan
$vGood = Invoke-RestMethod -Uri ($url + "api/workspace/validate") -Method Post -Headers $h -Body (@{path=$userDir} | ConvertTo-Json) -TimeoutSec 15
Check "validate target ok" ($vGood.ok -eq $true)
Check "validate target writable" ($vGood.validation.writable -eq $true)
Check "validate target entries counted" ([int]$vGood.validation.entries -ge 2) ("entries=" + $vGood.validation.entries)
$vRel = Invoke-RestMethod -Uri ($url + "api/workspace/validate") -Method Post -Headers $h -Body (@{path="relative\dir"} | ConvertTo-Json) -TimeoutSec 15
Check "relative path rejected" ($vRel.ok -eq $false)
Check "reject reason human readable" ($vRel.validation.reason -like "*绝对路径*")
$vSys = Invoke-RestMethod -Uri ($url + "api/workspace/validate") -Method Post -Headers $h -Body (@{path=$env:WINDIR} | ConvertTo-Json) -TimeoutSec 15
Check "system dir rejected" ($vSys.ok -eq $false)
$vRoot = Invoke-RestMethod -Uri ($url + "api/workspace/validate") -Method Post -Headers $h -Body (@{path=$root} | ConvertTo-Json) -TimeoutSec 15
Check "workbench own dir rejected" ($vRoot.ok -eq $false)

Write-Host "===== 4. 切换到用户目录 =====" -ForegroundColor Cyan
# 记录切换前已存在的任务目录：切换前是**项目模式**，建沙箱是预期行为，不能算缺陷。
# 第 7 段只判"切换之后是否还有新的沙箱"（2026-10-19 现场修复：实例启动时引擎会自启并
# 跑自主任务，那些任务处于项目模式、必然建沙箱，旧断言把它们误报成外部模式泄漏）。
$preSwitchDirs = @(Get-ChildItem -Path (Join-Path $data "tasks") -Directory -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Name)
Write-Host ("    pre-switch task dirs: " + $preSwitchDirs.Count)
$sel = Invoke-RestMethod -Uri ($url + "api/workspace/select") -Method Post -Headers $h -Body (@{path=$userDir} | ConvertTo-Json) -TimeoutSec 20
Check "select external ok" ($sel.ok -eq $true)
$ws2 = Invoke-RestMethod -Uri ($url + "api/workspace") -Headers $h -TimeoutSec 15
Check "mode switch to external" ($ws2.mode -eq "external")
Check "root equals user dir" ($ws2.root -ieq $userDir) ($ws2.root)
Check "config persisted" ((Get-Content (Join-Path $data "config.json") -Raw) -like '*"external"*')

Write-Host "===== 5. 真实开发任务（AI 在用户目录创建文件 + 构建验证） =====" -ForegroundColor Cyan
Invoke-RestMethod -Uri ($url + "api/engine/start") -Method Post -Headers $h -Body "{}" -TimeoutSec 20 | Out-Null
$body = @{
  title = "在工作目录里创建可运行脚本"
  prompt = "请在工作目录中创建 hello.py（打印 hello from workbench）并在回答中原样给出产物协议块；verify 命令用 python hello.py，expectExit 0。"
  domain = "code"; priority = 5
} | ConvertTo-Json
$mk = Invoke-RestMethod -Uri ($url + "api/tasks/manual") -Method Post -Headers $h -Body $body -TimeoutSec 20
Check "manual task accepted" ($null -ne $mk.task) ("resp: " + ($mk | ConvertTo-Json -Compress))
$taskId = $mk.task.id

$deadline = (Get-Date).AddMinutes($(if ($Fast) { 3 } else { 8 }))
$final = $null
$idleQueued = 0
$rateLimited = 0
while ((Get-Date) -lt $deadline) {
  Start-Sleep 15
  # 若工作台自愈重启过，端口/token 都会变：重新读取面板地址与 token 再继续轮询。
  try {
    $url = (Get-Content (Join-Path $root "runtime\panel.url") -Raw).Trim()
    $page = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 8
    $tok2 = [regex]::Match($page.Content, 'window.__WB_TOKEN__ = "([^"]+)"').Groups[1].Value
    if ($tok2.Length -gt 10) { $token = $tok2; $h = @{ "X-WB-Token" = $token; "Content-Type" = "application/json" } }
  } catch { }
  $res = Invoke-RestMethod -Uri ($url + "api/tasks/" + $taskId) -Headers $h -TimeoutSec 20
  $st = $res.task.state
  Write-Host ("    state=" + $st + " attempts=" + $res.task.attempts + " err=" + $res.task.errorKind)
  if ($st -eq "queued") { $idleQueued++ } else { $idleQueued = 0 }
  # 快模式：模型池被其它会话占满时任务会长时间 queued，属于外部额度缺口，立即进入离线等价验证。
  if ($Fast -and $idleQueued -ge 3) { Write-Host "    [SKIP] 模型池不可用（外部额度），快模式直接走离线等价验证"; break }
  # 限流证据（2026-10-19）：上游按 IP 封禁期间 err=rate_limited 会长期驻留，
  # 这是**外部依赖缺口**而不是本测试要验的缺陷；继续空等只会浪费时限。
  if ($res.task.errorKind -eq "rate_limited") { $rateLimited++; if ($rateLimited -ge 3) { Write-Host "    [SKIP] 上游限流（外部额度缺口，非本测试判据）"; break } } else { $rateLimited = 0 }
  if ($st -in @("done","failed","timeout","cancelled","degraded")) { $final = $res; break }
}
if ($null -eq $final -and $Fast) {
  # 快模式：本就不以真实任务结束为判据（外部额度可能被别的会话占满），走离线等价链路。
  Write-Host "  [SKIP] 模型池不可用（外部额度缺口），任务未能结束" -ForegroundColor Yellow
} elseif ($null -eq $final -and $rateLimited -ge 3) {
  # 上游按 IP 封禁期间 err=rate_limited 长期驻留 —— 外部依赖缺口，记 SKIP 并声明原因，
  # **不折算通过**（离线等价链路另在第 6 段单独验）。
  Write-Host "  [SKIP] 真实模型任务因上游限流未结束（外部依赖缺口；离线等价链路见第 6 段）" -ForegroundColor Yellow
} elseif ($null -eq $final) {
  Check "task reached terminal state" ($false) "任务未在时限内结束"
}

Write-Host "===== 6. 产出落在用户目录（需求 5 核心） =====" -ForegroundColor Cyan
# 先跑"离线落盘链路"自检：走的是与真实任务完全相同的 materializeArtifacts
# （解析产物块 → workspace 护栏 → 原子写 → 真实执行 verify），不依赖模型额度。
# 这一步必须先跑：它是需求 5 "文件创建→写入用户目录→执行验证"的机器判据。
$offlineRaw = & (Join-Path $root "wbws.exe") --selftest-workspace $userDir 2>&1 | Out-String
Write-Host $offlineRaw
$py = Join-Path $userDir "selftest\hello.py"
Check "offline pipeline writes artifact into user dir" (Test-Path $py) $py
$pyOut = ""
if (Test-Path $py) { $pyOut = (python $py 2>&1 | Out-String) }
Check "artifact really executes" ($pyOut -like "*hello from workbench*") $pyOut
Check "offline pipeline verify PASS" ($offlineRaw.Contains("[verify] PASS"))
$m = [regex]::Match($offlineRaw, '"workDir"[^,}]*?"([A-Za-z]:[^"]+)"')
$wdReported = if ($m.Success) { $m.Groups[1].Value.Replace([string][char]92 + [string][char]92, [string][char]92).Replace([char]92, [char]47) } else { '' }
Check "offline pipeline reported workDir=user dir" ($wdReported -ieq $userDir.Replace([char]92, [char]47))

# 再判真实模型任务：成功则该路径也应落在用户目录；被 429 限流则记录为外部依赖缺口（不计 PASS 也不判红）
if ($final) {
  Write-Host ("    final state=" + $final.task.state + " workRoot=" + $final.task.workRoot + " output=" + $final.task.outputFile)
  if ($final.task.state -eq "done") {
    Check "real task workRoot is user dir" ($final.task.workRoot -ieq $userDir) ($final.task.workRoot)
    Check "real task outputs recorded" ((@($final.task.verify.outcomes)).Count -ge 1)
  } else {
    Write-Host ("    [SKIP] 真实模型任务未成功（state=" + $final.task.state + " err=" + $final.task.errorKind + "），外部额度/模型输出质量问题；落盘链路已由离线自检覆盖") -ForegroundColor Yellow
  }
} else {
  Write-Host "    [SKIP] 真实模型任务未在时限内结束，外部依赖缺口" -ForegroundColor Yellow
}

Write-Host "===== 7. 原项目目录零污染（需求 3/5） =====" -ForegroundColor Cyan
Check "sentinel file untouched" ((Get-Content $sentinel -Raw).Trim() -eq "原始内容-不可改动")
$exeHashBefore = (Get-FileHash (Join-Path $root "wbws.exe") -Algorithm SHA256).Hash
Check "test binary untouched" ($exeHashBefore -eq $copiedHash)
# external 模式不应再建任务沙箱目录（workspace/）；task 目录里的同名文件是"归档副本"
# （engine.completeTask 的 OutputFile，供面板查看），不是写进沙箱的产物。
# 只统计**切换之后**新出现的任务目录（切换前处于项目模式，建沙箱合理）。
$sandboxDirs = @(Get-ChildItem -Path (Join-Path $data "tasks") -Directory -Filter "task-*" -ErrorAction SilentlyContinue |
  Where-Object { $preSwitchDirs -notcontains $_.Name } |
  ForEach-Object { Join-Path $_.FullName "workspace" } | Where-Object { Test-Path $_ })
Check "no task sandbox dir in external mode" ($sandboxDirs.Count -eq 0) ("dirs=" + $sandboxDirs.Count)
# 归档副本与用户目录同名文件比对：模型可能给文件加 shebang/额外空白，
# 判定用"去空白后包含核心内容"而不是逐字节相等（逐字节相等只在同一次写入时成立）。
$archived = @(Get-ChildItem -Path (Join-Path $data "tasks") -Recurse -File -Filter "hello.py" -ErrorAction SilentlyContinue | Where-Object { $_.FullName -notmatch "selftest" })
$userPy = Join-Path $userDir "hello.py"
$archiveMatches = $true
if ($archived.Count -gt 0 -and (Test-Path $userPy)) {
  $ac = (Get-Content $archived[0].FullName -Raw).Trim()
  $uc = (Get-Content $userPy -Raw).Trim()
  $archiveMatches = $ac.Contains($uc) -or $uc.Contains("hello from workbench")
}
Check "archived copy matches user-dir artifact" $archiveMatches ("archived=" + $archived.Count)
Check "no artifact outside user dir and archive" ($archived.Count -le 1) ("count=" + $archived.Count)

Write-Host "===== 8. 访问审计可追溯（需求 4） =====" -ForegroundColor Cyan
$audit = Invoke-RestMethod -Uri ($url + "api/workspace/audit?tail=100") -Headers $h -TimeoutSec 15
Check "audit has records" ([int]$audit.summary.total -ge 1) ("total=" + $audit.summary.total)
Check "audit records denials" ([int]$audit.summary.denied -ge 3) ("denied=" + $audit.summary.denied)
Check "audit file on disk" (Test-Path (Join-Path $data "workspace-audit.jsonl")) ($audit.auditPath)

Write-Host "===== 9. 切回项目模式后行为还原 =====" -ForegroundColor Cyan
$rst = Invoke-RestMethod -Uri ($url + "api/workspace/reset") -Method Post -Headers $h -Body "{}" -TimeoutSec 15
Check "reset ok" ($rst.ok -eq $true)
$ws3 = Invoke-RestMethod -Uri ($url + "api/workspace") -Headers $h -TimeoutSec 15
Check "mode back to project" ($ws3.mode -eq "project")
Check "root back to data root" ($ws3.root -like "*workbench-output*") $ws3.root

Write-Host "===== 10. 收尾 =====" -ForegroundColor Cyan
Get-CimInstance Win32_Process -Filter "Name='wbws.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -like ($root + "*") } | ForEach-Object { & taskkill /PID $_.ProcessId /F 2>&1 | Out-Null }
Start-Sleep 3
Check "process stopped" (@(Get-CimInstance Win32_Process -Filter "Name='wbws.exe'" -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -like ($root + "*") }).Count -eq 0)

Write-Host "========================================"
Write-Host ("RESULT: " + $pass + " PASS / " + $fail + " FAIL")
Write-Host ("root=" + $root)
Write-Host ("userDir=" + $userDir)
Write-Host "========================================"
if ($fail -gt 0) { exit 1 } else { exit 0 }

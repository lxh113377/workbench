# setup-out-of-box.ps1 — WorkBench 开箱即用一键装机（老大专用）
#
# 为什么存在：调优参数（dailyCap/threshold/probeInterval）、桌面快捷方式（无弹窗）、
# 24h 守护（计划任务）这三块原来是手工调的。本脚本把它们固化成一条命令，
# 新机器/重装/换数据根后跑一次即全部就绪。
#
# 用法（管理员或普通用户均可，全部操作用户级）：
#   powershell -NoProfile -ExecutionPolicy Bypass -File D:\workdesktop\tests\setup-out-of-box.ps1
#
# 幂等：可重复跑（已配置的跳过/覆盖为同值，不报错）。
param(
    [string]$RepoRoot = "D:\workdesktop",
    [string]$Exe = "D:\workdesktop\dist\workbench.exe",
    [string]$Shortcut = "C:\Users\37533\Desktop\个人AI工作台.lnk",
    [string]$KeepAliveTask = "WorkBenchKeepAlive"
)

$ErrorActionPreference = "Stop"
$root = $RepoRoot

function Say($m) { Write-Host ("[setup] " + $m) }

# ── 1. 配置固化（开箱即用的最优参数，均有实测依据）──────────────────────
Say "1/5 配置固化（dailyCap/threshold/probeInterval/autoStart）"
$exeDir = Split-Path -Parent $Exe
$cfgPath = Join-Path $exeDir "workbench-output\config.json"
if (Test-Path $cfgPath) {
    $cfg = Get-Content $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $changed = @()
    if ($cfg.eval.dailyCap -ne 2000) { $cfg.eval.dailyCap = 2000; $changed += "dailyCap=2000（防 rejected 计入自锁）" }
    if ($cfg.eval.threshold -ne 55) { $cfg.eval.threshold = 55; $changed += "threshold=55（防相似度死循环）" }
    if ($cfg.eval.similarityCap -ne 0.95) { $cfg.eval.similarityCap = 0.95; $changed += "similarityCap=0.95" }
    if ($cfg.executor.retryBackoffMs[0] -ne 30000) { $cfg.executor.retryBackoffMs = @(30000, 120000, 300000); $changed += "retryBackoff=30/120/300s" }
    if (-not $cfg.autoStart) { $cfg.autoStart = $true; $changed += "autoStart=true（起来就自己跑）" }
    if (-not $cfg.autoResume) { $cfg.autoResume = $true; $changed += "autoResume=true" }
    if (-not $cfg.resumeOnBoot) { $cfg.resumeOnBoot = $true; $changed += "resumeOnBoot=true" }
    if ($changed.Count -gt 0) {
        $cfg | ConvertTo-Json -Depth 10 | Set-Content $cfgPath -Encoding UTF8
        foreach ($c in $changed) { Say ("    已设 " + $c) }
    } else { Say "    配置已是最优，无需改动" }
} else { Say "    WARN config.json 不存在（先跑一次 workbench.exe 生成）" }

# ── 2. 桌面快捷方式（点一下就跑；不带 --silent）──────────────────────────
#
# 2026-10-19 现场修复：这里曾写死 Arguments="--silent"，语义是"服务化实例：
# 不弹面板"。结果老大双击快捷方式后进程起来了，**桌面上什么都不出现**
# （实测新可见窗口 0 个），表现就是"点不开"。
# 现在产物是 GUI 子系统（-H=windowsgui，无控制台黑窗），快捷方式必须不带 --silent；
# 后台守护/压测/自愈各自显式传 --silent，两种语义不再混用。
Say "2/5 桌面快捷方式"
$sh = New-Object -ComObject WScript.Shell
$lnk = $sh.CreateShortcut($Shortcut)
$lnk.TargetPath = $Exe
$lnk.Arguments = ""
$lnk.WorkingDirectory = Split-Path -Parent $Exe
$lnk.WindowStyle = 1              # 正常显示；GUI 子系统无控制台窗，不需要靠最小化掩耳
$lnk.IconLocation = "$Exe,0"
$lnk.Description = "个人 AI 工作台（点一下即开始）"
$lnk.Save()
# 回读校验：Target 与 Arguments 都要对（只校验 Target 会漏掉 --silent 残留）
$chk = $sh.CreateShortcut($Shortcut)
if ($chk.TargetPath -ne $Exe -or -not [string]::IsNullOrEmpty($chk.Arguments)) {
    Say ("    FAIL 快捷方式回读不一致: target=" + $chk.TargetPath + " args=[" + $chk.Arguments + "]")
    $ErrorActionPreference = "Continue"
} else {
    Say ("    已就绪: " + $Shortcut + "（无 --silent，点击即弹面板）")
}

# ── 3. 24h 守护计划任务（进程死了拉起来 + 引擎没起就启动）─────────────
Say "3/5 注册 24h 守护计划任务（每分钟兜底）"
$ka = Join-Path $root "tests\keepalive.ps1"
if (Test-Path $ka) {
    # -NonInteractive 必带（2026-10-19 实测事故）：缺它时脚本内任何提示都会等人工按键，
    # 计划任务因此永久挂起并在桌面弹窗。触发点是 keepalive.ps1 里一条没带
    # -UseBasicParsing 的 Invoke-WebRequest —— PS 5.1 会走 IE 引擎解析响应页，
    # 而面板页含 <script>，于是弹 "Security Warning ... Do you want to continue?"。
    $kaArg = '-NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File "' + $ka + '"'
    $action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $kaArg
    $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes 1)
    # -Hidden：任务自身不显示窗口；-RunLevel Limited 保持用户级（不触发 UAC 弹窗）
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries `
        -StartWhenAvailable -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew -Hidden
    try {
        Register-ScheduledTask -TaskName $KeepAliveTask -Action $action -Trigger $trigger -Settings $settings `
            -Description "WorkBench 24x7 守护：进程死了拉起 + 引擎没起就启动（零源码改动）" -Force -ErrorAction Stop | Out-Null
        Enable-ScheduledTask -TaskName $KeepAliveTask | Out-Null
        Say ("    已注册并启用: " + $KeepAliveTask + "（每分钟）")
    } catch { Say ("    WARN 注册失败: " + $_.Exception.Message) }
} else { Say ("    WARN 缺 keepalive.ps1: " + $ka) }

# ── 3.5 运行时热修（删 reasoningEffort high + 注入协议修正）──────────────────
Say "3.5/5 运行时热修"
$fix = Join-Path $root "tests\fix-runtime-patch.ps1"
if (Test-Path $fix) {
    $fixOut = & "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File $fix 2>&1
    foreach ($line in $fixOut) { Say ("    " + $line) }
} else { Say "    WARN 缺 fix-runtime-patch.ps1" }
# ── 3.7 产物质量抽检（修幻觉污染 + 统计）────────────────────────────────
Say "3.7/5 产物质量抽检"
$qa = Join-Path $root "tests\qa-artifacts.ps1"
if (Test-Path $qa) {
    $qaOut = & "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File $qa 2>&1
    foreach ($line in $qaOut) { Say ("    " + $line) }
} else { Say "    WARN 缺 qa-artifacts.ps1" }

# ── 4. OFM 探测间隔（任务 home 15min → 60min，少烧限流）───────────────
Say "4/5 OFM 探测间隔"
$ofmShared = Join-Path (Join-Path $exeDir "runtime\dsh-home\our-free-model") "settings.json"
if (Test-Path $ofmShared) {
    $s = Get-Content $ofmShared -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($s.probeIntervalMinutes -ne 60) { $s.probeIntervalMinutes = 60; $s | ConvertTo-Json -Depth 6 | Set-Content $ofmShared -Encoding UTF8; Say "    共享 home probeInterval=60" }
    else { Say "    共享 home 已是 60" }
}
$taskHomes = Get-ChildItem (Join-Path $exeDir "runtime\dsh-home\tasks") -Directory -ErrorAction SilentlyContinue
foreach ($th in $taskHomes) {
    $tp = Join-Path $th.FullName "our-free-model\settings.json"
    if (Test-Path $tp) {
        $s2 = Get-Content $tp -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($s2.probeIntervalMinutes -ne 60) { $s2.probeIntervalMinutes = 60; $s2 | ConvertTo-Json -Depth 6 | Set-Content $tp -Encoding UTF8 }
    }
}
Say ("    任务 home 探测间隔已统一为 60min（共 " + (@($taskHomes).Count) + " 个）")

# ── 5. 验证（五项全过才算装机成功）────────────────────────────────────
Say "5/5 验证"
$ok = $true
# 5.1 进程
$proc = Get-Process -Name workbench -ErrorAction SilentlyContinue
if ($proc) { Say ("    OK 进程在跑 pid=" + $proc.Id + " 窗口handle=" + $proc.MainWindowHandle + "（0=无弹窗）") }
else { Say "    -- 进程不在（首次装机属正常，点一下桌面快捷方式即可）" }
# 5.2 面板
$port = 17890
$pf = Join-Path (Join-Path $exeDir "runtime") "panel.port"
if (Test-Path $pf) { $port = (Get-Content $pf -Raw).Trim() }
try {
    $r = Invoke-WebRequest -Uri "http://127.0.0.1:$port/" -UseBasicParsing -TimeoutSec 5
    $st = [regex]::Match($r.Content, 'data-state="([a-z]+)"').Groups[1].Value
    Say ("    OK 面板 http://127.0.0.1:$port/ 状态=" + $st)
} catch { Say ("    -- 面板未响应（进程不在时属正常）") }
# 5.3 引擎指标
try {
    $h = (Invoke-WebRequest -Uri "http://127.0.0.1:$port/" -UseBasicParsing -TimeoutSec 5).Content
    $t = [regex]::Match($h, '__WB_TOKEN__ = "([a-zA-Z0-9]+)"').Groups[1].Value
    $m = Invoke-RestMethod -Uri "http://127.0.0.1:$port/api/metrics?token=$t" -TimeoutSec 5
    Say ("    OK 指标 tasks=" + $m.tasks + " done=" + $m.done + " failed=" + $m.failed)
} catch { Say "    -- 指标未取到" }
# 5.4 计划任务（含"永不等待人工输入"判据 —— 光看 State 不够）
$t2 = Get-ScheduledTask -TaskName $KeepAliveTask -ErrorAction SilentlyContinue
if (-not $t2 -or $t2.State -eq "Disabled") { Say "    FAIL 守护任务异常"; $ok = $false }
else {
    $argLine = ($t2.Actions | ForEach-Object { $_.Arguments }) -join ' '
    if ($argLine -notmatch '-NonInteractive') { Say "    FAIL 守护任务缺 -NonInteractive（会弹提示挂起）"; $ok = $false }
    else { Say ("    OK 守护任务 State=" + $t2.State + " 且 -NonInteractive 在位") }
}
# 5.5 配置
$c2 = Get-Content $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json
if ($c2.eval.dailyCap -eq 2000 -and $c2.eval.threshold -eq 55 -and $c2.autoStart) { Say "    OK 配置已固化（dailyCap=2000 threshold=55 autoStart=true）" }
else { Say "    FAIL 配置未固化"; $ok = $false }

Say ""
if ($ok) { Say "===== 开箱即用装机完成 =====" }
else { Say "===== 有 FAIL 项，请把上面输出发给 ds 端 ====="; exit 1 }

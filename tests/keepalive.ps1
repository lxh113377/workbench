# keepalive.ps1 - WorkBench 24x7 守护（零源码改动）
#
# 为什么存在：workbench.exe 的所有停机路径都是 os.Exit(0)（main.go:215/240/290），
# 进程退出后没有任何拉起机制；且 autoResume 只在 phase=running/paused 时生效，
# 优雅停机把 phase 写成 stopped 后即使进程被拉起，引擎也不会自启（首页按钮 idle/stopped）。
# 本脚本补上这两块：进程死了拉起来 + 引擎没起就 POST /api/engine/start。
#
# 幂等：已在跑且引擎在跑时零动作（不杀不重启，避免打断在跑任务）。
# 端口自适应：从 runtime/panel.port 读当前端口（ProbeRunning 可能漂移）。
#
# 按 CommandLine 精确匹配 EXE 路径，不按进程名 —— 实测 2026-10-19：同机同时有
# final-check/soak-24h-v2/ui-probe 等多个 workbench 实例（Lead 的验收套件），
# 按进程名匹配会误判别人的实例。
#
# 注册为计划任务（每分钟）：
#   schtasks /Create /TN "WorkBenchKeepAlive" /SC MINUTE /MO 1 /F /TR "powershell -NoProfile -NonInteractive -ExecutionPolicy Bypass -WindowStyle Hidden -File D:\workdesktop\tests\keepalive.ps1"
#   ^ -NonInteractive 必带：否则脚本内任何提示都会等人工按键，计划任务永不返回。
param(
    [string]$Exe = "D:\workdesktop\dist\workbench.exe",
    [string]$RuntimeDir = "D:\workdesktop\dist\runtime",
    # DataRoot = workbench-output 目录；留空按安装布局取 RuntimeDir 的兄弟目录。
    # 仅用于回退读取 state.json 的 userQuit 字段（主判据是 user-quit.flag）。
    [string]$DataRoot = "",
    [switch]$Loop,
    [int]$IntervalSeconds = 60,
    # 进度看门狗（round 64 · T65）：进程存在 ≠ 在干活。
    #
    # 现场（2026-10-29）：01:47:02 启动后日志在 02:5x 停写，直到 14:17:03 才有下一条 ——
    # 中间 11 小时 0 行日志、0 次尝试，而守护任务每分钟都在跑、Last Result=0。
    # 根因：本脚本只用「进程是否存在」判活 ⇒ 进程挂死（或静默死亡后旧句柄仍在）时零动作。
    # 引擎本身每 ~2 分钟写一条「模型路由表已更新」心跳 ⇒ 日志停写 ≥ StaleMinutes 即判挂死。
    # ⚠️ 阈值必须由**实测心跳间隔**推导（2026-10-29 自伤事故）：第一版取 8 分钟，当天 15:19 部署的健康实例
    # 在 15:28 被自己的看门狗误杀（「日志停写 8.8 分钟」）。实测 3 天日志里「模型路由表已更新」心跳：
    # 中位 0.6 分钟，但 **≥8 分钟的间隔有 53 次**（最长 131.8 分钟，且多数与停机期无关的静默都在 8 分钟以上）
    # ⇒ 8 分钟远在正常抖动的分布之内。现取 30 分钟（≈中位值的 50 倍、比实测最大正常静默仍留余量），
    # 并叠加"必须有待做工作"这道条件 —— 空闲是正常态，杀一个没活干的引擎只会制造噪声。
    [int]$StaleMinutes = 30,
    # 自检模式：只验看门狗判据本身（含负控），不碰真实进程。
    [switch]$SelfTest
)

$ErrorActionPreference = "Stop"

# 无人值守硬要求（2026-10-19 实测事故）：本脚本由计划任务每分钟拉起一次，
# 一旦有任何构造等待人工确认，就会**永久挂起**（老大桌面弹出
# "Security Warning: Script Execution Risk ... Do you want to continue?"）。
# 三条兜底：
#   ConfirmPreference=None  禁止任何 cmdlet 弹确认提示
#   ProgressPreference=SilentlyContinue  进度条在无人会话里也会拖慢/异常
#   末尾 try/catch 记录而非弹窗
$ConfirmPreference = "None"
$ProgressPreference = "SilentlyContinue"

# 守护自身的持久日志（round 64 · T65）：本轮排查 11 小时静默时，最大的障碍是**守护没留痕** ——
# 只能看到「Last Result=0」，无法区分「它在零动作」与「它根本没跑」。
# 固定名 + 追加写：日志只增不删，出问题第一眼就有现场。
$KaLog = Join-Path $RuntimeDir "keepalive.log"
function Add-KaLog {
    param([string]$Msg)
    $line = "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss') keepalive: $Msg"
    Write-Host $line
    try {
        $dir = Split-Path -Parent $KaLog
        if ($dir -and -not (Test-Path -LiteralPath $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }
        Add-Content -LiteralPath $KaLog -Value $line -Encoding UTF8
    } catch { }
}

# 进度看门狗判据（纯函数，便于自检）：日志文件存在且最后写入时间距今 ≥ StaleMinutes ⇒ 判挂死。
# 为什么以**日志停写**为判据而不是 CPU/端口：
#   - 引擎无论有没有任务，都会每 ~2 分钟写一条心跳（"模型路由表已更新"，实测 22:36:04/22:38:04…）；
#   - 端口与进程都可能在挂死状态下仍然存在（阻塞在锁上的 goroutine 不会关监听），
#      所以它们是「活着」的必要条件、却不是「在干活」的充分条件。
#   - StaleMinutes ≤ 0 表示**关闭**该判据（自检的负控也用它）。
# 正常路径的周期留痕判据（round 64 · T65 补充）：本轮的 11 小时静默之所以查不动，
# 正是因为"一切正常时守护一个字也不写"。每 EveryMinutes 写一条心跳，既证明守护真在跑，
# 也给出"最后已知正常时刻"（出问题时可据此定界）。
function Should-LogHeartbeat {
    param([string]$KaLog, [int]$EveryMinutes = 10)
    if ($EveryMinutes -le 0) { return $false }
    if (-not $KaLog -or -not (Test-Path -LiteralPath $KaLog)) { return $true }
    try {
        $tail = Get-Content -LiteralPath $KaLog -Tail 60 -ErrorAction SilentlyContinue
        $last = @($tail | Where-Object { $_ -match "心跳正常" } | Select-Object -Last 1)
        if ($last.Count -eq 0) { return $true }
        $m = [regex]::Match($last[0], "^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})")
        if (-not $m.Success) { return $true }
        $t = [datetime]::ParseExact($m.Groups[1].Value, "yyyy-MM-dd HH:mm:ss", $null)
        return (((Get-Date) - $t).TotalMinutes -ge $EveryMinutes)
    } catch { return $true }
}

# 待做工作判据（round 64 · T65 修正，2026-10-29）：空闲引擎日志同样会静下来，
# 那种情况重启没有收益（只是 churn）。只有"有任务在跑/排队、却长时间没有任何日志"才值得强杀。
# 读 queue.json 快照的原始文本做状态正则（不解整个队列，避免大文件反序列化开销）；
# 文件读不到 ⇒ 返回 false（**取不到证据就不杀**，与 T65 的保守取向一致）。
function Test-PendingWork {
    param([string]$DataRoot)
    if (-not $DataRoot) { return $false }
    $q = Join-Path $DataRoot "queue.json"
    if (-not (Test-Path -LiteralPath $q)) { return $false }
    try {
        $raw = [IO.File]::ReadAllText($q)
        return ($raw -match '"state"\s*:\s*"(running|queued)"')
    } catch { return $false }
}

function Test-EngineStalled {
    param([string]$LogPath, [int]$StaleMinutes)
    if ($StaleMinutes -le 0) { return $false }
    if (-not $LogPath -or -not (Test-Path -LiteralPath $LogPath)) { return $false }
    try {
        $age = ((Get-Date) - (Get-Item -LiteralPath $LogPath).LastWriteTime).TotalMinutes
        return ($age -ge $StaleMinutes)
    } catch {
        return $false
    }
}

function Get-PanelPort {
    $pf = Join-Path $RuntimeDir "panel.port"
    if (Test-Path $pf) {
        $v = (Get-Content $pf -Raw).Trim()
        if ($v -match "^\d+$") { return [int]$v }
    }
    return 17890
}

# 按 EXE 路径匹配（大小写不敏感），排除同名不同路径的实例
function Get-WorkbenchProc {
    # 只认「生产实例」：命令行等于 exe 路径或只带 --silent。
    # 排除 --ui-probe/--soak 等同机测试实例（实测 2026-10-19：Lead 的 ui-probe
    # 命令行也是 dist\workbench.exe --ui-probe，曾被误判成生产实例导致不拉起）。
    $want = $Exe.ToLower()
    Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" | Where-Object {
        $_.CommandLine -and $_.CommandLine.ToLower().Contains($want) -and
        $_.CommandLine -notmatch "--ui-probe|--soak|--diagnose|--status|--export"
    } | ForEach-Object {
        Get-Process -Id $_.ProcessId -ErrorAction SilentlyContinue
    } | Select-Object -First 1
}

function Get-EngineState {
    param([int]$Port)
    try {
        $h = (Invoke-WebRequest -Uri "http://127.0.0.1:$Port/" -UseBasicParsing -TimeoutSec 3).Content
        $m = [regex]::Match($h, 'data-state="([a-z]+)"')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return $null
}

function Get-PanelToken {
    param([int]$Port)
    try {
        $h = (Invoke-WebRequest -Uri "http://127.0.0.1:$Port/" -UseBasicParsing -TimeoutSec 3).Content
        $m = [regex]::Match($h, '__WB_TOKEN__\s*=\s*"([a-zA-Z0-9_-]+)"')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return $null
}

# 用户主动退出的判据（2026-10-19 现场缺陷：托盘点「退出工作台」关不掉）。
#
# 产品侧在用户主动退出时落两个标记（engine.MarkUserQuit）：
#   ① runtime\user-quit.flag          单行文件，shell/PS 一行即可判断
#   ② workbench-output\state.json 的 userQuit 字段（结构化）
# 本脚本必须尊重它，否则现场就是：
#   用户点「退出工作台」→ 进程结束、相位落 stopped
#   → ≤60s 本脚本发现"进程不在"→ 拉起 --silent 新实例 → POST /api/engine/start
#   → 回到 running ⇒ 用户观感「关不掉」，7x24 语义被反转成"用户无权停机"。
#
# 为什么不能只判 data-state=stopped：stopped 同时包含「意外崩溃后的优雅停机」，
# 那种**必须**拉起来（7x24 的本意）。用户意愿只能由这两个显式标记表达 ——
# 这也正是产品侧要先把意愿"说出口"的原因。
#
# 清除点在产品侧（用户点「启动引擎」/面板「开始」→ Engine.Start 清标记），
# 本脚本**绝不自己清**：那等于替用户改主意，会把"用户想停"又变回"被拉起"。
function Test-UserQuit {
    $flag = Join-Path $RuntimeDir "user-quit.flag"
    if (Test-Path $flag) { return $true }
    # 回退判据：结构化消费者。dataRoot 默认与 runtime 同级（<Root>\workbench-output）。
    $dataRoot = if ($DataRoot) { $DataRoot } else { Join-Path (Split-Path -Parent $RuntimeDir) "workbench-output" }
    $sf = Join-Path $dataRoot "state.json"
    if (Test-Path $sf) {
        try {
            $raw = [IO.File]::ReadAllText($sf)
            if ($raw -match '"userQuit"\s*:\s*true') { return $true }
        } catch { }
    }
    return $false
}

# 完整性标签自愈（2026-10-19 现场根因：托盘图标不出现）。
#
# 为什么必须在这里做：D:\workdesktop 曾被文件系统沙箱打上
#   "Mandatory Label\Low Mandatory Level:(OI)(CI)(NW)"
# 从此目录启动的进程会继承 Low IL，而 Low IL 进程**不能**向 Medium 的 Explorer
# 注册托盘图标（NIM_ADD -> Access is denied），现象就是"状态栏没有图标"。
# Low 进程自己也改不了（icacls 返回 1299），必须由**已经是 Medium** 的进程来修 ——
# 本脚本由计划任务以用户身份运行，正是 Medium，且每分钟跑一次 ⇒ 天然的修复位。
# 幂等：标签已是 Medium 时 icacls 输出相同、无副作用。
function Repair-IntegrityLabel {
    param([string]$Dir)
    if (-not (Test-Path $Dir)) { return }
    try {
        $lines = & icacls.exe $Dir 2>&1
        $label = ($lines | Where-Object { $_ -match 'Mandatory Label' } | Select-Object -First 1)
        if ($label -and $label -match 'Low Mandatory Level') {
            & icacls.exe $Dir /setintegritylevel '(OI)(CI)M' 2>&1 | Out-Null
            $after = (& icacls.exe $Dir 2>&1 | Where-Object { $_ -match 'Mandatory Label' } | Select-Object -First 1)
            Write-Host ("$(Get-Date -Format HH:mm:ss) keepalive: 完整性标签 Low -> " + ("$after".Trim()) + "（托盘图标修复）")
        }
    } catch {
        Write-Host "$(Get-Date -Format HH:mm:ss) keepalive: 完整性标签检查异常 - $($_.Exception.Message)"
    }
}

Repair-IntegrityLabel -Dir (Split-Path -Parent $Exe)

# 实例清场（2026-10-20 新增，修「后台一堆个人AI工作台」的根因收口）：
# 实测 20:30 任务管理器里 ~24 个家族进程、而生产实例 16:57 已退 —— 没有任何一处负责回收。
# 本调用让守护每分钟顺手做一次机器级盘点：生产实例只留 1 个、探针孤儿按 TTL/宽限期回收。
# 幂等、零交互；异常只打印不抛出，绝不影响守护主流程。
$guardScript = Join-Path $PSScriptRoot "instance_guard.ps1"
if (Test-Path -LiteralPath $guardScript) {
    try {
        & "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $guardScript -Apply -Quiet 2>&1 | ForEach-Object { Write-Host $_ }
    } catch {
        Write-Host ("$(Get-Date -Format HH:mm:ss) keepalive: 实例守卫异常 - " + $_.Exception.Message)
    }
}

# ── 留痕面判据的辅助件（round 66 · T68）──────────────────────────────────────
# 现场成因：守护由计划任务以 conhost --headless 每分钟拉起，stdout 被丢弃。实测生产日志全文
# 18 行里只有两类（心跳正常 12 条 + 判定挂死/已终止），grep「进程不在，拉起」「已拉起」「引擎已启动」
# 全为 0 ⇒ 实例更替（2026-10-29 的 pid 28788→4016）在留痕面查不到，只能靠人盯 pid 猜。
# 本组判据锁两件事：
#   ① Add-KaLog 真把行写进**文件**（不是只打到 stdout）；
#   ② 三类关键决策路径的源码里**确有** Add-KaLog 调用 —— 源文本不变量，参照本仓既有做法
#      （quit_mutation_check.ps1 读自身文本定位变异点、失配即 throw，不放行"判据自己被改坏还绿"）。
#
# 为什么用源文本不变量而不是"跑一遍看日志"：三条路径要真拉起/真重启生产实例才能触发，
# 自检不能拿生产实例当用例；而"下一轮有人把这条 Add-KaLog 删掉"正是**静默回归**，
# 唯一能当场判红的就是源文本。
#
# ⚠️ 两条纪律（都是为了让判据有牙）：
#   1) 断言针对**锚点文案**而非行号 —— 行号随任意加行漂移会把好代码判红；只有文案被改名才失配，
#      而改名正是"机读关键词变了、下游 grep 静默失效"那一种，必须判红。
#   2) 锚点必须**分段拼接**构造（Join-KaAnchor）—— 本脚本自己就是被检文本，锚点若以连续字面量写在
#      自检块里，IndexOf 会先命中自检块自己的字符串、区段就把守护代码和自检文案混在一起，
#      真调用被删也可能被自检文案洗成绿（判据失效于自身）。拼接后源码里不存在连续形态。
function Join-KaAnchor {
    param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Parts)
    return ($Parts -join "")
}

function Get-SelfSourceText {
    # 读本脚本自身全文；读不到返回空串 ⇒ 所有锚点失配 ⇒ 自检判红（宁可假红，不假绿）。
    $self = Join-Path $PSScriptRoot "keepalive.ps1"
    try { return [IO.File]::ReadAllText($self) } catch { return "" }
}

function Test-TraceInSourceRegion {
    param([string]$Text, [string]$FromAnchor, [string]$ToAnchor, [string[]]$Needles)
    # 取 [$FromAnchor 之后, $ToAnchor 之前) 这段源码，断言每个 needle 都出现在其中。
    # 为什么要限定区段而不是整文件 grep：三条路径的文案有交集（「拉起」同时出现在"不拉起"与"拉起"
    # 两条分支），整文件匹配会让别处的 Add-KaLog 把真缺失洗成绿；区段内断言才等于"这条路径自己留了痕"。
    if (-not $Text -or -not $FromAnchor) { return $false }
    $from = $Text.IndexOf($FromAnchor, [StringComparison]::Ordinal)
    if ($from -lt 0) { return $false }
    $from += $FromAnchor.Length
    $to = $Text.Length
    if ($ToAnchor) {
        $t = $Text.IndexOf($ToAnchor, $from, [StringComparison]::Ordinal)
        if ($t -gt $from) { $to = $t }
    }
    $region = $Text.Substring($from, $to - $from)
    foreach ($n in @($Needles)) {
        if (-not $n) { return $false }
        if ($region.IndexOf($n, [StringComparison]::Ordinal) -lt 0) { return $false }
    }
    return $true
}

# 自检模式（round 64 · T65）：只验看门狗判据，不碰真实进程 —— 判据本身必须能被验收。
#
# 三个必须过的点：① 新鲜日志 ⇒ 不判挂死（否则会把在跑实例反复强杀）
#                ② 陈旧日志 ⇒ 判挂死
#                ③ StaleMinutes=0 ⇒ 判据关闭（负控：证明上面那条真读了这个参数，不是恒真）
if ($SelfTest) {
    if (-not $DataRoot) { $DataRoot = Join-Path (Split-Path -Parent $RuntimeDir) "workbench-output" }
    $tmpLog = Join-Path $env:TEMP ("wb-stale-selftest-" + $PID + ".jsonl")
    Set-Content -LiteralPath $tmpLog -Value '{}' -Encoding UTF8
    $okAll = $true

    (Get-Item -LiteralPath $tmpLog).LastWriteTime = Get-Date
    $fresh = Test-EngineStalled -LogPath $tmpLog -StaleMinutes 8
    if ($fresh) { $okAll = $false }
    Write-Host ("selftest 1) 新鲜日志 ⇒ 期望 False 实得 " + $fresh)

    (Get-Item -LiteralPath $tmpLog).LastWriteTime = (Get-Date).AddMinutes(-30)
    $stale = Test-EngineStalled -LogPath $tmpLog -StaleMinutes 8
    if (-not $stale) { $okAll = $false }
    Write-Host ("selftest 2) 陈旧日志(30min) ⇒ 期望 True 实得 " + $stale)

    $off = Test-EngineStalled -LogPath $tmpLog -StaleMinutes 0
    if ($off) { $okAll = $false }
    Write-Host ("selftest 3) StaleMinutes=0 ⇒ 期望 False 实得 " + $off)

    Remove-Item -LiteralPath $tmpLog -Force -ErrorAction SilentlyContinue

    # 心跳留痕判据（同族验收）：无日志文件 ⇒ 该写；刚写过 ⇒ 不该写。
    $tmpKa = Join-Path $env:TEMP ("wb-ka-selftest-" + $PID + ".log")
    Remove-Item -LiteralPath $tmpKa -Force -ErrorAction SilentlyContinue
    $hbNoFile = Should-LogHeartbeat -KaLog $tmpKa -EveryMinutes 10
    if (-not $hbNoFile) { $okAll = $false }
    Write-Host ("selftest 4) 无守护日志 ⇒ 期望 True 实得 " + $hbNoFile)
    Add-Content -LiteralPath $tmpKa -Value ((Get-Date -Format 'yyyy-MM-dd HH:mm:ss') + " keepalive: engine 心跳正常（自检）") -Encoding UTF8
    $hbFresh = Should-LogHeartbeat -KaLog $tmpKa -EveryMinutes 10
    if ($hbFresh) { $okAll = $false }
    Write-Host ("selftest 5) 刚写过心跳 ⇒ 期望 False 实得 " + $hbFresh)
    Remove-Item -LiteralPath $tmpKa -Force -ErrorAction SilentlyContinue

    # 待做工作判据（T65 修正的第二个条件）：有 running/queued ⇒ true；全终态 ⇒ false；文件缺失 ⇒ false（取不到证据不杀）。
    # 注意：判据约定读的是 <DataRoot>\queue.json —— 用例必须**照约定**造文件（首版把临时文件
    # 命名成别的名字，结果函数找不到 queue.json 返回 false，判据没问题、是用例错了）。
    $tmpDir = Join-Path $env:TEMP ("wb-q-selftest-" + $PID)
    New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
    Set-Content -LiteralPath (Join-Path $tmpDir "queue.json") -Value '{"tasks":[{"id":"a","state":"running"}]}' -Encoding UTF8
    $pwYes = Test-PendingWork -DataRoot $tmpDir
    if (-not $pwYes) { $okAll = $false }
    Write-Host ("selftest 6) 有 running 任务 ⇒ 期望 True 实得 " + $pwYes)
    Set-Content -LiteralPath (Join-Path $tmpDir "queue.json") -Value '{"tasks":[{"id":"a","state":"done"}]}' -Encoding UTF8
    $pwNo = Test-PendingWork -DataRoot $tmpDir
    if ($pwNo) { $okAll = $false }
    Write-Host ("selftest 7) 全终态 ⇒ 期望 False 实得 " + $pwNo)
    Remove-Item -LiteralPath $tmpDir -Recurse -Force -ErrorAction SilentlyContinue

    # 留痕面自检（T68 的第二半）：前 7 条验的是「判据对不对」，这两条验的是「判断结果有没有被留下来」。
    # 为什么必须单独验：本轮 T68 的起因就是守护**跑过但什么都不写**（Last Result=0 分不清
    # 「零动作」与「没跑」）。只测判据不测留痕，等于把同一个盲区重新留下。
    $tmpKa2 = Join-Path $env:TEMP ("wb-ka-log-selftest-" + $PID + ".log")
    Remove-Item -LiteralPath $tmpKa2 -Force -ErrorAction SilentlyContinue
    $prevKa = $KaLog
    try {
        $KaLog = $tmpKa2
        Add-KaLog "自检写入行"
        $written = (Test-Path -LiteralPath $tmpKa2) -and ((Get-Content -LiteralPath $tmpKa2 -Raw) -match "自检写入行")
        if (-not $written) { $okAll = $false }
        Write-Host ("selftest 8) Add-KaLog 真落盘（非仅 stdout）⇒ 期望 True 实得 " + $written)
    } finally {
        $KaLog = $prevKa
        Remove-Item -LiteralPath $tmpKa2 -Force -ErrorAction SilentlyContinue
    }

    # 源文本不变量（本仓既有范式，同 daily_count_source_test.go）：三类关键路径必须各有一条 Add-KaLog。
    # 只认「该行同时含 Add-KaLog 与该文案」；Write-Host 不算 —— 那正是 T68 修掉的形态。
    # 钉锚点文案而不是行号：行号会随任何一次编辑漂移，文案才是这些路径的身份证。
    # 注意：这里刻意用 -match 的**纯子串**判断而不是正则 —— 上一版在双引号串里嵌套
    # 字符类（形如 [^"] 的写法）被 PowerShell 当成数组索引，脚本直接 ParserError（本仓在册引号坑）。
    $kaSrc = Get-Content -LiteralPath $PSCommandPath -Encoding UTF8
    $anchors = @("进程不在，拉起", "经 lib_silent_launch 拉起 pid=", "已拉起 pid=", "引擎已启动（running", "用户主动退出（进程不在）")
    $pathHits = 0
    foreach ($needle in $anchors) {
        foreach ($ln in $kaSrc) {
            if ($ln -like ("*Add-KaLog*")) {
                if ($ln.IndexOf($needle, [System.StringComparison]::Ordinal) -ge 0) { $pathHits++; break }
            }
        }
    }
    if ($pathHits -lt $anchors.Count) { $okAll = $false }
    Write-Host ("selftest 9) 关键路径持久留痕齐备 ⇒ 期望 " + $anchors.Count + " 实得 " + $pathHits + "（少一条 = 留痕被删，事后又查不动）")

    # ── 留痕面判据（round 66 · T68）：上面 8 验「真写文件」、9 整文件验三类路径都留痕；
    # 10~12 把同一件事按**区段**拆开验（哪条路径缺痕直接指出来），13 验「判据自己没被改坏」。
    # 锚点与文案一律用 Join-KaAnchor 分段构造（② 号纪律），每条 Write-Host 只打中文摘要、
    # 不打锚点原文 —— 否则自检块自己就成了命中点，判据恒绿。
    $selfText = Get-SelfSourceText
    # 调用前缀同样分段构造：自检块里任何一行只要**同时**出现 Add-KaLog 与某条锚点文案，
    # 就会替被删掉的真实留痕"顶名"命中（第 9 条整文件匹配尤其怕这个）。
    $kaTraceCall = Join-KaAnchor "Add-Ka" "Log "
    $kaQuit   = Join-KaAnchor "if (`$userQuit -and -not " "`$proc) {"
    $kaLaunch = Join-KaAnchor "if (-not `$proc -and -not " "`$userQuit) {"
    $kaEngine = Join-KaAnchor "if (`$proc -and (`$state -eq " "`"stopped`" -or `$state -eq `"idle`") -and -not `$userQuit) {"
    $kaWatch  = Join-KaAnchor "# ── 进度" "看门狗"
    $kaLoop   = Join-KaAnchor "if (`$Loop) " "{ Start-Sleep"

    # 落盘本身（Add-KaLog 真写进文件、不只打到 stdout）已由上面第 8 条实测覆盖，不重复。
    # 10) 拉起路径的五条结果路径都要留痕：进入拉起 / 经 lib 拉起得到 pid / 退回自带 Start-Process /
    #    拉起失败（未检测到进程）/ 已拉起后的端口与状态。区段 = 拉起分支到看门狗注释之间。
    # 每条 needle 都写成 $kaTraceCall + 文案片段：区段里要找的是「这一行真的调用了留痕函数」，
    # 但**本自检块自己也在被检文本里**，任何一行只要同时出现 Add-KaLog 字面量与锚点文案，
    # 就会替被删掉的真实调用顶名命中（第 9 条整文件匹配已实测踩到这个形态）。
    $traceLaunch = Test-TraceInSourceRegion -Text $selfText -FromAnchor $kaLaunch -ToAnchor $kaWatch -Needles @(
        (Join-KaAnchor $kaTraceCall "`"进程不在，拉起"),
        (Join-KaAnchor $kaTraceCall "`"经 lib_silent_launch 拉起"),
        (Join-KaAnchor $kaTraceCall "`"拉起方式=自带 Start-Process"),
        (Join-KaAnchor $kaTraceCall "`"拉起失败（30s 内未检测到进程）"),
        (Join-KaAnchor $kaTraceCall "`"已拉起 pid="))
    if (-not $traceLaunch) { $okAll = $false }
    Write-Host ("selftest 10) 拉起路径五条结果路径均有持久留痕 ⇒ 期望 True 实得 " + $traceLaunch)

    # 11) 引擎 stop/start 调用的四条结果路径都要留痕：start 调用异常 / 达 running（引擎已启动）/
    #     未达 running / 取不到 token 而跳过。区段 = 引擎分支到循环尾部。
    $traceEngine = Test-TraceInSourceRegion -Text $selfText -FromAnchor $kaEngine -ToAnchor $kaLoop -Needles @(
        (Join-KaAnchor $kaTraceCall "`"引擎start调用异常"),
        (Join-KaAnchor $kaTraceCall "`"引擎已启动"),
        (Join-KaAnchor $kaTraceCall "`"引擎start未达running"),
        (Join-KaAnchor $kaTraceCall "`"引擎start跳过"))
    if (-not $traceEngine) { $okAll = $false }
    Write-Host ("selftest 11) 引擎 start 四条结果路径均有持久留痕 ⇒ 期望 True 实得 " + $traceEngine)

    # 12) 「用户主动退出 ⇒ 不拉起」这条判断的落点也要留痕：留痕面查不到「这一轮为什么没动作」，
    #     现场就只能靠 pid 变化反推守护跑没跑。区段 = 不拉起分支到拉起分支之间。
    $traceQuit = Test-TraceInSourceRegion -Text $selfText -FromAnchor $kaQuit -ToAnchor $kaLaunch -Needles @(
        (Join-KaAnchor $kaTraceCall "`"用户主动退出"))
    if (-not $traceQuit) { $okAll = $false }
    Write-Host ("selftest 12) 用户主动退出不拉起判定有持久留痕 ⇒ 期望 True 实得 " + $traceQuit)

    # 13) 防「自己改坏自己」：源文本读得到、且上面五个区段锚点在文件里都在位。
    #     为什么要单独一条：锚点失配时 10~12 也会红，但红的原因是「锚点找不到」而不是「留痕缺失」——
    #     改名或挪动代码会把人引向错误结论。这条先红就说明是判据自身失配，须同步改锚点而不是补留痕。
    $kaAnchorOk = ($selfText.Length -gt 1000)
    foreach ($a in @($kaQuit, $kaLaunch, $kaEngine, $kaWatch, $kaLoop)) {
        if ($selfText.IndexOf($a, [StringComparison]::Ordinal) -lt 0) { $kaAnchorOk = $false }
    }
    if (-not $kaAnchorOk) { $okAll = $false }
    Write-Host ("selftest 13) 源文本可读且五个区段锚点在位（判据自身有效）⇒ 期望 True 实得 " + $kaAnchorOk)

    if ($okAll) { Write-Host "keepalive selftest: PASS"; exit 0 } else { Write-Host "keepalive selftest: FAIL"; exit 1 }
}

# 派生路径（自检之后才用得上，但放这里便于两条路径共用）：
# 日志默认取 DataRoot\logs\workbench.jsonl —— 与产品侧写日志的位置同构。
if (-not $DataRoot) { $DataRoot = Join-Path (Split-Path -Parent $RuntimeDir) "workbench-output" }
$LogPath = Join-Path $DataRoot "logs\workbench.jsonl"

do {
    $now = Get-Date -Format "HH:mm:ss"
    $port = Get-PanelPort
    $proc = Get-WorkbenchProc
    $state = if ($proc) { Get-EngineState -Port $port } else { $null }

    # 每轮只判一次，既做守卫又做诊断（避免重复读文件、避免两条分支各判一次）。
    $userQuit = Test-UserQuit
    if ($userQuit -and -not $proc) {
        # 用户已主动退出：进程不在是**预期**状态，不是故障。
        # 按用户意愿保持停机，等待用户自己点「启动引擎」/面板「开始」
        # （产品侧会在 Engine.Start 清除标记，之后本脚本恢复正常守护）。
        # 持久留痕（T68）：这条判断是"本轮为什么没动作"的唯一答案。原先只有 Write-Host
        # ⇒ keepalive.log 查不到它，现场只能靠 pid 变化反推守护到底跑没跑过这一轮。
        Write-Host "$now keepalive: 检测到用户主动退出标记，尊重用户意愿，不拉起"
        Add-KaLog "用户主动退出（进程不在）⇒ 本轮判定不拉起"
    }
    if (-not $proc -and -not $userQuit) {
        Write-Host "$now keepalive: $Exe 进程不在，拉起"
        # 持久留痕（round 66 · T68）：本条原先只走 Write-Host ⇒ 计划任务窗口一关就没了，
        # 事后无法回答"这次实例是谁、什么时候拉起来的"（2026-10-29 排查"哪个二进制产出的草案"时正是这个缺口）。
        Add-KaLog "进程不在，拉起 $Exe"
        # 优先用仓内共用件 lib_silent_launch.ps1（Lead 17:54 加：按 PE 子系统分流，
        # GUI 走 Win32_Process.Create 保持脱离作业对象；CONSOLE 走 Hidden）。
        # 不在时退回自带逻辑（Start-Process -WindowStyle Hidden，实测同样零弹窗）。
        $lib = Join-Path $PSScriptRoot "lib_silent_launch.ps1"
        $launched = $false
        if (Test-Path $lib) {
            try {
                . $lib
                $pid2 = Start-WBInstance -Path $Exe -Arguments "--silent"
                if ($pid2 -gt 0) {
                    $launched = $true
                    Write-Host "$now keepalive: 经 lib_silent_launch 拉起 pid=$pid2"
                    Add-KaLog "经 lib_silent_launch 拉起 pid=$pid2"
                }
            } catch {
                Write-Host "$now keepalive: lib 拉起失败，退回自带逻辑 - $($_.Exception.Message)"
                Add-KaLog "lib 拉起失败 ⇒ 退回自带逻辑 - $($_.Exception.Message)"
            }
        }
        if (-not $launched) {
            # 自带逻辑：stdio 必须重定向，否则子进程继承管道，父 powershell 会挂起等它退出（实测踩过）
            # 每次拉起用**唯一**日志名（2026-10-25 round 174）：本脚本由计划任务每分钟拉起，
            # 固定名 %TEMP%\wb-keepalive-out.log 会在重叠运行时被两个进程同时写（本仓在册族）。
            # 同时清理 1 小时前的陈旧件，避免每分钟一个文件无限堆积。
            foreach ($old in @(Get-ChildItem -LiteralPath $env:TEMP -Filter "wb-keepalive-out-*.log" -ErrorAction SilentlyContinue)) {
                if ($old.LastWriteTime -lt (Get-Date).AddHours(-1)) { Remove-Item -LiteralPath $old.FullName -Force -ErrorAction SilentlyContinue }
            }
            $nullOut = Join-Path $env:TEMP ("wb-keepalive-out-" + $PID + "-$((Get-Date -Format 'HHmmss')).log")
            Start-Process -FilePath $Exe -ArgumentList "--silent" -WindowStyle Hidden -RedirectStandardOutput $nullOut -RedirectStandardError "$nullOut.err"
            # 留痕（T68）：两条拉起通路的脱离作业对象语义不同（lib 走 Win32_Process.Create，
            # 本路走 Start-Process 挂在调用会话作业对象上）。记下走的哪条，事后才能判"实例是谁拉起的、
            # 会不会随计划任务退出而连坐"。
            Add-KaLog "拉起方式=自带 Start-Process（未经 lib_silent_launch）"
        }
        # 运行时热修：源码在 Lead 写入口未改，这里每次拉起后重放修正
        #（删 reasoningEffort high + 注入协议修正；幂等）。
        $fixScript = Join-Path $PSScriptRoot "fix-runtime-patch.ps1"
        if (Test-Path $fixScript) {
            try {
                & "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File $fixScript 2>&1 | ForEach-Object { Write-Host ("$now keepalive: " + $_) }
            } catch {
                Write-Host "$now keepalive: 热修脚本执行异常 - $($_.Exception.Message)"
            }
        }
        # 轮询等待进程出现（最多 30s），比固定 sleep 更快也更稳
        $deadline = (Get-Date).AddSeconds(30)
        while ((Get-Date) -lt $deadline) {
            $proc = Get-WorkbenchProc
            if ($proc) { break }
            Start-Sleep -Seconds 2
        }
        $proc = Get-WorkbenchProc
        if (-not $proc) {
            Write-Host "$now keepalive: 拉起失败（未检测到进程）"
            Add-KaLog "拉起失败（30s 内未检测到进程）$Exe"
        } else {
            $port = Get-PanelPort
            $state = Get-EngineState -Port $port
            Write-Host "$now keepalive: 已拉起 pid=$($proc.Id)，端口 $port，状态 $state"
            Add-KaLog "已拉起 pid=$($proc.Id)，端口 $port，状态 $state"
        }
    }

    # stopped = 优雅停机后；idle = 从未启动。两者都拉回 running。paused 是主动暂停，不动。
    # 例外：用户主动退出（user-quit 标记）**不得**拉回 —— 否则托盘「退出工作台」形同虚设。
    # ── 进度看门狗（round 64 · T65）────────────────────────────────────────
    # 与「进程不在」分开处理：这里是**进程在、但没在干活**的形态。
    # 判据 = 引擎日志停写 ≥ StaleMinutes（引擎每 ~2 分钟一条路由表心跳，停 8 分钟即异常）。
    # 处置 = 强制重启：kill 掉挂死实例，随后**复用下面"进程不在"那条路径**拉起 + 拉起引擎。
    # 取舍（写清）：强杀会打断在跑任务，但引擎有崩溃救援（CrashRescues）会把它救回来；
    # 而放任挂死 11 小时（实测）连救援的机会都没有 —— 两害相权取其轻。
    # 用户主动退出时**不**做这件事：那是用户意愿（进程本来就不该在）。
    if ($proc -and -not $userQuit -and (Test-EngineStalled -LogPath $LogPath -StaleMinutes $StaleMinutes) -and (Test-PendingWork -DataRoot $DataRoot)) {
        $ageMin = [Math]::Round(((Get-Date) - (Get-Item -LiteralPath $LogPath).LastWriteTime).TotalMinutes, 1)
        Add-KaLog "engine 存活但日志停写 $ageMin 分钟（阈值 $StaleMinutes）⇒ 判定挂死，强制重启 pid=$($proc.Id)"
        try {
            Stop-Process -Id $proc.Id -Force -ErrorAction Stop
            $deadline = (Get-Date).AddSeconds(30)
            while ((Get-Date) -lt $deadline -and (Get-Process -Id $proc.Id -ErrorAction SilentlyContinue)) { Start-Sleep -Seconds 2 }
            Add-KaLog "挂死实例已终止（pid=$($proc.Id)）"
        } catch {
            Add-KaLog "终止挂死实例失败 - $($_.Exception.Message)"
        }
        $proc = $null
        $state = $null
    }

    # 正常路径的周期留痕：只在"进程在 + 日志新鲜"时写，且 10 分钟内只写一条。
    if ($proc -and -not $userQuit -and -not (Test-EngineStalled -LogPath $LogPath -StaleMinutes $StaleMinutes) -and (Should-LogHeartbeat -KaLog $KaLog -EveryMinutes 10)) {
        $ageMin = 0
        if (Test-Path -LiteralPath $LogPath) { $ageMin = [Math]::Round(((Get-Date) - (Get-Item -LiteralPath $LogPath).LastWriteTime).TotalMinutes, 1) }
        Add-KaLog "engine 心跳正常（日志距今 $ageMin 分钟，pid=$($proc.Id)，状态 $state）"
    }

    if ($proc -and ($state -eq "stopped" -or $state -eq "idle") -and -not $userQuit) {
        $tok = Get-PanelToken -Port $port
        if ($tok) {
            $uri = "http://127.0.0.1:$port/api/engine/start?token=$tok"
            # PS 5.1 对「HTTP 200 + 空响应体」会抛 Object reference not set（已知缺陷），
            # 不能只凭异常判断成败 —— 调用后复查按钮状态才是判据。
            try {
                $null = Invoke-WebRequest -UseBasicParsing -Uri $uri -Method POST -Body '{}' -ContentType "application/json" -TimeoutSec 5
            } catch {
                if ($_.Exception.Message -notmatch "Object reference") {
                    Write-Host "$now keepalive: start 调用异常 - $($_.Exception.Message)"
                    Add-KaLog "引擎start调用异常 - $($_.Exception.Message)"
                }
            }
            Start-Sleep -Seconds 2
            $state2 = Get-EngineState -Port $port
            if ($state2 -eq "running") {
                Write-Host "$now keepalive: 引擎已启动（状态 running）"
                Add-KaLog "引擎已启动（running，调用前 $state，pid=$($proc.Id)）"
            } else {
                Write-Host "$now keepalive: start 后状态=$state2（未达 running）"
                Add-KaLog "引擎start未达running（调用前 $state，调用后 $state2）"
            }
        } else {
            Write-Host "$now keepalive: 引擎 $state 但取不到 token，跳过"
            Add-KaLog "引擎start跳过（$state 但取不到 token）"
        }
    }

    if ($Loop) { Start-Sleep -Seconds $IntervalSeconds }
} while ($Loop)
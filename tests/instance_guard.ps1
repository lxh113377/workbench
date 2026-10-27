# instance_guard.ps1 -- WorkBench 家族实例清场守卫（唯一的实例回收位）
#
# 为什么存在（2026-10-20 老大现场："怎么后台显示有这么多个人AI工作台在运行"）：
#   实测 20:30 任务管理器「后台进程」里有 ~24 个「个人AI工作台」（Go 主 EXE，单个 8-24MB），
#   而生产实例 16:57 就已被用户退出（日志停在 16:57、runtime\user-quit.flag 在册）——
#   **死了的实例没有回收者**。根因是四条互相独立、谁也不负责收尾的拉起链：
#     ① HKCU Run「PersonalWorkbench」        -> 登录即起 --silent
#     ② 计划任务 WorkBenchKeepAlive（每分钟）  -> 拉起 --silent（走 Win32_Process.Create，**刻意脱离作业对象**）
#     ③ supervisor.SelfHeal                  -> os.Executable() 再拉一份 --silent
#     ④ 探针/验收/压测装置                    -> 各自用独立 --dir 起实例（ui-probe / soak / audit / perf）
#   而单实例锁是**按 runtime 目录**的（internal/single + layout.RuntimeDir）：④天然允许 N 个并存；
#   ②③又刻意让子进程脱离父进程作业对象活下去（Win32_Process.Create 的 PPID 落在常驻的 WmiPrvSE 上，
#   连"父进程死了"这个判据都失效）。没有任何一处枚举"现在有几个实例、哪些没人要"——
#   keepalive 只看「至少有 1 个在跑」（Select-Object -First 1），从不判重、从不清理。于是只增不减。
#
# 本脚本补这个位：**机器级实例盘点 + 有界回收**，幂等、零人工交互、默认只报告。
#   家族判据：产物 ProductName 为「个人AI工作台」的任何 EXE（含 wb-*.exe / hiddenlaunch.exe 等副本）
#   生产实例：命令行不带探针/测试子命令（即 --silent 或裸启动）
#   探针实例：带 --ui-probe / --soak / --acceptance / --diagnose / --status ... 的实例
#
# 五条裁决（按序，互不冲突）：
#   A 生产实例最多 1 个（优先留 runtime\workbench.lock 里登记的 pid，否则留最老的）
#   B runtime\user-quit.flag 在册（用户显式退出）时，超过宽限期的生产实例一律回收 —— 直修
#     "点了退出、后台还剩一堆"
#   C 探针实例超过自己声明的 --seconds 预算 + 宽限期仍在跑 = 违约（挂死/没退）→ 回收
#   D 探针实例没有声明预算（或预算非法）且 age > ProbeTtlMinutes → 回收
#   E 全局硬上限：家族实例总数 > MaxInstances 时，从最老的探针实例起回收（绝不回收生产实例）
#     —— 这是"防再犯"的硬保证：无论谁拉起、拉多少个，机器上恒 ≤ MaxInstances 个
#   （另有 F：探针实例父进程已死且过宽限期 → 回收；Win32_Process.Create 场景下不常命中，留作兜底）
#
# 用法:
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\instance_guard.ps1               # 只盘点
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\instance_guard.ps1 -Apply        # 真清理
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\instance_guard.ps1 -Apply -Quiet # 给计划任务用
#
# 退出码: 0=现场干净；1=发现待清理实例（报告模式=待清理；-Apply 模式=本轮清过）

param(
  [string]$InstallDir = "",
  [string]$Exe = "",
  [int]$GraceSeconds = 90,
  [int]$ProbeTtlMinutes = 10,
  [int]$MaxInstances = 6,
  [switch]$Apply,
  [switch]$Quiet
)

$ErrorActionPreference = "Stop"
$ConfirmPreference = "None"
$ProgressPreference = "SilentlyContinue"

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
  $InstallDir = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
}
if ([string]::IsNullOrWhiteSpace($Exe)) {
  $Exe = Join-Path $InstallDir "dist\workbench.exe"
}
$RuntimeDir = Join-Path (Split-Path -Parent $Exe) "runtime"
if (-not (Test-Path -LiteralPath $RuntimeDir)) {
  $RuntimeDir = Join-Path $InstallDir "dist\runtime"
}

function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }

# 探针/测试子命令表：命中即为"测试实例"（与 cmd\workbench\main.go 的 dispatch 表一一对应）。
$ProbePattern = '--ui-probe|--soak|--acceptance|--ac(\s|$)|--diagnose|--status|--stop|--quit|--export-all|--prune|--model-pool|--pool|--autonomy|--auto|--governance|--gov|--workspace|--ws|--selftest-workspace|--setup|--version'

function Get-FamilyProcesses {
  $now = Get-Date
  $alive = @{}
  foreach ($p in (Get-CimInstance Win32_Process)) { $alive[[int]$p.ProcessId] = $true }
  $out = @()
  foreach ($p in (Get-CimInstance Win32_Process)) {
    $path = $p.ExecutablePath
    if (-not $path) { continue }
    $isFamily = $false
    try {
      $vi = (Get-Item -LiteralPath $path -ErrorAction Stop).VersionInfo
      if ($vi.ProductName -eq '个人AI工作台' -or $vi.FileDescription -like '*个人*工作台*') { $isFamily = $true }
    } catch { $isFamily = $false }
    if (-not $isFamily) { continue }
    $pr = Get-Process -Id $p.ProcessId -ErrorAction SilentlyContinue
    $start = $null
    try { $start = $pr.StartTime } catch { }
    $ageSec = if ($start) { [int]($now - $start).TotalSeconds } else { -1 }
    $cmd = [string]$p.CommandLine
    $isProbe = [bool]($cmd -match $ProbePattern)
    # Go 测试二进制误判修复（2026-10-21 现场，直接造成 test 随机红）：
    #
    # 判据缺口：本脚本按**文件版本元数据**识别家族（ProductName='个人AI工作台'），
    # 而 go test 编出的测试二进制同样带这套版本资源（cmd/workbench 的
    # rsrc_windows_amd64.syso 被链接进去，实测 ProductName 逐字相同），
    # 且它的命令行是 wb-test.exe -test.paniconexit0 -test.timeout=10m0s，
    # 不含任何 --ui-probe/--soak 前缀 => 被判成"生产实例重复" => 每分钟的
    # keepalive(-Apply) 把它 **直接杀掉**。现场表现：go test ./cmd/workbench
    # 隔几次就整包 exit status 0xffffffff（不是断言失败，是进程被外部终止），
    # 日志里没有任何线索 —— 属于"测试假红"，会让真缺陷被当噪声忽略。
    #
    # 修法：把 Go 测试二进制特征也纳入"非生产"判据，它们与探针实例一样
    # 只能按预算/TTL 回收，绝不按"生产重复"杀。
    $isGoTest = $false
    if ($cmd -match '(^|\s)-test\.' -or $path -like '*.test.exe' -or $cmd -match '-test\.(run|timeout|paniconexit0|v|count)=') {
      $isGoTest = $true
    }
    if ($isGoTest) { $isProbe = $true }
    $budget = -1
    if ($isProbe) {
      $m = [regex]::Match($cmd, '--seconds[=\s]+(\d+)')
      if ($m.Success) { $budget = [int]$m.Groups[1].Value }
      # Go 测试二进制用 -test.timeout 表达预算（默认 10m）；换算成秒给后续裁决用。
      if ($isGoTest -and $budget -lt 0) {
        $mt = [regex]::Match($cmd, '-test\.timeout=([0-9]+)([smh])')
        if ($mt.Success) {
          $n = [int]$mt.Groups[1].Value
          switch ($mt.Groups[2].Value) { 's' { $budget = $n } 'm' { $budget = $n * 60 } 'h' { $budget = $n * 3600 } }
        } else {
          $budget = 600
        }
      }
    }
    $out += [pscustomobject]@{
      Pid         = [int]$p.ProcessId
      Ppid        = [int]$p.ParentProcessId
      Path        = $path
      Cmd         = $cmd
      IsProbe     = $isProbe
      Budget      = $budget
      ParentAlive = $alive.ContainsKey([int]$p.ParentProcessId)
      AgeSec      = $ageSec
      WsMB        = if ($pr) { [math]::Round($pr.WorkingSet64 / 1MB, 1) } else { 0 }
    }
  }
  return $out
}

# 读生产运行时锁里登记的 pid（internal/single 写的是 {"pid":N,...}）
#
# 实测坑：锁文件由持有者以独占方式打开（LockFile 整文件锁），
# [IO.File]::ReadAllText 会抛 "another process has locked a portion of the file"。
# 所以必须用 FileShare.ReadWrite 共享句柄去读 —— 共享读拿到的正是"谁持有锁"的线索。
function Get-LockPid {
  $lf = Join-Path $RuntimeDir "workbench.lock"
  if (-not (Test-Path -LiteralPath $lf)) { return 0 }
  $raw = ""
  try {
    $fs = [IO.File]::Open($lf, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
    try { $sr = New-Object IO.StreamReader($fs); $raw = $sr.ReadToEnd(); $sr.Close() } finally { $fs.Close() }
  } catch { return 0 }
  $m = [regex]::Match($raw, '"pid"\s*:\s*(\d+)')
  if ($m.Success) { return [int]$m.Groups[1].Value }
  return 0
}

function Test-UserQuit {
  if (Test-Path -LiteralPath (Join-Path $RuntimeDir "user-quit.flag")) { return $true }
  $sf = Join-Path (Join-Path (Split-Path -Parent $RuntimeDir) "workbench-output") "state.json"
  if (Test-Path -LiteralPath $sf) {
    try { if ([IO.File]::ReadAllText($sf) -match '"userQuit"\s*:\s*true') { return $true } } catch { }
  }
  return $false
}

function Write-GuardLog([string]$line) {
  try {
    Add-Content -LiteralPath (Join-Path $RuntimeDir "instance-guard.log") -Value $line -Encoding UTF8
  } catch { }
}

# ---------------- 盘点 ----------------
$procs = @(Get-FamilyProcesses)
$prod = @($procs | Where-Object { -not $_.IsProbe })
$probe = @($procs | Where-Object { $_.IsProbe })
$lockPid = Get-LockPid
$userQuit = Test-UserQuit

Say ("=== WorkBench 实例守卫 === 家族=" + $procs.Count + " 生产=" + $prod.Count + " 探针=" + $probe.Count +
     " 上限=" + $MaxInstances + " 锁登记pid=" + $lockPid + " 用户已退出=" + $userQuit +
     " 模式=" + $(if ($Apply) { "APPLY" } else { "REPORT" }))

# ---------------- 裁决（pid -> reason，后写覆盖先写，保持幂等） ----------------
$verdict = @{}
$order = @()

# 注意：参数名不能用 $pid —— 它是 PowerShell 只读自动变量（当前进程号），
# 赋/绑会直接抛 "Cannot overwrite variable pid because it is read-only or constant"。
function Decide([int]$targetPid, [string]$reason) {
  if (-not $script:verdict.ContainsKey($targetPid)) { $script:order += $targetPid }
  $script:verdict[$targetPid] = $reason
}

# A 生产实例按「安装根」各留 1 个（round 56 · T49 修正）
#
# 为什么必须按根分组：internal/single 的实例锁是**按 runtime 目录**的（每个安装根一把锁），
# 所以「dist 生产根 + workbench-agent 实验根」并存是**设计允许**的（验收/实验各用独立 --dir 根）。
# 旧写法全机只留 1 个，会把另一个根的正当实例判成「生产实例重复」直接杀掉。
function Get-InstallRoot($procInfo) { return (Split-Path -Parent $procInfo.Path).ToLower() }
$keepProd = $null
$prodByRoot = @($prod | Group-Object { Get-InstallRoot $_ })
foreach ($grp in $prodByRoot) {
  $keep = $grp.Group | Where-Object { $_.Pid -eq $lockPid } | Select-Object -First 1
  if (-not $keep) { $keep = $grp.Group | Sort-Object AgeSec -Descending | Select-Object -First 1 }
  foreach ($p in $grp.Group) {
    if ($p.Pid -ne $keep.Pid) { Decide $p.Pid ("同安装根内生产实例重复（每根仅允许 1 个，保留 pid=" + $keep.Pid + "）") }
  }
}
# B 用户显式退出后仍活着的生产实例（round 56 · T49 修正：按**实例自身根**判退出旗标）
#
# 为什么必须按根：user-quit.flag 是**每个安装根各写各的**（产品侧 engine.MarkUserQuit 落在该实例
# 自己的 runtime 目录），而旧写法只读主根（dist\runtime）的旗标，却拿去裁决**全机所有**生产实例
# ⇒ 主根 10-25 那次「托盘点退出」留下的**过期旗标**，会把实验根刚拉起的实例在 300s 宽限后一并处死。
# 现场（时间反推精确命中）：实例 startedAt 23:36:29.9 → 日志停在 23:41:25（age=296s，下个整分钟 age≥300 被杀）；
# 第二次 startedAt 00:33:10 → 日志停在 00:37:03 的「尝试开始」、再无「尝试结束」（age≈300s 同刻静默死亡）。
# 两次都不是崩溃、也不是 --quit（无任何退出日志），而是 keepalive 每分钟的 guard 规则 B 执行了「用户已退出」。
# 修法：只认「该实例自己那个根」的旗标；主根旗标（$userQuit）继续用于面板展示，不再作为全机判据。
$quitGrace = [Math]::Max($GraceSeconds, 300)
foreach ($p in $prod) {
  $ownRuntime = Join-Path (Split-Path -Parent $p.Path) "runtime"
  $ownQuit = $false
  if (Test-Path -LiteralPath (Join-Path $ownRuntime "user-quit.flag")) { $ownQuit = $true }
  $ownState = Join-Path (Split-Path -Parent $ownRuntime) "workbench-output\state.json"
  if (-not $ownQuit -and (Test-Path -LiteralPath $ownState)) {
    try {
      $sj = Get-Content -LiteralPath $ownState -Raw -ErrorAction Stop | ConvertFrom-Json
      if ($sj.userQuit -eq $true) { $ownQuit = $true }
    } catch { }
  }
  if ($ownQuit -and $p.AgeSec -ge $quitGrace) {
    Decide $p.Pid ("该实例所属根的用户退出标记在册但实例仍在，属残留（root=" + (Split-Path -Parent $p.Path) + "）")
  }
}
# C/D/F 探针实例
foreach ($p in $probe) {
  if ($p.Budget -gt 0) {
    if ($p.AgeSec -ge ($p.Budget + $GraceSeconds)) {
      Decide $p.Pid ("探针超出自身预算（--seconds " + $p.Budget + "，age=" + $p.AgeSec + "s）＝违约未退")
    }
  } else {
    if ($p.AgeSec -ge ($ProbeTtlMinutes * 60)) {
      Decide $p.Pid ("探针无预算且超过 TTL " + $ProbeTtlMinutes + "min（age=" + $p.AgeSec + "s）")
    }
  }
  if (-not $verdict.ContainsKey($p.Pid) -and -not $p.ParentAlive -and $p.AgeSec -ge $GraceSeconds) {
    Decide $p.Pid ("探针已无父进程且超过宽限期（age=" + $p.AgeSec + "s）")
  }
}

# E 全局硬上限：只对探针动手，从最老的开始，直到 (存活数 - 待清理数) <= 上限
if ($MaxInstances -gt 0) {
  $aliveCount = $procs.Count - $verdict.Count
  if ($aliveCount -gt $MaxInstances) {
    $need = $aliveCount - $MaxInstances
    foreach ($p in ($probe | Sort-Object AgeSec -Descending)) {
      if ($need -le 0) { break }
      if ($verdict.ContainsKey($p.Pid)) { continue }
      Decide $p.Pid ("超出全局实例上限 " + $MaxInstances + "（本机家族实例过多，从最老探针起回收）")
      $need--
    }
  }
}

$decisions = @()
# 循环变量同样不能用 $pid（只读自动变量）。用 $targetPid。
foreach ($targetPid in $order) {
  if (-not $verdict.ContainsKey($targetPid)) { continue }
  $p = $procs | Where-Object { $_.Pid -eq $targetPid } | Select-Object -First 1
  if ($p) { $decisions += [pscustomobject]@{ P = $p; Reason = $verdict[$targetPid] } }
}

if ($decisions.Count -eq 0) {
  Say "GUARD verdict=CLEAN 现场无需动作"
  foreach ($p in $procs) { Say ("   KEEP pid=" + $p.Pid + " age=" + $p.AgeSec + "s ws=" + $p.WsMB + "MB probe=" + $p.IsProbe + " budget=" + $p.Budget) }
  exit 0
}

foreach ($d in $decisions) {
  $p = $d.P
  $line = "   " + $(if ($Apply) { "KILL " } else { "FIND " }) + "pid=" + $p.Pid + " ws=" + $p.WsMB + "MB age=" + $p.AgeSec +
          "s probe=" + $p.IsProbe + " budget=" + $p.Budget + " reason=" + $d.Reason
  if ($Apply) {
    try {
      Stop-Process -Id $p.Pid -Force -ErrorAction Stop
      Say ($line + " -> 已结束")
    } catch {
      Say ($line + " -> 结束失败: " + $_.Exception.Message)
    }
  } else {
    Say $line
  }
}

if ($Apply) {
  Start-Sleep -Seconds 2
  $left = @(Get-FamilyProcesses)
  $leftProbe = @($left | Where-Object { $_.IsProbe })
  $leftProd = @($left | Where-Object { -not $_.IsProbe })
  Say ("GUARD verdict=REAPED 清理=" + $decisions.Count + " 剩余家族进程=" + $left.Count + " (生产=" + $leftProd.Count + " 探针=" + $leftProbe.Count + ")")
  if ($left.Count -gt 0) { foreach ($p in $left) { Say ("   KEEP pid=" + $p.Pid + " probe=" + $p.IsProbe) } }
  # 留痕：每次真清理往 runtime\instance-guard.log 记一行，供事后回答"清过几次、清了什么"
  $killed = (@($decisions | ForEach-Object { $_.P.Pid }) -join ",")
  Write-GuardLog ((Get-Date).ToString("yyyy-MM-dd HH:mm:ss") + " verdict=REAPED killed=" + $killed +
                  " remaining=" + $left.Count + " prod=" + $leftProd.Count + " probe=" + $leftProbe.Count)
} else {
  Say ("GUARD verdict=FOUND 待清理=" + $decisions.Count + "（加 -Apply 执行清理）")
}
exit 1

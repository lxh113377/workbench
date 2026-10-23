# check_resident_longrun.ps1 -- 服务化长驻实例的**空闲态**内存上界判据（2026-10-23 新增）
#
# 为什么需要（两轮实测的结论）：
#   仓库已有 `residentMBMax=64`，但它属于 `--ui-probe --expect-idle` **空闲探针**形态
#   （实测 13.3MB × 余量）；对 `--silent` **服务化长驻**实例不适用 ——
#   那是另一个进程形态、另一种记录规模，两者不可混用（2026-10-23 曾把两种读数互比
#   而误判「基线被证伪」，已自我更正并留记录）。
#
# 实测数据（同一台机、同一产物、裁后 5,006 条记录）：
#   空闲活实例连续 10 次采样：46.9 / 46.9 / ... / 46.9 MB（**完全恒定**）
#   任务执行期（6 任务并发）：86~115 MB，峰值 752 MB（裁剪前 33,740 条时）
#   ⇒ 结论：**必须分状态取值**。空闲态可设固定上界；执行期随并发数变化属预期，不设。
#
# 判据（三态，与 check_perf 同口径）：
#   rc=0 PASS       —— 空闲活实例 RSS <= 上界（默认 **128MB**）。
#     为什么不是 96：2026-10-23 实测「跑过任务后空闲」的实例在 95.5~107.4MB 来回（GC 池保留），
#     96 会随机误判；128 仍能抓到「量级塌方」（如 200MB+ 或裁剪前的 170MB 稳态）。
#     这是当前证据能支撑的最大结论 —— 写得更紧就是虚报（与本仓「别把没测成当合格」同一纪律）。
#   rc=1 FAIL       —— 超界
#   rc=2 UNVERIFIED —— 没有活实例 / 队列有活跃任务 / 30s 内内存未稳（极差超带宽）/ 取不到读数
#     （**绝不把没测成当测得过**）。空闲判据自 round 47 起用 queue.json 进行态数
#     （running/evaluating，对齐引擎 model.IsActive()）—— 旧的 phase==running 拦截是
#     结构性 bug：phase 只反映引擎循环开关，7×24 服务恒 running，门禁因此永不落盘。
#
# 用法: powershell -File tests\check_resident_longrun.ps1 [-Exe <path>] [-BoundMB 96] [-Quiet]
param(
  [string]$Exe = "",
  [double]$BoundMB = 128,
  # 稳定带宽：空闲态 OS RSS 仍在 GC 锯齿里小幅波动（实测极差约 6MB），极差 > 此值
  #   说明内存还在爬升（执行期余震），读数属瞬态 => UNVERIFIED，不硬取虚高值。
  [double]$StableBandMB = 8,
  [switch]$Quiet,
  [switch]$NoRecord
)
$ErrorActionPreference = "Stop"
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path (Split-Path -Parent $PSScriptRoot) "dist\workbench.exe" }

function Say([string]$m) { Write-Host $m }
function Unverified([string]$why) {
  Say ("UNVERIFIED: " + $why + "（没测成 != 合格）")
  exit 2
}

if (-not (Test-Path -LiteralPath $Exe)) { Unverified "产物不存在: $Exe" }

# 只认**本产物路径**的实例（同机可能有压测/探针副本，按名字全取会串读数）
$procs = @(Get-Process -Name workbench -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq $Exe })
if ($procs.Count -eq 0) { Unverified "没有以该产物路径运行的实例" }
if ($procs.Count -gt 1) {
  # 多实例时取启动最早的那个（服务化主实例），并明示读数归属
  $procs = @($procs | Sort-Object StartTime)
  Say ("  注意：同产物有 " + $procs.Count + " 个实例，取启动最早者量测")
}
$p = $procs[0]

# 空闲判定（关键）：先看引擎相位，非 stopped/idle 时不判内存 ——
# 执行期内存随并发数变化属预期，拿固定上界卡它只会产生假红。
# 产物布局：<root>/dist/workbench.exe + <root>/dist/workbench-output/state.json
# 故 DataRoot = <Exe 所在目录>/workbench-output（一层，不是两层 —— 首版写错成两层，
# 导致恒 UNVERIFIED；这类「路径少算一层」在本仓工具链里是高频坑）。
$dataRoot = Join-Path (Split-Path -Parent $Exe) "workbench-output"
$statePath = Join-Path $dataRoot "state.json"
$phase = "unknown"
if (Test-Path -LiteralPath $statePath) {
  try {
    $st = Get-Content $statePath -Raw | ConvertFrom-Json
    $phase = [string]$st.phase
  } catch { Unverified "state.json 解析失败: $($_.Exception.Message)" }
} else { Unverified "缺 state.json（无法判定是否空闲）: $statePath" }

# 空闲判据（2026-10-24 round 47 修正，实测坐实的结构性 bug）：
#   原判据 `if ($phase -eq "running") { Unverified }` 是错的 —— state.json 的 phase 反映的是
#   **引擎循环 on/off（Start→PhaseRunning / Stop→PhaseStopped），不是任务活跃度**。7×24 常驻
#   服务只要引擎壳活着就恒 phase=running（实测该字段还会陈旧：一度 100+ 分钟不更新），
#   拿它当「忙」判据 => 本门禁自 round 41 装上起，在自动路径上**永不落盘**（resident 序列恒 0
#   正是这个 bug 的必然结果，不是偶然）；而「服务空闲稳态」恰恰是它最该守的状态。
#   真正的空闲 = 队列里没有进行态任务。引擎侧唯一权威判据是 model.IsActive()
#   = (state==running || state==evaluating)（model/types.go:43），据此读只读的 queue.json。
$queuePath = Join-Path $dataRoot "queue.json"
$activeTasks = -1
if (Test-Path -LiteralPath $queuePath) {
  try {
    $qj = Get-Content $queuePath -Raw | ConvertFrom-Json
    $activeTasks = @($qj.tasks | Where-Object { $_.state -eq 'running' -or $_.state -eq 'evaluating' }).Count
  } catch { Unverified ("queue.json 解析失败（无法判定是否有活跃任务）: " + $_.Exception.Message) }
} else { Unverified ("缺 queue.json（无法判定是否有活跃任务）: " + $queuePath) }
if ($activeTasks -lt 0) { Unverified "未能取得进行态任务数" }
if ($activeTasks -gt 0) {
  Unverified ("队列中有 " + $activeTasks + " 个进行态任务（running/evaluating），内存随并发变化属预期 —— 本判据只在无活跃任务时适用")
}

# 采样窗口 + 稳定性判据（2026-10-24 round 47 重写；原版为 stopped 回落场景设计）：
#   实测**服务运行中的空闲态**（无活跃任务）OS RSS 也在 GC 锯齿里持续波动
#   （15 次采样极差约 6MB），原「连续 5 次差 ≤1MB」判据在此场景永不满足；
#   若退化成单点读数又违背 round 41 立的「单次读数没有参照物」纪律。
#   折中：采满 15 点/30s 取**中位**（锯齿两端自然抵消），用**极差 ≤ StableBandMB**
#   判稳 —— 极差过大 = 仍在执行期余震（刚跑完任务回落中），读数属瞬态 => UNVERIFIED，
#   绝不硬取一个虚高值（实测刚跑完任务的实例会落在 85~107MB 区间）。
$samples = @()
$waited = 0
for ($i = 0; $i -lt 15; $i++) {
  $p.Refresh()
  if ($p.HasExited) { Unverified "实例在采样中途退出" }
  $samples += [math]::Round($p.WorkingSet64 / 1MB, 1)
  if ($i -lt 14) { Start-Sleep -Seconds 2; $waited += 2 }
}
$sorted = @($samples | Sort-Object)
$median = $sorted[[int][math]::Floor($sorted.Count / 2)]
$spread = [math]::Round($sorted[-1] - $sorted[0], 1)
if ($spread -gt $StableBandMB) {
  Unverified ("30s 采样极差 " + $spread + "MB > 带宽 " + $StableBandMB + "MB —— 内存仍在波动（执行期余震/未空闲），这不是稳态读数")
}

Say ("  相位=" + $phase + "(仅引擎循环开关) 活跃任务=" + $activeTasks + " 采样=" + $samples.Count + "点/" + $waited + "s 中位=" + $median + "MB 极差=" + $spread + "MB(带宽" + $StableBandMB + ") 上界=" + $BoundMB + "MB")

# ---- 读数落盘（2026-10-24 round 41 新增）----
# 为什么：untracked / health / coverage 三个序列都落盘了，长驻内存是唯一没落的 ——
# 而内存恰是前几轮反复量错的指标：round 22 把探针与长驻两种形态互比、
# round 25 单点抽样得出「稳态波动 2 倍」（后来证实是空闲/执行期两态之差）、
# round 28 又发现双环境与 Explorer 的影响。三次教训同一根因：单次读数没有参照物。
# 落盘后趋势器能回答：中位在涨还是在跌、极差是否变大（GC 归还变慢的早期信号）、
# 稳定等待耗时是否变长。
if (-not $NoRecord) {
  $memDir = Join-Path (Split-Path -Parent $PSScriptRoot) '.workbuddy\memory'
  if (Test-Path -LiteralPath $memDir) {
    $v = 'PASS'
    if ($median -gt $BoundMB) { $v = 'FAIL' }
    $rec = [ordered]@{
      at = (Get-Date).ToString('o')
      gate = 'resident_longrun'
      medianMB = $median
      spreadMB = $spread
      boundMB = $BoundMB
      phase = $phase
      activeTasks = $activeTasks
      stableWaitS = $waited
      pid = $p.Id
      verdict = $v
      head = (& git -C (Split-Path -Parent $PSScriptRoot) rev-parse --short HEAD 2>$null)
    }
    Add-Content -LiteralPath (Join-Path $memDir 'gate-readings.jsonl') -Value ($rec | ConvertTo-Json -Compress) -Encoding UTF8
  }
}

if ($median -gt $BoundMB) {
  Say ("CHECK residentLongrun result=FAIL median=" + $median + "MB bound=" + $BoundMB + "MB")
  Write-Host ("  空闲态常驻 " + $median + "MB 超过上界 " + $BoundMB + "MB —— 存在回归（口径见 性能基线口径.md）") -ForegroundColor Red
  exit 1
}
if (-not $Quiet) { Say ("CHECK residentLongrun result=PASS median=" + $median + "MB bound=" + $BoundMB + "MB") }
exit 0

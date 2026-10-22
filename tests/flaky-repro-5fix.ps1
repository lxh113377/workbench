# flaky-repro-5fix.ps1 -- 有界压力复现本轮五项改造涉及的判据（-cpu=1 放大调度窗口）
#
# 为什么需要（2026-10-22）：本轮新增判据含并发/句柄语义（TempDir 被在途写入击穿、
# 两阶段确认的时序）。这类缺陷本机普通跑常不复现，CI 慢 runner 才张开 ——
# 1852ac2 那轮「本机绿 + 结构错」上线即再红就是前车之鉴。固化成脚本，
# 让『先在能稳定复现的口径下证伪』成为可跑动作而不是口头纪律。
#
# 反掩盖纪律（2026-10-22 本文件第一版踩到）：每个判定都必须打印 rc 与原始输出，
# 绝不允许「rc 非 0 但拿不出证据」当成一次有效 FAIL —— 那是把脚本自身缺陷洗成判红。
# 跑不起来（go 不可用/无输出）判 UNVERIFIED，不判 FAIL。
#
# 三态：0=PASS / 1=FAIL（有判据原文为证）/ 2=UNVERIFIED（前置或取证不足）
param(
  [int]$Count = 8,
  [switch]$Quiet
)
$ErrorActionPreference = "Continue"   # 刻意不用 Stop：go 往 stderr 写进度时不得中断取证
function Say([string]$m) { if (-not $script:Quiet) { Write-Host $m } }

$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"
if (-not (Test-Path -LiteralPath (Join-Path $wb 'go.mod'))) {
  Write-Host "UNVERIFIED: 找不到 workbench 模块根: $wb"
  exit 2
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Host "UNVERIFIED: PATH 里没有 go，无法取证"
  exit 2
}

$targets = @(
  @{ name = 'planRoleFlaky'; pkg = './internal/engine/';   run = 'TestPlanModelDiffersFromExecutorRole|TestPlanFailureDegradesToDirectExecution|TestPlanStageRunsBeforeExecution' },
  @{ name = 'watchdog';      pkg = './internal/executor/'; run = 'LatestOutputMtime|IsStalled|StallLimit' },
  @{ name = 'ledgerOrder';   pkg = './internal/engine/';   run = 'TestWriteLedgerThenTransition|TestFailTaskWritesLedger|TestCondemnPathWritesAudit|TestAppendAuditOrEvent|TestCleanFailurePath' },
  @{ name = 'runtimeRescue'; pkg = './internal/engine/';   run = 'TestRescueAfterMs|TestOrphanCandidate|TestOrphanConfirmDue|TestSweep' },
  @{ name = 'retryInvalid';  pkg = './internal/retry/';    run = 'TestNewRecords|TestNextDelay|TestWarnings|TestCleanConfig|TestDecide' }
)

$fail = 0; $pass = 0; $unverified = 0
foreach ($t in $targets) {
  Say ("--- {0}  count={1} cpu=1 -race" -f $t.name, $Count)
  Push-Location $wb
  try {
    $out = (& go test $t.pkg "-count=$Count" '-cpu=1' -race "-run" $t.run 2>&1 | Out-String)
    $rc = $LASTEXITCODE
  } catch {
    $out = "调用异常: " + $_.Exception.Message
    $rc = -1
  }
  Pop-Location
  $out = $out.Trim()
  if ([string]::IsNullOrWhiteSpace($out)) {
    # 没有任何输出 = 无法判定（不是一次 FAIL）
    $unverified++
    Write-Host ("CHECK {0} result=UNVERIFIED rc={1} reason=无任何输出（跑不起来不等于判红）" -f $t.name, $rc)
    continue
  }
  if ($rc -eq 0) {
    $pass++
    Say ("CHECK {0} result=PASS rc=0 reps={1}  原文: {2}" -f $t.name, $Count, ($out -split "`n")[0])
  } else {
    $fail++
    # 打印 rc + 原始输出前 8 行：判红必须带可核对的证据
    Write-Host ("CHECK {0} result=FAIL rc={1} reps={2}" -f $t.name, $rc, $Count)
    ($out -split "`n" | Select-Object -First 8) | ForEach-Object { Write-Host ("    | " + $_) }
  }
}

Say ("--- 汇总 PASS={0} FAIL={1} UNVERIFIED={2}" -f $pass, $fail, $unverified)
if ($fail -gt 0) { exit 1 }
if ($unverified -gt 0) { exit 2 }
exit 0
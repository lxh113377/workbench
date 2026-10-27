# check_ledger_evidence.ps1 —— 台账取证不变量门禁（round 40 · T15-④ / T18 / T21）
#
# 为什么需要：本轮把「每次尝试可自证」做成了硬要求 ——
#   * 执行/方案台账带 stderrPath / producedRunes / dshCommand（T15-④ / T21）；
#   * 产出协议与方案提示词都含第 6 条「不许吐自有工具调用语法」（T18④ / T18④-2）。
# 而「接线取证字段」已被 live 实测打回过一次：round 38 只接了最容易想到的几处，
# 漏掉窗口里最常见的 rate_limited requeue 行（实测 dshLen=0）。本门禁把那条教训固化成判据：
# **凡 AppendAttempt 调用点，必须逐点带取证字段** —— 靠人数必然漏，靠 grep 才能不漏。
#
# 三态口径（与 check_perf / check_queue_size 一致）：PASS / FAIL / UNVERIFIED（不计 PASS 也不判红）。
# 运行侧在 CI 上没有 live 数据根 ⇒ 必须显式 UNVERIFIED，绝不静默通过（本仓在册失效形态）。
#
# 运行侧数据根解析顺序：参数 -DataRoot > 环境变量 WORKBENCH_DATA_ROOT > D:\workbench-agent\workbench-output。
# 运行侧历史行口径：以**最新一条带 dshCommand 的行**为基线 —— 早于基线的行是 T21 之前的历史台账，
# 不计红但披露条数；晚于基线的行若缺 dshCommand 则判红（否则「历史行永远拖红」会逼人放宽判据）。
# 用法: pwsh -NoProfile -File tests/check_ledger_evidence.ps1 [-Quiet] [-DataRoot <路径>]
param(
  [switch]$Quiet,
  [string]$DataRoot = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$enc = [Text.UTF8Encoding]::new($false)
$fails = @()
$unverified = @()

function Read-Text([string]$rel) {
  # 显式按 UTF-8 解码：默认码页（本机 GBK / CI 437）会把中文源码读坏，
  # 于是「判据明明在」被读成「判据缺失」——本仓已踩过这个坑（见 check_gate_coverage 注释）。
  $p = Join-Path $Root $rel
  if (-not (Test-Path $p)) { return $null }
  return [IO.File]::ReadAllText($p, $enc)
}

function Say([string]$s) { if (-not $Quiet) { Write-Host $s } }

# ── 源侧判据（仓内静态可判，CI 也跑）──────────────────────────────
Say "=== 源侧判据 ==="

# 1) 产出协议含第 6 条硬性要求（T18④）。
$protocol = Read-Text "workbench/internal/artifact/protocol.go"
if ($null -eq $protocol) { $fails += "artifact/protocol.go 不存在" }
elseif ($protocol -notmatch "6\. 不要输出你自己的工具调用语法") { $fails += "产出协议缺第 6 条（禁自有工具调用语法）" }
else { Say "  PASS 产出协议第 6 条" }

# 2) 方案提示词含同款第 6 条（T18④-2：方案是另一次调用，产出协议管不到它）。
$plan = Read-Text "workbench/internal/blueprint/plan.go"
if ($null -eq $plan) { $fails += "blueprint/plan.go 不存在" }
elseif ($plan -notmatch "6\. 不要输出你自己的工具调用语法") { $fails += "方案提示词缺第 6 条（禁自有工具调用语法）" }
else { Say "  PASS 方案提示词第 6 条" }

# 3) 台账字段定义含三件套（T15-④ / T21）。
$store = Read-Text "workbench/internal/store/store.go"
if ($null -eq $store) { $fails += "store/store.go 不存在" }
else {
  foreach ($key in @('json:"stderrPath,omitempty"', 'json:"producedRunes,omitempty"', 'json:"dshCommand,omitempty"')) {
    if ($store -notmatch [regex]::Escape($key)) { $fails += "AttemptRecord 缺字段 $key" }
    else { Say "  PASS 台账字段 $key" }
  }
}

# 4) **每一个** AppendAttempt 调用点都要带 DSHCommand（round 38 实测教训：漏了最常见的那条）。
$engine = Read-Text "workbench/internal/engine/engine.go"
if ($null -eq $engine) { $fails += "engine/engine.go 不存在" }
else {
  $lines = $engine -split "\r?\n"
  $sites = 0; $wired = 0
  for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -notmatch "AppendAttempt\(store\.AttemptRecord\{") { continue }
    # 字段必须在这条语句内部（下 12 行内）——判据按「同一条语句」而不是「文件里有这个词」，
    # 否则把 DSHCommand 写在别处也能骗过门禁（同 check_test_assertions 的反掩盖思路）。
    $sites++
    $tail = ($lines[$i..([Math]::Min($i + 12, $lines.Count - 1))] -join "`n")
    if ($tail -match "DSHCommand:") { $wired++ } else { $fails += "engine.go:$($i + 1) 的 AppendAttempt 未带 DSHCommand" }
  }
  if ($sites -eq 0) { $fails += "engine.go 未找到 AppendAttempt 调用点（判据失去受理面）" }
  else { Say "  PASS 台账写入点带取证字段 $wired/$sites" }
}

# 5) DSH 参数只经 BuildArgs 一处拼装 —— 判据落在**真正 spawn 的文件**（executor.go），
#    不能落在 engine.go（第一版就落错了文件，门禁自己把自己判红，属自纠记录）。
$exec = Read-Text "workbench/internal/executor/executor.go"
$pure = Read-Text "workbench/internal/executor/pure.go"
if ($null -eq $exec) { $fails += "executor/executor.go 不存在" }
elseif ($exec -notmatch "args := BuildArgs\(") { $fails += "executor.go 调用点未走 BuildArgs（参数被自抄）" }
elseif ($exec -match "--expose-internals") { $fails += "executor.go 里出现手抄的 --expose-internals（应只在 pure.go 出现）" }
else { Say "  PASS 调用点经 BuildArgs 拼参数（未自抄）" }
if ($null -eq $pure) { $fails += "executor/pure.go 不存在" }
elseif ($pure -notmatch "func DSHCommandLine\(") { $fails += "pure.go 缺 DSHCommandLine（台账自证入口）" }
else { Say "  PASS DSHCommandLine 在册" }

# ── 运行侧判据（本机 live 台账；CI 上无数据根 ⇒ UNVERIFIED）──────────────
if ([string]::IsNullOrWhiteSpace($DataRoot)) {
  if ($env:WORKBENCH_DATA_ROOT) { $DataRoot = $env:WORKBENCH_DATA_ROOT }
  else { $DataRoot = "D:\workbench-agent\workbench-output" }
}
$tasksDir = Join-Path $DataRoot "tasks"
Say "=== 运行侧判据（数据根 $DataRoot）==="
if (-not (Test-Path $tasksDir)) {
  $unverified += "live 台账不存在（$tasksDir）：运行侧判据未测成"
  Say "  UNVERIFIED 未找到 live 台账（CI 上属正常；本地请确认数据根）"
} else {
  $rows = @()
  $files = Get-ChildItem $tasksDir -Recurse -Filter "attempts.jsonl" -ErrorAction SilentlyContinue |
    Sort-Object LastWriteTime -Descending | Select-Object -First 5
  foreach ($f in $files) {
    foreach ($l in (Get-Content $f.FullName -Tail 200)) {
      if ([string]::IsNullOrWhiteSpace($l)) { continue }
      try { $j = $l | ConvertFrom-Json } catch { continue }
      $rows += [pscustomobject]@{
        at  = [int64]$j.at
        cmd = ("" + $j.dshCommand)
        has = (("" + $j.dshCommand) -match "--profile web-desktop" -and ("" + $j.dshCommand) -match "--json -")
        kind = ("" + $j.kind)
      }
    }
  }
  if ($rows.Count -eq 0) {
    $unverified += "台账里没有可判定的行（$tasksDir）"
    Say "  UNVERIFIED 台账 0 行"
  } else {
    $badKind = @($rows | Where-Object { [string]::IsNullOrWhiteSpace($_.kind) }).Count
    $withCmd = @($rows | Where-Object { $_.has })
    $wm = 0
    if ($withCmd.Count -gt 0) { $wm = ($withCmd | Measure-Object -Property at -Maximum).Maximum }
    $hist = @($rows | Where-Object { (-not $_.has) -and $_.at -le $wm }).Count
    $bad = @($rows | Where-Object { (-not $_.has) -and $_.at -gt $wm }).Count
    if ($badKind -gt 0) { $fails += "运行侧：$badKind/$($rows.Count) 行缺 kind" }
    if ($wm -eq 0) {
      $unverified += "运行侧：$($rows.Count) 行里还没有一条带 dshCommand（本机尚未产出 T21 之后的台账行）"
      Say "  UNVERIFIED 尚无带 dshCommand 的台账行（$($rows.Count) 行历史行）"
    } elseif ($bad -gt 0) {
      $fails += "运行侧：基线之后有 $bad 行缺 dshCommand（基线=$wm）"
    } else {
      $ok = @($rows | Where-Object { $_.has }).Count
      Say "  PASS 运行侧：$ok/$($rows.Count) 行带 dshCommand（含基线；早于基线的 $hist 行属 T21 前历史台账，仅披露）"
    }
  }
}

# ── 汇总（反掩盖：逐条列出，不用一句「全过」盖掉）──────────────────────
Say ""
if ($fails.Count -gt 0) {
  Write-Host "台账取证门禁未通过（$($fails.Count) 条）：" -ForegroundColor Red
  foreach ($f in $fails) { Write-Host "  FAIL $f" -ForegroundColor Red }
  exit 1
}
foreach ($u in $unverified) { Write-Host "  UNVERIFIED $u" -ForegroundColor Yellow }
Write-Host "台账取证门禁通过" -ForegroundColor Green
exit 0

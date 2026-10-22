# check_queue_size.ps1 -- queue.json 规模门禁（防「记录只增不减」复发）
#
# 为什么需要（2026-10-23 实测）：queue.json 曾达 **69.3MB / 33,740 条**（终态 33,733、
# 在跑仅 7 条），每次快照全量序列化 + 内存常驻 3.3 万对象 ⇒ 长驻实例 RSS 170MB、
# 峰值 752MB（基线口径 64MB）。根因是 store 从无删除任务记录的 API，记录只增不减。
# 已新增 Store.PruneTasks（启动期按 TerminalTaskDays + MaxTerminalTasks 裁剪）。
# 本门禁守的是**它真的在生效**：记录数不得超过配置上限（留 10% 余量）。
#
# 判据（任一不满足即 rc=1）：
#   1) queue.json 里的任务数 <= MaxTerminalTasks × 1.1（启动期裁剪上限 + 余量）。
#      余量用于吸收「启动后新增的终态任务」（裁剪只在启动期跑一次）。
#   2) 非终态任务数 < 总任务数（若全是终态而记录仍超上限，说明裁剪没跑）。
#
# 三态：无 queue.json / 无配置 => UNVERIFIED(rc=2)，不当 PASS。
#
# 用法: powershell -File tests\check_queue_size.ps1 [-DataRoot <path>] [-Quiet]
param(
  [string]$DataRoot = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $DataRoot) { $DataRoot = Join-Path (Split-Path -Parent $PSScriptRoot) "dist\workbench-output" }

$queuePath = Join-Path $DataRoot "queue.json"
if (-not (Test-Path -LiteralPath $queuePath)) {
  Write-Host ("UNVERIFIED: 无 queue.json（" + $queuePath + "）—— 未构建/未运行过，不算合格") -ForegroundColor Yellow
  exit 2
}
$cfgPath = Join-Path $DataRoot "config.json"
$maxTerminal = 5000
if (Test-Path -LiteralPath $cfgPath) {
  try {
    $cfg = Get-Content $cfgPath -Raw | ConvertFrom-Json
    if ($cfg.retention -and $cfg.retention.maxTerminalTasks -gt 0) { $maxTerminal = [int]$cfg.retention.maxTerminalTasks }
  } catch { Write-Host "WARN config.json 解析失败，用默认 5000" -ForegroundColor Yellow }
}

$raw = [IO.File]::ReadAllText($queuePath)
$sizeMB = [math]::Round($raw.Length / 1MB, 2)
$q = $raw | ConvertFrom-Json
$tasks = @($q.tasks)
$n = $tasks.Count
$limit = [math]::Floor($maxTerminal * 1.1)

$terminalStates = @("rejected","done","failed","cancelled","degraded")
$nonTerminal = 0
foreach ($t in $tasks) { if ($terminalStates -notcontains $t.state) { $nonTerminal++ } }

Write-Host ("  queue.json = " + $sizeMB + "MB, 任务 " + $n + " 条（非终态 " + $nonTerminal + "），上限 " + $maxTerminal + "")

$fails = @()
if ($n -gt $limit) {
  $fails += ("任务记录 " + $n + " 条超过上限 " + $maxTerminal + "（余量后上限 " + $limit + "）—— 启动期裁剪未生效或已复发")
}
if ($n -gt 0 -and $nonTerminal -eq $n) {
  $fails += "全部都是非终态而记录仍超限 —— 裁剪逻辑可能被绕过（只会删终态）"
}

if ($fails.Count -gt 0) {
  Write-Host "queue 规模门禁未通过：" -ForegroundColor Red
  foreach ($f in $fails) { Write-Host ("  - " + $f) -ForegroundColor Red }
  Write-Host "修法：确认引擎启动走过 Recover()->PruneTasks；或手工调 Store.PruneTasks(pol,false)。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ("queue 规模合规：" + $n + " 条（裁剪上限 " + $maxTerminal + "，含 10% 余量后 " + $limit + "），" + $sizeMB + "MB") -ForegroundColor Green
}
exit 0

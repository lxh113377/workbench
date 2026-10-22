# check_untracked_go.ps1 -- 未跟踪 Go 文件与「只在本地跑的测试」门禁（2026-10-23 新增）
#
# 为什么需要（2026-10-23 实测）：
#   workbench 下有 **42 个未跟踪 .go**（41 个 _test.go + 3 个生产文件 lanes.go 等）。
#   后果分两层：
#   ① **安全层**：受跟踪代码引用未跟踪文件时，干净 clone 编译失败 —— 本仓有同族事故
#      （check_snapshot_build 的由来：4 个依赖文件从未入库，git archive 后 undefined）。
#   ② **质量层（本轮量化）**：HEAD 提交的测试函数 **717** 个，工作区（含未跟踪）**815** 个
#      —— **98 个测试只在本机跑，CI 永远看不到**。
#
# 判据（三态，与全仓口径一致）：
#   rc=0 PASS       —— 无未跟踪 .go，或全部是「已披露的本地专用件」
#   rc=1 FAIL       —— 存在**生产代码**未跟踪（工作区与提交语义不一致，一定坏）
#   rc=2 UNVERIFIED —— 不在 git 仓库 / 取不到 ls-files
#
# 读数落盘（2026-10-23 第二轮新增）：把三行核心读数追加到
#   .workbuddy/memory/gate-readings.jsonl
# 为什么：**单次读数不够**。本轮实测过一次「临时统计脚本正则失效 => 报 untracked=0」
# 而门禁报 42 个 —— 人肉/一次性脚本会骗人，序列读数不会。落盘后趋势可见：
# 差额（本地-only 测试数）应随并行会话提交而**下降**，若持续上升说明在途件在堆积。
#
# 用法: powershell -File tests\check_untracked_go.ps1 [-Quiet] [-NoRecord]
param([switch]$Quiet, [switch]$NoRecord)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"

$tracked = @(git -C $root ls-files "*.go")
if ($LASTEXITCODE -ne 0) { Write-Host "UNVERIFIED: 取不到 git ls-files" -ForegroundColor Red; exit 2 }
$tracked = @($tracked | ForEach-Object { $_.Trim('"') })

# 只取 ?? 行；porcelain 的 ?? 后紧跟一个空格，故 Substring(3) 正确剥掉 '?? '。
# 关键：必须先确认以 '??' 开头，**不能**用 -like '*.go' 直接筛全表 ——
# 那会把 ' M path.go' 这类已跟踪修改行也匹配进来（实测首版即此 bug：
# 把 41 个并行在途的 M 文件误报成「生产代码未入库」，FAIL 从 3 处虚增到 41 处）。
$untracked = @()
foreach ($line in @(git -C $root status --porcelain=v1 --untracked-files=all)) {
  if (-not $line.StartsWith('?? ')) { continue }
  $rel = $line.Substring(3).Trim('"')
  if ($rel -like '*.go') { $untracked += $rel }
}

function Get-TestCount([string[]]$rels) {
  $n = 0
  foreach ($rel in $rels) {
    $p = Join-Path $root $rel
    if (-not (Test-Path -LiteralPath $p)) { continue }
    $t = [IO.File]::ReadAllText($p)
    $n += ([regex]::Matches($t, "(?m)^func Test\w+")).Count
  }
  return $n
}
$committed = Get-TestCount $tracked
$worktree = Get-TestCount ($tracked + $untracked)
$gap = $worktree - $committed

$prodUntracked = @($untracked | Where-Object { $_ -notmatch '_test\.go$' })

# ---- 读数落盘 ----
if (-not $NoRecord) {
  $memDir = Join-Path $root '.workbuddy\memory'
  if (Test-Path -LiteralPath $memDir) {
    $rec = [ordered]@{
      at = (Get-Date).ToString('o')
      gate = 'check_untracked_go'
      untrackedGo = $untracked.Count
      untrackedProd = $prodUntracked.Count
      committedTests = $committed
      worktreeTests = $worktree
      gapLocalOnly = $gap
      head = (& git -C $root rev-parse --short HEAD 2>$null)
    }
    $line = ($rec | ConvertTo-Json -Compress)
    Add-Content -LiteralPath (Join-Path $memDir 'gate-readings.jsonl') -Value $line -Encoding UTF8
  }
}

if ($untracked.Count -gt 0 -and -not $Quiet) {
  Write-Host ("未跟踪 .go 文件 " + $untracked.Count + " 个：") -ForegroundColor Yellow
  foreach ($u in ($untracked | Sort-Object)) {
    $kind = if ($u -match '_test\.go$') { "test" } else { "**生产代码**" }
    Write-Host ("  ~ [" + $kind + "] " + $u) -ForegroundColor Yellow
  }
}

Write-Host ("  提交的测试函数 = " + $committed)
Write-Host ("  工作区测试函数 = " + $worktree + "（差额 " + $gap + " 个只在本地跑）")

if ($prodUntracked.Count -gt 0) {
  Write-Host ""
  Write-Host ("FAIL: " + $prodUntracked.Count + " 个**生产代码**文件未入库：") -ForegroundColor Red
  foreach ($p in $prodUntracked) { Write-Host ("  - " + $p) -ForegroundColor Red }
  Write-Host "  危害：工作区能编译/能跑，但提交出去的东西与工作区不是同一套语义。" -ForegroundColor Red
  exit 1
}

if (-not $Quiet) {
  Write-Host ""
  Write-Host ("PASS: 未跟踪件均为测试文件（提交决策由人按 ownership 判；差额 " + $gap + " 个测试只在本地跑）") -ForegroundColor Green
}
exit 0
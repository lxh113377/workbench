# check_untracked_go.ps1 -- 未跟踪 Go 文件与「只在本地跑的测试」门禁（2026-10-23 新增）
#
# 为什么需要（2026-10-23 实测）：
#   workbench 下有 **42 个未跟踪 .go**（41 个 _test.go + 1 个生产文件 lanes.go）。
#   后果分两层：
#   ① **安全层**：受跟踪代码引用未跟踪文件时，干净 clone 编译失败 —— 本仓有同族事故
#      （check_snapshot_build 的由来：4 个依赖文件从未入库，git archive 后 undefined）。
#      当前实测干净 clone build rc=0，说明**尚未发生**，但缺一道常驻判据。
#   ② **质量层（本轮量化）**：HEAD 提交的测试函数 **711** 个，工作区（含未跟踪）**815** 个
#      —— **104 个测试只在本机跑，CI 永远看不到**。也就是说「本地全绿」比「CI 全绿」
#      多跑了 104 个判据；拿本地绿推断提交质量会系统性偏乐观。
#
# 判据（三态，与全仓口径一致）：
#   rc=0 PASS       —— 无未跟踪 .go，或全部是「已披露的本地专用件」
#   rc=1 FAIL       —— 存在未跟踪 .go 且未披露
#   rc=2 UNVERIFIED —— 不在 git 仓库 / 取不到 ls-files
#
# 刻意**不判红**的理由（避免制造假红）：并行会话的在途件天然 untracked，
# 那是正常工作流而非缺陷；本门禁的职责是**把差距摆出来并可追踪**，
# 具体提交决策由人按 ownership 判。真正的红线由 check_gate_links（引用断链）
# 与 check_snapshot_build（编译不过）守 —— 那两类才是「一定坏」。
#
# 用法: powershell -File tests\check_untracked_go.ps1 [-Quiet]
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"

$tracked = @(git -C $root ls-files "*.go")
if ($LASTEXITCODE -ne 0) { Write-Host "UNVERIFIED: 取不到 git ls-files" -ForegroundColor Red; exit 2 }
$tracked = @($tracked | ForEach-Object { $_.Trim('"') })

$untracked = @(git -C $root status --porcelain=v1 | Where-Object { $_ -match '^\?\? .*\.go$' } | ForEach-Object { $_.Substring(3).Trim('"') })

# 统计测试函数数（含/不含未跟踪件）
function Get-TestCount([string[]]$rels, [bool]$includeUntracked) {
  $n = 0
  foreach ($rel in $rels) {
    if (-not $includeUntracked -and ($untracked -contains $rel)) { continue }
    $p = Join-Path $root $rel
    if (-not (Test-Path -LiteralPath $p)) { continue }
    $t = [IO.File]::ReadAllText($p)
    $n += ([regex]::Matches($t, "(?m)^func Test\w+")).Count
  }
  return $n
}
$committed = Get-TestCount $tracked $false
$worktree = Get-TestCount ($tracked + $untracked) $true

if ($untracked.Count -gt 0) {
  Write-Host ("未跟踪 .go 文件 " + $untracked.Count + " 个：") -ForegroundColor Yellow
  foreach ($u in ($untracked | Sort-Object)) {
    $kind = if ($u -match '_test\.go$') { "test" } else { "**生产代码**" }
    Write-Host ("  ~ [" + $kind + "] " + $u) -ForegroundColor Yellow
  }
}

Write-Host ("  提交的测试函数 = " + $committed)
Write-Host ("  工作区测试函数 = " + $worktree + "（差额 " + ($worktree - $committed) + " 个只在本地跑）")

# 生产代码未跟踪 => 这是**必须**修的（干净 clone 语义与工作区不一致）
$prodUntracked = @($untracked | Where-Object { $_ -notmatch '_test\.go$' })
if ($prodUntracked.Count -gt 0) {
  Write-Host ""
  Write-Host ("FAIL: " + $prodUntracked.Count + " 个**生产代码**文件未入库：") -ForegroundColor Red
  foreach ($p in $prodUntracked) { Write-Host ("  - " + $p) -ForegroundColor Red }
  Write-Host "  危害：工作区能编译/能跑，但提交出去的东西与工作区不是同一套语义。" -ForegroundColor Red
  exit 1
}

if (-not $Quiet) {
  Write-Host ""
  Write-Host ("PASS: 未跟踪件均为测试文件（提交决策由人按 ownership 判；差额 " + ($worktree - $committed) + " 个测试只在本地跑）") -ForegroundColor Green
}
exit 0
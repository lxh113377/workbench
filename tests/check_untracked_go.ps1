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
# 计数的**修订版本**必须与声称的口径一致（2026-10-25 round 179 修正）：
#   旧写法用 git ls-files 取**路径**再读**工作区内容** ⇒ 对「已跟踪但有未提交改动」的文件，数的是工作区版本，
#   却被当成「提交的测试函数」，差额（只在本地跑的测试）因此被低估。现：脏文件从 HEAD blob 数，干净文件读工作区（等价且快）。
$dirtyTracked = @(git -C $root diff --name-only --diff-filter=M HEAD -- "*.go" | Where-Object { $_ })  # 只取 HEAD 里存在且被改的；暂存新增件 git show HEAD: 必炸 fatal（2026-10-26 round 185 实测红）
function Get-HeadTestCount([string[]]$rels) { $n = 0; foreach ($r in $rels) { $t = (& git -C $root show ("HEAD:" + $r) 2>$null | Out-String); if ($t) { $n += ([regex]::Matches($t, "(?m)^func Test" + [char]92 + "w+")).Count } }; return $n }
$cleanTracked = @($tracked | Where-Object { $dirtyTracked -notcontains $_ })
$committed = (Get-TestCount $cleanTracked) + (Get-HeadTestCount $dirtyTracked)
$worktree = (Get-TestCount @($tracked | Where-Object { Test-Path -LiteralPath (Join-Path $root $_) })) + (Get-TestCount $untracked)
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
  Write-Host "  => 查看 docs/handoff/在途件交接记录-*.md（谁引用它、必须跟谁一起提交）" -ForegroundColor Red
  exit 1
}

if (-not $Quiet) {
  Write-Host ""
  Write-Host ("PASS: 未跟踪件均为测试文件（提交决策由人按 ownership 判；差额 " + $gap + " 个测试只在本地跑）") -ForegroundColor Green
}
# ---------- 未跟踪测试的健康度检测（2026-10-24 round 36 新增）----------
# 为什么需要（round 36 实测）：本门禁此前只报「有 N 个测试只在本地跑」这个数量，
# 不报它们的状态。round 36 手动 go test 才发现工作区有 6 个测试失败
#（autonomy 4 + shortcut 1 + cmd/workbench build failed），全部由未跟踪测试文件引起，
# 而本门禁的读数完全看不出 —— 数量稳定在 31/gap=77，像是一切正常。
#
# 危害：并行会话在途件若长期停在「工作区红」状态，
#   该会话自己可能没意识到；别人看到 gap=77 也只以为「只是没提交」。
#
# 判据（informational，刻意不计 rc）：把未跟踪测试文件按包分组，对涉及包跑 go test，
# 报告哪些包因未跟踪件而失败。不判红的理由：未跟踪件所有权不在本门禁
#（提交决策由人按 ownership 判），判红会让它从「可见性工具」变成「拦截器」。
# 但必须可见 —— 这是数量读数看不到的那一半。
$untrackedTests = @($untracked | Where-Object { $_ -match '_test\.go$' })
if ($untrackedTests.Count -gt 0) {
  $pkgs = @{}
  foreach ($u in $untrackedTests) {
    $dir = Split-Path $u -Parent
    if ($dir) { $pkgs[$dir] = $true }
  }
  $goExe = (Get-Command go -ErrorAction SilentlyContinue).Source
  if (-not $goExe) {
    $cand = @('C:\Program Files\Go\bin\go.exe')
    foreach ($c in $cand) { if (Test-Path $c) { $goExe = $c; break } }
  }
  if ($goExe) {
    $badPkgs = @()
    $wbDir = Join-Path $root 'workbench'
    foreach ($dir in ($pkgs.Keys | Sort-Object)) {
      # 坑（2026-10-24 实测，10 个包里误报 7 个）：未跟踪文件的路径是
      # `workbench/internal/engine`（**已含** workbench 前缀），而 go test 的
      # WorkingDirectory 已设为 <root>/workbench —— 直接拼会变成
      # `workbench/workbench/internal/engine`（directory not found，ExitCode=1 被当成 FAIL）。
      # 故必须先剥掉前缀，且只对 workbench 下的包做检测（tests/ 等非模块目录跳过）。
      $dirNorm = ($dir -replace '\\', '/')
      if (-not $dirNorm.StartsWith('workbench/')) { continue }
      $pkgRel = './' + $dirNorm.Substring('workbench/'.Length)
      $tpsi = New-Object System.Diagnostics.ProcessStartInfo
      $tpsi.FileName = $goExe
      $tpsi.Arguments = 'test -count=1 ' + $pkgRel
      $tpsi.WorkingDirectory = $wbDir
      $tpsi.UseShellExecute = $false
      $tpsi.CreateNoWindow = $true
      $tpsi.RedirectStandardOutput = $true
      $tpsi.RedirectStandardError = $true
      try {
        $tp = [System.Diagnostics.Process]::Start($tpsi)
        $null = $tp.StandardOutput.ReadToEnd()
        $null = $tp.StandardError.ReadToEnd()
        $tp.WaitForExit()
        if ($tp.ExitCode -ne 0) { $badPkgs += $dir }
      } catch { }
    }
    if ($badPkgs.Count -gt 0) {
      Write-Host ''
      Write-Host ('WARN: 以下包因未跟踪测试文件而测试失败（' + $badPkgs.Count + ' 个）：') -ForegroundColor Yellow
      foreach ($b in $badPkgs) { Write-Host ('  ! ' + $b) -ForegroundColor Yellow }
      Write-Host '  形态：「工作区红 / 干净 HEAD 绿」—— 提交本身没问题，但在途件是红的。' -ForegroundColor Yellow
    # ---- 健康度读数落盘（2026-10-24 round 37 新增）----
    # 为什么单独一段（不并进上面的读数落盘段）：那段在文件前部，跑在健康度检测**之前**，
    # 拿不到 $badPkgs。round 37 首版试过「把落盘段整体搬到健康度之后」，结果搬坏了括号
    # （parse 报 L174 Unexpected token '}'）—— 搬整个块风险大于收益，故改为就地追加一段。
    # 序列意义：单次 WARN 只是快照，序列才能看出「在途件红了多久」（round 20 的同个理由）。
    if (-not $NoRecord) {
      $memDir2 = Join-Path $root '.workbuddy\memory'
      if (Test-Path -LiteralPath $memDir2) {
        $rec2 = [ordered]@{
          at = (Get-Date).ToString('o')
          gate = 'check_untracked_go_health'
          badPkgCount = $badPkgs.Count
          badPkgs = ((@($badPkgs) | ForEach-Object { $_ -replace '\\', '/' }) -join ';')
          untrackedTests = $untrackedTests.Count
          head = (& git -C $root rev-parse --short HEAD 2>$null)
        }
        Add-Content -LiteralPath (Join-Path $memDir2 'gate-readings.jsonl') -Value ($rec2 | ConvertTo-Json -Compress) -Encoding UTF8
      }
    }
      Write-Host '  本门禁刻意不判红（所有权不在它），但这条 WARN 是数量读数看不到的那一半。' -ForegroundColor Yellow
    } elseif (-not $Quiet) {
      Write-Host ''
      Write-Host ('未跟踪测试健康度：' + $pkgs.Count + ' 个涉及包 go test 全绿') -ForegroundColor Green
    }
  }
}

exit 0
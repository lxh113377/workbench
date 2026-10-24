# check_staged_bom.ps1 -- 提交前闸：即将入库的文件不得有多重 BOM，.ps1 不得缺 BOM（round 95 新增）
#
# 为什么需要（round 95 实测，第 10 次 BOM 类错误）：
#   CI 上 check_multi_bom 判红 tests/css-token-debt.txt 有 3 个前导 BOM ——
#   我三次用 Buffer.concat([BOM, ...]) 写该文件，每次都 prepend 却没先剥旧的。
#   **本地 check_all 当时是绿的**（33 PASS），因为它判 HEAD 口径，而文件还没 commit、不在 HEAD 里。
#   这就是「本地绿 = CI 红」的一个具体机制：HEAD 口径的门禁天然看不见「即将入库」的内容，
#   靠人记得在提交后再跑一次不可靠（我恰恰没记得）。
#
# 判据（判**索引里的 blob**，即 git add 之后、commit 之前的内容）：
#   对每个已暂存（diff --cached）的文件：
#   1) 索引 blob 有 >=2 个连续前导 EF BB BF => 判红（任何文件类型都不该有双 BOM）；
#   2) .ps1/.psm1 含非 ASCII 字节但索引 blob **无** BOM => 判红（CI 的 PS5.1 会 ParserError）；
#      —— 口径与 check_ps1_encoding 一致，但查的是索引而非工作区，故能在 commit 前拦住。
#
# 与既有门禁的分工：
#   check_multi_bom / check_ps1_encoding 判 HEAD（CI 用，历史真相）；
#   本件判索引（本地提交前用，抓「即将入库」的缺陷）—— 补上 HEAD 口径看不见暂存内容这段窗口。
#
# 用法: powershell -File tests/check_staged_bom.ps1 [-Quiet]
# 退出码: 0=暂存内容合规 / 1=有缺陷 / 2=UNVERIFIED（不在 git 仓库/取不到 git）
param([switch]$Quiet)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git' -ForegroundColor Red; exit 2 }

# 已暂存（新增/修改/复制）的文件清单
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$q = [string][char]34
$psi.Arguments = '-C ' + $q + $root + $q + ' diff --cached --name-only --diff-filter=ACM -z'
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
$p1 = [System.Diagnostics.Process]::Start($psi)
$so1 = $p1.StandardOutput.ReadToEnd()
$p1.WaitForExit()
if ($p1.ExitCode -ne 0) { Write-Host 'UNVERIFIED: git diff --cached 失败' -ForegroundColor Red; exit 2 }
$staged = @($so1 -split [char]0 | Where-Object { $_ -ne '' })
if ($staged.Count -eq 0) {
  if (-not $Quiet) { Write-Host '暂存区无文件（无需 BOM 检查）' -ForegroundColor Green }
  exit 0
}

$bad = @()
foreach ($rel in $staged) {
  # 取索引里的 blob 原始字节（:path 即暂存内容）
  $psi2 = New-Object System.Diagnostics.ProcessStartInfo
  $psi2.FileName = $gitExe
  $psi2.Arguments = '-C ' + $q + $root + $q + ' cat-file blob ' + $q + (':' + $rel) + $q
  $psi2.WorkingDirectory = $root
  $psi2.UseShellExecute = $false
  $psi2.CreateNoWindow = $true
  $psi2.RedirectStandardOutput = $true
  $psi2.RedirectStandardError = $true
  $p2 = [System.Diagnostics.Process]::Start($psi2)
  $ms = New-Object System.IO.MemoryStream
  $p2.StandardOutput.BaseStream.CopyTo($ms)
  $se2 = $p2.StandardError.ReadToEnd()
  $p2.WaitForExit()
  if ($p2.ExitCode -ne 0) { $p2.Dispose(); continue }
  $bytes = $ms.ToArray()
  $p2.Dispose()
  # 数连续前导 BOM
  $n = 0; $i = 0
  while ($bytes.Length -ge $i + 3 -and $bytes[$i] -eq 0xEF -and $bytes[$i+1] -eq 0xBB -and $bytes[$i+2] -eq 0xBF) { $n++; $i += 3 }
  if ($n -ge 2) {
    $bad += ($rel + '  索引 blob 有 ' + $n + ' 个前导 BOM（第 2 个起会被当作正文首字符）')
    continue
  }
  # .ps1/.psm1：含非 ASCII 却无 BOM => 判红
  $lower = $rel.ToLowerInvariant()
  if ($lower.EndsWith('.ps1') -or $lower.EndsWith('.psm1')) {
    if ($n -eq 0) {
      $hasNonAscii = $false
      foreach ($bb in $bytes) { if ($bb -gt 127) { $hasNonAscii = $true; break } }
      if ($hasNonAscii) {
        $bad += ($rel + '  含非 ASCII 但索引 blob 无 BOM —— CI 的 PS5.1 会 ParserError')
      }
    }
  }
}

if ($bad.Count -gt 0) {
  Write-Host ('暂存区 BOM 门禁未通过：' + $bad.Count + ' 个即将入库的文件有 BOM 缺陷') -ForegroundColor Red
  foreach ($b in $bad) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  修法：按连续前导循环剥净，再按需前置恰好一个（.ps1 含非 ASCII 才需要 BOM；.go 绝不可有）。' -ForegroundColor Yellow
  Write-Host '  为什么本地门禁没提前发现：check_multi_bom / check_ps1_encoding 判 HEAD 口径，' -ForegroundColor Yellow
  Write-Host '    文件没 commit 就不在 HEAD 里 —— 本件补的正是这段窗口（判索引）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('暂存区 BOM 合规：' + $staged.Count + ' 个已暂存文件，索引 blob 无多重 BOM、.ps1 该带的都带了') -ForegroundColor Green
}
exit 0
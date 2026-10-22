# check_staged_gofmt.ps1 -- 提交前闸：暂存区里的 .go 必须已 gofmt（2026-10-24 新增）
#
# 为什么需要（本人在 CHANGELOG 里记过多次、实际踩过三次的同类疏漏）：
#   改完 .go 文件忘跑 gofmt -w => CI 的 build job 判红 => 额外一轮提交去修格式。
#   三次实例：round 14（3 个文件）、round 22（阈值改动无关但同轮踩）、round 23（protocol.go）。
#   「记在 CHANGELOG 里」显然不够 —— 人会忘，机制不会。
#
# 判据（任一命中即 rc=1）：
#   **已暂存**（git diff --cached --name-only）的 .go 文件，LF 归一化后跑 gofmt -l 非空。
#   只查暂存区：那正是「即将被提交」的内容，也是 CI 将要判定的内容。
#   未暂存的工作区改动不判（可能还在编辑中，判了会挡住正常流水）。
#
# 与 tests/check_gofmt.ps1 的分工（刻意不重复）：
#   check_gofmt.ps1  —— 判 **HEAD** 全树漂移 + 债务豁免过期（CI repo-gates 用）
#   check_staged_gofmt.ps1 —— 判 **暂存区**（本地提交前闸，秒级返回）
#   后者是前者的「早一步」：在提交前就发现，而不是等 CI 五分钟后再判红。
#
# 用法: powershell -File tests\check_staged_gofmt.ps1 [-Quiet]
# 退出码: 0=暂存区无漂移 / 1=有漂移 / 2=UNVERIFIED（不在 git 仓库/无暂存文件）
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

# git 可执行名解析：hook 环境（core.hooksPath）里 git **不在 PATH** ——
# 2026-10-24 实测：hook 调用本脚本时报「术语 'git' 不会被识别为 cmdlet」，
# 而直接在终端跑同一脚本却正常（终端 PATH 有 git）。这是只在 hook 环境出现的缺陷，
# 故这里显式找 git：先 PATH，再 Git 安装目录，再 GIT_DIR 反推。
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe', 'C:\Program Files (x86)\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host "UNVERIFIED: 找不到 git 可执行文件（PATH 与常见安装目录均无）" -ForegroundColor Red
  exit 2
}

# 坑 4（2026-10-24 实测，第 4 个）：hook 环境里 git 传下 **相对路径** 的
# GIT_INDEX_FILE=.git/index。PowerShell 进程的 cwd 虽是仓库根，但 git 解析该相对路径时
# 仍可能失败（且 $LASTEXITCODE 变为空、stdout/stderr 皆空 —— 静默失败最难查）。
# 解法：把 GIT_INDEX_FILE 规范化为**绝对路径**再调 git；并清理它以免影响子进程。
$idxEnv = $env:GIT_INDEX_FILE
if ($idxEnv -and -not [IO.Path]::IsPathRooted($idxEnv)) {
  $env:GIT_INDEX_FILE = [IO.Path]::GetFullPath((Join-Path $root $idxEnv))
}

$diffArgs = @('-C', $root, 'diff', '--cached', '--name-only', '--diff-filter=ACM', '--', '*.go')
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$psi.Arguments = ($diffArgs | ForEach-Object { '"' + $_ + '"' }) -join ' '
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$proc = [System.Diagnostics.Process]::Start($psi)
$so = $proc.StandardOutput.ReadToEnd()
$se = $proc.StandardError.ReadToEnd()
$proc.WaitForExit()
$grc = $proc.ExitCode
if ($grc -ne 0) {
  Write-Host ("UNVERIFIED: git diff --cached 失败 rc=" + $grc) -ForegroundColor Red
  Write-Host ("  stderr=" + $se.Trim())
  exit 2
}
$staged = @($so -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ -like '*.go' })
$staged = @($staged | ForEach-Object { $_.Trim('"') } | Where-Object { $_ -like '*.go' })
if ($staged.Count -eq 0) {
  if (-not $Quiet) { Write-Host "暂存区无 .go 文件（无需 gofmt 检查）" }
  exit 0
}

# 坑 5（2026-10-24 实测，第 5 个）：hook 环境里 **go / gofmt 也不在 PATH**
# （与坑 1 的 git 同源：hook 由 git spawn，PATH 是 mingw 那套，没有 Windows 工具链）。
# 首版在这里直接 `& go env GOROOT`，hook 环境下报「术语 'go' 不会被识别」，
# 整个 gate 挂在 UNVERIFIED —— 又一个只在 hook 环境出现的缺陷。
# 解法：gofmt 依次从 PATH、GOROOT（用显式找到的 go）、常见安装目录解析。
$gofmt = (Get-Command gofmt -ErrorAction SilentlyContinue).Source
if (-not $gofmt) {
  $goExe = (Get-Command go -ErrorAction SilentlyContinue).Source
  if (-not $goExe) {
    foreach ($cand in @('C:\Program Files\Go\bin\go.exe', 'C:\Program Files\Go\bin\go.exe')) {
      if (Test-Path $cand) { $goExe = $cand; break }
    }
  }
  if ($goExe) {
    $goroot = (& $goExe env GOROOT 2>$null)
    if ($goroot) {
      $cand = Join-Path ($goroot.Trim()) 'bin\gofmt.exe'
      if (Test-Path $cand) { $gofmt = $cand }
    }
  }
  if (-not $gofmt) {
    foreach ($cand in @('C:\Program Files\Go\bin\gofmt.exe', 'C:\Program Files\Go\bin\gofmt.exe')) {
      if (Test-Path $cand) { $gofmt = $cand; break }
    }
  }
}
if (-not $gofmt -or -not (Test-Path $gofmt)) {
  Write-Host "UNVERIFIED: 找不到 gofmt（PATH 与 GOROOT 均无）" -ForegroundColor Red
  exit 2
}

$bad = @()
$tmp = Join-Path $env:TEMP ("stagedfmt-" + [guid]::NewGuid().ToString('N').Substring(0,8) + ".go")
foreach ($rel in $staged) {
  $p = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $p)) { continue }
  # LF 归一化后判（与 check_gofmt 同口径：gofmt 只接受 LF，工作区是 CRLF）
  $text = ([IO.File]::ReadAllText($p)) -replace "`r`n", "`n"
  [IO.File]::WriteAllText($tmp, $text)
  # gofmt 的两种非零退出都要算「未通过」：
  #   rc!=0 且 stderr 非空 => 语法错误（比格式漂移更严重，同样不该进提交）
  #   stdout 非空（-l 列出文件）=> 格式漂移
  # 2026-10-24 实测教训：首版只看 stdout，一个语法错误的探针文件被判成「通过」——
  # 「stderr 有内容但 stdout 空」被漏掉了。
  $gErr = $null
# 坑 6（2026-10-24 实测，第 6 个）：gofmt 也必须用**显式进程**调用，不能用 `& $gofmt ... 2>&1`。
# 后者把 gofmt 的 stderr 并进输出流，PowerShell 会把 NativeCommandError 记进 $Error，
# $LASTEXITCODE 行为不稳定 —— 表现为「文件已 gofmt 却报 gofmt 报错、且错误信息为空」。
# 与坑 4（git diff --cached 同样的病）同一解法：ProcessStartInfo + 分别重定向。
$fpsi = New-Object System.Diagnostics.ProcessStartInfo
$fpsi.FileName = $gofmt
$fpsi.Arguments = '-l "' + $tmp + '"'
$fpsi.UseShellExecute = $false
$fpsi.CreateNoWindow = $true
$fpsi.RedirectStandardOutput = $true
$fpsi.RedirectStandardError = $true
$fproc = [System.Diagnostics.Process]::Start($fpsi)
$fout = $fproc.StandardOutput.ReadToEnd()
$ferr = $fproc.StandardError.ReadToEnd()
$fproc.WaitForExit()
$frc = $fproc.ExitCode
if ($frc -ne 0) {
  $bad += ($rel + '  [gofmt 报错 rc=' + $frc + '：' + (($ferr -split "`n" | Select-Object -First 1).Trim()) + ']')
  continue
}
if ($fout.Trim()) { $bad += $rel }
}
Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue

if ($bad.Count -gt 0) {
  Write-Host ("提交前闸：以下已暂存的 .go 未 gofmt（" + $bad.Count + " 个）：") -ForegroundColor Red
  foreach ($f in $bad) {
    Write-Host ("  - " + $f) -ForegroundColor Red
  }
  Write-Host "修复: cd workbench; gofmt -w <上述文件>   然后 git add 它们" -ForegroundColor Yellow
  Write-Host "（这类疏漏已让人在 CI 上多跑三轮：round 14 / 22 / 23）" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ("提交前闸通过：暂存的 " + $staged.Count + " 个 .go 均已 gofmt") -ForegroundColor Green
}
exit 0
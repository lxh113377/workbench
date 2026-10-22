# check_ps1_eol.ps1 -- .ps1 入库内容必须是 LF（2026-10-24 新增）
#
# 为什么需要（2026-10-24 实测，一次真实的 169 行假 diff）：
#   round 25 我改 tests/check_all.ps1 后用 write 工具写回（CRLF），git add 后
#   入库内容变成 **CRLF=173 / LF=16**（原是 LF=181）。git 认为每一行都变了，
#   diff 显示 `-142 +150` —— 一个 8 行的新增被放大成 177 增 / 169 删，
#   评审者根本看不出真实改动，只能靠我逐行核对才发现。
#
# 机理（不是猜的，cat-file blob 实测）：
#   .gitattributes 写 `*.ps1 text eol=crlf`，本意是「检出转 CRLF、入库转 LF」。
#   但**直接覆写工作区文件后 git add，clean 过滤器并未完全归一** ——
#   173 个 CRLF 原样进了对象库。新文件（未跟踪）反而会被归一（探针实测：
#   写 CRLF => 入库 LF）。所以这个坑只咬「改已有文件」的场景，最需要防。
#
# 判据（任一命中即 rc=1）：
#   **已暂存**（git diff --cached --name-only）的 .ps1，其**入库内容**
#   （git cat-file blob :<file>，即索引中的版本）含 CRLF。
#   查索引而非工作区：那正是将被提交的内容。
#
# 与 check_ps1_encoding.ps1 的分工：后者管 BOM（有非 ASCII 就必须有 BOM），
#   本件管换行（入库必须 LF）。两者都是「提交内容形态」判据。
#
# 用法: powershell -File tests\check_ps1_eol.ps1 [-Quiet]
# 退出码: 0=无 CRLF / 1=有 CRLF / 2=UNVERIFIED
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host "UNVERIFIED: 找不到 git" -ForegroundColor Red
  exit 2
}

# 只查已暂存的 .ps1（那正是将被提交的内容）
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$psi.Arguments = '-C "' + $root + '" diff --cached --name-only --diff-filter=ACM -- "*.ps1"'
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$proc = [System.Diagnostics.Process]::Start($psi)
$so = $proc.StandardOutput.ReadToEnd()
$proc.WaitForExit()
if ($proc.ExitCode -ne 0) {
  Write-Host ("UNVERIFIED: git diff --cached 失败 rc=" + $proc.ExitCode) -ForegroundColor Red
  exit 2
}
$staged = @($so -split "`n" | ForEach-Object { $_.Trim() } | Where-Object { $_ -like '*.ps1' })
if ($staged.Count -eq 0) {
  if (-not $Quiet) { Write-Host "暂存区无 .ps1 文件（无需 EOL 检查）" }
  exit 0
}

$bad = @()
foreach ($rel in $staged) {
  # 取**索引中**的原始字节（git cat-file blob :<path>）—— 不是工作区文件
  $bpsi = New-Object System.Diagnostics.ProcessStartInfo
  $bpsi.FileName = $gitExe
  $bpsi.Arguments = '-C "' + $root + '" cat-file blob ":' + $rel + '"'
  $bpsi.UseShellExecute = $false
  $bpsi.CreateNoWindow = $true
  $bpsi.RedirectStandardOutput = $true
  $bpsi.RedirectStandardError = $true
  $bproc = [System.Diagnostics.Process]::Start($bpsi)
  # 用 latin1 读，把每个字节映射为一个字符，才能精确数 CRLF
  $ms = New-Object System.IO.MemoryStream
  $bproc.StandardOutput.BaseStream.CopyTo($ms)
  $bproc.WaitForExit()
  $bytes = $ms.ToArray()
  $ms.Dispose()
  $crlf = 0
  for ($i = 0; $i -lt $bytes.Length - 1; $i++) {
    if ($bytes[$i] -eq 13 -and $bytes[$i + 1] -eq 10) { $crlf++ }
  }
  if ($crlf -gt 0) {
    $bad += ($rel + '  含 ' + $crlf + ' 个 CRLF')
  }
}

if ($bad.Count -gt 0) {
  Write-Host ("入库 EOL 门禁未通过（" + $bad.Count + " 个 .ps1 含 CRLF）：") -ForegroundColor Red
  foreach ($b in $bad) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "  后果：diff 会把每一行都算作改动（实测 8 行新增被放大成 177 增 / 169 删），真实改动被淹没。" -ForegroundColor Yellow
  Write-Host "  修复: 用 LF 写回该文件（或 git add 后用 git diff --cached --numstat 核对增删数）" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("入库 EOL 合规：暂存的 " + $staged.Count + " 个 .ps1 均为 LF") -ForegroundColor Green }
exit 0
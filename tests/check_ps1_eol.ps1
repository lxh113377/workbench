# check_ps1_eol.ps1 -- .ps1 入库内容必须是 LF（2026-10-24 新增）
#
# 为什么需要（2026-10-24 实测，一次真实的 169 行假 diff）：
#   round 25 我改 tests/check_all.ps1 后用 write 工具写回（CRLF），git add 后
#   入库内容变成 **CRLF=173 / LF=16**（原是 LF=181）。git 认为每一行都变了，
#   diff 显示 `-142 +150` —— 一个 8 行的新增被放大成 177 增 / 169 删，
#   评审者根本看不出真实改动，只能靠我逐行核对才发现。
#
# 机理（2026-10-24 round 31 三轮实测后的**更正** —— round 26 的结论有错）：
#   round 26 我当时的结论是「clean 过滤器对直接覆写不生效，CRLF 会进索引」。
#   **该结论不成立**。round 31 精确复刻 round 25 的操作序列
#   （读字符串 -> 改 -> 写回 -> add -> commit），索引与 HEAD **均为 LF**（CRLF=0）。
#   当前配置（`.gitattributes` 的 `*.ps1 text eol=crlf` + `core.autocrlf=true`）下，
#   CRLF **进不了 HEAD** —— add 时即被归一。
#   round 25 那次的真实成因：`.gitattributes` 首版是 `*.ps1 text=auto`（不强制 eol），
#   round 22 才改成 `eol=crlf`；173 个 CRLF 发生在**规则切换的过渡期**
#   （`git log --diff-filter=A -- .gitattributes` 实测：首版 text=auto）。
#
# **故本门禁当前是预防性护栏，不是活跃检测**：
#   它防的是「将来有人改 .gitattributes / autocrlf / 用 -c 覆盖配置，使 CRLF 又能进索引」
#   这种**配置回归**。当前配置下它恒报 PASS（实测：造 CRLF 文件 add 后索引即 LF）。
#   保留理由：配置回归无声无息，而它的代价（round 25 的 169 行假 diff）极高；
#   删掉只要一行，重新发现却要对全仓做字节审计。
#   这段更正留着，是为了下一个读到的人不再重复我 round 26 的错误推断 ——
#   **「实测过一次」不等于「机理正确」；机制会随配置变化而过期**。
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
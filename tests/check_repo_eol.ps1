# check_repo_eol.ps1 -- 仓库里以 CRLF / 混合换行入库的文本文件（2026-10-24 round 44 新增）
#
# 为什么需要（round 25 真实事故 + round 43/44 实验坐实的机理）：
#   round 25 我改 tests/check_all.ps1 时，173 个 CRLF 进了 HEAD，diff 从 8 行新增
#   被放大成 177 增 / 169 删，真实改动被淹没（那次成因是 .gitattributes 规则切换的过渡期）。
#   round 44 用一次性临时仓库做了两关键实验，把机理钉死：
#     实验1：无属性、core.autocrlf=false 时把 CRLF 文件 add -> 报 i/crlf；
#     实验2：**之后**再补上 "*.ps1 text eol=crlf" 并提交，**不 renormalize**
#            -> 依旧报 i/crlf，HEAD 原始字节仍是 CRLF。
#   结论：CRLF 一旦进了对象库，后来加规则**不会自动纠正**，会一直残留。
#
# 与已有两个 EOL 门禁的分工（三层各管一个时点，刻意不重复）：
#   check_ps1_eol.ps1        查**暂存区**里 .ps1 的入库内容（提交前，仅本地）
#   check_resident_* 等      不相关
#   check_repo_eol.ps1(本件) 查**已在库**的全部文本文件（CI 上兜底，能抓到已入库的残留）
#   前者的盲区正是本件要补的：残留已经提交后，查暂存区就再也看不到了。
#
# 判据（窄面，只报可证伪的那两类）：
#   git ls-files --eol 报告 i/crlf 或 i/mixed 的受跟踪文件 => 判红。
#   刻意不管的（避免假红）：
#     i/-text  git 视为二进制/标了 -text（如 .exe/.syso/.pyc、故意保留编码的样本），不算违例；
#     i/none   文件内根本没有换行（单行无结尾符，实测证据件 result.py 即此类），不算违例。
#   上闸前的实况：全仓 2043 个受跟踪文件 = i/lf 1944 + i/none 95 + i/-text 4，
#   **i/crlf 与 i/mixed 均为 0** => 上线即绿，不是给 CI 添噪声。
#
# 用法: powershell -File tests/check_repo_eol.ps1 [-Quiet] [-RepoPath <path>]
#   （-RepoPath 供自测/维护注入别的仓库；默认根由脚本位置推导，不硬编码 —— 2026-10-22 在册教训）
# 退出码: 0=无 CRLF/混合入库 / 1=有 / 2=UNVERIFIED
param(
  [switch]$Quiet,
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red
  exit 2
}

# 用显式进程调用（2026-10-24 在册坑：& git ... 2>&1 会把 stderr 并进输出流，
# $LASTEXITCODE 变空、stdout/stderr 皆空 —— 静默失败最难查，正是 check_ps1_eol 首版的病）
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$psi.Arguments = '-C "' + $root + '" ls-files --eol -z'
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$proc = [System.Diagnostics.Process]::Start($psi)
$so = $proc.StandardOutput.ReadToEnd()
$se = $proc.StandardError.ReadToEnd()
$proc.WaitForExit()
if ($proc.ExitCode -ne 0) {
  Write-Host ('UNVERIFIED: git ls-files --eol 失败 rc=' + $proc.ExitCode + ' ' + $se.Trim()) -ForegroundColor Red
  exit 2
}

$records = @($so -split [char]0 | Where-Object { $_.Trim() })
if ($records.Count -eq 0) {
  Write-Host 'UNVERIFIED: 未取到任何受跟踪文件（可能不在 git 仓库内）' -ForegroundColor Red
  exit 2
}

$bad = @()
$kinds = @{}
foreach ($rec in $records) {
  $tab = $rec.IndexOf([char]9)
  if ($tab -lt 1) { continue }
  $meta = $rec.Substring(0, $tab).Trim()
  $path = $rec.Substring($tab + 1).Trim()
  if (-not $path) { continue }
  $cols = @($meta -split '\s+' | Where-Object { $_ })
  $first = $cols[0]                       # 形如 i/lf / i/crlf / i/mixed / i/none / i/-text
  $kinds[$first] = 1 + [int]$kinds[$first]
  if ($first -eq 'i/crlf' -or $first -eq 'i/mixed') {
    $w = $cols[1]
    $bad += ($path + '  ' + $first + '（' + $w + '，规则后补也不会自动纠正，需 git add --renormalize）')
  }
}

if ($bad.Count -gt 0) {
  Write-Host ('入库 EOL 门禁未通过（' + $bad.Count + ' 个文件以 CRLF/混合换行在库，共扫 ' + $records.Count + ' 个受跟踪文件）：') -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object)) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  危害：每次改动都会被算成全行变更（实测 8 行新增被放大成 177 增 / 169 删），评审看不出真实改动。' -ForegroundColor Yellow
  Write-Host '  修复: git add --renormalize <上述文件> 后提交（一次性把库里换行归一）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  $ksum = @()
  foreach ($k in ($kinds.Keys | Sort-Object)) { $ksum += ($k + '=' + $kinds[$k]) }
  Write-Host ('入库 EOL 合规：' + $records.Count + ' 个受跟踪文件无 CRLF/混合入库（' + ($ksum -join ' ') + '）') -ForegroundColor Green
}
exit 0

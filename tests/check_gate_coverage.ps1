# check_gate_coverage.ps1 -- 每个门禁脚本都必须挂在某个**可执行受理面**上（2026-10-24 round 50 新增）
#
# 为什么需要（本仓在册失效形态，第 N 次）：
#   这个仓库反复出现「判据写好了，但没有任何自动化会跑它」——
#   round 22 之前 tests/ 有 23 个门禁、CI 只跑 4 个；round 47 更隐蔽：内存门禁
#   挂在 check_all 上却因判据错误恒不产出读数，静默失效 6 小时。
#   本件把「至少挂一个受理面」变成机器可检查的不变量，防的是**下一个**忘挂的门禁。
#
# 受理面（会被执行的地方，注释里的提及不算）：
#   .github/workflows/ci.yml   —— repo-gates 清单 + build job 里的直接调用
#   tests/check_all.ps1        —— 本地全量运行器
#   .githooks/pre-commit       —— 提交前闸
#   setup/build.ps1            —— 构建链内联校验
#
# 口径（刻意宽松，避免制造噪声 => 门禁被删，本仓在册教训）：
#   只要求**至少一个**受理面引用，**不要求必须上 CI** —— 本仓刻意分层是合理的：
#     * resident_longrun / readings_trend 依赖本机活实例与本地读数，CI 上只能 UNVERIFIED；
#     * check_staged_gofmt 属提交前闸；hooks_enabled 在 CI 干净检出里没有 .git 配置。
#   引用匹配用**带路径限定 + 排除注释行**（round 50 实测：宽正则会把「文档里提到」
#   当成已挂载，反向把孤儿看成非孤儿；只认 tests/x.ps1 或 ./tests/x.ps1 这类调用形态）。
#
# 自指要求：本件自己也必须挂在受理面上（CI repo-gates）。写它的时候若还没挂，
#   它会如实把自己的缺口报出来 —— 这是设计的一部分，不是 bug（先暴露再补挂，
#   比「写了个检查别人、自己却没人跑」诚实）。
#
# 用法: powershell -File tests/check_gate_coverage.ps1 [-Quiet]
# 退出码: 0=全部有受理面 / 1=有孤儿 / 2=UNVERIFIED（取不到受跟踪清单）
param(
  [switch]$Quiet,
  # 供自测/维护注入别的仓库（默认根由脚本位置推导，不硬编码 —— 2026-10-22 在册教训）
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red; exit 2 }

function Invoke-Git([string[]]$gitArgs) {
  # 显式进程调用：& git ... 2>&1 会把 stderr 并进输出流、$LASTEXITCODE 变空（在册坑）
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = $gitExe
  $q = [string][char]34
  $bs = [string][char]92
  $psi.Arguments = (($gitArgs | ForEach-Object { if (($_.Contains($bs)) -or ($_.Contains(' '))) { $q + $_ + $q } else { $_ } }) -join ' ')
  $psi.WorkingDirectory = $root
  $psi.UseShellExecute = $false
  $psi.CreateNoWindow = $true
  $psi.RedirectStandardOutput = $true
  # 关键：显式按 UTF-8 解码。默认码页（本机 GBK / CI 的 437）会把
  # cat-file 输出里的多字节序列成对吞字节，**连带吃掉相邻的 ASCII**——实测 8 个
  # 明明挂在 check_all 上的门禁被解成"没有该引用"，凭空造出 8 个假孤儿。
  # （round 45 栽在 stdin 方向的 BOM，这次是 stdout 方向的码页——同一个洞的两面。）
  $psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
  $psi.RedirectStandardError = $true
  $proc = [System.Diagnostics.Process]::Start($psi)
  $so = $proc.StandardOutput.ReadToEnd()
  $se = $proc.StandardError.ReadToEnd()
  $proc.WaitForExit()
  return @{ rc = $proc.ExitCode; out = $so; err = $se }
}

# 取 HEAD 的清单与内容（判 HEAD 不判工作区：工作区可能有并行会话的在途文件，
#   它们还没挂受理面是正常的，不该由本件判红 —— 与 check_repo_eol/check_gofmt 同口径）
$ls = Invoke-Git @('ls-files', '-z', 'tests/check_*.ps1')
if ($ls.rc -ne 0) { Write-Host ('UNVERIFIED: ls-files 失败: ' + $ls.err.Trim()) -ForegroundColor Red; exit 2 }
$gates = @($ls.out -split [char]0 | ForEach-Object { $_.Trim() } | Where-Object { $_ })
# check_all 是运行器本体、不是被判覆盖的判据：它挂在别人的受理面上没有意义
#   （变异测试时暴露：临时仓库里它被当孤儿点名，等于逼后来人把运行器塞进它自己）。$ls 只匹配
#   tests/check_*.ps1，运行器恰好也符合该模式，故在此显式排除。
if ($gates.Count -eq 0) { Write-Host 'UNVERIFIED: 没有受跟踪的门禁脚本（异常）' -ForegroundColor Red; exit 2 }

$surfaces = @('.github/workflows/ci.yml', 'tests/check_all.ps1', '.githooks/pre-commit', 'setup/build.ps1')
$wired = @{}
$surfaceTexts = @{}
# 受理面内容优先读**索引**（:path）、回退 HEAD：新门禁与它的挂载通常在同一个提交里落地，
#   只读 HEAD 会出现「暂存后、提交前那一刻，新门禁把自己报成孤儿」的先有鸡还是先有蛋
#   窗口 —— 那会惩罚本件最想鼓励的动作（补门禁 + 同提交挂上受理面）。CI 干净检出里索引==HEAD，
#   两侧语义一致（round 33 教训「要同时覆盖受跟踪与暂存」的受理面版）。
foreach ($s in $surfaces) {
  $r = Invoke-Git @('cat-file', 'blob', (':' + $s))
  if ($r.rc -ne 0) { $r = Invoke-Git @('cat-file', 'blob', ('HEAD:' + $s)) }
  if ($r.rc -ne 0) { $surfaceTexts[$s] = ''; continue }
  $surfaceTexts[$s] = $r.out
  foreach ($line in ($r.out -split [char]10)) {
    $t = $line.Trim()
    if ($t.StartsWith('#')) { continue }        # 注释里的提及不算挂载
    foreach ($m in [regex]::Matches($t, '[\\/](check_[a-z0-9_]+\.ps1)')) { $wired[$m.Groups[1].Value] = $true }
  }
}

$orphans = @()
foreach ($gp in $gates) {
  $name = ($gp -split '[\\/]')[-1]
  if ($name -eq 'check_all.ps1') { continue }
  if (-not $wired.ContainsKey($name)) { $orphans += $name }
}

if ($orphans.Count -gt 0) {
  Write-Host ('受理面覆盖门禁未通过（' + $orphans.Count + ' 个门禁脚本没有任何受理面会执行它，共 ' + $gates.Count + ' 个）：') -ForegroundColor Red
  foreach ($o in ($orphans | Sort-Object)) { Write-Host ('  - ' + $o) -ForegroundColor Red }
  Write-Host '  危害：判据存在但没人跑 = 以为有护栏，实际是空气（本仓 round 22/47 的真实失效形态）。' -ForegroundColor Yellow
  Write-Host ('  修法：加进 ' + ($surfaces -join ' / ') + ' 之一（按性质选：CI 兜底 / 本地全量 / 提交前 / 构建链）。') -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  $surfaceCount = @($surfaces | Where-Object { $surfaceTexts[$_] }).Count
  Write-Host ('受理面覆盖合规：' + $gates.Count + ' 个门禁脚本全部至少挂一个受理面（检查了 ' + $surfaceCount + '/' + $surfaces.Count + ' 个受理面，内容按索引优先、回退 HEAD）') -ForegroundColor Green
}
exit 0

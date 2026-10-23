# check_artifact_soak_fresh.ps1 -- 产物不得比「影响 soak/A7 结论的源码」旧（2026-10-24 round 52 新增）
#
# 为什么需要（round 52 实撞现场）：
#   round 39 修了 A7 趋势判据（跨进程重启的 metrics.csv 会把 96h 误报成 heap +174% leak），
#   提交时间 2026-10-23 12:14。而 dist 下产物的 --version 报构建时间 2026-10-22 23:41
#   —— **修复已在源码、不在产物**。后果不是抽象的：
#     * tests/e2e_soak.ps1 默认用 dist 下的 workbench.exe（其第 18 行）
#     * tests/keepalive.ps1 拉起的也是这个产物
#   任何人跑 --soak 拿到的是**修复前的假 leak**。本轮实测同一份 CSV 得出两个结论：
#     旧产物     verdict=leak  spanHours=99.73
#     当前源码   verdict=ok    spanHours=14.58
#   修好了判据却没让它生效，比没修更糟 —— 它给「已经修了」的错觉。
#
# 判据（刻意极窄，只管「会让结论失效」的那一类陈旧）：
#   取 --version 里的构建时间，与**影响 soak/A7 判定的源码**在 HEAD 的最新提交时间比较；
#   源码更新 => 判红（产物里的判定逻辑是旧的，据它得出的 leak/ok 不可信）。
#   不比较其它源码：「产物总落后于 README/无关包」是常态，做成硬闸会天天红 =>
#   噪声 => 门禁被删（本仓在册教训）。清单只 4 个文件，多加一个都要想清楚。
#
# 三态：
#   rc=0 PASS       —— 产物构建时间 >= 相关源码最新提交时间（判定逻辑已在产物里）
#   rc=1 FAIL       —— 相关源码比产物新（据产物得出的 soak/A7 结论无效，需重建产物）
#   rc=2 UNVERIFIED —— 产物不存在（CI 未构建/新机器）/ --version 取不到输出（GUI 子系统
#                       在无控制台环境下 stdout 为空，已知行为）/ 时间解析失败
#                       —— **绝不把没测成当合格**
#
# 实现注记：本文件由 write 通道直写（不再用 JS 拼字符串数组）—— JS 模板会把 d 类
#   转义吃掉、把 PowerShell 反引号当模板语法，round 38/45/48/50 连踩四次。
#   写完仍必须 Parser::ParseFile 验语法 + 实跑，不能信「写成功」的日志。
#
# 用法: powershell -File tests/check_artifact_soak_fresh.ps1 [-Exe path] [-Quiet]
param(
  [string]$Exe = '',
  [switch]$Quiet,
  # 自测口：注入别的源码清单来验证 PASS/FAIL 两个方向都能走通
  #   （不注入时固定用下面那 4 个文件；本仓在册要求「判据必须双向变异验证」）。
  [string[]]$PathsOverride = @()
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path $root 'dist/workbench.exe' }

function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }
function Unverified([string]$why) {
  Say ('UNVERIFIED: ' + $why + '（没测成 != 合格）')
  exit 2
}

# 只列**会改变 soak/A7 结论**的文件（趋势算法 + 采样写出 + 判定出口），不多不少。
$paths = @(
  'workbench/internal/supervisor/trend.go',
  'workbench/internal/supervisor/supervisor.go',
  'workbench/cmd/workbench/soak.go',
  'workbench/cmd/workbench/acceptance.go'
)

if ($PathsOverride.Count -gt 0) { $paths = @($PathsOverride) }

if (-not (Test-Path -LiteralPath $Exe)) {
  Unverified ('产物不存在: ' + $Exe + '（未构建/新机器；本件属本地判据，CI 的 build job 之前没有产物是正常的）')
}

# 1) 产物构建时间：--version 形如 workbench 1.0.0 (2026-10-22T23:41:11.1411030+08:00)
$verOut = ''
try { $verOut = (& $Exe --version 2>&1 | Out-String).Trim() } catch { $verOut = '' }
if (-not $verOut) { Unverified '--version 无输出（GUI 子系统在无控制台环境下取不到 stdout，已知行为）' }
$isoPat = '[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}'
$tm = [regex]::Match($verOut, $isoPat)
if (-not $tm.Success) {
  Unverified ('--version 输出里解析不到构建时间: ' + $verOut.Substring(0, [Math]::Min(90, $verOut.Length)))
}
$builtAt = [datetime]::Parse($tm.Value)

# 2) 相关源码在 HEAD 的最新提交时间（用 unix 秒比较，避开时区歧义）
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Unverified '找不到 git 可执行文件' }

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$q = [string][char]34
$psi.Arguments = '-C ' + $q + $root + $q + ' log -1 --format=%ct -- ' + ($paths -join ' ')
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
# 显式 UTF-8（check_gate_coverage 的在册教训：默认码页在 CI 是 437）
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
$proc = [System.Diagnostics.Process]::Start($psi)
$so = $proc.StandardOutput.ReadToEnd().Trim()
$se = $proc.StandardError.ReadToEnd()
$proc.WaitForExit()
if ($proc.ExitCode -ne 0) { Unverified ('git log 失败: ' + $se.Trim()) }
if (-not $so) { Unverified '相关源码在 HEAD 没有提交记录（异常）' }
$srcAt = [DateTimeOffset]::FromUnixTimeSeconds([long]$so).LocalDateTime

Say ('  产物构建: ' + $builtAt.ToString('yyyy-MM-dd HH:mm') + ' | soak/A7 判定源码最新提交: ' + $srcAt.ToString('yyyy-MM-dd HH:mm'))

if ($srcAt -gt $builtAt) {
  $lagH = [math]::Round(($srcAt - $builtAt).TotalHours, 1)
  Say ('CHECK artifactSoakFresh result=FAIL lag=' + $lagH + 'h')
  Write-Host ('  产物比 soak/A7 判定源码旧 ' + $lagH + ' 小时 —— 产物里的趋势判据是旧版，据它跑出的 --soak / A7 结论无效。') -ForegroundColor Red
  Write-Host '  受影响面：tests/e2e_soak.ps1 与 tests/keepalive.ps1 都默认使用这个产物。' -ForegroundColor Yellow
  Write-Host '  修法：重跑 setup/build.ps1 产出新产物（不要靠改判据把结论圆回去）。' -ForegroundColor Yellow
  exit 1
}
Say 'CHECK artifactSoakFresh result=PASS'
if (-not $Quiet) { Write-Host '  产物已包含当前的 soak/A7 判定逻辑' -ForegroundColor Green }
exit 0

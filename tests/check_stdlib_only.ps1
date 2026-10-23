# check_stdlib_only.ps1 -- 守「纯标准库 / 零第三方依赖」这条硬约束（2026-10-24 round 57 新增）
#
# 为什么需要：
#   本仓的设计前提就是「Go 1.27 纯标准库 + 单 Windows EXE」，docs 里多处明文承诺：
#     * docs/benchmark/WorkBench-开源对标报告.md:3「Go 1.27 纯标准库，Windows 单 EXE」
#     * docs/架构与运维.md:11「workbench.exe（单进程，7.7MB，纯 Go 标准库）」
#     * docs/benchmark/peer源码精读笔记.md:132「照抄外部存储与分布式协调会把『零依赖』这条
#       硬约束打掉 —— 这类差异记为取舍，不记为差距」
#   但**没有任何门禁守着它**：round 57 实测 32 个受跟踪门禁里零个检查 go.mod/go.sum/vendor/
#   外部 import。约束写在文档里、靠人记得 —— 这正是本仓反复记录的失效形态（判据没挂受理面
#   的变体：这里连判据都没有）。加一条第三方 require 会静默改变项目形态（不再单 EXE、
#   引入供应链与版本管理面），且 CI 只会在构建时以「找不到包」形式暴露，归因不清。
#
# 判据（四条，全部为可证伪的客观事实）：
#   1) workbench/go.mod 的 HEAD 内容里不得出现**非空** require 块/行。
#   2) HEAD 不得受跟踪 workbench/go.sum。
#   3) HEAD 不得受跟踪 workbench/vendor/ 下任何文件。
#   4) workbench/（Go 模块）下的受跟踪 .go 不得出现**外部模块路径**的 import
#      （判据：import 行的路径首段含「.」，如 github.com/x/y；标准库首段永不含点，
#        本模块内部是 workbench/... 也不含点）。范围限定模块 = docs/benchmark/evidence/ 下的
#        对标取证摘录（非编译产物）不算违规，见实现段注记。
#   四条任一违反 => rc=1。刻意只看这四条：够窄、够客观，不掺「这个依赖合不合理」的语义判断。
#
# 判 HEAD 而非工作区（与 check_repo_eol / check_multi_bom 同口径）：并行会话的在途文件
#   不该由本件判红；CI 看的也是 HEAD。CI 侧同一条判据即可拦住入库。
# 已知边界（round 58 实测记录）：git 会从 WorkingDirectory **向上找父目录**的 .git —— 用 -RepoPath
#   注入一个「本身不是仓库、但位于某仓库之内」的目录时，判的其实是外层仓库的 HEAD。
#   默认路径（$PSScriptRoot 的父目录）不存在这个问题；注入口是自测用的，如实标注而非静默。
#
# 用法: powershell -File tests/check_stdlib_only.ps1 [-Quiet] [-RepoPath <path>]
# 退出码: 0=合规 / 1=检出第三方依赖 / 2=UNVERIFIED（取不到 git 或清单为空）
param(
  [switch]$Quiet,
  # 自测/维护用：注入别的仓库（默认根由脚本位置推导，不硬编码 —— 2026-10-22 在册教训）
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red; exit 2 }
if (-not (Test-Path -LiteralPath $root)) { Write-Host ('UNVERIFIED: 目录不存在: ' + $root) -ForegroundColor Yellow; exit 2 }

function Invoke-Git([string[]]$gitArgs) {
  # 显式进程调用 + 显式 UTF-8（& git ... 2>&1 会并流且丢 $LASTEXITCODE；默认码页在 CI 是 437）
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = $gitExe
  $q = [string][char]34
  $bs = [string][char]92
  $psi.Arguments = (($gitArgs | ForEach-Object { if (($_.Contains($bs)) -or ($_.Contains(' '))) { $q + $_ + $q } else { $_ } }) -join ' ')
  $psi.WorkingDirectory = $root
  $psi.UseShellExecute = $false
  $psi.CreateNoWindow = $true
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
  $proc = [System.Diagnostics.Process]::Start($psi)
  $so = $proc.StandardOutput.ReadToEnd()
  $se = $proc.StandardError.ReadToEnd()
  $proc.WaitForExit()
  return @{ rc = $proc.ExitCode; out = $so; err = $se }
}

$bad = @()

# --- 1) go.mod 的 require ---
$modPath = 'workbench/go.mod'
$modRes = Invoke-Git @('cat-file', 'blob', ('HEAD:' + $modPath))
if ($modRes.rc -ne 0) {
  Write-Host ('UNVERIFIED: 取不到 HEAD:' + $modPath + ' —— 单模块布局变了？判据要跟着更新') -ForegroundColor Red
  exit 2
}
$inReq = $false
foreach ($ln in ($modRes.out -split [char]10)) {
  $t = $ln.Trim()
  if ($t.StartsWith('require (')) { $inReq = $true; $bad += ($modPath + ' 出现 require 块 —— 本仓设计前提是零第三方依赖'); continue }
  if ($inReq -and $t -eq ')') { $inReq = $false; continue }
  if ($inReq -and $t -ne '') { $bad += ($modPath + ' require ' + $t) }
  elseif ($t.StartsWith('require ') -and $t -ne 'require (') {
    $rest = $t.Substring(7).Trim()
    if ($rest -ne '') { $bad += ($modPath + ' ' + $t) }
  }
}

# --- 2) go.sum ---
$sumRes = Invoke-Git @('ls-files', '--', 'workbench/go.sum')
if ($sumRes.rc -eq 0 -and $sumRes.out.Trim() -ne '') {
  $bad += 'HEAD 受跟踪 workbench/go.sum —— 有依赖校验和即有第三方依赖'
}

# --- 3) vendor/ ---
$venRes = Invoke-Git @('ls-files', '--', 'workbench/vendor')
if ($venRes.rc -eq 0 -and $venRes.out.Trim() -ne '') {
  $n = @($venRes.out.Trim() -split [char]10).Count
  $bad += ('HEAD 受跟踪 workbench/vendor/ 下 ' + $n + ' 个文件 —— 引入 vendored 依赖')
}

# --- 3.5) 模式自检（round 58）：判据 4 的正则必须证明它认得出各种 import 形态 ---
#   判据 4 的全部价值在那条正则上，而它**静默失效过一次**（round 57：正则穿四层转义，
#   \. 被中间层吃掉 => 外部 import 判 PASS）。光跑真仓库抓不到漏报（真仓库本就合规）。
#   所以每次运行都用 git grep 自己的引擎（POSIX ERE，**不是 .NET regex** —— 两者对
#   [[:space:]] 语义不同；round 58 用 .NET 测会把 11 种形态全判不匹配，那是测法错误）
#   跑一遍已知答案的形态矩阵，任一行不符 => UNVERIFIED：连自己模式都证不了的判据没资格宣称合规。
#   两个实测踩过的坑（都写进来防再犯）：
#   ① 临时文件必须写到 TEMP 并 Push-Location 过去用**相对路径**跑 git grep。
#      否则 cwd 在仓库内、目标在 TEMP（仓库外），即便加了 --no-index，git 仍 fatal
#      「outside repository」=> 输出为空 => 自检把所有应命中判成不命中。
#   ② 形态矩阵用**两个平行数组**（want / lines），不能用 @('Y', a + b) 这种嵌套：
#      PowerShell 里**逗号优先级高于 +**，@('Y', $tab + 'x') 会被解析成 @('Y', $tab) + 'x'
#      压平成 3 元素数组（round 58 实测），取 [1] 只剩一个制表符、[2] 才是字符串。
$patS = '^[[:space:]]*([A-Za-z_.][A-Za-z0-9_]*[[:space:]]+)?' + [string][char]34 + '[a-z0-9.-]+[.][a-z]+/'
$tab = [string][char]9
$dq = [string][char]34
$want = @('Y', 'Y', 'Y', 'N', 'N', 'N', 'N', 'N', 'N', 'Y', 'Y')
$lines = @(
  ($tab + $dq + 'github.com/pkg/errors' + $dq),
  ($tab + 'errorsx ' + $dq + 'github.com/x/y' + $dq),
  ('import ' + $dq + 'github.com/x/y' + $dq),
  ('import stdlibx ' + $dq + 'strings' + $dq),
  ($tab + $dq + 'slices' + $dq),
  ($tab + $dq + 'workbench/internal/x' + $dq),
  ('url = ' + $dq + 'github.com/foo/bar' + $dq),
  ('// see github.com/foo/bar'),
  ('repoURL: ' + $dq + 'github.com/foo/bar' + $dq + ','),
  ($tab + 'gjson ' + $dq + 'github.com/tidwall/gjson' + $dq),
  ($tab + '_ ' + $dq + 'github.com/x/y' + $dq)
)
$selfFail = ''
if ($want.Count -ne $lines.Count) { $selfFail = '自检数组长度不齐' }
$patT = Join-Path ([IO.Path]::GetTempPath()) ('stl-pat-' + [Guid]::NewGuid().ToString('N').Substring(0, 8) + '.pat')
$txtT = Join-Path ([IO.Path]::GetTempPath()) ('stl-txt-' + [Guid]::NewGuid().ToString('N').Substring(0, 8) + '.txt')
[IO.File]::WriteAllText($patT, $patS + [string][char]10, (New-Object System.Text.UTF8Encoding($false)))
[IO.File]::WriteAllText($txtT, (($lines) -join [string][char]10) + [string][char]10, (New-Object System.Text.UTF8Encoding($false)))
# 在 TEMP 里用相对文件名跑（见坑①）
$oldLoc = Get-Location
try {
  Set-Location -LiteralPath ([IO.Path]::GetTempPath())
  $ptN = [IO.Path]::GetFileName($patT)
  $txN = [IO.Path]::GetFileName($txtT)
  $gpsi = New-Object System.Diagnostics.ProcessStartInfo
  $gpsi.FileName = $gitExe
  $gpsi.WorkingDirectory = [IO.Path]::GetTempPath()
  $gq = [string][char]34
  $gpsi.Arguments = 'grep -n -h --no-index -E -f ' + $gq + $ptN + $gq + ' -- ' + $gq + $txN + $gq
  $gpsi.UseShellExecute = $false
  $gpsi.CreateNoWindow = $true
  $gpsi.RedirectStandardOutput = $true
  $gpsi.RedirectStandardError = $true
  $gpsi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
  $gp = [System.Diagnostics.Process]::Start($gpsi)
  $gout = $gp.StandardOutput.ReadToEnd()
  $gerr = $gp.StandardError.ReadToEnd()
  $gp.WaitForExit()
} finally {
  Set-Location -LiteralPath $oldLoc
  try { Remove-Item -LiteralPath $patT -Force -ErrorAction SilentlyContinue } catch { }
  try { Remove-Item -LiteralPath $txtT -Force -ErrorAction SilentlyContinue } catch { }
}
if ($gp.ExitCode -eq 2) { $selfFail = 'git grep 自检失败 (rc=2): ' + $gerr.Trim() }
elseif ($selfFail -eq '') {
  $hitSet = @{}
  foreach ($h in ($gout -split [char]10)) {
    $h2 = $h.Trim()
    if (-not $h2) { continue }
    # -h 抑制文件名前缀，输出即 `行号:内容`；只按第一个冒号切（内容也可能含冒号）
    $ci = $h2.IndexOf([char]58)
    if ($ci -gt 0) {
      $lnTxt = $h2.Substring(0, $ci)
      if ($lnTxt -match '^[0-9]+$') { $hitSet[[string]([int]$lnTxt)] = $true }
    }
  }
  for ($fi = 0; $fi -lt $want.Count; $fi++) {
    $ln = $fi + 1
    $wantY = ($want[$fi] -eq 'Y')
    $gotY = $hitSet.ContainsKey([string]$ln)
    if ($wantY -ne $gotY) {
      $selfFail = ('第 ' + $ln + ' 行形态期望 ' + $(if ($wantY) { '命中' } else { '不命中' }) + '，实得 ' + $(if ($gotY) { '命中' } else { '不命中' }))
      break
    }
  }
}
if ($selfFail -ne '') {
  Write-Host ('UNVERIFIED: import 正则自检未通过 —— ' + $selfFail + '（判据 4 的模式不可信，拒绝按它宣称合规）') -ForegroundColor Red
  exit 2
}

# --- 4) 外部 import（git grep -f 模式文件；范围限定 workbench/ = Go 模块根）---
# 正则：可选别名 + 双引号路径，且路径首段含点（github.com/... golang.org/x/...）。
# 标准库首段永不含点；本模块是 workbench/... 也不含点 —— 判据即「模块路径 import」的形态。
# 为什么用模式文件而不是命令行内嵌正则：这条正则需要同时含双引号与反斜杠/点，要穿过
# 「JS 字符串 → PS 单引号串 → 我的 Invoke-Git 给含空格/反斜杠的参数加双引号 → git 自己的
# argv 解析」**四层**转义 —— 实测第 99 行的 `\.` 会被中间某层吃掉，门禁静默漏报（round 57
# 变异测试抓到：外部 import 变体判 PASS）。模式文件走 UTF8 字节直写，只穿一层路径。
# 为什么范围限定 workbench/：docs/benchmark/evidence/*.go 是 asynq/river 的**对标取证摘录**
# （不在 Go 模块里、不参与编译），全仓扫会把 47 行外部 import 误判违规 —— 判据过宽制造噪声
# 正是本仓在册的「模糊语义检查 => 门禁被删」形态。威胁面是模块，判据范围就等模块。
$patFile = Join-Path ([IO.Path]::GetTempPath()) ('stdlibimp-' + [Guid]::NewGuid().ToString('N').Substring(0, 8) + '.pat')
[IO.File]::WriteAllText($patFile, $patS, (New-Object System.Text.UTF8Encoding($false)))
$impRes = Invoke-Git @('grep', '-n', '-I', '-E', '-f', $patFile, 'HEAD', '--', 'workbench')
try { Remove-Item -LiteralPath $patFile -Force -ErrorAction SilentlyContinue } catch { }
if ($impRes.rc -eq 0 -and $impRes.out.Trim() -ne '') {
  foreach ($ln in ($impRes.out.Trim() -split [char]10)) {
    if ($ln.Trim() -eq '') { continue }
    $bad += ('外部 import: ' + $ln.Trim())
  }
}
elseif ($impRes.rc -ne 0 -and $impRes.rc -ne 1) {
  Write-Host ('UNVERIFIED: git grep 失败 (rc=' + $impRes.rc + '): ' + $impRes.err.Trim()) -ForegroundColor Red
  exit 2
}

if ($bad.Count -gt 0) {
  Write-Host ('stdlib-only 门禁未通过：检出 ' + $bad.Count + ' 处第三方依赖迹象') -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object -Unique)) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  这条约束是设计前提（纯标准库 + 单 Windows EXE），docs 多处明文承诺；' -ForegroundColor Yellow
  Write-Host '  若确实要引入依赖，应先改设计前提与文档，而不是让门禁静默放过。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host 'stdlib-only 合规：go.mod 无 require、无 go.sum、无 vendor、受跟踪 .go 无外部 import' -ForegroundColor Green
}
exit 0

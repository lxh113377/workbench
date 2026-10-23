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
$patS = '^[[:space:]]*([A-Za-z_.][A-Za-z0-9_]*[[:space:]]+)?' + [string][char]34 + '[a-z0-9.-]+[.][a-z]+/'
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

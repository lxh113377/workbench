<#
  check_no_shrink.ps1 — 合并前门禁：禁止「整文件覆盖」把并发会话的代码改没。

  为什么需要（2026-10-19 现场，同一错误犯了三次）：
    这个工作区有 4+ 个 DSH 会话在同时改同一棵树。我用 Copy-Item 整文件覆盖
    cmd/workbench/main.go 时，把另一会话刚做的 ProbeRunning->ProbeExactPort 重构
    覆盖掉了，编译立刻 undefined: single.ProbeRunning。
    前两次同类事故也是整文件覆盖造成。

  判据：把「来源文件」与「目标文件」的行集合做对比 ——
    目标里**独有的行**（target-only）如果有实质内容，说明目标承载了来源没有的改动，
    整文件覆盖就会丢掉它们 => 拒绝，必须改用 edit 做增量合并。

  用法：
    powershell -NoProfile -File tests/check_no_shrink.ps1 -Source <我的文件> -Target <live 文件>
  退出码：0 = 覆盖安全 / 1 = 目标有来源没有的实质行（禁止覆盖）/ 2 = 参数错误
#>
param(
  [Parameter(Mandatory=$true)][string]$Source,
  [Parameter(Mandatory=$true)][string]$Target,
  # 允许忽略的纯装饰性差异（空行、纯注释/括号），默认不忽略以便最保守
  [switch]$IgnoreComments
)
$ErrorActionPreference = 'Stop'
if (-not (Test-Path $Source)) { Write-Error ("source not found: " + $Source); exit 2 }
if (-not (Test-Path $Target)) { Write-Host '  target 不存在 => 新建，覆盖安全'; exit 0 }

function NormLines([string]$path) {
  $out = New-Object 'System.Collections.Generic.List[string]'
  foreach ($l in (Get-Content $path -ErrorAction SilentlyContinue)) {
    $n = (($l -replace "`t", ' ') -replace ' +', ' ').Trim()
    if ($n -eq '') { continue }
    if ($IgnoreComments -and ($n.StartsWith('//') -or $n.StartsWith('#'))) { continue }
    $out.Add($n)
  }
  return ,$out
}

$src = NormLines $Source
$tgt = NormLines $Target
if ($tgt.Count -eq 0) { Write-Host '  target 为空 => 覆盖安全'; exit 0 }

$setSrc = New-Object 'System.Collections.Generic.HashSet[string]'
foreach ($x in $src) { [void]$setSrc.Add($x) }
$setTgt = New-Object 'System.Collections.Generic.HashSet[string]'
foreach ($x in $tgt) { [void]$setTgt.Add($x) }

$targetOnly = @($setTgt | Where-Object { -not $setSrc.Contains($_) })
$srcOnly = @($setSrc | Where-Object { -not $setTgt.Contains($_) })

Write-Host ("  source 行=" + $src.Count + "  target 行=" + $tgt.Count) -ForegroundColor Gray
Write-Host ("  target-only（会被覆盖丢掉）=" + $targetOnly.Count + "   source-only（新增）=" + $srcOnly.Count) -ForegroundColor Gray

if ($targetOnly.Count -eq 0) {
  Write-Host '  PASS: 覆盖安全（目标没有来源缺失的独有行）' -ForegroundColor Green
  exit 0
}

# 关键安全信号：目标行数比来源多，且差额都体现在 target-only => 极可能覆盖丢东西
Write-Host ('  FAIL: 目标有 ' + $targetOnly.Count + ' 行是来源没有的 —— 整文件覆盖会丢掉它们') -ForegroundColor Red
Write-Host '    正确的做法是用 edit 做**增量**合并，而不是 Copy-Item 整文件覆盖。' -ForegroundColor Yellow
Write-Host '    以下是最多 15 行「会丢的内容」：' -ForegroundColor Yellow
$targetOnly | Select-Object -First 15 | ForEach-Object { Write-Host ('      ' + $_.Substring(0, [Math]::Min(110, $_.Length))) -ForegroundColor DarkYellow }
if ($targetOnly.Count -gt 15) { Write-Host ('      ... 另有 ' + ($targetOnly.Count - 15) + ' 行') -ForegroundColor DarkYellow }
exit 1
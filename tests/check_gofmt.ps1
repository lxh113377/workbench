# check_gofmt.ps1 -- Go 代码格式门禁（LF 归一化后判，防 CRLF 假红淹没）
#
# 为什么需要（2026-10-22 实测）：本仓长期「CI 无 gofmt 门禁」，直接原因是
# 工作副本在 core.autocrlf=true 下全是 CRLF，而 gofmt 只接受 LF —— 直接跑
# `gofmt -l .` 会把 73 个文件全判红（干净 clone 上 286 个，整文件重写）。
# 实测反证：chain.go 归一化成 LF 后 gofmt -l 立刻为 0，而 config.go / main.go /
# html.go 归一化后**仍有**真漂移 —— 两类混在一起，谁都不敢上闸。
#
# 本脚本的口径：把每个受跟踪 .go 读到内存、CRLF->LF 归一化、写到临时文件，
# 再对该临时文件跑 gofmt -l。= 只判「代码格式」，不判「行尾」；行尾由
# .gitattributes（*.go text eol=lf）负责，两者职责分开，互不掩盖。
#
# 判据（任一不满足即 rc=1）：
#   1) 受跟踪的 .go 文件，LF 归一化后 gofmt 仍判为需要格式化的，逐个点名。
#   2) 输出保留真实相对路径（临时文件只是判据载体），便于直接 gofmt -w 修。
#
# 用法: powershell -File tests\check_gofmt.ps1 [-Root <repoRoot>] [-Quiet]
# 退出码: 0=全部合规 / 1=有漂移 / 2=环境不对（不在 git 仓库或 go 不可用）
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }
if (-not (Test-Path -LiteralPath $Root)) { Write-Error ("根目录不存在: " + $Root); exit 2 }
if (-not (Get-Command gofmt -ErrorAction SilentlyContinue)) { Write-Error "gofmt 不可用（需 Go 工具链）"; exit 2 }

$tracked = @(& git -C $Root ls-files "*.go" 2>$null)
if ($LASTEXITCODE -ne 0) { Write-Error "git ls-files 失败（不在 git 仓库内？）"; exit 2 }
$tracked = @($tracked | ForEach-Object { $_.Trim('"') })

$tmpdir = Join-Path ([IO.Path]::GetTempPath()) ("gofmtgate-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $tmpdir | Out-Null
# 显式债务豁免（不是静默跳过）：tests/gofmt-debt.txt 每行一个路径，# 开头为注释。
# 为什么需要：并发会话在途件不能代改（gofmt 会重排其注释结构 = 代写他人代码），
# 但门禁又不能因此永远假红。折中 = 债务**显式登记 + 每次运行都打印 + 计数可见**，
# 收尾时清空文件即恢复零豁免。判据语义：豁免文件仍会被点名，只是不计入 rc。
$debtPath = Join-Path $Root 'tests\gofmt-debt.txt'
$debt = @{}
if (Test-Path -LiteralPath $debtPath) {
  foreach ($l in ([IO.File]::ReadAllLines($debtPath))) {
    $t = $l.Trim()
    if ($t -eq '' -or $t.StartsWith('#')) { continue }
    $debt[$t.Replace([char]92, [char]47)] = $true
  }
}
$drift = @()
$exempt = @()
try {
  foreach ($rel in $tracked) {
    $abs = Join-Path $Root $rel
    if (-not (Test-Path -LiteralPath $abs)) { continue }
    $lf = ([IO.File]::ReadAllText($abs)) -replace "`r`n", "`n"
    $tmp = Join-Path $tmpdir (($rel -replace '[\\/]', '__'))
    [IO.File]::WriteAllText($tmp, $lf)
    $out = & gofmt -l $tmp 2>$null
    if ($LASTEXITCODE -ne 0) { Write-Error ("gofmt 执行失败于 " + $rel); exit 2 }
    if ($out) { $drift += $rel }
  }
} finally {
  Remove-Item -Recurse -Force $tmpdir -ErrorAction SilentlyContinue
}

foreach ($d0 in @($drift)) { if ($debt.ContainsKey($d0)) { continue } } # no-op keeps shape
$exempt = @($drift | Where-Object { $debt.ContainsKey($_) })
$drift = @($drift | Where-Object { -not $debt.ContainsKey($_) })

# 失效豁免检测（2026-10-22 实测补）：债务清单会腐烂 —— 文件修好后条目仍在册，
# 于是该文件将来**再次漂移也不会判红**（豁免把它永久遮住），而门禁看上去一直是绿的。
# 实测：35 条债务里 33 条已失效（对应文件已由并行会话入库并格式化），只剩 2 条 live。
# 判据：登记但已不再漂移 = 失效条目，必须点名要求删除；计入 rc（否则没人会去清）。
$staleDebt = @($debt.Keys | Where-Object { $exempt -notcontains $_ })
if ($staleDebt.Count -gt 0) {
  Write-Host ("失效的债务豁免（登记但已不漂移，必须从 tests/gofmt-debt.txt 删除）：" + $staleDebt.Count) -ForegroundColor Red
  foreach ($s in ($staleDebt | Sort-Object)) { Write-Host ("  x " + $s) -ForegroundColor Red }
  Write-Host "为什么必须删：在册豁免会让该文件**将来的漂移**静默放过（门禁对该文件永久失明）。" -ForegroundColor Yellow
  exit 1
}

if ($exempt.Count -gt 0) {
  Write-Host ("显式债务豁免（并发在途件，非静默跳过）：" + $exempt.Count + " 个") -ForegroundColor Yellow
  foreach ($e in $exempt) { Write-Host ("  ~ " + $e + "（登记于 tests/gofmt-debt.txt）") -ForegroundColor Yellow }
}
if ($drift.Count -gt 0) {
  Write-Host ("Go 格式门禁未通过（" + $drift.Count + " 个文件 LF 归一化后仍需 gofmt）：") -ForegroundColor Red
  foreach ($d in $drift) { Write-Host ("  - " + $d) -ForegroundColor Red }
  Write-Host "修复: cd workbench && gofmt -w <上述文件>（gofmt 输出 LF，与 .gitattributes 的 *.go eol=lf 一致）" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  if ($exempt.Count -gt 0) {
    # 三态表述：不能在有豁免时打「合规」二字 —— 那会让豁免看起来像全绿。
    Write-Host ("Go 格式门禁 PASS（口径：LF 归一化）：检查 " + $tracked.Count + " 个受跟踪 .go，" + $exempt.Count + " 个登记豁免未清（见 tests/gofmt-debt.txt，非静默跳过）") -ForegroundColor Yellow
  } else {
    Write-Host ("Go 格式合规：检查 " + $tracked.Count + " 个受跟踪 .go 文件（LF 归一化口径，零豁免）") -ForegroundColor Green
  }
}
exit 0

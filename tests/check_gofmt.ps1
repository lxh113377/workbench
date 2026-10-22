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

# 工作区 vs HEAD 两面分开计（2026-10-22 三轮修正，解死结）：
#   本机工作区常有并行会话**未提交**的改动，它在漂移而 HEAD 已合规 —— 若一律判红，
#   本机会长期红；而 CI 判的是 HEAD，那里是绿的。同一门禁两个环境两种结论，没人会信。
#   故：① HEAD 漂移 = 真需关注（计入 rc）
#       ② 仅工作区漂移且该文件在 HEAD 合规 = 并行在途件（提示，不计 rc）
#       ③ 登记豁免 = 已知缺陷（逐条点名，不计 rc），必要性按 HEAD 判定（见下）
$headDriftAll = @{}
$gitProbeAll = (& git -C $Root rev-parse --show-toplevel 2>$null)
$hasGitAll = ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace([string]$gitProbeAll))
if ($hasGitAll) {
  $rr = ([string]$gitProbeAll).Trim()
  $td = Join-Path ([IO.Path]::GetTempPath()) ('hwchk-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
  New-Item -ItemType Directory -Force $td | Out-Null
  try {
    foreach ($rel in @($tracked)) {
      $tf = Join-Path $td (($rel -replace '[\\/]', '__'))
      cmd /c "git -C `"$rr`" cat-file blob HEAD:$rel > `"$tf`"" 2>$null
      if ($LASTEXITCODE -ne 0 -or -not (Test-Path $tf)) { $headDriftAll[$rel] = $true; continue }
      $o = & gofmt -l $tf 2>$null
      $headDriftAll[$rel] = [bool]$o
    }
  } finally { Remove-Item -Recurse -Force $td -ErrorAction SilentlyContinue }
}
# 分类（2026-10-22 四轮校准：全程按 HEAD 判定，因为 CI 判的就是 HEAD）：
#   exempt     = 登记于债务清单且 HEAD 仍漂移（已知缺陷，点名不计 rc）
#   fatal      = HEAD 漂移但未登记（CI 会红，计入 rc）
#   inflightOnly = 工作区漂移但 HEAD 合规（并行在途件，不计 rc；附加信息）
$exempt = @()
$driftFatal = @()
$inflightOnly = @()
foreach ($rel in $tracked) {
  if (-not $headDriftAll.ContainsKey($rel)) { continue }
  if (-not $headDriftAll[$rel]) {
    # HEAD 合规：若工作区漂移，只是并行在途件
    if ($drift -contains $rel) { $inflightOnly += $rel }
    continue
  }
  if ($debt.ContainsKey($rel)) { $exempt += $rel; continue }
  $driftFatal += $rel
}
$drift = @($driftFatal)

# 失效豁免检测（2026-10-22 实测补）：债务清单会腐烂 —— 文件修好后条目仍在册，
# 于是该文件将来**再次漂移也不会判红**（豁免把它永久遮住），而门禁看上去一直是绿的。
# 实测：35 条债务里 33 条已失效（对应文件已由并行会话入库并格式化），只剩 2 条 live。
# 判据：登记但已不再漂移 = 失效条目，必须点名要求删除；计入 rc（否则没人会去清）。
# 必要性按 **HEAD** 判定（ISO CI 判的就是 HEAD）：若某条目在 HEAD 版本已不漂移，
# 它就是腐烂条目 —— 无论本地工作区是否临时又漂移起戥（并行在途件）。
# 但豁免匹配本身仍按工作区（那才是本次扫描的对象）。两面分工写清，避免死结。
$staleDebt = @()
if ($debt.Count -gt 0) {
  $headDrift = @{}
  $gitProbe = (& git -C $Root rev-parse --show-toplevel 2>$null)
  $hasGit = ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace([string]$gitProbe))
  if ($hasGit) {
    $repoRootForGit = ([string]$gitProbe).Trim()
    $tdir = Join-Path ([IO.Path]::GetTempPath()) ('headchk-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force $tdir | Out-Null
    try {
      foreach ($k in @($debt.Keys)) {
        # 取 HEAD 内容必须走 cmd 重定向写原始字节（2026-10-22 实测三次同族坑）：
        #   - `git show | -join "`n"` 丢最后一个换行（2659 vs 2660）=> gofmt 误判缺结尾换行
        #   - PowerShell 管道还会按出站编码改写内容（CRLF/BOM 同族问题本轮已踩）
        # cmd 重定向是唯一不��改写层的通道。
        $tf = Join-Path $tdir (($k -replace '[\\/]', '__'))
        cmd /c "git -C `"$repoRootForGit`" cat-file blob HEAD:$k > `"$tf`"" 2>$null
        if ($LASTEXITCODE -ne 0 -or -not (Test-Path $tf)) { $headDrift[$k] = $false; continue }
        $o = & gofmt -l $tf 2>$null
        $headDrift[$k] = [bool]$o
      }
    } finally { Remove-Item -Recurse -Force $tdir -ErrorAction SilentlyContinue }
    $staleDebt = @($debt.Keys | Where-Object { -not $headDrift[$_] })
  }
}
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
if ($inflightOnly.Count -gt 0) {
  Write-Host ("并行在途件（工作区漂移、HEAD 已合规，不计 rc）：" + $inflightOnly.Count) -ForegroundColor DarkGray
  foreach ($e in $inflightOnly) { Write-Host ("  . " + $e) -ForegroundColor DarkGray }
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

# check_ps1_syntax.ps1 -- 受跟踪 .ps1 必须能被 PowerShell 解析（2026-10-24 新增）
#
# 为什么需要（2026-10-24 第 30 轮盘点时发现的覆盖缺口）：
#   全仓 23 个门禁里**没有一个检查 .ps1 的语法可解析性**。
#   实测确认：造一个「字符串未闭合」的 .ps1，现有 3 个最相关的门禁全部 rc=0 放过 ——
#     check_ps1_encoding（管 BOM）rc=0
#     check_ps1_eol（管换行）rc=0
#     check_gate_links（管引用）rc=0
#   而 PowerShell 解析器明确报 `The string is missing the terminator: '`。
#   后果：语法错误的脚本能顺利进库，直到有人运行才炸 ——
#   本仓已有同族事故多次（BOM 缺失/双重 BOM/未闭合字符串，均靠跑起来才暴露）。
#
# 与其它门禁的分工（三层，各自管一个面）：
#   check_ps1_encoding -- 字节层：BOM 有无/重复
#   check_ps1_eol       -- 字节层：CRLF 混入
#   check_ps1_syntax    -- **语法层：能否被解析**（本件）
# 三层合起来才覆盖「一个 .ps1 从字节到可执行」的完整链路。
#
# 判据（任一命中即 rc=1）：
#   **受跟踪**的 .ps1/.psm1（git ls-files，与 check_ps1_encoding/eol 同口径），
#   用 PowerShell 官方 AST 解析器解析，有错误即判红并给出 file:line + 消息。
#
# 实现要点：用 [System.Management.Automation.Language.Parser]::ParseFile ——
#   **只解析不执行**（纯静态，无副作用、不需要权限、不会真跑脚本）。
#
# 用法: powershell -File tests\check_ps1_syntax.ps1 [-Quiet]
# 退出码: 0=全部可解析 / 1=有语法错误 / 2=UNVERIFIED
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

$relFiles = @(& $gitExe -C $root ls-files '*.ps1' '*.psm1')
if ($LASTEXITCODE -ne 0) {
  Write-Host "UNVERIFIED: git ls-files 失败" -ForegroundColor Red
  exit 2
}
if ($relFiles.Count -eq 0) {
  Write-Host "UNVERIFIED: 未取到任何受跟踪 .ps1/.psm1" -ForegroundColor Red
  exit 2
}

$bad = @()
$checked = 0
foreach ($rel in $relFiles) {
  $rel = $rel.Trim('"')
  if (-not $rel) { continue }
  $full = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $full)) { continue }
  $checked++
  $tokens = $null
  $errs = $null
  try {
    $null = [System.Management.Automation.Language.Parser]::ParseFile($full, [ref]$tokens, [ref]$errs)
  } catch {
    # 解析器自身抛异常（极少见，如文件被占用）—— 按未验证处理，不判红也不放过
    $bad += ($rel + '  [解析器异常: ' + $_.Exception.Message + ']')
    continue
  }
  if ($errs -and @($errs).Count -gt 0) {
    foreach ($e in @($errs)) {
      $msg = $e.Message
      if ($msg.Length -gt 100) { $msg = $msg.Substring(0, 100) }
      $bad += ($rel + ':' + $e.Extent.StartLineNumber + '  ' + $msg)
    }
  }
}

if ($bad.Count -gt 0) {
  Write-Host ("PowerShell 语法门禁未通过（" + $bad.Count + " 处，共检查 " + $checked + " 个脚本）：") -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object)) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "  危害：语法错误的脚本能进库，直到有人运行才炸（本仓已有 BOM/双 BOM/未闭合字符串同族事故）。" -ForegroundColor Yellow
  Write-Host "  修复: 按上行 file:line 改；本地可先自检：" -ForegroundColor Yellow
  Write-Host "    `$e=`$null; [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path <file>).Path,[ref]`$null,[ref]`$e); `$e" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("PowerShell 语法合规：" + $checked + " 个受跟踪脚本全部可解析") -ForegroundColor Green }
exit 0
# check_skip_disclosure.ps1 -- Go 测试的 t.Skip 必须带理由，且总数要可见（round 87 新增）
#
# 为什么需要（round 87 实测侦察）：
#   全仓受跟踪 *_test.go 共 172 个文件，其中 **53 处 t.Skip / t.Skipf，涉及 29 个文件**。
#   抽查确认这些 skip 大多有明确理由（「icacls 施加 deny 失败」「需要构建真实二进制」
#   「环境限制」），注释里甚至写了纪律「禁把没验成当验过了」—— 质量是好的。
#   但存在两个真问题：
#   ① **没有任何机制统计它们**：53 个 skip 散在 29 个文件里，总数不可见。
#      于是「某个包其实全靠 skip 凑数」这种腐化**静默发生** —— 与 round 47「内存门禁恒 0 条」
#      同族（沉默 != 正常，只是这次沉默的是「跳过」而不是「没落盘」）。
#   ② **个别 skip 可能没写理由**（t.Skip() 空参数），静态可判。
#
#   本仓铁律是「没测成 != 合格」（check_resident_longrun / check_perf / check_queue_size 都按此设计），
#   Go 侧的 t.Skip 正是「没测成」的本体，却一直不在任何判据视野里。
#
# 判据（窄、可证伪）：
#   1) **无理由 skip**：t.Skip() 不带任何参数 => 判红（静态可判，无需语义推断）。
#      t.Skip("...") / t.Skipf("...") 带字符串参数的视为已披露，不判。
#   2) **总数可见**：把 skip 总数 + 涉及文件数 + Top N 文件打进输出（不计 rc）。
#      理由：本仓在册教训是「模糊语义检查制造噪声 => 门禁被删」，故不做「skip 太多就判红」
#      这类需要定阈值的语义判断；只做「没披露」这一客观事实 + 「让数字可见」。
#   3) 落盘读数（与 check_untracked_go 同构）：skip 总数进 gate-readings.jsonl，
#      趋势器可判「是否在单调上升」（上升 = 在靠 skip 凑数，round 47 同族信号）。
#
# 与 check_test_assertions 的分工：
#   后者管「Test 函数有没有断言」（零断言 = 假测试）；
#   本件管「Test 有没有真的跑」（skip = 没跑）以及「跳过是否披露」。
#   两者合起来才覆盖「测试既存在、又真在测」。
#
# 用法: powershell -File tests/check_skip_disclosure.ps1 [-Quiet] [-NoRecord]
# 退出码: 0=全部 skip 均带理由 / 1=有无理由 skip / 2=UNVERIFIED（取不到 git）
param(
  [switch]$Quiet,
  [switch]$NoRecord
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git' -ForegroundColor Red; exit 2 }

$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$q = [string][char]34
$bs = [string][char]92
$psi.Arguments = '-C ' + $q + $root + $q + ' ls-files workbench/*_test.go'
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
if ($proc.ExitCode -ne 0) { Write-Host ('UNVERIFIED: git ls-files 失败: ' + $se.Trim()) -ForegroundColor Red; exit 2 }

$testFiles = @($so -split [char]10 | ForEach-Object { $_.Trim().Trim([char]34) } | Where-Object { $_ -ne '' })
if ($testFiles.Count -eq 0) { Write-Host 'UNVERIFIED: 未取到任何 _test.go' -ForegroundColor Red; exit 2 }

$bare = @()
$total = 0
$perFile = @{}
foreach ($rel in $testFiles) {
  $full = Join-Path $root ($rel.Replace('/', $bs))
  if (-not (Test-Path -LiteralPath $full)) { continue }
  $lines = @([IO.File]::ReadAllLines($full))
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $s = $lines[$i].Trim()
    if ($s -notmatch 't[.]Skip') { continue }
    # 只认调用点：形如 t.Skip( / t.Skipf( （排除注释与字符串里的提及）
    if ($s -match 't[.]Skip(f?)[(]') {
      $total++
      if (-not $perFile.ContainsKey($rel)) { $perFile[$rel] = 0 }
      $perFile[$rel] = $perFile[$rel] + 1
      # 无理由判定：Skip() 或 Skipf() 的括号内为空
      if ($s -match 't[.]Skip(f?)[(][)]') {
        $bare += ($rel + ':' + ($i + 1) + '  ' + $s)
      }
    }
  }
}

# 读数落盘（与 check_untracked_go / check_resident_longrun 同构：单次读数不够，序列才能判趋势）
if (-not $NoRecord) {
  $memDir = Join-Path $root '.workbuddymemory'
  if (Test-Path -LiteralPath $memDir) {
    $rec = [ordered]@{
      at = (Get-Date).ToString('o')
      gate = 'skip_disclosure'
      skipTotal = $total
      bareCount = $bare.Count
      testFiles = $testFiles.Count
      filesWithSkip = $perFile.Count
    }
    Add-Content -LiteralPath (Join-Path $memDir 'gate-readings.jsonl') -Value ($rec | ConvertTo-Json -Compress)
  }
}

if ($bare.Count -gt 0) {
  Write-Host ('skip 披露门禁未通过：' + $bare.Count + ' 处 t.Skip 无理由（没测成 != 合格，静默跳过比失败更危险）') -ForegroundColor Red
  foreach ($b in $bare) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  修法：补一句为什么跳过（环境限制/前置条件不成立/需要真实二进制等），或改用 t.Skipf 带格式串。' -ForegroundColor Yellow
  Write-Host '  参考在册的好范例：internal/workspace/probe_windows_test.go 的 icacls 三类跳过（理由+后果都写了）。' -ForegroundColor Yellow
  exit 1
}

if (-not $Quiet) {
  Write-Host ('skip 披露合规：' + $total + ' 处 t.Skip 全部带理由（共 ' + $testFiles.Count + ' 个 _test.go，其中 ' + $perFile.Count + ' 个含 skip）') -ForegroundColor Green
  $top = $perFile.GetEnumerator() | Sort-Object Value -Descending | Select-Object -First 5
  foreach ($t in $top) { Write-Host ('    ' + $t.Value + '  ' + $t.Key) -ForegroundColor DarkGray }
}
exit 0

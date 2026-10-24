# check_ignored_error.ps1 -- 打印成功消息前的错误不得被 _ = 忽略（round 89 新增）
#
# 为什么需要（round 89 实测，objective「包裹代码质量、报错修复」的直接抓手）：
#   全仓 151 个非测试 .go 里有 **122 处 `_ = xxx(...)`** 忽略返回值。抽查确认大多合理
#   （退出路径 / 指标写盘失败无害 / 清理可能不存在的链接），但发现一处**真漏判**：
#     cmd/workbench/main.go:731  `_ = st.Savepoint()`
#     上一行刚 `if err := st.SaveState(state); err != nil { ... return 1 }`，
#     紧接着打印「已标记停止」—— 若 Savepoint 失败，用户看到的就是**虚假成功反馈**：
#     以为停成了，重启后接续判据读到的却是旧状态。SaveState 与 Savepoint 是「停机器」
#     这件事的两半，一半判错一半忽略，逻辑自相矛盾。
#
#   形态归纳：**「同一次操作里，一部分错误被严格判、另一部分被 _ = 忽略」** ——
#   这不是风格问题，是「已打印的成功消息可能变成假话」的实质缺陷。
#
# 判据（窄、可证伪，只抓这一种形态）：
#   同一函数体内，若存在 `if err := <f>(...); err != nil { ... return }` 的严格判错，
#   且同函数体内另有 `_ = <f>(...)` 忽略**同名函数**的返回值 => 判红。
#   （同名 = 同一函数的严格模式与忽略模式并存，正是自相矛盾的可机判信号。）
#
# 刻意不收窄/放宽的理由：
#   - 不判所有 `_ = x`：实测 122 处里绝大多数是合理的尽力而为（退出路径、清理、
#     指标写盘），全判会制造大量噪声 => 门禁被撤（本仓在册教训）。
#   - 不跨函数判：不同函数的不同语境无从比较，跨了就是猜。
#   - 只认「同名函数」这个客观信号：它精确对应「同一操作的两半被区别对待」。
#
# 豁免：测试文件（_test.go）不判 —— 测试里 _ = 常是故意的（构造失败场景）。
#
# 用法: powershell -File tests/check_ignored_error.ps1 [-Quiet]
# 退出码: 0=无自相矛盾 / 1=检出 / 2=UNVERIFIED（取不到 git）
param([switch]$Quiet)
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
$psi.Arguments = '-C ' + $q + $root + $q + ' ls-files workbench/*.go'
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
$proc = [System.Diagnostics.Process]::Start($psi)
$so = $proc.StandardOutput.ReadToEnd()
$proc.WaitForExit()
if ($proc.ExitCode -ne 0) { Write-Host 'UNVERIFIED: git ls-files 失败' -ForegroundColor Red; exit 2 }

$goFiles = @($so -split [char]10 | ForEach-Object { $_.Trim().Trim([char]34) } | Where-Object { $_ -ne '' -and $_ -notmatch '_test[.]go$' })
if ($goFiles.Count -eq 0) { Write-Host 'UNVERIFIED: 未取到非测试 .go' -ForegroundColor Red; exit 2 }

$bs = [string][char]92
$bad = @()
$checked = 0
foreach ($rel in $goFiles) {
  $full = Join-Path $root ($rel.Replace('/', $bs))
  if (-not (Test-Path -LiteralPath $full)) { continue }
  $lines = @([IO.File]::ReadAllLines($full))
  # 收集本文件「严格判错的函数名」与「被忽略的函数名」
  $strictFns = @{}
  $ignoredFns = @{}
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $s = $lines[$i].Trim()
    # 严格判错形态：if err := x.Fn( ... ) ; err != nil {
    #   不用正则（本文件由 write 通道生成，反斜杠会被吃 —— round 89 实测第 7 次）：
    #   字面前缀 + IndexOf 即可，无需 \s。
    if ($s.StartsWith('if err') -and $s.Contains(':=') -and $s.Contains('.')) {
      $dot1 = $s.IndexOf('.')
      $par1 = $s.IndexOf('(', $dot1)
      if ($dot1 -ge 0 -and $par1 -gt $dot1) {
        $fn = $s.Substring($dot1 + 1, $par1 - $dot1 - 1).Trim()
        if ($fn -match '^[A-Za-z_][A-Za-z0-9_]*$') {
          if (-not $strictFns.ContainsKey($fn)) { $strictFns[$fn] = $i + 1 }
        }
      }
    }
    # 忽略形态：_ = x.Fn( ... )
    if ($s.StartsWith('_ =') -and $s.Contains('.')) {
      $dot2 = $s.IndexOf('.')
      $par2 = $s.IndexOf('(', $dot2)
      if ($dot2 -ge 0 -and $par2 -gt $dot2) {
        $fn2 = $s.Substring($dot2 + 1, $par2 - $dot2 - 1).Trim()
        if ($fn2 -match '^[A-Za-z_][A-Za-z0-9_]*$') {
          if (-not $ignoredFns.ContainsKey($fn2)) { $ignoredFns[$fn2] = $i + 1 }
        }
      }
    }
  }
  # 【2026-10-24 round 89 二次收窄】首版按「文件级同名函数」判定，实测 6 处全是误报：
  #   wal.go 的 f.Close()（L70 忽略 vs L150 严格）在不同函数、不同语境；
  #   plan_stage.go 的 WriteJSONAtomic（L206 忽略 vs L193 严格）一个是 best-effort 备注、
  #   一个是主路径 —— 都是合理设计，不是「同一操作的两半被区别对待」。
  # 真缺陷（round 89 首例）的形态是：同一文件、同一操作、相邻几行内一半判错一半忽略。
  # 故收窄为「忽略点与严格点相距 <= 25 行」—— 足够覆盖「停机器的两半」这类真实缺陷，
  # 又不会把跨函数/跨语境的标准库同名用法卷进来。
  # 这是 round 64「判据本身误报」的同族第 2 次：判据放宽容易、收窄必须靠实测。
  foreach ($k in $ignoredFns.Keys) {
    if ($strictFns.ContainsKey($k)) {
      $dist = [Math]::Abs($ignoredFns[$k] - $strictFns[$k])
      # 【round 89 三次收窄】25 行窗口仍误报 plan_stage.go（L193/L206 相距 13 行）：
      #   该函数对全部证据落盘一致采用 best-effort（Warn 或不判），设计自洽，
      #   不是「一半严格一半忽略」。真缺陷（Savepoint）的形态是**相邻几个语句内的直接矛盾**
      #   （L727 判 SaveState、L731 忽略 Savepoint，相距 4 行且是连续两次调用）。
      #   5 行窗口足以覆盖「同一操作的两半」，又排除跨语句的一致设计。
      if ($dist -le 5) {
        $bad += ($rel + '  函数 ' + $k + '：L' + $strictFns[$k] + ' 严格判错，但 L' + $ignoredFns[$k] + ' 被 _ = 忽略（相距 ' + $dist + ' 行 —— 同一操作的两半被区别对待，已打印的成功消息可能是假话）')
      }
    }
  }
  $checked++
}

if ($bad.Count -gt 0) {
  Write-Host ('忽略错误门禁未通过：' + $bad.Count + ' 处「同一函数被区别对待」（strict 与 ignored 并存）') -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object -Unique)) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  形态：同一次操作里一部分错误严格判、另一部分 _ = 忽略 —— 已打印的成功消息可能变成假话。' -ForegroundColor Yellow
  Write-Host '  修法：把 _ = 改成 if err := ...; err != nil { 报错并返回非零 }；若确实要忽略，注释说明为什么失败无害。' -ForegroundColor Yellow
  Write-Host '  首例（round 89 已修）：cmd/workbench/main.go 的 st.Savepoint() —— 与上一行 SaveState 是「停机器」的两半。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('忽略错误门禁通过：' + $checked + ' 个非测试 .go 中无「同名函数 strict/ignored 并存」') -ForegroundColor Green
}
exit 0

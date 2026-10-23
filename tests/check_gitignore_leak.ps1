# check_gitignore_leak.ps1 -- 可再生产副产物不得漏出 .gitignore（round 71 新增）
#
# 为什么需要（本仓三连实测）：
#   round 67：tests/_egress/ 41.2MB 一次性 GeoIP 数据没被忽略（混了 5 天）；
#   round 68：workbench/build/ 32.1MB 构建目录（含 node_modules）没被忽略；
#   round 71：workbench/exec 是 17KB Go 覆盖率 profile（无 coverage 前缀、无扩展名），
#             上面的 coverage* / *.coverprofile 都匹配不到。
#   三次同一根因：**.gitignore 靠人凭记忆补，漏一类就静默积累**。untracked 列表越来越长，
#   后来者分不清「这是并行会话的在途工作」还是「这是忘了忽略的垃圾」——
#   而 round 69 已经演示了后果：73 份真有价值的报告因为在 untracked 里躺了几天，
#   处于「误删即永失、门禁看不到」的状态。
#
# 判据（窄、只认客观形态，不做语义推断）：
#   扫 git status 的未跟踪文件，命中以下任一族即判红（这些形态 100% 可再生/非源码）：
#     A) 构建与依赖：node_modules/、*.exe、*.dll、*.so、*.dylib、*.wasm、dist 外的 *.map
#     B) 覆盖率/测试副产物：mode: set 开头（Go coverprofile 特征）、coverage*、*.test
#     C) 大型二进制数据：> 5MB 且非源码扩展名（mmdb/dat/db/bin/pack/idx 等）
#     D) 缓存：*.cache、cache.db、.cache/、*.pyc、__pycache__/
#   豁免（显式登记，带理由）：
#     * workbench/cmd/workbench/rsrc_windows_amd64.syso —— 受跟踪的 Windows 资源文件
#       （setup/build.ps1 会 windres 重新生成，但**入库版是交付必需**，见 build.ps1:70-77）；
#     * 并行会话在途的 *.go —— 那是别人的工作，不是漏 ignore（本仓在册：不代改、不判红）；
#     * .workbuddy/memory/gate-readings.jsonl —— 读数数据，按约定故意不跟踪也不忽略（可见）。
#
# 为什么不整目录判「untracked 太多」：那会把并行会话的正常在途工作判红（本仓在册：判据过宽
#   制造噪声 => 门禁被删）。只判「形态上一定是垃圾」的那几族。
#
# 用法: powershell -File tests/check_gitignore_leak.ps1 [-Quiet] [-RepoPath p]
# 退出码: 0=无漏出 / 1=检出 / 2=UNVERIFIED（取不到 git）
param(
  [switch]$Quiet,
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red; exit 2 }
if (-not (Test-Path -LiteralPath $root)) { Write-Host ('UNVERIFIED: 目录不存在: ' + $root) -ForegroundColor Red; exit 2 }

# 用 -z 取原始路径（中文路径 + 空格 + 换行都安全；不经 quotePath 八进制转义）
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $gitExe
$q = [string][char]34
$psi.Arguments = '-C ' + $q + $root + $q + ' status --porcelain=v1 -uall -z'
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
if ($proc.ExitCode -ne 0) { Write-Host ('UNVERIFIED: git status 失败: ' + $se.Trim()) -ForegroundColor Red; exit 2 }

# porcelain -z 格式：每项两段（XY + path），未跟踪项 XY = '?? '
$entries = @($so -split [char]0 | Where-Object { $_ -ne '' })
$untracked = @()
for ($i = 0; $i -lt $entries.Count; $i++) {
  $e = $entries[$i]
  if ($e.Length -ge 4 -and $e.Substring(0, 2) -eq '??') {
    $pth = $e.Substring(3)
    if ($pth -ne '') { $untracked += $pth }
  }
}
if ($untracked.Count -eq 0) {
  if (-not $Quiet) { Write-Host 'gitignore 泄漏门禁通过：无未跟踪文件' -ForegroundColor Green }
  exit 0
}

$bs = [string][char]92
# 豁免清单（受跟踪的交付必需 + 他人所有权 + 故意可见的数据）
$exemptExact = @(
  'workbench/cmd/workbench/rsrc_windows_amd64.syso'
)
$exemptPrefix = @(
  '.workbuddy/memory/gate-readings.jsonl'
)

# 形态族：名字 + 匹配函数（用字面 Contains/EndsWith，不用正则 —— 见 check_caliber_isolation 注记：
# write 通道会把反斜杠双重转义，判据用字面片段才稳）
function Get-LeakReason([string]$path) {
  $name = $path
  $idx = $path.LastIndexOf('/')
  if ($idx -lt 0) { $idx = $path.LastIndexOf([char]92) }
  if ($idx -ge 0) { $name = $path.Substring($idx + 1) }
  $lower = $name.ToLowerInvariant()

  # A) 构建与依赖
  if ($lower -eq 'node_modules') { return 'A:node_modules' }
  if ($path.Contains('node_modules/')) { return 'A:node_modules 内文件' }
  if ($lower.EndsWith('.exe')) { return 'A:*.exe 构建产物' }
  if ($lower.EndsWith('.dll')) { return 'A:*.dll' }
  if ($lower.EndsWith('.so')) { return 'A:*.so' }
  if ($lower.EndsWith('.dylib')) { return 'A:*.dylib' }
  if ($lower.EndsWith('.wasm')) { return 'A:*.wasm' }
  if ($lower.EndsWith('.map')) { return 'A:*.map' }

  # B) 覆盖率/测试副产物
  if ($lower.StartsWith('coverage')) { return 'B:coverage* 覆盖率产物' }
  if ($lower.EndsWith('.test')) { return 'B:*.test 测试二进制' }
  if ($lower.EndsWith('.coverprofile')) { return 'B:*.coverprofile' }
  # Go coverprofile 的内容特征：首行就是 'mode: set' / 'mode: count' / 'mode: atomic'。
  # round 71 的真实案例 workbench/exec 既无 coverage 前缀也无扩展名，只能靠内容认。
  # 只对 8KB 以下的小文本文件读首行（大文件读首行浪费 IO，且大文件已由 C 族覆盖）。
  $full2 = Join-Path $root ($path.Replace('/', $bs))
  if ((Test-Path -LiteralPath $full2)) {
    $it2 = Get-Item -LiteralPath $full2 -Force
    if ((-not $it2.PSIsContainer) -and ($it2.Length -lt 8KB) -and ($it2.Length -gt 0)) {
      try {
        $head = [IO.File]::ReadAllText($full2)
        if ($head.StartsWith('mode: set') -or $head.StartsWith('mode: count') -or $head.StartsWith('mode: atomic')) {
          return 'B:Go coverprofile（mode: 开头）'
        }
      } catch { }
    }
  }

  # D) 缓存
  if ($lower.EndsWith('.pyc')) { return 'D:*.pyc' }
  if ($lower -eq '__pycache__') { return 'D:__pycache__' }
  if ($path.Contains('__pycache__/')) { return 'D:__pycache__ 内文件' }
  if ($lower.EndsWith('.cache')) { return 'D:*.cache' }
  if ($lower -eq 'cache.db') { return 'D:cache.db' }

  # C) 大型二进制数据（> 5MB 且非源码扩展名）
  $srcExt = @('.go', '.ps1', '.psm1', '.md', '.yml', '.yaml', '.json', '.jsonl', '.txt', '.cmd', '.bat', '.vbs', '.ts', '.js', '.html', '.css', '.svg', '.patch', '.diff', '.rc', '.syso', '.gitignore', '.gitattributes', '.tmpl', '.in')
  $isSrc = $false
  foreach ($x in $srcExt) { if ($lower.EndsWith($x)) { $isSrc = $true; break } }
  if (-not $isSrc) {
    $full = Join-Path $root ($path.Replace('/', $bs))
    if (Test-Path -LiteralPath $full) {
      $it = Get-Item -LiteralPath $full -Force
      if (-not $it.PSIsContainer) {
        if ($it.Length -gt 5MB) { return ('C:大二进制 ' + [math]::Round($it.Length / 1MB, 1) + 'MB') }
      }
    }
  }
  return ''
}

$bad = @()
foreach ($pth in $untracked) {
  # 豁免：并行会话在途的 .go（他人所有权）
  if ($pth.EndsWith('.go')) { continue }
  # 豁免：显式登记的精确项与前缀项
  $isExempt = $false
  foreach ($x in $exemptExact) { if ($pth -eq $x) { $isExempt = $true; break } }
  foreach ($x in $exemptPrefix) { if ($pth -eq $x) { $isExempt = $true; break } }
  if ($isExempt) { continue }

  $reason = Get-LeakReason $pth
  if ($reason -ne '') { $bad += ($pth + '  [' + $reason + ']') }
}

if ($bad.Count -gt 0) {
  Write-Host ('gitignore 泄漏门禁未通过：' + $bad.Count + ' 个未跟踪文件形态上是可再生产副产物') -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object -Unique)) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  这些不是源码、也不是并行会话的在途工作，应当进 .gitignore（留档理由而非静默忽略）。' -ForegroundColor Yellow
  Write-Host '  修法：在 .gitignore 里逐条列名 + 注释写明为什么，或整目录加 dir/ 形式。' -ForegroundColor Yellow
  Write-Host '  参考在册案例：tests/_egress/（41MB GeoIP）、workbench/build/（32MB 构建）、workbench/exec（coverprofile）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('gitignore 泄漏门禁通过：' + $untracked.Count + ' 个未跟踪文件均无可再生产副产物形态') -ForegroundColor Green
}
exit 0

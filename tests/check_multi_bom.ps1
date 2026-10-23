# check_multi_bom.ps1 -- 受跟踪文本文件不得有多个前导 UTF-8 BOM（2026-10-24 round 45 新增）
#
# 为什么需要（round 44 记的待办，本轮兑现）：
#   我本人跨多轮犯同一个错：改文件时用「读出来 -> replace 去掉一个 BOM -> 再前置一个」，
#   而 replace 只去一个，于是每改一次多攒一个。CHANGELOG.md 因此攒到 5 个前导 BOM
#   （289 个 .md 里唯一一个多 BOM 文件）。已有的 check_ps1_encoding 只管 .ps1/.psm1 的
#   「有没有 BOM」，多 BOM 无人守 —— 尤其 .md 这类基本不带 BOM 的文件，混进去没人发现。
#
# 机理（round 45 用一次性临时仓库实测，用完即删）：
#   工作区 3 个前导 BOM -> git add 后 index 仍 3 -> 补 text eol=crlf 规则仍 3
#   -> 提交后 HEAD 里仍 3。即 clean 过滤器只管换行、**不碰 BOM**，多 BOM 会一路进库。
#   所以必须有自己的门禁，且判据查 HEAD（与 check_repo_eol 同层，不受本地脏工作区干扰）。
#
# 危害（不是洁癖）：
#   1) 多 BOM 文件被按「一个 BOM + 正文」解析的工具看到时，第二个 BOM 成了正文首字符 ——
#      与 .ps1 双重 BOM 那次（PS5.1 把第二个 BOM 当命令字符报 ParserError）同型；
#   2) 逐轮累积，文件开头越来越脏，diff / 搜索 / 渲染都可能受影响。
#
# 判据（窄面、可证伪）：
#   受跟踪的下列文本类型，在 HEAD 中的原始字节若有 >=2 个**连续前导** EF BB BF => 判红。
#   单 BOM 与无 BOM 都合规（本仓 .md 多为无 BOM、.ps1 必须带 BOM，两类都不该被强迫改）。
#   刻意不扫二进制与未列出类型（.exe/.syso/.pyc 等，BOM 对它们无意义）。
#
# 上闸前实况（round 45 实测）：1,277 个受跟踪文本文件 = 无BOM 1210 / 单BOM 67 / 多BOM 0
#   （CHANGELOG.md 的 5 个已在 round 44 修好并入 HEAD）=> 上线即绿，不给 CI 添噪声。
#
# 实现要点：
#   **这个门禁开发过程中最值钱的发现：传输层自己会污染数据。**
#   首版把请求写进 git cat-file --batch 的 stdin，两条常见写法都被污染：
#     (1) New-Object IO.StreamWriter($stream) —— PS5.1（.NET Framework）默认吐 UTF-8 BOM，
#         第一条请求变成 <BOM>HEAD:x.md，git 回 missing；
#     (2) 直接 BaseStream.Write($bytes) —— 逐字节 hexdump 证明写入**干净**，git 仍回 BOM 前缀。
#   后果是「**漏检还很像全绿**」：首版报「查 1620 个」完全正常，实际第一个文件从未被检查
#   （对齐核算后真实数是 1621，差的那 1 个就是被跳过的）。查 BOM 的门禁被 BOM 咬。
#   **非 ASCII（中文）文件名不是盲区**（round 47 双重验证，此前一度担心它在 CI 437 码页下
#   被解码成 mojibake → 请求名对不上 → 记成 missing 而假绿）：
#   ① CI 日志实证：`查 1622 个 … 缺HEAD 0 个` —— 39 个中文/非 ASCII 名不但被遍历到，且全部对齐；
#   ② 针对性变异：另造一个**中文文件名 + 3 前导 BOM** 的一次性仓库，门禁正确点名判红、
#      且报「共查 2 个」（两文件都遍历）。ls-files 走 ReadToEnd 默认码页、请求写 UTF-8 无 BOM，
#   两条路在本仓与 runner 上都对得上。故**不改 Get-GitText 的解码方式** —— 改了反而可能把
#   现在能用的中文名路径弄坏（无证据不动生产判据）。
#   定稿：请求写临时文件 + cmd 文件重定向（字节由 OS 保证），并强制「响应记录数 == 请求数」，
#   对不上直接 UNVERIFIED —— 静默漏检比误报更危险，因为它看起来是绿的。
#   - 用一次 git cat-file --batch 进程流式取全部 blob 的**前导字节**（逐文件起进程要 1277 次，
#     约 13~25 秒；单进程 + --batch 亚秒级）。
#   - 辅助函数必须定义在使用之前（PowerShell 顺序执行，写在 exit 之后的函数等于没定义 —— 
#     本文件首版就踩过这个错，靠 parse 检查发现）。
#   - 换行统一用 [char]10，不写反引号转义（外层脚本语言会把 PowerShell 的反引号吃掉）。
#
# 用法: powershell -File tests/check_multi_bom.ps1 [-Quiet] [-RepoPath <path>]
#   （-RepoPath 供自测注入别的仓库；默认根由脚本位置推导，不硬编码 —— 2026-10-22 在册教训）
# 退出码: 0=无多 BOM / 1=有 / 2=UNVERIFIED
param(
  [switch]$Quiet,
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$NLc = [string][char]10

function Read-StreamLine([System.IO.Stream]$s) {
  $sb = New-Object System.Text.StringBuilder
  $b = New-Object byte[] 1
  while ($true) {
    $read = $s.Read($b, 0, 1)
    if ($read -le 0) { break }
    if ($b[0] -eq 10) { break }
    [void]$sb.Append([char]$b[0])
  }
  return $sb.ToString()
}

function Read-StreamBytes([System.IO.Stream]$s, [int]$want) {
  $buf = New-Object byte[] $want
  $off = 0
  while ($off -lt $want) {
    $rd = $s.Read($buf, $off, $want - $off)
    if ($rd -le 0) { break }
    $off += $rd
  }
  return $buf
}

function Count-LeadBom([byte[]]$bytes) {
  $n = 0; $i = 0
  while (($i + 2) -lt ($bytes.Count + 0)) {
    if ($bytes[$i] -eq 239 -and $bytes[$i + 1] -eq 187 -and $bytes[$i + 2] -eq 191) { $n++; $i += 3 }
    else { break }
  }
  return $n
}

$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red
  exit 2
}

if (-not (Test-Path -LiteralPath $root)) {
  # 目录不存在时 ProcessStartInfo(WorkingDirectory) 会抛裸 Win32Exception「The directory
  # name is invalid」—— 路径打错字必须走 UNVERIFIED，不是崩栈（round 46 实测引出）。
  Write-Host ('UNVERIFIED: 仓库目录不存在: ' + $root) -ForegroundColor Yellow
  exit 2
}

$patterns = @('*.md','*.go','*.ps1'
,'*.psm1','*.sh','*.yml','*.yaml','*.json','*.txt','*.css','*.js','*.mjs','*.cmd','*.bat','*.html','*.py','*.psd1')

function Get-GitText([string[]]$gitArgs) {
  # 显式进程调用（2026-10-24 在册坑：& git ... 2>&1 会把 stderr 并进输出流、
  # $LASTEXITCODE 变空且 stdout/stderr 皆空 —— 静默失败最难查）
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
  $proc = [System.Diagnostics.Process]::Start($psi)
  $so = $proc.StandardOutput.ReadToEnd()
  $se = $proc.StandardError.ReadToEnd()
  $proc.WaitForExit()
  return @{ rc = $proc.ExitCode; out = $so; err = $se }
}

$files = @()
foreach ($pat in $patterns) {
  $r = Get-GitText @('ls-files', '-z', $pat)
  if ($r.rc -ne 0) { continue }
  foreach ($f in ($r.out -split [char]0)) { $f2 = $f.Trim(); if ($f2) { $files += $f2 } }
}
$files = @($files | Sort-Object -Unique)
if ($files.Count -eq 0) {
  Write-Host 'UNVERIFIED: 未取到任何受跟踪文本文件（可能不在 git 仓库内）' -ForegroundColor Yellow
  exit 2
}

# ---- 传输层（round 45 定稿）----
# 请求写临时文件 + cmd 文件重定向喂给 git cat-file --batch，单进程流式读全部响应。
# 为什么放弃交互式写 stdin（实测两条路径**都**被污染）：
#   (1) New-Object IO.StreamWriter($stream)：PS5.1（.NET Framework）默认吐 UTF-8 BOM，
#       第一条请求被污染成 <BOM>HEAD:x.md，git 回 missing => 第一个文件被静默跳过；
#   (2) 直接 $StandardInput.BaseStream.Write($bytes)：逐字节 hexdump 证明写入干净
#       （48 45 41 44...），git 仍回「<BOM>HEAD:multi.md missing」，BOM 来源未查明。
#   首版因此报「查 1620 个」看着完全正常，实际 1 个从未检查 —— 查 BOM 的门禁被 BOM
#   咬了还漏检。文件重定向的字节由 OS 保证（node 同字节 input 实测返回 blob 17 + 3 个 BOM）。
$reqFiles = @($files | Where-Object { $_.IndexOf($NLc) -lt 0 })
$skippedNl = $files.Count - $reqFiles.Count
$reqText = ($reqFiles | ForEach-Object { 'HEAD:' + $_ }) -join $NLc
$reqText = $reqText + $NLc
$reqFile = Join-Path ([IO.Path]::GetTempPath()) ('mbom-req-' + [Guid]::NewGuid().ToString('N').Substring(0, 8) + '.txt')
[IO.File]::WriteAllText($reqFile, $reqText, (New-Object System.Text.UTF8Encoding($false)))

$bp = New-Object System.Diagnostics.ProcessStartInfo
$bp.FileName = if ($env:ComSpec) { $env:ComSpec } else { 'cmd.exe' }
$q = [string][char]34
# cmd /c 的引号规则（实测三轮才定稿）：/c 后若首字符是引号，cmd 会把**最外层一对**剥掉，
# 于是 '/c "C:\Program Files\..." cat-file ...' 会去执行 'C:\Program' 报 not recognized；
# '/c "<exe> <args>" < "<file>"' 又会把重定向当参数喂给 git（unknown option）。
# 唯一可靠的写法是**整体再包一层引号**：/c ""<exe>" cat-file --batch < "<file>""
$bp.Arguments = '/c ' + $q + $q + $gitExe + $q + ' cat-file --batch < ' + $q + $reqFile + $q + $q
$bp.WorkingDirectory = $root
$bp.UseShellExecute = $false
$bp.CreateNoWindow = $true
$bp.RedirectStandardOutput = $true
$bp.RedirectStandardError = $true
$batch = [System.Diagnostics.Process]::Start($bp)
$stdout = $batch.StandardOutput.BaseStream

$bad = @()
$singleCount = 0
$noneCount = 0
$checked = 0
$missingCount = 0
$parseErrors = 0
$errReason = ''
$idx = 0
while ($true) {
  $hdr = Read-StreamLine $stdout
  if ($hdr.Length -eq 0) { break }   # header 不可能为空，空即 EOF
  $parts = @($hdr.Trim() -split ' ')
  # blob 响应严格判形：<40hex> blob <size>；其它一律视为请求回显（missing）
  $isBlob = ($parts.Count -ge 3) -and ($parts[0].Length -eq 40) -and ($parts[1] -eq 'blob')
  if ($idx -ge $reqFiles.Count) { $parseErrors++; $errReason = '响应多于请求'; break }
  $fname = $reqFiles[$idx]
  $idx++
  if ($isBlob) {
    $size = [int]$parts[2]
    $checked++
    $take = [Math]::Min(9, $size)
    $n = 0
    if ($take -gt 0) {
      $headBytes = Read-StreamBytes $stdout $take
      $n = Count-LeadBom $headBytes
      $remain = $size - $take
      if ($remain -gt 0) { $null = Read-StreamBytes $stdout $remain }
    }
    $null = Read-StreamBytes $stdout 1   # blob 后的那个换行
    if ($n -ge 2) { $bad += ($fname + '  前导BOM=' + $n) }
    elseif ($n -eq 1) { $singleCount++ } else { $noneCount++ }
  }
  else {
    # 回显行：该文件不在 HEAD（本地已 add 未 commit 的新文件，并行会话常态）
    $missingCount++
  }
}
try { $stdout.Dispose() } catch { }
if (-not $batch.WaitForExit(15000)) { try { $batch.Kill() } catch { } }
try { Remove-Item -LiteralPath $reqFile -Force -ErrorAction SilentlyContinue } catch { }

if (($checked + $missingCount) -eq 0) {
  Write-Host 'UNVERIFIED: cat-file --batch 未取到任何响应' -ForegroundColor Yellow
  exit 2
}
# 对齐核算（round 45 变异测试引出的硬要求）：本件的病是「漏检还很像全绿」——
# 首版被 BOM 污染时报「查 1620 个」完全正常，实际第一个文件从未被检查。
# 现在强制 响应记录数 == 请求数，对不上就是协议错位/流污染，结果不可信。
if ($parseErrors -gt 0) {
  Write-Host ('UNVERIFIED: 协议错位（' + $errReason + '，解析异常 ' + $parseErrors + ' 条），结果不可信') -ForegroundColor Red
  exit 2
}
if (($checked + $missingCount) -ne $reqFiles.Count) {
  Write-Host ('UNVERIFIED: 响应与请求不对齐（查 ' + $checked + ' + 缺 ' + $missingCount + ' ！= 应查 ' + $reqFiles.Count + '）') -ForegroundColor Red
  exit 2
}
if ($skippedNl -gt 0) {
  Write-Host ('  （跳过 ' + $skippedNl + ' 个文件名含换行的条目 —— batch 协议按行分割，无法安全请求）') -ForegroundColor DarkGray
}if ($bad.Count -gt 0) {
  Write-Host ('多 BOM 门禁未通过（' + $bad.Count + ' 个文件有多个前导 BOM，共查 ' + $checked + ' 个）：') -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object)) { Write-Host ('  - ' + $b) -ForegroundColor Red }
  Write-Host '  成因（本仓踩过）：改文件时「去一个 BOM + 加一个 BOM」，多出的没剥掉，逐轮累积。' -ForegroundColor Yellow
  Write-Host '  危害：第二个 BOM 会被当作正文首字符（.ps1 双 BOM 会让 PS5.1 报 ParserError）；clean 过滤器不碰 BOM，会一路进库。' -ForegroundColor Yellow
  Write-Host '  修法：按**连续前导**循环剥净后只前置恰好一个（或直接剥光 —— 无 BOM 也合规）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('多 BOM 合规：查 ' + $checked + ' 个受跟踪文本文件（单BOM=' + $singleCount + ' 无BOM=' + $noneCount + ' 多BOM=0 缺HEAD ' + $missingCount + ' 个）') -ForegroundColor Green
}
exit 0

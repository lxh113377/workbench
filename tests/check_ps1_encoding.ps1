# check_ps1_encoding.ps1 -- PowerShell 脚本编码门禁（含非 ASCII 必须带 UTF-8 BOM）
#
# 为什么需要（2026-10-22 两次实测事故）：
#   ① 本轮新建 check_gofmt / check_gate_links 时没带 BOM -> CI（PS5.1）把中文按 ANSI 解码 ->
#      ParserError，而本机 PS7 一切正常（PS7 默认按 UTF-8 读无 BOM 文件）。
#   ② 修好后又踩一次：编辑 check_tray_window / check_spawn_silent / check_snapshot_build 时
#      写回文件剥掉了 BOM -> 同型 ParserError，三个门禁在 CI 上整体不可执行。
#   两次都是「判据自身坏了」，比缺判据更隐蔽：门禁报的是它自己的语法错，不是被测缺陷。
#
# 判据（任一不满足即 rc=1）：
#   1) tests/ 及仓库根下所有 .ps1/.psm1：若文件含非 ASCII 字节，必须带 UTF-8 BOM。
#      纯 ASCII 脚本不需要 BOM（也允许）。
#   2) 口径：只查**受跟踪**文件（git ls-files）。
#      【2026-10-24 round 79 更正】此处原写「受跟踪与未跟踪都查 —— 未跟踪的脚本一旦入库
#      就会带着缺陷进来」，与下面的实现不符：round 73 性能修复后已改为只取受跟踪列表
#      （理由见下面「性能修复」段：未跟踪的在途件不该判，与 check_gofmt / check_ps1_eol 的
#      HEAD/暂存区口径一致）。**头注比实现旧了两天没人发现** —— 因为 pattern 型门禁
#      （check_comment_staleness）只抓「只剩/共 N」这类数字断言，抓不到语义声明漂移。
#      本次同步头注，并把「声明与实现不一致」列为该门禁的已知盲区（留档，不扩判据 ——
#      扩了就是模糊语义检查，会制造噪声）。
#
# 为什么不是「一律要求 BOM」：BOM 对纯 ASCII 脚本无意义，且会给 diff 增加噪声；
# 判据只针对真实失效面（非 ASCII）。
#
# 用法: powershell -File tests\check_ps1_encoding.ps1 [-Quiet]
# 退出码: 0=合规 / 1=有缺 BOM 的非 ASCII 脚本
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }

# 2026-10-24 性能修复（实测依据：原实现 20.3s，其中 19.8s 花在 Get-ChildItem -Recurse
# 遍历**整个仓库** —— 275,006 个文件，含 .git/src-backup/_recovery/dist 这些巨型目录；
# 而本门禁真正要检查的只有 52 个受跟踪脚本）。
#
# 改用 git ls-files 取受跟踪列表，三个好处：
#   1) 快三个数量级（不再遍历未跟踪/产物目录）；
#   2) **语义更对** —— 门禁判的是「将进版本库的内容」，未跟踪的在途件不该判
#      （与 check_gofmt / check_ps1_eol 的 HEAD/暂存区口径一致）；
#   3) 不再需要那一串目录排除列表（.git/src-backup/... 本来就不该被 git ls-files 返回）。
#
# 保留的历史注记（坑仍在，只是不再触发）：
#   -Include 在 -LiteralPath 下**不生效**（本仓在册坑，实测会扫进全部 4000+ 文件）。
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host "UNVERIFIED: 找不到 git（无法取受跟踪文件列表）" -ForegroundColor Red
  exit 2
}
$tracked = @(& $gitExe -C $Root ls-files '*.ps1' '*.psm1')
if ($LASTEXITCODE -ne 0) {
  Write-Host "UNVERIFIED: git ls-files 失败" -ForegroundColor Red
  exit 2
}
$files = @($tracked | ForEach-Object {
  $rel = $_.Trim('"')
  if (-not $rel) { return $null }
  $full = Join-Path $Root $rel
  if (-not (Test-Path -LiteralPath $full)) { return $null }
  Get-Item -LiteralPath $full
} | Where-Object { $null -ne $_ })

$bad = @()
$checked = 0
foreach ($f in $files) {
  $checked++
  $b = [IO.File]::ReadAllBytes($f.FullName)
  if ($b.Length -eq 0) { continue }
  $hasBom = ($b.Length -ge 3 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF)
  $hasNonAscii = $false
  foreach ($x in $b) { if ($x -gt 127) { $hasNonAscii = $true; break } }
  # 双重 BOM（2026-10-22 实测：我自己两次犯）：EF BB BF 连续出现 2 次会让 PS5.1 把
  # 第二个 BOM 当命令字符，报 `The term '#' is not recognized` / `assignment expression is not valid`。
  # 判据必须覆盖 —— 「补 BOM」这类修复最容易在一个已有 BOM 的文件上再前置一次。
  if ($b.Length -ge 6 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF -and $b[3] -eq 0xEF -and $b[4] -eq 0xBB -and $b[5] -eq 0xBF) {
    $relD = $f.FullName.Substring($Root.Length).TrimStart([char]92, [char]47)
    $bad += ($relD + "  <== 双重 BOM（连出两个 EF BB BF）")
    continue
  }
  if ($hasNonAscii -and -not $hasBom) {
    $rel = $f.FullName.Substring($Root.Length).TrimStart([char]92, [char]47)
    $bad += $rel
  }
}

if ($bad.Count -gt 0) {
  Write-Host ("PowerShell 编码门禁未通过（" + $bad.Count + " 个含中文但缺 UTF-8 BOM，CI 的 PS5.1 会 ParserError）：") -ForegroundColor Red
  foreach ($b in $bad) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "修复: 在文件头补 EF BB BF（不要重写正文，只前置 BOM 字节）" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("PowerShell 编码合规：检查 " + $checked + " 个脚本，含非 ASCII 者均带 BOM") -ForegroundColor Green }
exit 0
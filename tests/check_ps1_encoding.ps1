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
#   2) 受跟踪与未跟踪文件都查 —— 未跟踪的脚本一旦入库就会带着缺陷进来。
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

# 实现注记：-Include 在 -LiteralPath 下**不生效**（本仓在册坑，实测会扫进全部 4000+
# 文件把 .md/.json 也判成脚本）。故显式按扩展名过滤。
$files = @(Get-ChildItem -LiteralPath $Root -Recurse -File -ErrorAction SilentlyContinue | Where-Object {
  $ext = $_.Extension.ToLower()
  if ($ext -ne '.ps1' -and $ext -ne '.psm1') { return $false }
  $p = $_.FullName.Replace([char]92, [char]47)
  # 跳过第三方依赖与产物目录（与 check_noninteractive 同口径）
  foreach ($sd in @('/node_modules/','/.git/','/dist/','/runtime/','/_recovery/','/src-backup/','/xtmp/','/vendor/')) { if ($p.Contains($sd)) { return $false } }
  return $true
})

$bad = @()
$checked = 0
foreach ($f in $files) {
  $checked++
  $b = [IO.File]::ReadAllBytes($f.FullName)
  if ($b.Length -eq 0) { continue }
  $hasBom = ($b.Length -ge 3 -and $b[0] -eq 0xEF -and $b[1] -eq 0xBB -and $b[2] -eq 0xBF)
  $hasNonAscii = $false
  foreach ($x in $b) { if ($x -gt 127) { $hasNonAscii = $true; break } }
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
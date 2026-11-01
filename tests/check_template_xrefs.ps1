# check_template_xrefs.ps1 -- 模板间跨引用的可解析性（2026-11-02 round 238 新增）
#
# 为什么需要（round 236 实测）：quality-gate-spec 里写着「第五条边界（防滥用）」，
#   但该模板编号序列是 ②③⑤⑥⑦⑧⑨⑩㉑㉒㉓ —— 根本没有「第五条」⇒ 读者只能靠位置猜它约束谁。
#   这类「悬空引用」读起来完全通顺，编译器与既有门禁都抓不到，只有逐条核对编号集合才发现。
#
# 它守的是什么：模板里所有形如 `quality-verify` ⑮ / `quality-audit` ⑤ 的跨模板引用，
#   其指向的编号必须在**目标模板的编号集合**里真实存在。
#
# 三态口径（与全仓一致，round 61 在册事故：把没跑写成 rc=0 会把「未执行」洗成 PASS）：
#   rc=0 PASS —— 无悬空引用
#   rc=1 FAIL —— 存在悬空引用，逐条列出 line/from/to/circ
#   rc=2 UNVERIFIED —— node 能跑但输出无 XREF 结论行（语义不明，不计 PASS）
#   rc=3 SKIP —— 找不到 node 或工具件不存在（测不了 ≠ 不合格，也不许判红）
#
# 用法: powershell -File tests\check_template_xrefs.ps1 [-Quiet]
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$tool = Join-Path $root 'setup\check-template-xrefs.js'
if (-not (Test-Path $tool)) {
  if (-not $Quiet) { Write-Host "SKIP: 检查工具件不存在（测不了 ≠ 不合格）" -ForegroundColor Yellow }
  exit 3
}
$nodeCmd = Get-Command node -ErrorAction SilentlyContinue
if (-not $nodeCmd) {
  if (-not $Quiet) { Write-Host "SKIP: 找不到 node（测不了 ≠ 不合格）" -ForegroundColor Yellow }
  exit 3
}
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $nodeCmd.Source
$psi.Arguments = [char]34 + $tool + [char]34
$psi.WorkingDirectory = $root
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
try { $proc = [System.Diagnostics.Process]::Start($psi) } catch {
  Write-Host ("UNVERIFIED: 无法启动 node 进程（" + $_.Exception.Message + "）") -ForegroundColor Yellow
  exit 2
}
$so = $proc.StandardOutput.ReadToEnd()
$se = $proc.StandardError.ReadToEnd()
$proc.WaitForExit()
$text = ($so + $se).Trim()
# 不只看退出码：必须真看到结论行，否则可能是「什么都没跑出来」的空绿。
if (-not ($text -match "XREF_OK")) {
  if (-not $Quiet) { Write-Host ("UNVERIFIED: 无 XREF_OK 结论行（语义不明，不计 PASS）：" + $text) -ForegroundColor Yellow }
  exit 2
}
# 结论行里带着悬空条数：XREF_OK dangling=<n>
$m = [regex]::Match($text, "XREF_OK dangling=(\d+)")
if (-not $m.Success) { if (-not $Quiet) { Write-Host "UNVERIFIED: 无法解析悬空条数" -ForegroundColor Yellow }; exit 2 }
$n = [int]$m.Groups[1].Value
if ($n -gt 0) {
  Write-Host ("FAIL: " + $n + " 处悬空引用（读者无法定位其约束对象）") -ForegroundColor Red
  Write-Host $text -ForegroundColor Red
  exit 1
}
if (-not $Quiet) { Write-Host "PASS: 模板间跨引用全部可解析" -ForegroundColor Green }
exit 0

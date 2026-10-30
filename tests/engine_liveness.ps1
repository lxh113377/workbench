# engine_liveness.ps1 - WorkBench 实例存活体检（多通道交叉判定，防单通道假阴性）
#
# 起因（2026-10-30，本会话第 3 次同一形态误判）：三次报出 engine=2 / agentEngine=0 / 实例=0，
# 全错在同一处：含 Windows 路径的判据写在 JS 字符串或内联命令里时反斜杠被吞，
# 于是"按路径匹配"的计数恒为 0，我就断言出"引擎没在跑"。
# 那是探针缺陷、不是产品缺陷，但更危险：它让线上验证产出假结论，进而触发不必要的修复。
#
# 判据设计：任何单一通道都不许独立下"没在跑"的结论。三通道投票：
#   1 进程：CIM 按可执行文件路径匹配（路径一律 Join-Path 逐段拼，源码里不出现单反斜杠字面量）
#   2 面板：HTTP 200（端口读 runtime 下的 panel.port，读不到回落 17890）
#   3 日志：mtime 是否落在 WindowMinutes 窗口内
# 大于等于 2 通道判活 => ALIVE；0 通道 => DEAD；仅 1 通道 => UNKNOWN（拒绝靠单通道猜）。
#
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File tests/engine_liveness.ps1 [-Root D:/x] [-SelfTest]
# 期望末行：SELFTEST PASS 或 RESULT=ALIVE / RESULT=DEAD / RESULT=UNKNOWN

param(
  [string]$Root = "",
  [int]$WindowMinutes = 10,
  [switch]$SelfTest
)

$ErrorActionPreference = "Continue"
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Join-Path "D:" "workbench-agent" }

function Get-EnginePath([string]$root) { return Join-Path $root "workbench.exe" }

function Get-PortOrDefault([string]$root) {
  $pf = Join-Path (Join-Path $root "runtime") "panel.port"
  if (Test-Path $pf) {
    $v = 0
    $raw = (Get-Content $pf -Raw).Trim()
    if ([int]::TryParse($raw, [ref]$v) -and $v -gt 0) { return $v }
  }
  return 17890
}

function Test-ChannelProcess([string]$root) {
  $want = Get-EnginePath -root $root
  $ps = @(Get-CimInstance Win32_Process -Filter "Name=""workbench.exe""" -ErrorAction SilentlyContinue |
         Where-Object { $_.ExecutablePath -eq $want })
  $d = "实例数=" + $ps.Count + " 期望=[" + $want + "]"
  return @{ alive = ($ps.Count -gt 0); detail = $d }
}

function Test-ChannelPanel([string]$root) {
  $port = Get-PortOrDefault -root $root
  try {
    $resp = Invoke-WebRequest -Uri ("http://127.0.0.1:" + $port + "/") -UseBasicParsing -TimeoutSec 8
    $d = "端口 " + $port + " HTTP " + $resp.StatusCode
    return @{ alive = ($resp.StatusCode -eq 200); detail = $d }
  } catch {
    return @{ alive = $false; detail = ("端口 " + $port + " 无响应") }
  }
}

function Test-ChannelLog([string]$root, [int]$win) {
  $lg = Join-Path (Join-Path (Join-Path $root "workbench-output") "logs") "workbench.jsonl"
  if (-not (Test-Path $lg)) { return @{ alive = $false; detail = "日志文件不存在" } }
  $ageMin = [Math]::Round(((Get-Date) - (Get-Item $lg).LastWriteTime).TotalMinutes, 1)
  $d = "日志距今 " + $ageMin + " 分钟（窗口 " + $win + "）"
  return @{ alive = ($ageMin -le $win); detail = $d }
}

function Get-Verdict([string]$root, [int]$win) {
  $c1 = Test-ChannelProcess -root $root
  $c2 = Test-ChannelPanel -root $root
  $c3 = Test-ChannelLog -root $root -win $win
  $yes = 0
  if ($c1.alive) { $yes++ }
  if ($c2.alive) { $yes++ }
  if ($c3.alive) { $yes++ }
  $v = "UNKNOWN"
  if ($yes -ge 2) { $v = "ALIVE" } elseif ($yes -eq 0) { $v = "DEAD" }
  Write-Host ("  通道1 进程 : " + $(if ($c1.alive) { "活" } else { "死" }) + " ｜ " + $c1.detail)
  Write-Host ("  通道2 面板 : " + $(if ($c2.alive) { "活" } else { "死" }) + " ｜ " + $c2.detail)
  Write-Host ("  通道3 日志 : " + $(if ($c3.alive) { "活" } else { "死" }) + " ｜ " + $c3.detail)
  Write-Host ("  投票 活=" + $yes + "/3 => " + $v)
  return @{ verdict = $v; c1 = $c1.alive; c2 = $c2.alive; c3 = $c3.alive }
}

if ($SelfTest) {
  $fail = 0
  $p = Get-EnginePath -root (Join-Path "D:" "x")
  if ($p -notmatch "workbench[.]exe$") { Write-Host "自检1) 失败：Join-Path 未产出可执行路径"; $fail++ }
  else { Write-Host ("自检1) 通过：路径拼装=[" + $p + "]") }
  $bogus = Get-Verdict -root (Join-Path "D:" "no-such-workbench-root-9f3c") -win 10
  if ($bogus.c1 -or $bogus.c3) { Write-Host "自检2) 失败：不存在的根被判活（判据过松）"; $fail++ }
  else { Write-Host "自检2) 通过：不存在的根 通道1/3 均判死" }
  if ($bogus.verdict -eq "ALIVE") { Write-Host "自检3) 失败：不存在的根结论 ALIVE"; $fail++ }
  else { Write-Host ("自检3) 通过：不存在的根结论 " + $bogus.verdict) }
  $t = Get-Verdict -root $Root -win $WindowMinutes
  $y = 0
  if ($t.c1) { $y++ }
  if ($t.c2) { $y++ }
  if ($t.c3) { $y++ }
  $expect = "UNKNOWN"
  if ($y -ge 2) { $expect = "ALIVE" } elseif ($y -eq 0) { $expect = "DEAD" }
  if ($t.verdict -ne $expect) { Write-Host "自检4) 失败：投票结论与通道数不符"; $fail++ }
  else { Write-Host ("自检4) 通过：活通道 " + $y + "/3 => " + $t.verdict) }
  if ($fail -gt 0) { Write-Host "SELFTEST FAIL"; exit 1 }
  Write-Host "SELFTEST PASS"
  exit 0
}

Write-Host ("=== 引擎存活体检（根 " + $Root + "）===")
$r = Get-Verdict -root $Root -win $WindowMinutes
Write-Host ("RESULT=" + $r.verdict)
if ($r.verdict -eq "DEAD") { exit 1 }
exit 0

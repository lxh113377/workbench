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
  [switch]$SelfTest,
  # -Strict 把「交付完整性」并入退出码：校验和缺失/与实体不符 ⇒ 退 1。
  # 默认不参与投票 —— 校验和文件陈旧不代表引擎没在跑（那是两件事，混投票会假报 DEAD）。
  [switch]$Strict
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

# 通道4（完整性，不参与存活投票）：exe 的 SHA256 是否等于同目录 SHA256SUMS 里的 token。
//
// 为什么需要它（2026-10-30 13:31 现场）：部署脚本用的是"若 dist 里有 SHA256SUMS 就一起拷贝"，
// 而 setup/build.ps1 -SkipTests **不产**这个文件 ⇒ agent 根的 SUMS 停在几小时前的旧版，
// 于是"校验和一致=False"。真因不是二进制错（vcs.revision 明摆着等于 HEAD），
// 而是**验收用的对照物本身过期了** —— 拿一个过期的对照物去验，要么假红要么假绿。
// ⇒ 把这件事做成一条可复算的通道：要么 OK，要么明确 STALE / MISSING，别靠人记得手工重算。
function Test-ChannelIntegrity([string]$root) {
  $exe = Get-EnginePath -root $root
  $sums = Join-Path $root "SHA256SUMS"
  if (-not (Test-Path $exe)) { return @{ state = "MISSING-EXE"; detail = "找不到可执行文件" } }
  if (-not (Test-Path $sums)) { return @{ state = "MISSING"; detail = "无 SHA256SUMS 对照物" } }
  $tok = ""
  foreach ($line in (Get-Content $sums -ErrorAction SilentlyContinue)) {
    $s = ("$line").Trim()
    if ($s -match "^[0-9a-fA-F]{64}") { $tok = ($s -split "\s+")[0]; break }
  }
  if ($tok -eq "") { return @{ state = "MISSING"; detail = "SHA256SUMS 里没有可用的 64 位摘要" } }
  $h = (Get-FileHash $exe -Algorithm SHA256).Hash
  $ok = (0 -eq [String]::Compare($h, $tok, $true))
  $age = [Math]::Round(((Get-Item $exe).LastWriteTime - (Get-Item $sums).LastWriteTime).TotalMinutes, 1)
  if ($ok) { return @{ state = "OK"; detail = ("摘要一致（SUMS 比 exe 旧 " + $age + " 分钟）") } }
  return @{ state = "STALE"; detail = ("exe=" + $h.Substring(0, 12) + "… SUMS=" + $tok.Substring(0, 12) + "…（SUMS 比 exe 旧 " + $age + " 分钟 ⇒ 部署时未随实体一起更新）") }
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
  # 自检5-7：完整性通道必须有牙 —— 造一个"实体新、对照物旧"的临时根（正是 13:31 的现场形态）
  $tmp = Join-Path ([IO.Path]::GetTempPath()) ("wb-liveness-" + [Guid]::NewGuid().ToString("N"))
  New-Item -ItemType Directory -Path $tmp -Force | Out-Null
  try {
    $fakeExe = Join-Path $tmp "workbench.exe"
    [IO.File]::WriteAllBytes($fakeExe, [Text.Encoding]::UTF8.GetBytes("payload-v1"))
    $h1 = (Get-FileHash $fakeExe -Algorithm SHA256).Hash
    [IO.File]::WriteAllText((Join-Path $tmp "SHA256SUMS"), ($h1 + "  workbench.exe"))
    $g1 = Test-ChannelIntegrity -root $tmp
    if ($g1.state -ne "OK") { Write-Host ("自检5) 失败：一致的摘要被判 " + $g1.state); $fail++ }
    else { Write-Host "自检5) 通过：摘要一致判 OK" }
    # 换实体但不动 SUMS ⇒ 必须 STALE（这就是部署时忘记重算 SUMS 的形态）
    [IO.File]::WriteAllBytes($fakeExe, [Text.Encoding]::UTF8.GetBytes("payload-v2-different"))
    Start-Sleep -Seconds 1
    $g2 = Test-ChannelIntegrity -root $tmp
    if ($g2.state -ne "STALE") { Write-Host ("自检6) 失败：实体变了而对照物未变，应判 STALE，实得 " + $g2.state); $fail++ }
    else { Write-Host "自检6) 通过：陈旧对照物被抓住（STALE）" }
    Remove-Item (Join-Path $tmp "SHA256SUMS") -Force
    $g3 = Test-ChannelIntegrity -root $tmp
    if ($g3.state -notmatch "MISSING") { Write-Host ("自检7) 失败：缺对照物应判 MISSING，实得 " + $g3.state); $fail++ }
    else { Write-Host ("自检7) 通过：缺对照物判 " + $g3.state) }
  } finally {
    # 临时件用完即删（写面纪律：不在仓库根、temp 里也不留残留）
    if (Test-Path $tmp) { Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue }
    if (Test-Path $tmp) { Write-Host "自检8) 失败：临时目录未清理"; $fail++ }
    else { Write-Host "自检8) 通过：临时目录已清理" }
  }
  if ($fail -gt 0) { Write-Host "SELFTEST FAIL"; exit 1 }
  Write-Host "SELFTEST PASS"
  exit 0
}

Write-Host ("=== 引擎存活体检（根 " + $Root + "）===")
$r = Get-Verdict -root $Root -win $WindowMinutes
Write-Host ("RESULT=" + $r.verdict)
$i = Test-ChannelIntegrity -root $Root
Write-Host ("  通道4 完整性 : " + $i.state + " ｜ " + $i.detail)
Write-Host ("INTEGRITY=" + $i.state)
if ($r.verdict -eq "DEAD") { exit 1 }
if ($Strict -and $i.state -ne "OK") {
  Write-Host "   Strict：交付完整性未过（校验和缺失或与实体不符 ⇒ 部署验收不成立，须重生成 SUMS）"
  exit 1
}
exit 0

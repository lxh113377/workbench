# check_perf.ps1 -- 性能基线门禁：把 docs/benchmark/perf-baseline.json 变成 CI 可跑的判据。
#
# 为什么需要（2026-10-20，知识盲区 B8 / 学习计划 L8）：
#   对标报告 §3.5 的性能读数（首页 11ms、EXE 7.74MB、内存 28.9MB、句柄 464）全是**手工单次读数**，
#   没有进 CI，防不住回归；ci.yml 的 Verify artifact 只有一条硬编码 3MB..40MB 的区间断言。
#   本件把读数落成数据件 + 机器判据，并对齐 river/temporal 的做法：产物断言挂 build job。
#
# 三态语义（与 tests/check_all.ps1 的「SKIP 不计入 PASS」同一口径，绝不把没测成当测得过）：
#   rc=0 PASS       —— 至少做成一项判定且全部通过；未做的判定逐条打印 SKIP + 原因
#   rc=1 FAIL       —— 做成的判定里有超上界（含 0 字节空产物）
#   rc=2 UNVERIFIED —— 无法判定：产物不存在 / 基线缺失或解析失败 / 一项判定都没做成 /
#                       被显式要求的检查取不到读数（例如 -Runtime 但探针起不来）
#
# 判据范围：体积上界恒判；运行期读数（响应时间/常驻内存/句柄）只在显式 -Runtime 时判，
#   因为基线读数是本机取证、CI runner 机型与噪声不同（见 docs/benchmark/性能基线口径.md §5）。
#
# 用法: pwsh -File tests/check_perf.ps1 -Exe dist/workbench.exe [-Baseline <json>] [-Runtime] [-Quiet]
# 退出码: 0=PASS / 1=FAIL / 2=UNVERIFIED
#
# 零人工交互自证：无 Read-Host、无按键等待构造；-Exe 故意**不做 Mandatory** ——
#   缺参时 PowerShell 会交互式提示等待输入（实测坑），这里改为直接判 rc=2 并说明。
#   全部 Invoke-WebRequest 带 -UseBasicParsing（tests/check_noninteractive.ps1 的判据）。
#   只用纯字符串/JSON 解析，不碰正则转义（照 tests/check_changelog.ps1 的实现注记避坑）。
param(
  [string]$Exe = "",
  [string]$Baseline = "",
  [switch]$Runtime,
  # 仅用于变异测试自证（把上界临时压低看是否判红）；CI 不传。生效时会打醒目告警。
  [long]$MaxBytesOverride = 0,
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }

$root = Split-Path -Parent $PSScriptRoot
$verdict = "UNVERIFIED"
$passed = 0; $failed = 0; $skipped = 0; $unable = 0
$notes = @()

function Skip([string]$check, [string]$why) {
  $script:skipped++
  Write-Host ("CHECK {0} result=SKIP reason={1}" -f $check, $why) -ForegroundColor Yellow
}
function Unable([string]$check, [string]$why) {
  $script:unable++
  Write-Host ("CHECK {0} result=UNVERIFIED reason={1}" -f $check, $why) -ForegroundColor DarkYellow
}

# ---------- 前置：产物与基线都必须拿得到，否则无从判定 ----------
if ([string]::IsNullOrWhiteSpace($Exe)) {
  Write-Host "UNVERIFIED: 未给 -Exe（缺参不提示、直接判 rc=2，保证零人工交互）" -ForegroundColor Red
  Write-Host "用法: pwsh -File tests/check_perf.ps1 -Exe dist/workbench.exe"
  exit 2
}
$exePath = $Exe
if (-not [IO.Path]::IsPathRooted($exePath)) { $exePath = Join-Path $root $Exe }
if (-not (Test-Path -LiteralPath $exePath)) {
  Write-Host ("UNVERIFIED: 产物不存在: " + $exePath) -ForegroundColor Red
  Write-Host "  「没产物」不等于「性能合格」，也不等于「性能不合格」——判 rc=2。" -ForegroundColor DarkYellow
  exit 2
}
if ([string]::IsNullOrWhiteSpace($Baseline)) { $Baseline = Join-Path $root "docs\benchmark\perf-baseline.json" }
if (-not (Test-Path -LiteralPath $Baseline)) {
  Write-Host ("UNVERIFIED: 基线数据件不存在: " + $Baseline) -ForegroundColor Red
  Write-Host "  没有基线就没有上界，任何读数都无从判定。" -ForegroundColor DarkYellow
  exit 2
}
$bl = $null
try { $bl = ([IO.File]::ReadAllText($Baseline) | ConvertFrom-Json) }
catch {
  Write-Host ("UNVERIFIED: 基线 JSON 解析失败: " + $Baseline + " ; " + $_.Exception.Message) -ForegroundColor Red
  exit 2
}
if ($null -eq $bl) { Write-Host ("UNVERIFIED: 基线为空对象: " + $Baseline) -ForegroundColor Red; exit 2 }

Say ("=== 性能基线门禁 ===  基线: " + (Split-Path -Leaf $Baseline) + " schemaVersion=" + $bl.schemaVersion + " 取证于 " + $bl.capturedAt)
Say ("    产物: " + $exePath)

# ---------- 判定 1：EXE 体积 <= exeMaxBytes ----------
$size = 0
$size = (Get-Item -LiteralPath $exePath).Length
Say ("    实测体积: " + $size + " bytes")
$bound = $null
if ($null -ne $bl.exeMaxBytes) { $bound = [long]$bl.exeMaxBytes }
if ($MaxBytesOverride -gt 0) {
  Write-Host ("    告警: -MaxBytesOverride=" + $MaxBytesOverride + " 已覆盖基线 exeMaxBytes=" + $bound + "（仅自证用途，CI 禁止传）") -ForegroundColor Magenta
  $bound = $MaxBytesOverride
}
if ($null -eq $bound -or $bound -le 0) {
  # 注：整串必须括号包成一个参数——裸写 "a" + $x + "b" 会被 PowerShell 拆成多个位置参数，只传进第一段。
  Skip "exeSize" ("基线 exeMaxBytes 为 " + $(if ($null -eq $bl.exeMaxBytes) { "null（本项未实测，严禁臆造）" } else { "$bound（非法值）" }) + " —— 无上界可判")
}
elseif ($size -le 0) {
  $failed++
  Write-Host ("CHECK exeSize result=FAIL reason=空产物 " + $size + " bytes（体积上界对 0 字节无意义，单列判红）") -ForegroundColor Red
}
elseif ($size -le $bound) {
  $passed++
  Say ("CHECK exeSize result=PASS value=" + $size + " bound=" + $bound + " 余量=" + [math]::Round(($bound - $size) / 1MB, 2) + "MB")
}
else {
  $failed++
  Write-Host ("CHECK exeSize result=FAIL value=" + $size + " bound=" + $bound + " 超出=" + [math]::Round(($size - $bound) / 1MB, 2) + "MB（体积膨胀回归，查新增依赖/资产）") -ForegroundColor Red
}

# ---------- 判定 1b：EXE 体积 >= exeMinBytes（双向判之下界，issue #8 判据 3） ----------
# 上界拦「膨胀」，下界拦「缩水」：单 EXE 内联全部前端资产与包，体积跌破上一代完整功能基准
# 只可能是构建半成品/资产丢失/误裁包 —— 那和膨胀一样是回归，只是方向相反。
$minBound = $null
if ($null -ne $bl.exeMinBytes) { $minBound = [long]$bl.exeMinBytes }
if ($null -eq $minBound -or $minBound -le 0) {
  Skip "exeSizeMin" ("基线 exeMinBytes 为 " + $(if ($null -eq $bl.exeMinBytes) { "null（本项未实测，严禁臆造）" } else { "$minBound（非法值）" }) + " —— 无下界可判")
}
elseif ($size -le 0) {
  # 空产物已在判定 1 单列判红，这里不重复计数（同一次失败只记一次，防双计虚高失败面）。
}
elseif ($size -ge $minBound) {
  $passed++
  Say ("CHECK exeSizeMin result=PASS value=" + $size + " bound=" + $minBound + " 余量=" + [math]::Round(($size - $minBound) / 1MB, 2) + "MB")
}
else {
  $failed++
  Write-Host ("CHECK exeSizeMin result=FAIL value=" + $size + " bound=" + $minBound + " 缺失=" + [math]::Round(($minBound - $size) / 1MB, 2) + "MB（体积异常缩水：内联资产/包丢失或构建半成品）") -ForegroundColor Red
}

# ---------- 判定 2-4：运行期读数（需 -Runtime 且基线字段非 null） ----------
function Get-FreePort([int]$Start) {
  for ($p = $Start; $p -lt ($Start + 40); $p++) {
    $c = Get-NetTCPConnection -State Listen -LocalPort $p -ErrorAction SilentlyContinue
    if (-not $c) { return $p }
  }
  return 0
}

$probe = $null
if (-not $Runtime) {
  $nullFields = @()
  foreach ($k in @("homeP95Ms", "residentMBMax", "handleMax", "taskDurationSecMax", "readyMsMax", "idleGrowthMBPerHourMax")) {
    if ($null -eq $bl.$k) { $nullFields += $k }
  }
  foreach ($k in @("homeP95Ms", "residentMBMax", "handleMax")) {
    if ($null -ne $bl.$k) {
      Skip $k ("未指定 -Runtime（基线该值为本机取证读数，CI runner 口径不可比，见 性能基线口径.md §5）。bound=" + $bl.$k)
    }
  }
  if ($nullFields.Count -gt 0) {
    $why = "基线字段为 null = 未实测（原因见基线 nullFieldReasons）：" + ($nullFields -join ", ")
    foreach ($k in $nullFields) { Skip $k $why }
    Say ("    null 字段口径：" + $why)
  }
}
else {
  # 明确要求的运行期检查：起一次探针，读三项指标
  foreach ($k in @("homeP95Ms", "residentMBMax", "handleMax")) {
    if ($null -eq $bl.$k) {
      $nullReason = ""
      try { $nullReason = [string]$bl.nullFieldReasons.$k } catch { $nullReason = "" }
      Skip $k ("基线该字段为 null（未实测，严禁拿别的数冒充实测）。" + $(if ($nullReason) { " 原因: " + $nullReason } else { "" }))
    }
  }
  $measurable = @()
  foreach ($k in @("homeP95Ms", "residentMBMax", "handleMax")) { if ($null -ne $bl.$k) { $measurable += $k } }
  if ($measurable.Count -eq 0) {
    Unable "runtime" "-Runtime 已指定但基线三项运行期字段全为 null，无可判读数"
  }
  else {
    $port = Get-FreePort 20770
    $dir = Join-Path $env:TEMP ("workbench-perf-" + (Get-Date -Format "yyyyMMddHHmmss") + "-" + (Get-Random -Maximum 9999))
    $procId = 0
    try {
      if ($port -le 0) { throw "找不到空闲端口（20770 起 40 个都被占用）" }
      New-Item -ItemType Directory -Force -Path $dir | Out-Null
      $p = Start-Process -FilePath $exePath -ArgumentList @("--ui-probe", "--dir", $dir, "--port", [string]$port, "--seconds", "120", "--expect-idle") -WorkingDirectory (Split-Path -Parent $exePath) -WindowStyle Hidden -PassThru
      $procId = $p.Id
      $ready = $false
      $url = "http://127.0.0.1:" + $port + "/"
      for ($i = 0; $i -lt 120; $i++) {
        try { $null = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 2; $ready = $true; break }
        catch { Start-Sleep -Milliseconds 250 }
      }
      if (-not $ready) { throw "探针 30s 内未就绪（--ui-probe 通道不可用或分发表缺该格）" }
      for ($i = 0; $i -lt 5; $i++) { $null = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 5 }
      $lat = @()
      for ($i = 0; $i -lt 40; $i++) {
        $t = Measure-Command { $null = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 5 }
        $lat += $t.TotalMilliseconds
        Start-Sleep -Milliseconds 30
      }
      $srt = @($lat | Sort-Object)
      if ($srt.Count -lt 20) { throw ("延迟样本不足：" + $srt.Count + "/40（中途取不到读数，不能当成通过）") }
      $p95 = $srt[[int]([math]::Ceiling(0.95 * $srt.Count)) - 1]
      $q = Get-Process -Id $procId
      $probe = [pscustomobject]@{
        p95Ms = [math]::Round($p95, 2)
        n = $srt.Count
        p50Ms = [math]::Round($srt[[int]([math]::Ceiling(0.50 * $srt.Count)) - 1], 2)
        maxMs = [math]::Round($srt[-1], 2)
        residentMB = [math]::Round($q.WorkingSet64 / 1MB, 2)
        handles = $q.HandleCount
        threads = $q.Threads.Count
        port = $port
        sha256 = (Get-FileHash -LiteralPath $exePath -Algorithm SHA256).Hash
        bytes = $size
        capturedAt = (Get-Date).ToString("o")
      }
      Say ("    探针读数: P50=" + $probe.p50Ms + "ms P95=" + $probe.p95Ms + "ms MAX=" + $probe.maxMs + "ms (n=" + $probe.n + ") 常驻=" + $probe.residentMB + "MB 句柄=" + $probe.handles + " 线程=" + $probe.threads)
    }
    catch {
      foreach ($k in $measurable) { Unable $k ("-Runtime 要求的读数未取到: " + $_.Exception.Message) }
    }
    finally {
      if ($procId -gt 0) { Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue }
      # 只删自己刚建的那个目录：必须在 TEMP 根下、且目录名是本次的前缀。
      # （不要用 -replace 转分隔符：-replace 的模式是正则，反斜杠在正则里非法，实测报
      #  "The regular expression pattern \ is not valid" 并把清理整段跳过，留下垃圾目录。）
      $tmpRoot = [IO.Path]::GetFullPath($env:TEMP)
      $full = [IO.Path]::GetFullPath($dir)
      if ($full.StartsWith($tmpRoot, [StringComparison]::OrdinalIgnoreCase) -and $full.Contains("workbench-perf-")) {
        Remove-Item -LiteralPath $full -Recurse -Force -ErrorAction SilentlyContinue
      }
    }
    if ($probe) {
      foreach ($k in $measurable) {
        $b = [double]$bl.$k
        if ($k -eq "homeP95Ms") { $v = $probe.p95Ms }
        elseif ($k -eq "residentMBMax") { $v = $probe.residentMB }
        else { $v = $probe.handles }
        if ($v -le $b) {
          $passed++
          Say ("CHECK {0} result=PASS value={1} bound={2}" -f $k, $v, $b)
        }
        else {
          $failed++
          Write-Host ("CHECK {0} result=FAIL value={1} bound={2}（运行期回归，口径见 性能基线口径.md §3）" -f $k, $v, $b) -ForegroundColor Red
        }
      }
    }
  }
}

# ---------- 汇总：FAIL > UNVERIFIED > PASS ----------
Write-Host "----------------------------------------" -ForegroundColor DarkGray
if ($failed -gt 0) { $verdict = "FAIL"; $rc = 1 }
elseif ($passed -eq 0) { $verdict = "UNVERIFIED"; $rc = 2 }
elseif ($unable -gt 0) { $verdict = "UNVERIFIED"; $rc = 2 }
else { $verdict = "PASS"; $rc = 0 }
Write-Host ("判定 {0} 已做成={1} 超界={2} SKIP={3} 取不到读数={4}" -f $verdict, $passed, $failed, $skipped, $unable) -ForegroundColor $(if ($rc -eq 0) { "Green" } elseif ($rc -eq 1) { "Red" } else { "DarkYellow" })
Write-Host ("VERDICT: " + $verdict + " rc=" + $rc)
exit $rc

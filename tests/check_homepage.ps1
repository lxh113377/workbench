# check_homepage.ps1 -- 首页/入口契约门禁：产物里必须是**当前源码**的首页，而不是旧页面。
#
# 为什么需要（2026-10-20 现场，老大："打开的是旧的页面,已经有新的了"）：
#   dist\workbench.exe 在 21:52 被重建成 9.65MB 的旧版 —— 里面是**旧首页模板**
#   （<div class="home-badge"> + 「运行中」+「引擎状态：」+「开始运行」+ 隐藏的 · 入口），
#   同时**没有** --ui-probe 这一格（二进制里找不到 UI_PROBE_READY），
#   于是任何探针拉起都会静默回落成 GUI 启动。而工作区源码早已是新版首页
#   （<nav class="home-entries"> 管理面板/模型池/治理 + 按钮 data-state）。
#   根因：产物是从**旧树**（src-backup / _recovery 那类 home-badge 版）构建后覆盖进来的，
#   而当时的门禁只查 PE 子系统（GUI/不弹窗）和体积，没有任何一条判据看「页面内容是不是当前源码的」。
#
# 判据（三条，都从产物字节 + 实际 HTTP 响应取证，不看源码、不看注释）：
#   1) 产物里必须含新首页锚点 home-entries，且**不得**含旧首页锚点 home-badge
#   2) 产物里必须含 --ui-probe 通道锚点 UI_PROBE_READY（否则探针静默回落成 GUI）
#   3) 起一次真探针，抓 HTTP 首页，断言响应里同为新页（防止"字节里有、路由没用上"）
#
# 用法:
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\check_homepage.ps1 -Exe dist\workbench.exe
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\check_homepage.ps1 -Exe dist\workbench.exe -SkipRuntime
# 退出码: 0=PASS / 1=FAIL / 2=UNVERIFIED（产物不存在或运行时检查起不来）

param(
  [string]$Exe = "",
  [string]$SrcRoot = "",
  [int]$Port = 0,
  [switch]$SkipRuntime,
  [switch]$Quiet
)

$ErrorActionPreference = "Stop"
$ConfirmPreference = "None"
$ProgressPreference = "SilentlyContinue"
$ConfirmPreference = "None"

function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }

$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path $root "dist\workbench.exe" }
if (-not (Test-Path -LiteralPath $Exe)) {
  Write-Host ("UNVERIFIED: 产物不存在: " + $Exe) -ForegroundColor Red
  exit 2
}
if ([string]::IsNullOrWhiteSpace($SrcRoot)) { $SrcRoot = Join-Path $root "workbench\internal\web" }
$srcHTML = Join-Path $SrcRoot "html.go"
if (-not (Test-Path -LiteralPath $srcHTML)) {
  Write-Host ("UNVERIFIED: 源码首页模板不存在: " + $srcHTML) -ForegroundColor Red
  exit 2
}

# ---------- 判定 1-2：产物字节里的页面锚点 ----------
$bytes = [IO.File]::ReadAllBytes($Exe)
$text = [Text.Encoding]::UTF8.GetString($bytes)
$hasNew = $text.Contains("home-entries")
$hasOld = $text.Contains("home-badge")
$hasProbe = $text.Contains("UI_PROBE_READY")

# 以源码为准决定"应该有什么"：源码里没有 home-entries 说明新页还没落地，本门禁自然要判断点别的
$src = [IO.File]::ReadAllText($srcHTML)
$srcNew = $src.Contains("home-entries")
$srcOld = $src.Contains("home-badge")
Say ("=== 首页契约门禁 ===  产物: " + $Exe)
Say ("    源码模板: new=" + $srcNew + " old=" + $srcOld + "   产物: new=" + $hasNew + " old=" + $hasOld + " probe=" + $hasProbe)

$failed = 0
if ($srcNew -and -not $hasNew) {
  Write-Host ("CHECK homeMarker result=FAIL 产物缺少新首页锚点 home-entries —— 产物与当前源码不是同一棵树") -ForegroundColor Red
  $failed++
} elseif ($srcNew) {
  Say "CHECK homeMarker result=PASS 产物含新首页锚点 home-entries"
}
if ($hasOld -and -not $srcOld) {
  Write-Host ("CHECK staleMarker result=FAIL 产物含旧首页锚点 home-badge —— 旧树产物盖回来了") -ForegroundColor Red
  $failed++
} elseif (-not $hasOld) {
  Say "CHECK staleMarker result=PASS 产物无旧首页锚点 home-badge"
}
if (-not $hasProbe) {
  Write-Host ("CHECK probeChannel result=FAIL 产物缺 UI_PROBE_READY —— --ui-probe 会静默回落成 GUI 启动") -ForegroundColor Red
  $failed++
} else {
  Say "CHECK probeChannel result=PASS 产物含 --ui-probe 通道"
}

# ---------- 判定 3：真探针抓 HTTP 首页 ----------
if (-not $SkipRuntime -and $failed -eq 0) {
  $usedPort = $Port
  if ($usedPort -le 0) {
    for ($p = 20890; $p -lt 20930; $p++) {
      $c = Get-NetTCPConnection -State Listen -LocalPort $p -ErrorAction SilentlyContinue
      if (-not $c) { $usedPort = $p; break }
    }
  }
  if ($usedPort -le 0) {
    Write-Host "CHECK livePage result=SKIP 找不到空闲端口（20890 起 40 个都被占用）" -ForegroundColor Yellow
  } else {
    $dir = Join-Path $env:TEMP ("wb-homepage-gate-" + (Get-Date -Format "yyyyMMddHHmmss") + "-" + (Get-Random -Maximum 9999))
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $procId = 0
    try {
      $cl = '"' + $Exe + '" --ui-probe --dir "' + $dir + '" --port ' + $usedPort + ' --seconds 40 --expect-idle'
      $rr = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $cl; CurrentDirectory = (Split-Path -Parent $Exe) }
      if ($rr.ReturnValue -ne 0) { throw ("探针进程创建失败 rc=" + $rr.ReturnValue) }
      $procId = [int]$rr.ProcessId
      $url = "http://127.0.0.1:" + $usedPort + "/"
      $page = $null
      for ($i = 0; $i -lt 60; $i++) {
        try { $page = (Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 2).Content; break } catch { Start-Sleep -Milliseconds 300 }
      }
      if (-not $page) { throw "探针 18s 内未就绪（--ui-probe 不可用或端口被占）" }
      $liveNew = $page.Contains("home-entries")
      $liveOld = $page.Contains("home-badge")
      if ($liveNew -and -not $liveOld) {
        Say ("CHECK livePage result=PASS HTTP 首页为新页（len=" + $page.Length + "）")
      } else {
        Write-Host ("CHECK livePage result=FAIL HTTP 首页不符：new=" + $liveNew + " old=" + $liveOld + "（产物字节里有新页但路由没用上？）") -ForegroundColor Red
        $failed++
      }
    } catch {
      Write-Host ("CHECK livePage result=UNVERIFIED " + $_.Exception.Message) -ForegroundColor DarkYellow
      $failed += 100
    } finally {
      if ($procId -gt 0) { Stop-Process -Id $procId -Force -ErrorAction SilentlyContinue }
      $tmpRoot = [IO.Path]::GetFullPath($env:TEMP)
      $full = [IO.Path]::GetFullPath($dir)
      if ($full.StartsWith($tmpRoot, [StringComparison]::OrdinalIgnoreCase) -and $full.Contains("wb-homepage-gate-")) {
        Remove-Item -LiteralPath $full -Recurse -Force -ErrorAction SilentlyContinue
      }
    }
  }
} elseif ($SkipRuntime) {
  Say "CHECK livePage result=SKIP 指定了 -SkipRuntime"
}

Write-Host "----------------------------------------" -ForegroundColor DarkGray
if ($failed -ge 100) {
  Write-Host "VERDICT: UNVERIFIED（运行期检查取不到读数，不能当通过）rc=2" -ForegroundColor DarkYellow
  exit 2
} elseif ($failed -gt 0) {
  Write-Host ("VERDICT: FAIL 失败判据=" + $failed + " rc=1") -ForegroundColor Red
  exit 1
}
Write-Host "VERDICT: PASS rc=0" -ForegroundColor Green
exit 0

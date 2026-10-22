#!/usr/bin/env pwsh
# dom_probe.ps1 — 用 Edge/Chromium 无头模式抓「渲染后的 DOM」，把前端的机器判据落到文件。
#
# 为什么不用 agent-browser：本机它的守护进程会挂起（eval 长时间无返回），
# 拿不到数就不能算 PASS。改用一次性的 headless --dump-dom：
#   - 无守护进程、无交互会话，跑完即退；
#   - --virtual-time-budget 让页面里的定时器/轮询/自动点击在虚拟时间里跑完，
#     所以 dump 出来的是「自动按钮已经自动触发之后」的真实 DOM。
#
# 用法：pwsh tests/ui/dom_probe.ps1 -Base http://127.0.0.1:17899 -Token <token>

param(
    [string]$Base = "http://127.0.0.1:17899",
    [Parameter(Mandatory = $true)][string]$Token,
    [string]$OutDir = "D:\workdesktop\dist\ui-shots",
    [string]$Edge = "C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe"
)

$ErrorActionPreference = "Continue"
New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$profile = Join-Path $OutDir "edge-profile"
New-Item -ItemType Directory -Force -Path $profile | Out-Null

function Get-Dom {
    param([string]$Name, [string]$Url, [int]$BudgetMs = 6000)
    $target = Join-Path $OutDir ("dom-" + $Name + ".html")
    if (Test-Path $target) { Remove-Item -LiteralPath $target -Force }
    $args = @(
        "--headless=new",
        "--disable-gpu",
        "--no-first-run",
        "--no-default-browser-check",
        "--disable-extensions",
        "--hide-scrollbars",
        "--user-data-dir=$profile",
        "--virtual-time-budget=$BudgetMs",
        "--dump-dom",
        $Url
    )
    # 每次用独立 profile：共用一个 profile 时第二个进程会因锁竞争静默失败（实测 bytes=0）。
    $prof = Join-Path $profile ("p-" + $Name)
    New-Item -ItemType Directory -Force -Path $prof | Out-Null
    $args = $args | ForEach-Object { if ($_ -like '--user-data-dir=*') { "--user-data-dir=$prof" } else { $_ } }
    $errFile = Join-Path $OutDir ("dom-" + $Name + ".err")
    $p = Start-Process -FilePath $Edge -ArgumentList $args -RedirectStandardOutput $target -RedirectStandardError $errFile -Wait -PassThru
    $len = 0
    if (Test-Path $target) { $len = (Get-Item $target).Length }
    $errLen = 0
    if (Test-Path $errFile) { $errLen = (Get-Item $errFile).Length }
    Write-Host ("[dom-probe] " + $Name + " exit=" + $p.ExitCode + " bytes=" + $len + " err=" + $errLen)
    return [pscustomobject]@{ name = $Name; exit = $p.ExitCode; bytes = $len; errBytes = $errLen; file = $target }
}

$results = @()
$results += Get-Dom 'home-360' "$Base/" 7000
$results += Get-Dom 'admin-390' "$Base/admin?token=$Token" 7000
$results += Get-Dom 'governance-390' "$Base/governance?token=$Token" 7000
$results += Get-Dom 'modelpool-390' "$Base/modelpool?token=$Token" 7000

# 截图（同样一次性，不用守护进程）
function Get-Shot {
    param([string]$Name, [string]$Url, [int]$W, [int]$H, [int]$BudgetMs = 6000)
    $target = Join-Path $OutDir ("shot-" + $Name + ".png")
    $args = @(
        "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
        "--hide-scrollbars", "--window-size=$W,$H", "--virtual-time-budget=$BudgetMs",
        "--screenshot=$target", $Url
    )
    $prof = Join-Path $profile ("p-" + $Name)
    New-Item -ItemType Directory -Force -Path $prof | Out-Null
    $args = $args | ForEach-Object { if ($_ -like '--user-data-dir=*') { "--user-data-dir=$prof" } else { $_ } }
    $args = @("--user-data-dir=$prof") + $args
    $errFile = Join-Path $OutDir ("shot-" + $Name + ".err")
    $p = Start-Process -FilePath $Edge -ArgumentList $args -RedirectStandardError $errFile -Wait -PassThru
    $len = 0
    if (Test-Path $target) { $len = (Get-Item $target).Length }
    Write-Host ("[shot] " + $Name + " exit=" + $p.ExitCode + " bytes=" + $len)
    return [pscustomobject]@{ name = $Name; exit = $p.ExitCode; bytes = $len; file = $target }
}

$results += Get-Shot 'home-360' "$Base/" 360 640
$results += Get-Shot 'admin-390' "$Base/admin?token=$Token" 390 844
$results += Get-Shot 'modelpool-390' "$Base/modelpool?token=$Token" 390 844

$results | ConvertTo-Json | Set-Content -Path (Join-Path $OutDir "dom-report.json") -Encoding UTF8
$bad = @($results | Where-Object { $_.bytes -le 0 }).Count
Write-Host ("报告: " + (Join-Path $OutDir "dom-report.json"))
if ($bad -gt 0) { Write-Host ("UNVERIFIED: " + $bad + " 项未取到数"); exit 2 }
exit 0

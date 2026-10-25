# check_crossbuild.ps1 -- 交叉编译门禁（防「只有本机平台能编译」）
#
# 为什么存在（2026-10-26 round 222 实测缺口，代价是 CI 连红 6 个提交）：
#   本仓是 Windows 单 EXE，CI 的 go vet / go test / build 三条 job 全在 windows-latest 上跑；
#   唯一覆盖别的平台的是那道 Autonomy 容器 E2E，而它只跑 go build（不跑 go vet ⇒ 连 _test.go 都不编译）。
#   于是带 //go:build !windows 的文件里 "imported and not used" 在 windows 上永远编译不到，E2E 因此连红；
#   另有两个 _test.go 缺 build tag（引用 windows 专有标识符），linux 下 go vet / go test 编译不过而 CI 完全不报。
#
# 判据：对 GOOS=linux/darwin/windows 各跑 go build ./... 与 go vet ./...（CGO_ENABLED=0, GOARCH=amd64）。
#   build 管生产代码，vet 额外编译测试文件 —— 两条都要，缺一漏一半。
#   与 check_snapshot_build.ps1 互补：那道判「HEAD 快照在本平台能不能编译」，本道判「同一份代码在别的平台能不能编译」。
#
# 成本（2026-10-26 两种口径都实测）：热缓存（GOCACHE 指向已有库）累计 5.1s；冷缓存（CI 新 runner 或默认库）
# 累计 23.3s（逐项 3/2.2/8.4/4.1/2.9/2.7s）。本闸不控制 GOCACHE ⇒ 报数以冷缓存为准，仍属可每次提交都跑的量级。
#
# 退出码：0=三平台全可编译 / 1=任一平台失败 / 2=环境缺 go 或模块目录不存在（UNVERIFIED，不洗成 0 也不冒充 1）

param(
  # 默认由脚本位置推导（tests/ 的父目录），不硬编码 —— 否则 CI 里指向不存在的 D:\workdesktop。
  [string]$Repo = "",
  [string]$ModuleDir = "workbench",
  [string]$GoArch = "amd64"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

if ([string]::IsNullOrWhiteSpace($Repo)) { $Repo = Split-Path -Parent $PSScriptRoot }
$mod = Join-Path $Repo $ModuleDir
if (-not (Test-Path -LiteralPath $mod)) {
  Write-Host ("UNVERIFIED 模块目录不存在: " + $mod) -ForegroundColor Yellow
  exit 2
}

# 只探测存在性，实际调用一律用命令名 go —— 房内其它门禁（check_snapshot_build / check_gofmt）都是这么调的。
# 实测把完整路径存进变量再 & $go 会踩 CantActivateDocumentInPipeline（round 221 自己撞过一次）：
# PowerShell 在管道里不接受"文档/可执行路径变量"作为命令，必须走命令解析。
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
  Write-Host "UNVERIFIED 找不到 go 可执行文件（交叉编译无法判定，不是代码有问题）" -ForegroundColor Yellow
  exit 2
}

# 外部命令的 stderr 必须落回输出流，而不是变成终止错误：Stop 下 & 原生命令一有 stderr 就抛
# NativeCommandError（round 202/205/213 三组对照实测：2>$null 与 2>&1 都拦不住，try/catch 能接但会丢掉
# 真实退出码）。所以这一段临时把 EAP 降到 Continue，直接取 $LASTEXITCODE —— 编译器的返回码才是判据。
$prevEAP = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$fails = @()
$totalSec = 0.0
try {
  Write-Host ("交叉编译门禁：GOOS=linux/darwin/windows  build+vet  CGO_ENABLED=0  GOARCH=" + $GoArch) -ForegroundColor Cyan
  Push-Location $mod
  try {
    foreach ($os in @("linux", "darwin", "windows")) {
      foreach ($sub in @("build", "vet")) {
        $t0 = Get-Date
        $env:GOOS = $os
        $env:CGO_ENABLED = "0"
        $env:GOARCH = $GoArch
        $out = & go $sub ./... 2>&1 | Out-String
        $rc = $LASTEXITCODE
        $sec = [math]::Round(((Get-Date) - $t0).TotalSeconds, 1)
        $totalSec = $totalSec + $sec
        if ($rc -ne 0) {
          $fails += ($os + "/" + $sub)
          Write-Host ("  FAIL " + $os + " " + $sub + " rc=" + $rc + " (" + $sec + "s)") -ForegroundColor Red
          $errs = @(($out -split "`n") | Where-Object { $_.Trim() -and $_ -match "\.go:" } | Select-Object -First 6)
          foreach ($e in $errs) { Write-Host ("        " + $e.Trim()) -ForegroundColor Red }
          if ($errs.Count -eq 0) {
            $head2 = @(($out -split "`n") | Where-Object { $_.Trim() } | Select-Object -First 2)
            foreach ($h in $head2) { Write-Host ("        " + $h.Trim()) -ForegroundColor DarkRed }
          }
        } else {
          Write-Host ("  ok   " + $os + " " + $sub + " (" + $sec + "s)") -ForegroundColor Green
        }
      }
    }
  } finally {
    Pop-Location
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
  }
} finally {
  $ErrorActionPreference = $prevEAP
}

if ($fails.Count -gt 0) {
  Write-Host ("交叉编译未通过：" + ($fails -join ", ")) -ForegroundColor Red
  Write-Host "常见成因：带 //go:build !windows 的文件里有未用 import；或 _test.go 引用平台专有标识符却缺 build tag。" -ForegroundColor Yellow
  exit 1
}
Write-Host ("PASS 三平台 build+vet 均可编译（累计 " + $totalSec + "s）") -ForegroundColor Green
exit 0

# check_artifact_checksum.ps1 -- 产物与校验清单（SHA256SUMS）必须一致（2026-10-25 round 154）
#
# 为什么需要（2026-10-20 实发）：产物重建后清单还记着旧 hash，于是**所有以清单为凭的
#   验收证据全部失效**。CI 的 build job 已有一条独立读回校验（.github/workflows/ci.yml 的
#   Verify artifact 步骤，不一致即 throw），但它只在 CI 的 build job 里跑 —— **本地没有等价判据**：
#   用裸 go build / 手工复制重建 dist\workbench.exe 时清单不会自动更新，本地跑全套件也不报警。
#   同时 docs/benchmark/性能基线口径.md §6 曾把「dist\SHA256SUMS 已过期」登记为未覆盖缺口；
#   2026-10-25 实测清单与产物已一致（3335F81A…），本判据把该缺口在**本地侧**补上。
#
# 判据（窄面、可证伪）：
#   1) 产物不存在 => rc=3 SKIP（未构建；「没测成 != 合格」）
#   2) 产物在、清单不在 => rc=1（有产物无清单 = 没走构建链，验收证据无效）
#   3) 清单里没有 workbench.exe 条目 => rc=1
#   4) 清单 hash != 产物实测 Get-FileHash => rc=1（漂移，打印两个 hash 便于取证）
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File tests\check_artifact_checksum.ps1 [-Exe <path>] [-Sums <path>] [-Quiet]
# 退出码: 0=PASS / 1=FAIL / 3=SKIP（无产物，未构建）
param(
  [string]$Exe = "",
  [string]$Sums = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Exe))  { $Exe  = Join-Path $root "dist\workbench.exe" }
if ([string]::IsNullOrWhiteSpace($Sums)) { $Sums = Join-Path $root "dist\SHA256SUMS" }
if (-not (Test-Path -LiteralPath $Exe)) {
  Write-Host ("SKIP 产物不存在（未构建；没测成 != 合格）: " + $Exe) -ForegroundColor Yellow
  exit 3
}
if (-not (Test-Path -LiteralPath $Sums)) {
  Write-Host ("FAIL 有产物但没有校验清单: " + $Sums + " —— 产物没走构建链（裸 go build / 手工复制）？") -ForegroundColor Red
  exit 1
}
$listed = (Get-Content -LiteralPath $Sums | Where-Object { $_ -match "workbench\.exe" } | Select-Object -First 1)
if (-not $listed) {
  Write-Host "FAIL dist\SHA256SUMS 里没有 workbench.exe 条目" -ForegroundColor Red
  exit 1
}
$listedHash = ($listed -split "\s+")[0].ToUpper()
$actual = (Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash.ToUpper()
if ($listedHash -ne $actual) {
  Write-Host ("FAIL SHA256SUMS 漂移: 清单 " + $listedHash + " != 实测 " + $actual + "（产物与清单不一致）") -ForegroundColor Red
  exit 1
}
if (-not $Quiet) { Write-Host ("PASS 产物与清单一致: " + $actual) -ForegroundColor Green }
exit 0

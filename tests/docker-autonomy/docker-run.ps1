# docker-run.ps1 — 宿主侧一键跑「容器内自主闭环端到端验收」
#
# 职责分工：本脚本只做编排（编译 → 起容器 → 收证据），不碰 workbench/ 源码。
# 判据在容器内由 judge.py 核对，宿主只转述退出码与证据路径。
#
# 用法：
#   pwsh tests/docker-autonomy/docker-run.ps1                 # 全链：编译二进制 → 跑容器 → 收证据
#   pwsh tests/docker-autonomy/docker-run.ps1 -SkipBuild      # 复用已有 dist/ 产物
#   pwsh tests/docker-autonomy/docker-run.ps1 -Image alpine:3.20   # 换基础镜像（musl 腿）
param(
    [switch]$SkipBuild,
    [string]$Image = "debian:bookworm-slim",
    [string]$Container = "",
    [string]$RunId = "",
    # 源码目录（相对仓库根或绝对路径）。默认 workbench/；仓库源码未恢复时可指向备份副本。
    [string]$SrcDir = "workbench"
)
$ErrorActionPreference = "Stop"
$here = $PSScriptRoot
$repo = Split-Path -Parent (Split-Path -Parent $here)
$dist = Join-Path $repo "dist"

if (-not $RunId) { $RunId = "run-" + (Get-Date -Format "yyyyMMdd-HHmmss") }
if (-not $Container) { $Container = "fenjue-autonomy-e2e-" + $RunId }

Write-Host "==> 0/4 检查 Docker daemon" -ForegroundColor Cyan
$srv = docker version --format "{{.Server.Version}}" 2>$null
if ($LASTEXITCODE -ne 0 -or -not $srv) { throw "Docker daemon 未就绪（请先启动 Docker Desktop）" }
Write-Host "    daemon: $srv"

if (-not $SkipBuild) {
    Write-Host "==> 1/4 编译静态 Linux 二进制 + DSH 替身" -ForegroundColor Cyan
    $srcAbs = if ([System.IO.Path]::IsPathRooted($SrcDir)) { $SrcDir } else { Join-Path $repo $SrcDir }
    $relSrc = $srcAbs.Substring($repo.Length).Replace("\", "/").TrimStart("/")
    Write-Host "    源码目录: $srcAbs"
    docker run --rm --name "$Container-build" `
        -v "${repo}:/src" -v "${dist}:/out" -v "${here}:/dk:ro" `
        -e "SRC_DIR=/src/$relSrc" `
        golang:1.27-bookworm bash -lc "bash /dk/build-linux.sh && bash /dk/build-fakedsh.sh"
    if ($LASTEXITCODE -ne 0) { throw "编译失败" }
} else {
    Write-Host "==> 1/4 跳过编译（-SkipBuild）" -ForegroundColor Yellow
}

$exe = Join-Path $dist "workbench-linux-amd64"
$fake = Join-Path $dist "fakedsh"
if (-not (Test-Path $exe)) { throw "缺少 $exe，请先不带 -SkipBuild 跑一次" }
if (-not (Test-Path $fake)) { throw "缺少 $fake，请先不带 -SkipBuild 跑一次" }

Write-Host "==> 2/4 容器内跑端到端（镜像 $Image）" -ForegroundColor Cyan
docker rm -f $Container 2>&1 | Out-Null
docker run --rm --name $Container `
    -v "${exe}:/test/wb:ro" -v "${fake}:/test/fakedsh:ro" -v "${here}:/dk" `
    -e "RUN_ID=$RunId" `
    $Image bash -lc "apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq python3 >/dev/null 2>&1 && bash /dk/container.sh"
$rc = $LASTEXITCODE

Write-Host "==> 3/4 回收证据" -ForegroundColor Cyan
$ev = Join-Path $here ("evidence\" + $RunId)
if (Test-Path $ev) {
    Write-Host "    $ev" -ForegroundColor Green
    $log = Join-Path $ev "run.log"
    if (Test-Path $log) { Get-Content $log | Select-Object -Last 14 | ForEach-Object { "    " + $_ } }
} else {
    Write-Host "    (无证据目录，容器可能在更早阶段失败)" -ForegroundColor Yellow
}

Write-Host "==> 4/4 结论" -ForegroundColor Cyan
if ($rc -ne 0) { Write-Host "E2E_FAIL (exit $rc)" -ForegroundColor Red; exit $rc }
Write-Host "E2E_PASS（容器内自主闭环端到端全绿）" -ForegroundColor Green
exit 0

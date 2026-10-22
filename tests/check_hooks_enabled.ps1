# check_hooks_enabled.ps1 -- 本仓 git hooks 是否已启用（2026-10-24 新增）
#
# 为什么需要：上一轮加了提交前 gofmt 闸（.githooks/pre-commit + tests/check_staged_gofmt.ps1），
# 但 **git 的 core.hooksPath 写在 .git/config，不随 clone 传播**（2026-10-24 实测确认）。
# 于是「hook 在库里」不等于「hook 在生效」：CI、并行会话的机器、任何新 clone 默认都没启用。
# 本判据把「闸门实际是否装着」变成可判事实，避免「以为有保护但其实没有」。
#
# 判据（三态，与全仓口径一致）：
#   rc=0 PASS       —— core.hooksPath 指向 .githooks 且 hook 文件在位
#   rc=1 FAIL       —— 配置缺失/指向别处/hook 文件缺失（闸门没装）
#   rc=2 UNVERIFIED —— 不在 git 仓库（CI 干净检出本就不装 hook，
#                      按项目铁律记 UNVERIFIED，不当 PASS 也不判红）
#
# 与 setup/install-hooks.ps1 的分工：后者是**修复手段**（一键启用），
# 本判据是**发现手段**（报告状态）。FAIL 时的提示直接指向那个脚本。
#
# 用法: powershell -File tests\check_hooks_enabled.ps1 [-Quiet]
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Push-Location $root
try {
  if (-not (Test-Path -LiteralPath ".git")) {
    Write-Host "UNVERIFIED: 不在 git 仓库（CI 干净检出本就不装 hook）" -ForegroundColor Yellow
    exit 2
  }

  $expected = ".githooks"
  $actual = git config --get core.hooksPath 2>$null

  if (-not $actual) {
    if (-not $Quiet) {
      Write-Host "FAIL: core.hooksPath 未设置 —— 提交前 gofmt 闸未启用" -ForegroundColor Red
      Write-Host "  影响：改完 .go 忘跑 gofmt 不会被本地拦住，只能等 CI 判红（多一轮提交）。" -ForegroundColor Red
      Write-Host "  修复: powershell -ExecutionPolicy Bypass -File setup\install-hooks.ps1" -ForegroundColor Yellow
    }
    exit 1
  }
  if ($actual -ne $expected) {
    if (-not $Quiet) {
      Write-Host ("FAIL: core.hooksPath=[" + $actual + "]，期望 [" + $expected + "]") -ForegroundColor Red
      Write-Host "  修复: powershell -ExecutionPolicy Bypass -File setup\install-hooks.ps1" -ForegroundColor Yellow
    }
    exit 1
  }

  $hooks = @(Get-ChildItem -LiteralPath $expected -File -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike '*.md' })
  if ($hooks.Count -eq 0) {
    if (-not $Quiet) {
      Write-Host ("FAIL: " + $expected + " 下没有任何 hook 文件") -ForegroundColor Red
      Write-Host "  该目录受版本控制；缺文件说明 clone 不完整或被人删了。" -ForegroundColor Red
    }
    exit 1
  }

  if (-not (Test-Path -LiteralPath (Join-Path $expected "pre-commit"))) {
    if (-not $Quiet) { Write-Host "FAIL: 缺 .githooks/pre-commit" -ForegroundColor Red }
    exit 1
  }

  if (-not $Quiet) {
    Write-Host ("git hooks 已启用：core.hooksPath=" + $actual + "，" + $hooks.Count + " 个 hook 文件在位") -ForegroundColor Green
  }
  exit 0
}
finally {
  Pop-Location
}
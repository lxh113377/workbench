# setup/install-hooks.ps1 -- 一键安装本仓的 git hooks（2026-10-24 新增）
#
# 为什么需要（git 的固有限制，2026-10-24 实测确认）：
#   `core.hooksPath` 写在 `.git/config`（仓库本地配置），**不随 clone 传播** ——
#   所以 `.githooks/` 虽已受版本控制，但每台新机器 / 每个新 clone / CI 都需要重新启用。
#   本脚本把「启用」变成一条命令，并在文档里写明，避免「hook 在库里但没人装」。
#
# 用法: powershell -ExecutionPolicy Bypass -File setup\install-hooks.ps1 [-Uninstall]
# 退出码: 0=成功 / 1=失败
param([switch]$Uninstall)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Push-Location $root
try {
  $hooksDir = ".githooks"
  if ($Uninstall) {
    git config --unset core.hooksPath 2>$null
    if ($LASTEXITCODE -eq 0) {
      Write-Host "已卸载：core.hooksPath 取消设置（.githooks/ 文件保留）" -ForegroundColor Green
    } else {
      Write-Host "core.hooksPath 本就未设置，无需卸载" -ForegroundColor Yellow
    }
    exit 0
  }

  # 1) 前置校验：hook 目录与 hook 文件必须存在（受版本控制的那几个）
  if (-not (Test-Path -LiteralPath $hooksDir)) {
    Write-Host ("缺 " + $hooksDir + " 目录 —— 该目录受版本控制，缺它说明 clone 不完整") -ForegroundColor Red
    exit 1
  }
  $required = @("pre-commit")
  foreach ($h in $required) {
    $p = Join-Path $hooksDir $h
    if (-not (Test-Path -LiteralPath $p)) {
      Write-Host ("缺 " + $p + " —— 受版本控制的 hook 文件缺失") -ForegroundColor Red
      exit 1
    }
  }

  # 2) 设置 core.hooksPath（幂等：重复执行结果一致）
  git config core.hooksPath $hooksDir
  if ($LASTEXITCODE -ne 0) {
    Write-Host "git config core.hooksPath 失败" -ForegroundColor Red
    exit 1
  }

  # 3) 自检：确认 git 认到了这个配置
  $eff = git config --get core.hooksPath
  if ($eff -ne $hooksDir) {
    Write-Host ("自检失败：core.hooksPath 实为 [" + $eff + "]，期望 [" + $hooksDir + "]") -ForegroundColor Red
    exit 1
  }

  Write-Host ("git hooks 已启用：core.hooksPath=" + $eff) -ForegroundColor Green
  Write-Host ""
  Write-Host "当前生效的 hook："
  foreach ($f in (Get-ChildItem -LiteralPath $hooksDir -File | Sort-Object Name)) {
    Write-Host ("  - " + $f.Name)
  }
  Write-Host ""
  Write-Host "hook 做的事：提交前对**已暂存**的 .go 跑 gofmt（tests/check_staged_gofmt.ps1）。"
  Write-Host "紧急跳过: git commit --no-verify（但 CI 的 check_gofmt 会判红，代价更大）。"
}
finally {
  Pop-Location
}
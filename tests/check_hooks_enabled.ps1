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

# git 解析必须显式兜底（2026-11-01 round 190 实测修的第 3 个同族实例）：
#   本文件在 $ErrorActionPreference="Stop" 下裸调 `git config`，而 PowerShell 常常解析不到 git
#   （Get-Command 返回空是本机常态，round 183 实测 GC=[]）⇒ 抛 CommandNotFoundException 直接逃逸，
#   脚本以 **rc=1（崩溃）** 结束，而本件头部注释写明的设计是三态里「没测成 ⇒ rc=2 UNVERIFIED」。
#   更糟的是 rc=1 在聚合器看来等于「闸门确实没装」⇒ 一个环境缺工具的问题被报成仓库配置缺陷，
#   下一个读到红的人只会去跑 install-hooks.ps1（跑了也没用，因为问题不在配置）。
#   样板同 check_gate_coverage.ps1 / check_untracked_go.ps1 / check_deadcode.ps1。
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host "UNVERIFIED: 找不到 git 可执行文件（闸门状态无从判定，不是闸门没装）" -ForegroundColor Yellow; exit 2 }

Push-Location $root
try {
  if (-not (Test-Path -LiteralPath ".git")) {
    Write-Host "UNVERIFIED: 不在 git 仓库（CI 干净检出本就不装 hook）" -ForegroundColor Yellow
    exit 2
  }

  $expected = ".githooks"
  # 三态要分清：git config --get 在**键未设置时自身返回 rc=1**（这是正常语义，不是调用失败）。
  # 所以「rc≠0 ⇒ UNVERIFIED」会把「闸门确实没装」这个真 FAIL 伪装成测不了 —— 比原来的崩溃更糟
  # （崩溃至少会逼人去看，伪装成 UNVERIFIED 会让它永远绿着不装）。只有进程起不来才算没测成。
  # 为什么不用 & $gitExe：PowerShell 拿到绝对路径后仍可能按**文档关联**解释它，实测抛
  #   "Cannot run a document in the middle of a pipeline"（round 183 与本轮 round 190 各撞一次），
  #   try/catch 兜住了也只是「测不了」。正解 = 显式进程调用（样板 check_gate_coverage.ps1）。
  $hg = New-Object System.Diagnostics.ProcessStartInfo
  $hg.FileName = $gitExe
  $hg.Arguments = "config --get core.hooksPath"
  $hg.WorkingDirectory = $root
  $hg.UseShellExecute = $false
  $hg.CreateNoWindow = $true
  $hg.RedirectStandardOutput = $true
  $hg.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
  $hg.RedirectStandardError = $true
  $actual = $null
  $gitRc = -1
  $gitErr = ""
  try {
    $hp = [System.Diagnostics.Process]::Start($hg)
    $actual = $hp.StandardOutput.ReadToEnd()
    $gitErr = $hp.StandardError.ReadToEnd()
    $hp.WaitForExit()
    $gitRc = $hp.ExitCode
  } catch {
    $actual = $null; $gitRc = -1
    Write-Host ("UNVERIFIED: 无法启动 git 进程（" + $_.Exception.Message + "）—— 闸门状态无从判定，不代表未启用") -ForegroundColor Yellow
    exit 2
  }
  if ($gitRc -eq 0) {
    $actual = $actual.Trim()
  } elseif ($gitRc -eq 1 -and $actual.Trim() -eq "" -and $gitErr.Trim() -eq "") {
    # 键确实没设：交给下面的 FAIL 分支（这是本判据存在的意义）
    $actual = ""
  } else {
    Write-Host ("UNVERIFIED: git config 异常退出 rc=" + $gitRc + " " + $gitErr.Trim() + " —— 不是闸门未启用") -ForegroundColor Yellow
    exit 2
  }

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
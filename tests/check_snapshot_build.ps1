# check_snapshot_build.ps1 -- 纯净检出编译门禁（防「提交了编译不过的快照」）
#
# 为什么存在（2026-10-21 实测抓到自己的问题）：
#   提交 tray 加固时，工作树里 go test 全绿 —— 但那只证明**工作树**能跑，
#   不证明**提交进去的东西**能跑。实测：git archive 导出 HEAD 快照后
#   go vet ./... 立刻报 undefined: tryRaiseIntegrity / hiddenCmd，
#   因为 tray_windows.go / executor.go 依赖的 4 个文件从未入库
#   （git log --all 实测 NEVER TRACKED）。
#
# 判据：把 HEAD 导出到临时目录，从零编译。任何 undefined/缺失 = FAIL。
# 与「工作树里跑测试」互为补充：后者可能被未提交文件喂饱而假绿。
#
# 退出码: 0=快照可编译 / 1=快照编译失败 / 2=环境缺工具

param(
  [string]$Repo = "D:\workdesktop",
  # 快照里 Go 模块根（相对快照根）
  [string]$ModuleDir = "workbench",
  # 超时（秒）
  [int]$TimeoutSec = 300
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$gitDir = Join-Path $Repo ".git"
if (-not (Test-Path -LiteralPath $gitDir)) {
  Write-Host "FAIL 不是 git 仓库: $Repo" -ForegroundColor Red
  exit 1
}

$tmp = Join-Path $env:TEMP ("wb-snapshot-" + [DateTime]::Now.ToString("HHmmss"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

Write-Host "=== 纯净检出编译门禁 ===" -ForegroundColor White
Write-Host ("  仓库: " + $Repo)

try {
  $tar = Join-Path $tmp "snap.tar"
  & git --git-dir="$gitDir" archive --format=tar -o "$tar" HEAD
  if ($LASTEXITCODE -ne 0) { Write-Host "FAIL git archive 失败" -ForegroundColor Red; exit 1 }
  Write-Host ("  快照大小: " + [math]::Round((Get-Item $tar).Length / 1MB, 2) + " MB")

  $snapRoot = Join-Path $tmp "snap"
  New-Item -ItemType Directory -Force -Path $snapRoot | Out-Null
  & tar -xf "$tar" -C "$snapRoot"
  if ($LASTEXITCODE -ne 0) { Write-Host "FAIL tar 解包失败" -ForegroundColor Red; exit 1 }

  $modRoot = Join-Path $snapRoot $ModuleDir
  if (-not (Test-Path -LiteralPath $modRoot)) {
    Write-Host ("FAIL 快照里找不到模块根: " + $ModuleDir) -ForegroundColor Red
    exit 1
  }

  Push-Location $modRoot
  try {
    # 注意：$ErrorActionPreference="Stop" 下，go 写 stderr 会被当 NativeCommandError 抛出，
    # 2>&1 | Out-String 抓不到（2026-10-21 实测踩到）。必须临时改 Continue 才读得到编译器错误。
    $eapBackup = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $out = & go build ./... 2>&1 | Out-String
    $rc = $LASTEXITCODE
    $ErrorActionPreference = $eapBackup
    if ($out.Trim()) { Write-Host $out }
    if ($rc -ne 0) {
      Write-Host "FAIL 纯净检出编译失败 —— 提交了编译不过的快照（上面即为缺失符号）" -ForegroundColor Red
      Write-Host "  常见原因：tracked 文件引用了从未入库的符号/文件。" -ForegroundColor Yellow
      Write-Host "  自查: git log --all -- <缺失文件>  (空 = NEVER TRACKED)" -ForegroundColor Yellow
      exit 1
    }
    Write-Host "PASS 纯净检出 go build ./... 成功" -ForegroundColor Green

    # 有测试就再跑一轮（快照里的测试才是提交真相）
    $ErrorActionPreference = "Continue"
    $tout = & go test ./... -count=1 2>&1 | Out-String
    $trc = $LASTEXITCODE
    $ErrorActionPreference = $eapBackup
    if ($trc -ne 0) {
      Write-Host "FAIL 纯净检出 go test 失败" -ForegroundColor Red
      if ($tout.Trim()) { Write-Host $tout }
      exit 1
    }
    Write-Host "PASS 纯净检出 go test ./... 全绿" -ForegroundColor Green
  } finally {
    Pop-Location
  }

  Write-Host "----------------------------------------"
  Write-Host "CHECK snapshotBuild result=PASS" -ForegroundColor Green
  exit 0
} finally {
  try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue } catch {}
}

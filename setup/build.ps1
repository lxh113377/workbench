# build.ps1 — 一键构建「个人 AI 工作台」单 EXE
# 用法: powershell -ExecutionPolicy Bypass -File setup\build.ps1 [-Version 1.0.0]
param(
  [string]$Version = "1.0.0",
  [switch]$SkipTests,
  [switch]$SkipCoverGate,
  [double]$CoverThreshold = 55,
  # PerPackageCover：按"包:阈值"覆盖个别包的基线（新抽出的纯逻辑包更高，
  # 与子进程强耦合的 engine 包先按当前基线守护、逐步抬升）。
  # 分档基线依据：engine/web/executor 与大块子进程/HTTP 耦合，先按当前水平守护、
  # 每次改造只许抬不许降；logging/retry/queue/model 等纯逻辑包走全局 55% 阈值。
  [string[]]$PerPackageCover = @("workbench/internal/engine=20", "workbench/internal/web=15", "workbench/internal/executor=26", "workbench/internal/modelpool=49", "workbench/internal/shortcut=30", "workbench/internal/supervisor=46", "workbench/cmd/workbench=3")
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"
$dist = Join-Path $root "dist"
$build = Join-Path $wb "build"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Write-Host "==> 1/6 生成图标" -ForegroundColor Cyan
$makeIcon = Join-Path $PSScriptRoot "make-icon.ps1"
if (Test-Path $makeIcon) {
  New-Item -ItemType Directory -Force -Path $build | Out-Null
  & $makeIcon -Out (Join-Path $build "app.ico") | Out-Null
} else {
  Write-Warning "    make-icon.ps1 不存在，跳过图标生成（EXE 仍可用）"
}

Write-Host "==> 2/6 编译资源（图标 + 版本信息）" -ForegroundColor Cyan
$windres = Get-ChildItem "C:\Users\$env:USERNAME\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs*" -Recurse -Filter windres.exe -ErrorAction SilentlyContinue | Select-Object -First 1
if ($windres) {
  Push-Location $build
  & $windres.FullName -i app.rc -o rsrc_windows_amd64.syso -O coff
  Pop-Location
  Copy-Item (Join-Path $build "rsrc_windows_amd64.syso") (Join-Path $wb "cmd\workbench\rsrc_windows_amd64.syso") -Force
  Write-Host "    资源编译完成"
} else {
  Write-Warning "    未找到 windres，跳过图标与版本信息（EXE 仍可用）"
}

Write-Host "==> 3/6 代码检查" -ForegroundColor Cyan
Push-Location $wb
& go vet ./...
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "go vet 失败" }

if (-not $SkipTests) {
  Write-Host "==> 4/6 运行测试 + 覆盖率门禁" -ForegroundColor Cyan
  $coverOut = (& go test ./... -count=1 -cover 2>&1 | Out-String)
  $testRc = $LASTEXITCODE
  Write-Host $coverOut
  if ($testRc -ne 0) { Pop-Location; throw "测试失败" }

  # 覆盖率门禁（对标 river/asynq/temporal 的 CI 保护）：
  # 只约束"已有测试"的包，未写测试的包不计入分母，避免为了凑数补空测试。
  # 阈值可用 -CoverThreshold 覆盖，个别包用 -PerPackageCover 单独设线，-SkipCoverGate 可临时跳过。
  if (-not $SkipCoverGate) {
    $gateFailed = @()
    $testedCount = 0
    foreach ($line in ($coverOut -split "`n")) {
      if ($line -match '^(?:ok|FAIL)\s+(\S+)\s+.*coverage:\s+([0-9.]+)%') {
        $pkg = $Matches[1]
        $cov = [double]$Matches[2]
        $testedCount++
        $threshold = $CoverThreshold
        foreach ($entry in $PerPackageCover) {
          $kv = $entry -split "=", 2
          if ($kv.Count -eq 2 -and $kv[0] -eq $pkg) { $threshold = [double]$kv[1] }
        }
        if ($cov -lt $threshold) { $gateFailed += ($pkg + "=" + $cov + "%<阈值" + $threshold + "%") }
      }
    }
    if ($testedCount -eq 0) { Pop-Location; throw "覆盖率门禁：未解析到任何带覆盖率的包，判据失效" }
    if ($gateFailed.Count -gt 0) {
      Pop-Location
      throw ("覆盖率门禁未通过（阈值 " + $CoverThreshold + "%）：" + ($gateFailed -join ", "))
    }
    Write-Host ("    覆盖率门禁通过：" + $testedCount + " 个有测试的包均达标") -ForegroundColor Green
  } else {
    Write-Host "    覆盖率门禁已跳过（-SkipCoverGate）" -ForegroundColor Yellow
  }
} else {
  Write-Host "==> 4/6 跳过测试" -ForegroundColor Yellow
}

Write-Host "==> 5/6 构建单 EXE" -ForegroundColor Cyan
$buildTime = Get-Date -Format o
$env:CGO_ENABLED = "0"
& go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version -X main.buildTime=$buildTime" -o (Join-Path $dist "workbench.exe") ./cmd/workbench
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "构建失败" }
Pop-Location

Write-Host "==> 6/6 校验产物" -ForegroundColor Cyan
$exe = Join-Path $dist "workbench.exe"
if (-not (Test-Path $exe)) { throw "产物不存在" }
$info = (Get-Item $exe).VersionInfo
$size = [math]::Round((Get-Item $exe).Length / 1MB, 2)
Write-Host "    文件: $exe"
Write-Host "    大小: $size MB"
Write-Host "    版本: $($info.FileVersion)"
Write-Host "    产品: $($info.ProductName)"
$hash = (Get-FileHash $exe -Algorithm SHA256).Hash
"$hash  workbench.exe" | Set-Content (Join-Path $dist "SHA256SUMS")
Write-Host "    SHA256: $hash"
Write-Host ""
Write-Host "构建成功！双击 $exe 或运行 setup\install.ps1 安装快捷方式。" -ForegroundColor Green
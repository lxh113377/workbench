# tests/docker/run_pool_e2e.ps1 — 模型池 Docker 端到端验收（宿主驱动）
#
# 用法:
#   powershell -NoProfile -ExecutionPolicy Bypass -File tests\docker\run_pool_e2e.ps1
#   ... -SelfCheckOnly      只验 mock 网关地基
#   ... -Binary D:\x\wb     指定现成的 linux 二进制（源码树未恢复时用）
#   ... -Source D:\path\workbench  对指定源码树构建并跑门禁（对照验证用）
#   ... -SkipBuild          复用已构建夹具
#
# 四步：交叉构建(静态 CGO=0) -> debian:bookworm-slim(dash) -> alpine:3.20(busybox)
#       -> golang:1.27-bookworm 跑 go vet + go test（干净工具链，不碰宿主 Go 环境）。
# 边界：容器一律 --rm + 前缀名；/test 只读；不发布宿主端口；不挂宿主 ~/.dsh。
param(
  [string]$Source = "",
  [string]$Binary = "",
  [switch]$SkipBuild,
  [switch]$SelfCheckOnly
)
$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$fix  = Join-Path $PSScriptRoot "fixtures"
$srcTree = if ($Source) { $Source } else { (Join-Path $repo "workbench") }
$srcMount = $srcTree -replace "\\", "/"   # Windows 路径转正斜杠（容器挂载要正斜杠）
$stamp = Get-Date -Format "yyMMdd-HHmmss"
$script:fail = 0

function Step([string]$name, [scriptblock]$body) {
  Write-Host ("==> " + $name) -ForegroundColor Cyan
  $rc = 0
  try { & $body; $rc = $global:LASTEXITCODE } catch { Write-Host ("    " + $_.Exception.Message) -ForegroundColor Red; $rc = 1 }
  if ($rc -ne 0) { Write-Host ("    FAIL " + $name + " (rc=" + $rc + ")") -ForegroundColor Red; $script:fail++ }
  else { Write-Host ("    PASS " + $name) -ForegroundColor Green }
}

# 跑一个容器：返回合并输出，并按标记行判成败（不靠退出码，防管道吞码）
function Run-Case([string]$image, [string]$script, [string]$marker) {
  $tag = ($image -replace "[^a-z0-9]", "") + "-" + ($marker -replace "[^a-z0-9]", "")
  $name = "wbp-" + $tag + "-" + $stamp
  $out = docker run --rm --name $name -v ($PSScriptRoot + ":/test:ro") -v ($fix + ":/w") -e HOME=/root $image sh $script 2>&1
  $text = ($out | Out-String)
  $out | ForEach-Object { Write-Host ("    " + $_) }
  $global:LASTEXITCODE = 0
  if ($text -notmatch [regex]::Escape($marker)) {
    Write-Host ("    缺判据标记: " + $marker) -ForegroundColor Red
    $global:LASTEXITCODE = 1
  } else {
    $line = ($text -split [char]10 | Where-Object { $_ -like ("*" + $marker + "*") } | Select-Object -First 1)
    Write-Host ("    判据: " + $line.Trim()) -ForegroundColor Green
  }
}

New-Item -ItemType Directory -Path $fix -Force | Out-Null

if (-not $SkipBuild) {
  Step "交叉构建夹具 linux/amd64 CGO=0" {
    Push-Location $PSScriptRoot
    try {
      $env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
      foreach ($t in @("mockofm","pooljson","poolapi","httpget")) {
        & go build -o ("bin/" + $t) ("./" + $t)
        if ($LASTEXITCODE -ne 0) { throw ("构建夹具 " + $t + " 失败") }
      }
      & go vet ./...
      if ($LASTEXITCODE -ne 0) { throw "夹具 go vet 失败" }
      $global:LASTEXITCODE = 0
    } finally { Pop-Location }
  }

  if (-not $SelfCheckOnly -and $Binary -eq "") {
    Step "交叉构建 workbench linux/amd64" {
      Push-Location $(if ($Source) { $Source } else { (Join-Path $repo "workbench") })
      try {
        $env:CGO_ENABLED = "0"; $env:GOOS = "linux"; $env:GOARCH = "amd64"
        & go build -trimpath -o (Join-Path $fix "workbench-linux") ./cmd/workbench
        if ($LASTEXITCODE -ne 0) { throw "workbench linux 构建失败（源码树不完整：先恢复 workbench/ 或改用 -Binary）" }
        $global:LASTEXITCODE = 0
      } finally { Pop-Location }
    }
  }
}

if ($Binary -ne "") { Copy-Item $Binary (Join-Path $fix "workbench-linux") -Force }

if ($SelfCheckOnly) {
  Step "mock 地基自检 debian(dash)"   { Run-Case "debian:bookworm-slim" "/test/mock_selfcheck.sh" "MOCK_CHECK_PASS" }
  Step "mock 地基自检 alpine(busybox)" { Run-Case "alpine:3.20"          "/test/mock_selfcheck.sh" "MOCK_CHECK_PASS" }
} else {
  $wbl = Join-Path $fix "workbench-linux"
  if (-not (Test-Path $wbl)) { throw ("缺 " + $wbl + "；先构建或用 -Binary 指定") }

  Step "产物静态性自检(ELF)" {
    $sig = [System.IO.File]::ReadAllBytes($wbl)[0..3]
    if ($sig[0] -ne 0x7F -or $sig[1] -ne 0x45 -or $sig[2] -ne 0x4C -or $sig[3] -ne 0x46) { throw "不是 ELF 二进制" }
    Write-Host ("    ELF ok size=" + (Get-Item $wbl).Length)
    $global:LASTEXITCODE = 0
  }

  Step "模型池全链验收 debian:bookworm-slim(dash)" { Run-Case "debian:bookworm-slim" "/test/pool_e2e.sh" "E2E_PASS" }
  Step "模型池全链验收 alpine:3.20(busybox ash)"   { Run-Case "alpine:3.20"          "/test/pool_e2e.sh" "E2E_PASS" }
}

# golang 官方镜像腿分两段：
#   A) 池门禁（必绿）—— modelpool 全包 + web 的池接口测试
#   B) 全仓测试（情报项）—— 容器内会暴露平台耦合用例（硬编码 Windows 盘符等），
#      那是 workbench/ 侧待修项，记成 PLATFORM-COUPLING 清单，不折算通过、
#      也不让它把池门禁静默拖绿或拖红。
Step "池门禁 go test (golang:1.27-bookworm)" {
  $name = "wbp-pooltest-" + $stamp
  $out = docker run --rm --name $name -v ($srcMount + ":/src") -w /src -e CGO_ENABLED=0 golang:1.27-bookworm bash -c "cd /src && go vet ./... && go test ./internal/modelpool/ -count=1 && go test ./internal/web/ -run TestPool -count=1" 2>&1
  $text = ($out | Out-String)
  $out | ForEach-Object { Write-Host ("    " + $_) }
  $global:LASTEXITCODE = 0
  if ($text -notmatch 'ok\s+workbench/internal/modelpool') { Write-Host "    缺判据: modelpool 未通过" -ForegroundColor Red; $global:LASTEXITCODE = 1 }
  if ($text -notmatch 'ok\s+workbench/internal/web') { Write-Host "    缺判据: web 池接口测试未通过" -ForegroundColor Red; $global:LASTEXITCODE = 1 }
}

Step "全仓 go test 平台耦合体检（情报项，不计门禁）" {
  $name = "wbp-alltest-" + $stamp
  $out = docker run --rm --name $name -v ($srcMount + ":/src") -w /src -e CGO_ENABLED=0 golang:1.27-bookworm bash -c "cd /src && go test ./... -count=1 2>&1 | grep -E '^(FAIL|--- FAIL|ok)' " 2>&1
  $text = ($out | Out-String)
  $fails = @($text -split [char]10 | Where-Object { $_ -match '^--- FAIL' })
  $pkgs = @($text -split [char]10 | Where-Object { $_ -match '^FAIL\t' } | ForEach-Object { ($_ -split '\s+')[1] })
  $oks = @($text -split [char]10 | Where-Object { $_ -match '^ok\s' })
  Write-Host ("    容器内通过包=" + $oks.Count + " 失败用例=" + $fails.Count + " 涉及包=" + $pkgs.Count) -ForegroundColor Yellow
  $fails | ForEach-Object { Write-Host ("      " + $_.Trim()) -ForegroundColor DarkYellow }
  # 落盘供主会话定位修复（写 fixtures 目录，不写宿主源码树）
  ($text -split [char]10 | Where-Object { $_ -match '^(--- FAIL|FAIL\t)' }) -join [char]10 |
    Set-Content -Path (Join-Path $fix "platform-coupling.txt") -Encoding UTF8
  Write-Host ("    清单已写 " + (Join-Path $fix "platform-coupling.txt")) -ForegroundColor DarkGray
  $global:LASTEXITCODE = 0   # 情报项：不阻断池门禁，但清单必须产出
}

Write-Host ("=" * 46)
if ($script:fail -eq 0) { Write-Host "DOCKER_GATE_PASS" -ForegroundColor Green; exit 0 }
Write-Host ("DOCKER_GATE_FAIL (" + $script:fail + " 段失败)") -ForegroundColor Red
exit 1

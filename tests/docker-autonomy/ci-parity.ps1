# ci-parity.ps1 — 本地 1:1 预演 CI(autonomy-e2e.yml) 的每个判据步骤
#
# 为什么需要：CI 的判据分散在两种运行环境里（宿主机 go 编译 + 容器内行为），
# 推远端才发现红很贵。本脚本按 CI 的**实际顺序与镜像**逐步跑一遍。
#
# 修正记录（本脚本首版的错）：把"编译"步骤放进 debian 容器 ⇒ rc=127（debian 无 go）。
# CI 的真实结构是：宿主 ubuntu-latest 装 go 后编译，再把二进制挂进 debian 容器跑行为。
param(
    [string]$Repo = "D:\workdesktop",
    [string]$RunId = ""
)
$ErrorActionPreference = "Continue"
if (-not $RunId) { $RunId = "parity-" + (Get-Date -Format "HHmmss") }
$here = $PSScriptRoot
$dist = Join-Path $Repo "dist"
$pass = 0; $fail = 0
function Chk([string]$name, [int]$rc, [string]$tail = "") {
    if ($rc -eq 0) { Write-Host "  [PASS] $name" -ForegroundColor Green; $script:pass++ }
    else { Write-Host "  [FAIL] $name (rc=$rc)" -ForegroundColor Red; $script:fail++ }
    if ($tail) { Write-Host ("      " + $tail) }
}

# ── 段 A：golang 镜像（等价于 CI 的 setup-go + 编译） ──
Write-Host "=== A1 交叉编译 + 静态自检 ===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-build" -v "${Repo}:/src" -v "${dist}:/out" -v "${here}:/dk:ro" `
    golang:1.27-bookworm bash /dk/build-linux.sh 2>&1 | Out-String
Write-Host $o
Chk "build-linux(static)" $LASTEXITCODE (($o -split "`n" | Select-String "BUILD_PASS|STATIC_OK|BUILD_FAIL") -join " | ")

Write-Host "=== A2 替身编译 ===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-fake" -v "${here}:/dk:ro" -v "${dist}:/out" `
    golang:1.27-bookworm bash /dk/build-fakedsh.sh 2>&1 | Out-String
Chk "build-fakedsh" $LASTEXITCODE (($o -split "`n" | Select-String "FAKEDSH_BUILD_OK") -join "")

# ── 段 B：debian 镜像（等价于 CI 的 docker run 步骤） ──
Write-Host "=== B1 判据覆盖（静态闸 1）===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-crit" -v "${Repo}:/src:ro" -v "${here}:/dk:ro" `
    debian:bookworm-slim bash /dk/criteria-coverage.sh /src/workbench 2>&1 | Out-String
Chk "criteria-coverage" $LASTEXITCODE (($o -split "`n" | Select-String "CRITERIA_COVERAGE_OK|CRITERIA_COVERAGE_FAIL|UNHANDLED") -join " | ")

Write-Host "=== B2 枚举一致性（静态闸 2）===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-cons" -v "${Repo}:/src:ro" -v "${here}:/dk:ro" `
    debian:bookworm-slim bash /dk/consistency-scan.sh /src/workbench 2>&1 | Out-String
$warnCount = (($o -split "`n" | Select-String "WARN:").Count)
Chk "consistency-scan" $LASTEXITCODE ("WARN=$warnCount")

Write-Host "=== B3 能力面冒烟（行为闸 1）===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-cap" -v "${dist}/workbench-linux-amd64:/test/wb:ro" -v "${here}:/dk:ro" `
    debian:bookworm-slim bash -c "cp /test/wb /tmp/wb && chmod +x /tmp/wb && bash /dk/capability-smoke.sh /tmp/wb" 2>&1 | Out-String
Chk "capability-smoke" $LASTEXITCODE (($o -split "`n" | Select-String "CAPABILITY_SMOKE_OK|CAPABILITY_SMOKE_FAIL") -join "")

Write-Host "=== B4 容器闭环端到端（行为闸 2）===" -ForegroundColor Cyan
$o = docker run --rm --name "fenjue-parity-e2e" `
    -v "${dist}/workbench-linux-amd64:/test/wb:ro" -v "${dist}/fakedsh:/test/fakedsh:ro" -v "${here}:/dk" `
    -e "RUN_ID=$RunId" debian:bookworm-slim `
    bash -lc "apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq python3 >/dev/null 2>&1 && bash /dk/container.sh" 2>&1 | Out-String
Chk "closed-loop e2e" $LASTEXITCODE (($o -split "`n" | Select-String "E2E_PASS|E2E_FAIL|SUMMARY_PASS|SUMMARY_FAIL") -join " | ")

Write-Host ""
Write-Host "CI_PARITY: $pass PASS / $fail FAIL"
if ($fail -gt 0) { Write-Host "CI_PARITY_FAIL" -ForegroundColor Red; exit 1 }
Write-Host "CI_PARITY_PASS（本地已按 CI 顺序全绿）" -ForegroundColor Green
exit 0

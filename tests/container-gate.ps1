#!/usr/bin/env pwsh
# container-gate.ps1 — 容器门禁（Linux VM 模拟），本仓唯一的 Linux 侧真实验收入口
#
# 为什么存在（2026-10-19 老大指令「用 Docker 虚拟机模拟，别乱折腾了」）：
#   宿主是 Windows，本机 go test 全绿掩盖了平台耦合；上游免费车道又按出口 IP 封禁，
#   真实模型 e2e 在宿主上做不了。把「不依赖模型」的全部验收搬进 Linux 容器后，
#   一次就抓出 9 处「本机绿、容器红」的真缺陷（见 fails.json 分类）。
#
# 用法（不会碰宿主任何数据，不发布端口，只在容器内跑）：
#   pwsh tests/container-gate.ps1                 # 默认对 src-backup 跑全链
#   pwsh tests/container-gate.ps1 -Source workbench   # 指向 <repo>/workbench
#   pwsh tests/container-gate.ps1 -Only VET       # 只跑单项（VET / TEST / BUILD）
#
# 判据（三态，禁止把「取不到数」洗成通过）：
#   PASS      —— 该阶段退出码 0
#   FAIL      —— 该阶段退出码非 0，且失败可归因到源码（打印原始输出）
#   UNVERIFIED—— 前置条件缺失（如镜像不在本地、源码树不存在），必须显式说明，不得折算成 PASS
#
# 作用域守卫：
#   - 容器名统一 wb-gate-<时间戳>，只删自己建的；不发布宿主端口；不挂宿主数据根。
#   - 一切写入限制在容器 /tmp；宿主只读挂载源码。
#   - 退出码：0 全 PASS；1 有 FAIL；2 有 UNVERIFIED 且无 FAIL。

param(
    [string]$Source = "src-backup",
    [string]$Image = "golang:1.27-bookworm",
    [ValidateSet("ALL", "VET", "TEST", "BUILD", "CRITERIA", "LOOP")]
    [string]$Only = "ALL",
    # 闭环行为验收需要的自主验收集目录（相对仓库根）
    [string]$AutonomyDir = "tests/docker-autonomy",
    [string]$RepoRoot = ""
)

$ErrorActionPreference = "Stop"
if (-not $RepoRoot) { $RepoRoot = Split-Path -Parent $PSScriptRoot }

$srcPath = Join-Path $RepoRoot $Source
$workDir = Join-Path $srcPath "workbench"
if (-not (Test-Path $workDir)) {
    # 兼容「源码根本身就是 workbench/」的形态
    if ((Test-Path (Join-Path $srcPath "go.mod")) -and (Test-Path (Join-Path $srcPath "internal"))) {
        $workDir = $srcPath
        $srcPath = Split-Path -Parent $srcPath
    }
}

Write-Host "==> 0/4 前置检查" -ForegroundColor Cyan
$results = [ordered]@{}
# 挂载串提到脚本作用域：Invoke-GateStage 内的局部变量在后续阶段取不到（实测 CRITERIA 阶段报
# "invalid reference format"，因为 $mount 为空）。
$mount = "{0}:/src" -f $srcPath

if (-not (Test-Path $workDir)) {
    Write-Host "  源码树不存在: $workDir" -ForegroundColor Red
    $results["PRECHECK"] = "UNVERIFIED: 源码树不存在 ($workDir)"
} else {
    Write-Host "  源码树: $workDir"
    $goCount = @(Get-ChildItem $workDir -Recurse -Force -File -Filter '*.go' -ErrorAction SilentlyContinue).Count
    Write-Host "  .go 文件数: $goCount"
    if ($goCount -eq 0) { $results["PRECHECK"] = "UNVERIFIED: 源码树为空（0 个 .go 文件）" }
}

$imageOk = $false
if (-not $results.Contains("PRECHECK")) {
    $img = docker images --format "{{.Repository}}:{{.Tag}}" 2>$null | Where-Object { $_ -eq $Image }
    if ($img) { $imageOk = $true; Write-Host "  镜像已就绪: $Image" }
    else { $results["PRECHECK"] = "UNVERIFIED: 本地无镜像 $Image（不自动拉取，避免网络噪音）" }
}

function Invoke-GateStage {
    param([string]$Name, [string]$Inner, [string]$WorkDir)
    Write-Host ""
    Write-Host "==> $Name" -ForegroundColor Cyan
    $cname = "wb-gate-" + (Get-Date -Format 'HHmmss') + "-" + $Name.ToLower()
    $innerCmd = "cd '$WorkDir' && " + $Inner
    $out = docker run --rm --name $cname -v $mount -w /src $Image bash -c $innerCmd 2>&1 | Out-String
    $rc = $LASTEXITCODE
    if ($rc -eq 0) {
        Write-Host "  PASS" -ForegroundColor Green
        $script:results[$Name] = "PASS"
    } else {
        Write-Host "  FAIL (exit $rc)" -ForegroundColor Red
        Write-Host ($out.Trim() -split "`n" | Select-Object -First 40 | Out-String)
        $script:results[$Name] = "FAIL (exit $rc)"
    }
    return $out
}

if ($imageOk) {
    $common = 'export PATH=$PATH:/usr/local/go/bin; export CGO_ENABLED=0; export GOFLAGS=-mod=mod'
    if ($Only -in @("ALL", "VET")) {
        Invoke-GateStage -Name "VET" -Inner "$common; go vet ./... 2>&1 | head -40; echo VET_RC=${PIPESTATUS[0]}; exit ${PIPESTATUS[0]}" -WorkDir "/src/workbench" | Out-Null
    }
    if ($Only -in @("ALL", "TEST")) {
        Invoke-GateStage -Name "TEST" -Inner "$common; go test ./... -count=1 2>&1 | tail -70; echo TEST_RC=${PIPESTATUS[0]}; exit ${PIPESTATUS[0]}" -WorkDir "/src/workbench" | Out-Null
    }
    if ($Only -in @("ALL", "BUILD")) {
        Invoke-GateStage -Name "BUILD" -Inner "$common; go build -trimpath -o /tmp/wb-linux ./cmd/workbench 2>&1 | head -30; RC=${PIPESTATUS[0]}; if [ $RC -eq 0 ]; then ls -l /tmp/wb-linux; fi; exit $RC" -WorkDir "/src/workbench" | Out-Null
    }
    if ($Only -in @("ALL", "CRITERIA")) {
        # 判据「声明 vs 实现」交叉校验：编译前即可抓住"声明了判据却没实现判定"整类缺陷。
        # 实测由来：types.go 声明 CriterionVerified，planner 会生成它，acceptance 的 switch
        # 却缺分支 ⇒ 判据恒 false ⇒ 目标永不通过验收 ⇒ goalDone/nextGoal 永不出现。
        $dk = Join-Path $RepoRoot $AutonomyDir
        if (-not (Test-Path (Join-Path $dk "criteria-coverage.sh"))) {
            $results["CRITERIA"] = "UNVERIFIED: 缺 $AutonomyDir/criteria-coverage.sh"
        } else {
            $dkMount = "{0}:/dk:ro" -f $dk
            $cname = "wb-gate-" + (Get-Date -Format 'HHmmss') + "-criteria"
            $out = docker run --rm --name $cname -v $mount -v $dkMount -w /src $Image bash -c 'bash /dk/criteria-coverage.sh /src/workbench 2>&1; echo RC=$?' 2>&1 | Out-String
            $rc = $LASTEXITCODE
            if ($rc -eq 0) { Write-Host "  PASS" -ForegroundColor Green; $results["CRITERIA"] = "PASS" }
            else { Write-Host "  FAIL (exit $rc)" -ForegroundColor Red; Write-Host ($out.Trim() -split "`n" | Select-Object -First 20 | Out-String); $results["CRITERIA"] = "FAIL (exit $rc)" }
        }
    }
    if ($Only -in @("ALL", "LOOP")) {
        # 闭环行为验收：容器内确定性通道跑完整自主闭环（不依赖外部模型可用性）。
        # 这是「能编译 ≠ 行为正确」的那道闸：编译通过、单测全绿，闭环仍可能跑不到终点。
        $runner = Join-Path $RepoRoot (Join-Path $AutonomyDir "docker-run.ps1")
        if (-not (Test-Path $runner)) {
            $results["LOOP"] = "UNVERIFIED: 缺 $AutonomyDir/docker-run.ps1"
        } else {
            Write-Host ""
            Write-Host "==> LOOP (容器内自主闭环端到端)" -ForegroundColor Cyan
            # 源码树的相对路径必须区分两种形态（实测 bug：-Source workbench 时拼成 workbench/workbench）：
            #   a) <Source>/workbench/go.mod 存在 -> 源码根是 <Source>，模块在 <Source>/workbench
            #   b) <Source>/go.mod 存在          -> <Source> 本身就是模块根
            $relSrc = if (Test-Path (Join-Path $RepoRoot (Join-Path $Source "workbench\go.mod"))) {
                $Source + "/workbench"
            } elseif (Test-Path (Join-Path $RepoRoot (Join-Path $Source "go.mod"))) {
                $Source
            } else {
                $Source + "/workbench"   # 兜底：按 a) 处理，让错误在 docker-run 内显式报出
            }
            # RunId 必须唯一：固定名会与上一次残留容器撞名（实测 exit 137）。
            $runId = "gate-" + (Get-Date -Format 'HHmmss')
            $out = & pwsh -NoProfile -File $runner -SrcDir $relSrc -RunId $runId 2>&1 | Out-String
            $rc = $LASTEXITCODE
            if ($rc -eq 0 -and $out -match "E2E_PASS") {
                Write-Host "  PASS (E2E_PASS)" -ForegroundColor Green
                $results["LOOP"] = "PASS"
            } elseif ($rc -in @(125, 126, 137)) {
                # 基础设施失败 ≠ 行为不合格。实测两类：
                #   137 = 容器被 docker rm -f 杀掉（并发会话争抢同名容器，docker-run.ps1 第 55 行先删后建）；
                #   125/126 = docker 本身拒绝执行（引用格式错、命令不可执行）。
                # 这类必须记 UNVERIFIED 并说明原因，禁止折算成 FAIL（那是把"没测成"当"测得差"）。
                Write-Host "  UNVERIFIED (基础设施失败 exit $rc)" -ForegroundColor Yellow
                Write-Host ($out.Trim() -split "`n" | Select-Object -Last 12 | Out-String)
                $results["LOOP"] = "UNVERIFIED: 容器被并发争抢/基础设施失败 (exit $rc)，非行为不合格；复核请单独跑 docker-run.ps1"
            } else {
                Write-Host "  FAIL (exit $rc)" -ForegroundColor Red
                Write-Host ($out.Trim() -split "`n" | Select-Object -Last 25 | Out-String)
                $results["LOOP"] = "FAIL (exit $rc)"
            }
        }
    }
}

Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
$fails = @($results.GetEnumerator() | Where-Object { $_.Value -like "FAIL*" })
$unver = @($results.GetEnumerator() | Where-Object { $_.Value -like "UNVERIFIED*" })
foreach ($k in $results.Keys) {
    $color = if ($results[$k] -like "FAIL*") { "Red" } elseif ($results[$k] -like "UNVERIFIED*") { "Yellow" } else { "Green" }
    Write-Host ("  " + $k.PadRight(12) + $results[$k]) -ForegroundColor $color
}
Write-Host "========================================" -ForegroundColor Cyan
if ($fails.Count -gt 0) { exit 1 }
if ($unver.Count -gt 0) { exit 2 }
exit 0
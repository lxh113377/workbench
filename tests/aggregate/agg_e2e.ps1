# agg_e2e.ps1 -- 聚合车道端到端验收的可复跑入口（Windows）
#
# 证明链：池内自填兼容端点 → OFM aggregate 车道 → 真实 DSH headless → 请求真打到该端点。
# 判据：mock 的 chatHits>=1（不是静默改道免费车道）+ DSH 退出码 0 + 产出 final 文本。
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File tests\aggregate\agg_e2e.ps1
# 退出码: 0 = 证据齐全；非 0 = 证据不足（详见末行 RESULT）
param([int]$Port = 18434)
$ErrorActionPreference = "Stop"
if (-not (Test-Path "D:\workdesktop\dist\mockofm.exe")) { throw "缺 dist\mockofm.exe（先跑 setup\docker\build-mockofm.sh 或 go build tests\docker\mockofm）" }
$env:AGG_E2E_PORT = "$Port"
$env:AGG_E2E_ROOT = Join-Path $env:TEMP ("wb-agg-e2e-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
$py = (Get-Command python.exe -ErrorAction SilentlyContinue)
if (-not $py) { throw "需要本机 python.exe（仅用于派生进程与轮询，不参与产品链路）" }
# 固定名会被**重叠运行**互写/互删（计划任务/并行会话，本仓在册族）：
# 2026-10-25 round 174：此前 root 恒为 %TEMP%\wb-agg-e2e ⇒ 两次并发跑同一根，且跨运行残留。
# 现在每次运行取新 guid 的根；跑完把本次的根删掉（best-effort，失败不改变判据结论）。
$aggRootForCleanup = $env:AGG_E2E_ROOT
& $py.Source "$PSScriptRoot\agg_e2e.py"
$rc = $LASTEXITCODE
if ($aggRootForCleanup -and (Test-Path -LiteralPath $aggRootForCleanup)) { Remove-Item -Recurse -Force -LiteralPath $aggRootForCleanup -ErrorAction SilentlyContinue }
"AGG_E2E_RC=$rc"
exit $rc
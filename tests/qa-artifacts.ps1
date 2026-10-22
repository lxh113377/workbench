# qa-artifacts.ps1 — 产物质量抽检（ds 端 · 建议每次导出前跑）
#
# 查三件事：
#   1) final.md 是否被"幻觉工具调用"污染（含 tool_call/param_key 字样）
#      —— 命中则用 workspace 主产物替换；没有主产物则从 param_value 壳里剥出真实内容
#   2) 所有完成任务的 workspace 产物统计（文件数/字节/是否有可执行校验）
#   3) 输出摘要（通过/失败/污染修复数）
param(
    [string]$OutputRoot = "D:\workdesktop\dist\workbench-output"
)
$ErrorActionPreference = "Stop"
$taskRoot = Join-Path $OutputRoot "tasks"
$badMark = "<" + "tool_" + "call" + ">"
$report = @{ scanned = 0; polluted = 0; repaired = 0; artifacts = 0; bytes = 0; verified = 0 }
foreach ($td in (Get-ChildItem $taskRoot -Directory -Filter "task-*" -ErrorAction SilentlyContinue)) {
    $fp = Join-Path $td.FullName "final.md"
    if (-not (Test-Path $fp)) { continue }
    $report.scanned++
    $txt = [IO.File]::ReadAllText($fp)
    $isBad = ($txt -match $badMark) -or ($txt -match "param_key")
    if (-not $isBad) { continue }
    $report.polluted++
    # 优先用 workspace 里最大的 md
    $best = $null; $bestSize = 0
    $ws = Join-Path $td.FullName "workspace"
    if (Test-Path $ws) {
        foreach ($f in (Get-ChildItem $ws -Recurse -File -Filter *.md -ErrorAction SilentlyContinue)) {
            if ($f.Length -gt $bestSize) { $best = $f; $bestSize = $f.Length }
        }
    }
    $done = $false
    if ($best -and $bestSize -gt 200) {
        [IO.File]::Copy($best.FullName, $fp, $true); $done = $true
    } else {
        $i = $txt.IndexOf("<param_value>")
        if ($i -ge 0) {
            $j = $txt.IndexOf("</param_value>", $i)
            if ($j -gt $i) {
                $inner = $txt.Substring($i + 13, $j - $i - 13)
                if ($inner.Length -gt 200 -and $inner -notmatch $badMark) {
                    [IO.File]::WriteAllText($fp, $inner, (New-Object System.Text.UTF8Encoding($false))); $done = $true
                }
            }
        }
    }
    if ($done) { $report.repaired++ }
}
# 产物统计
foreach ($td in (Get-ChildItem $taskRoot -Directory -Filter "task-*" -ErrorAction SilentlyContinue)) {
    $ws = Join-Path $td.FullName "workspace"
    if (-not (Test-Path $ws)) { continue }
    foreach ($f in (Get-ChildItem $ws -Recurse -File -ErrorAction SilentlyContinue)) {
        $report.artifacts++
        $report.bytes += $f.Length
    }
    if (Test-Path (Join-Path $td.FullName "verify.json")) {
        $v = (Get-Content (Join-Path $td.FullName "verify.json") -Raw | ConvertFrom-Json)
        if ($v.pass) { $report.verified++ }
    }
}
Write-Host ("[qa] scanned=" + $report.scanned + " polluted=" + $report.polluted + " repaired=" + $report.repaired)
Write-Host ("[qa] artifacts=" + $report.artifacts + " bytes=" + $report.bytes + " verified=" + $report.verified)
if ($report.polluted -eq $report.repaired) { Write-Host "[qa] PASS (no residual pollution)" } else { Write-Host ("[qa] WARN residual=" + ($report.polluted - $report.repaired)) }
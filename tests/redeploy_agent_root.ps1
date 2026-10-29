# redeploy_agent_root.ps1 -- controlled swap of the live agent-root engine (round 66; tracked 2026-10-29).
#
# Why this file is now tracked, not left in xtmp/ (issue #14, measured 2026-10-29): the repo had NO
#   tracked deploy-authority artifact at all (`git ls-files` grep deploy = 0). CONTRIBUTING.md:159 only
#   says "confirm dist/SHA256SUMS was updated" -- a manual step. The real swap was done by this script
#   sitting in gitignored xtmp/, and it copied the exe WITHOUT rewriting the manifest, so the manifest
#   and the artifact were bound to drift (measured twice: manifest mtime 1-2 min older than the exe).
#   Rule #29 (zero manual steps) says: turn the manual confirmation into code. So the deploy authority
#   is tracked, and the swap now regenerates the manifest from the artifact bytes itself.
# Keep this file ASCII-only: OEM-codepage readers mangle non-ASCII (unchanged from the original).
# -Src defaults to dist/workbench.exe (the build output); pass it explicitly to swap from a worktree.
param(
    [string]$Root = 'D:\workbench-agent',
    [string]$Src = 'D:\workdesktop\dist\workbench.exe'
)
$ErrorActionPreference = 'Stop'
$Exe = Join-Path $Root 'workbench.exe'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$DQ = [string][char]34

function Get-Token([int]$port, [string]$dq) {
    try {
        $h = (Invoke-WebRequest -Uri ('http://127.0.0.1:' + $port + '/') -UseBasicParsing -TimeoutSec 5).Content
        $m = [regex]::Match($h, '__WB_TOKEN__\s*=\s*' + $dq + '([a-zA-Z0-9_-]+)')
        if ($m.Success) { return $m.Groups[1].Value }
    } catch { }
    return $null
}

$portPath = Join-Path $Root 'runtime\panel.port'
$port = [int](Get-Content -LiteralPath $portPath -Raw).Trim()
$tok = Get-Token $port $DQ
Write-Host ('STEP0 port=' + $port + ' token_found=' + [bool]$tok)
if (-not $tok) { Write-Host 'ABORT no panel token'; exit 3 }

$uStop = 'http://127.0.0.1:' + $port + '/api/engine/stop?token=' + $tok
try { $null = Invoke-WebRequest -Uri $uStop -Method POST -Body '{}' -ContentType 'application/json' -UseBasicParsing -TimeoutSec 10 } catch { Write-Host ('note stop: ' + $_.Exception.Message) }
Start-Sleep -Seconds 3

& $Exe --quit 2>&1 | ForEach-Object { Write-Host ('quit said: ' + $_) }
$deadline = (Get-Date).AddSeconds(45)
while ((Get-Date) -lt $deadline -and @(Get-Process -Name 'workbench' -ErrorAction SilentlyContinue).Count -gt 0) { Start-Sleep -Seconds 2 }
$alive = @(Get-Process -Name 'workbench' -ErrorAction SilentlyContinue).Count
Write-Host ('STEP2 procs_after_quit=' + $alive)
if ($alive -gt 0) { Write-Host 'ABORT process still alive (not touching the exe)'; exit 4 }

Copy-Item -LiteralPath $Exe -Destination ($Exe + '.bak-round66-' + $stamp) -Force
Copy-Item -LiteralPath $Src -Destination $Exe -Force
# issue #14: rewrite the checksum manifest right after the swap, from the artifact bytes. The manifest
# is the only machine-readable proof of artifact identity (#40 verifies live sha == manifest), yet only
# build.ps1 used to write it -- and this swap path does not go through build.ps1. Share the single
# formatter in tests/sync_dist_manifest.ps1 so the text format has exactly one definition.
& powershell -NoProfile -ExecutionPolicy Bypass -File 'D:\workdesktop\tests\sync_dist_manifest.ps1' -Exe $Exe | Out-Null
# 0 = rewritten and read back; 3 = already correct (idempotent). Anything else (1 = failure) must not
# pass silently: a stale manifest here is exactly the false-green #40 warned about.
if ($LASTEXITCODE -ne 0 -and $LASTEXITCODE -ne 3) { Write-Host ('ABORT manifest sync rc=' + $LASTEXITCODE + ' -- refusing to leave agent root with a stale SHA256SUMS'); exit 6 }
$h = (Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash
$eol = [string][char]13 + [string][char]10
Set-Content -LiteralPath (Join-Path $Root 'SHA256SUMS') -Value ($h + '  workbench.exe' + $eol) -Encoding ascii -NoNewline
$revline = ((& go version -m $Exe 2>&1 | Out-String) -split [string][char]10 | Where-Object { $_ -match 'vcs.revision' } | Select-Object -First 1)
Write-Host ('STEP3 sha256=' + $h)
Write-Host ('STEP3 ' + $revline.Trim())

. 'D:\workdesktop\tests\lib_silent_launch.ps1'
$pid2 = Start-WBInstance -Path $Exe -Arguments '--silent'
Write-Host ('STEP4 launched_pid=' + $pid2)
$deadline = (Get-Date).AddSeconds(45)
$newPort = 0
$tok2 = $null
while ((Get-Date) -lt $deadline) {
    if (Test-Path -LiteralPath $portPath) {
        $c = [int](Get-Content -LiteralPath $portPath -Raw).Trim()
        $t = Get-Token $c $DQ
        if ($c -gt 0 -and $t) { $newPort = $c; $tok2 = $t; break }
    }
    Start-Sleep -Seconds 2
}
Write-Host ('STEP4 panel_port=' + $newPort)
if ($newPort -le 0) { Write-Host 'ABORT panel unreachable after launch'; exit 5 }

$uStart = 'http://127.0.0.1:' + $newPort + '/api/engine/start?token=' + $tok2
try { $null = Invoke-WebRequest -Uri $uStart -Method POST -Body '{}' -ContentType 'application/json' -UseBasicParsing -TimeoutSec 15 } catch { Write-Host ('note start: ' + $_.Exception.Message) }
Start-Sleep -Seconds 4
$st = (Invoke-WebRequest -Uri ('http://127.0.0.1:' + $newPort + '/') -UseBasicParsing -TimeoutSec 5).Content
$phase = ([regex]::Match($st, 'data-state=' + $DQ + '([a-z]+)')).Groups[1].Value
$flag = Join-Path $Root 'runtime\user-quit.flag'
Write-Host ('STEP5 engine_state=' + $phase + ' user_quit_marker_present=' + [bool](Test-Path -LiteralPath $flag))
$fail = Invoke-RestMethod -Uri ('http://127.0.0.1:' + $newPort + '/api/failures?token=' + $tok2) -TimeoutSec 10
$kinds = ($fail.buckets | ForEach-Object { $_.kind + ':' + $_.count }) -join ', '
Write-Host ('STEP5 failures_total=' + $fail.total + ' buckets=' + $kinds)
Write-Host 'REDEPLOY_DONE'


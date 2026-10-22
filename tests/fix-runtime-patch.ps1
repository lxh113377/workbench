# fix-runtime-patch.ps1 - runtime guard (2026-10-21): keep profile patch SAFE
#
# 2026-10-19 history: rewrote agent-instructions from disabled:true into a
#   "config: append: |" block injecting a PROTOCOL FIX.
# 2026-10-21 root cause: dsh-agent-instructions 0.2.0-rc.2 DROPPED append and
#   made maxBytes REQUIRED, so that injected block crashes every dsh subprocess
#   at resolveConfig (maxBytes missing) -> boot_failed storm -> queue fills but
#   nothing runs (panel: "running but 0 output today"). workbench.exe already
#   writes a SAFE template (agent-instructions: disabled, keeps reasoningEffort).
#   keepalive re-runs this after every relaunch, so the old injection kept
#   re-poisoning the patch. This guard REVERSES it: self-heal any append /
#   PROTOCOL-FIX poison back to disabled:true, never inject. Idempotent.
param(
    [string]$PatchFile = "D:\workdesktop\dist\runtime\dsh-home\profiles\web-desktop\cordis.patch.yml"
)
$ErrorActionPreference = "Stop"
if (-not (Test-Path $PatchFile)) { Write-Host "[fix-patch] WARN not found"; exit 0 }
$raw = [IO.File]::ReadAllText($PatchFile)
if ($raw.IndexOf("append:") -lt 0 -and $raw.IndexOf("PROTOCOL FIX") -lt 0) {
    Write-Host "[fix-patch] already safe"
    exit 0
}
$NL = [string][char]10
$lines = $raw -split "\r?\n"
$out = New-Object System.Collections.Generic.List[string]
$i = 0
while ($i -lt $lines.Count) {
    $ln = $lines[$i]
    if ($ln -match "^- id: agent-instructions") {
        $out.Add("- id: agent-instructions")
        $out.Add("  disabled: true")
        $i++
        while ($i -lt $lines.Count) {
            $n = $lines[$i]
            if ($n -match "^- id:" -or $n -match "^#") { break }
            $i++
        }
        continue
    }
    $out.Add($ln)
    $i++
}
$clean = New-Object System.Collections.Generic.List[string]
$skip = $false
foreach ($ln in $out) {
    if ($ln -match "PROTOCOL FIX") { $skip = $true; continue }
    if ($skip) {
        if ($ln -match "^\s") { continue }
        $skip = $false
    }
    $clean.Add($ln)
}
$text = ($clean -join $NL) + $NL
[IO.File]::WriteAllText($PatchFile, $text, (New-Object System.Text.UTF8Encoding($false)))
Write-Host "[fix-patch] self-healed agent-instructions append-injection -> disabled"

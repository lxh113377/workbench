# ability-probe.ps1 — 旧 EXE 能力面探针（帮 Lead 界定"丢了多少能力"）
#
# 目的：源码被清空后，用留存的时间点 EXE 做**行为级**能力比对。
# 只读探针：每个 EXE 复制到临时隔离目录再跑，不改任何已有目录。
#
# 两个必须注意的实现点（本脚本已踩过并修）：
#   1) 不能用「先 WaitForExit 再 ReadToEnd」：子进程 stdout 管道缓冲区写满后
#      会阻塞在写，而父进程还在 WaitForExit ⇒ 互相等待直到超时被误判成"命令不存在"。
#      正解：BeginOutputReadLine/BeginErrorReadLine 异步收流，或先起读任务再等退出。
#   2) 子命令名必须与源码注册一致（--model-pool，不是 --pool），否则探到的"缺失"是探针的错。
param(
    [string[]]$Exe = @(),
    [string]$OutJson = ""
)
$ErrorActionPreference = "Continue"
$here = $PSScriptRoot
$repo = Split-Path -Parent (Split-Path -Parent $here)
if ($Exe.Count -eq 0) {
    $Exe = @(Get-ChildItem (Join-Path $repo "_recovery") -Filter "workbench*.exe" | Select-Object -ExpandProperty FullName)
    $Exe += (Join-Path $repo "dist\workbench.exe")
}
if (-not $OutJson) { $OutJson = Join-Path $here ("ability-" + (Get-Date -Format "yyyyMMdd-HHmmss") + ".json") }

$probes = @(
    @{ name = "version";          args = @("--version");                expect = "workbench" },
    @{ name = "diagnose";         args = @("--diagnose");               expect = "layout|profileError" },
    @{ name = "status";           args = @("--status");                 expect = "phase" },
    @{ name = "governance";       args = @("--governance","status");    expect = "契约|level|Level|Summary" },
    @{ name = "acceptance";       args = @("--acceptance");             expect = "PASS|UNVERIFIED|FAIL|合计" },
    @{ name = "autonomySelftest"; args = @("--autonomy","selftest");     expect = "RESULT|PASS" },
    @{ name = "autonomyStatus";   args = @("--autonomy","status");       expect = "enabled|active" },
    @{ name = "autonomyPlan";     args = @("--autonomy","plan");         expect = "规划|title|Draft" },
    @{ name = "workspaceStatus";  args = @("--workspace","status");      expect = "mode|project" },
    @{ name = "poolStatus";       args = @("--model-pool","status");     expect = "model|lane|可用|total" },
    @{ name = "soak";             args = @("--soak");                   expect = "soak|unknown|required|数据" },
    @{ name = "poolLegacyName";   args = @("--pool","status");          expect = "(__none__)" }
)

# Run-One 异步收流跑一个子命令，返回 @{ exit; text }
# Run-One 跑一个子命令。用 ReadToEndAsync 的 Task 读取（避免"管道写满 + WaitForExit"死锁，
# 也避免 Register-ObjectEvent 在独立 runspace 里拿不到文本的问题）。
function Run-One([string]$exePath, [string[]]$argList, [int]$timeoutMs = 20000) {
    $psi = [System.Diagnostics.ProcessStartInfo]::new()
    $psi.FileName = $exePath
    $psi.WorkingDirectory = (Split-Path -Parent $exePath)
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    foreach ($a in $argList) { [void]$psi.ArgumentList.Add($a) }
    $proc = [System.Diagnostics.Process]::new()
    $proc.StartInfo = $psi
    [void]$proc.Start()
    $outTask = $proc.StandardOutput.ReadToEndAsync()
    $errTask = $proc.StandardError.ReadToEndAsync()
    $exited = $proc.WaitForExit($timeoutMs)
    if (-not $exited) {
        try { $proc.Kill($true) } catch {}
        [void]$proc.WaitForExit(3000)
    }
    $outText = ""
    $errText = ""
    try { $outText = $outTask.GetAwaiter().GetResult() } catch {}
    try { $errText = $errTask.GetAwaiter().GetResult() } catch {}
    $code = if ($exited) { $proc.ExitCode } else { -1 }
    $flat = (($outText + " " + $errText) -replace "\s+", " ").Trim()
    $proc.Dispose()
    return @{ exit = $code; text = $flat; timedOut = (-not $exited) }
}

$report = @()
foreach ($e in $Exe) {
    if (-not (Test-Path $e)) { continue }
    $name = Split-Path -Leaf $e
    $sandbox = Join-Path ([System.IO.Path]::GetTempPath()) ("wbprobe-" + [guid]::NewGuid().ToString("N").Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $sandbox | Out-Null
    $copy = Join-Path $sandbox "workbench.exe"
    Copy-Item $e $copy -Force
    $row = [ordered]@{ exe = $name; bytes = (Get-Item $e).Length; version = ""; probes = [ordered]@{} }
    foreach ($pr in $probes) {
        $r = Run-One -exePath $copy -argList @($pr.args)
        $matched = if ($pr.expect -eq "(__none__)") { -not [string]::IsNullOrWhiteSpace($r.text) } else { $r.text -match $pr.expect }
        $row.probes[$pr.name] = [ordered]@{
            exit = $r.exit; matched = [bool]$matched; timedOut = [bool]$r.timedOut
            head = ($r.text -replace "\s+", " ").Substring(0, [Math]::Min(110, ($r.text -replace "\s+", " ").Length))
        }
        if ($pr.name -eq "version") { $row.version = ($r.text -replace "\s+", " ").Trim() }
    }
    Remove-Item $sandbox -Recurse -Force -ErrorAction SilentlyContinue
    $report += [pscustomobject]$row
}

$report | ConvertTo-Json -Depth 6 | Set-Content -Path $OutJson -Encoding UTF8
Write-Host "报告: $OutJson"
foreach ($r in $report) {
    $ok = 0; $tot = 0
    foreach ($k in $r.probes.Keys) { if ($k -eq "poolLegacyName") { continue }; $tot++; if ($r.probes[$k].matched) { $ok++ } }
    Write-Host ("  " + $r.exe + "  v=" + $r.version + "  能力 " + $ok + "/" + $tot)
    foreach ($k in $r.probes.Keys) {
        $v = $r.probes[$k]
        $mark = if ($v.matched) { "OK" } else { if ($v.timedOut) { "TIMEOUT" } else { "--" } }
        Write-Host ("      [" + $mark + "] " + $k + " (exit=" + $v.exit + ")")
    }
}

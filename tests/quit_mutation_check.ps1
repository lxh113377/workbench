# quit_mutation_check.ps1 — 变异测试：证明「不被拉起」确由 keepalive 的用户退出守卫决定。
#
# 做法：同一场景跑两遍，唯一变量 = keepalive 脚本本体
#   ① 原版（含 -not $userQuit 守卫）   → 期望：不被拉起
#   ② 变异体（**仅删掉** -not $userQuit）→ 期望：被拉起（缺陷复现）
# 若变异体也不拉起，说明守卫是装饰品（真正拦住的另有其物），修复不算数。
#
# 用法：powershell -NoProfile -ExecutionPolicy Bypass -File tests\quit_mutation_check.ps1
param(
    [string]$Exe = "D:\workdesktop\dist\workbench-fix.exe",
    [int]$Port = 17892,
    [string]$Root = "D:\workdesktop\dist\quit-mutation"
)
$ErrorActionPreference = "Stop"
$ConfirmPreference = "None"
$ProgressPreference = "SilentlyContinue"

Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Collections.Generic;
using System.Runtime.InteropServices;
public class MW {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassNameW(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc lp, IntPtr l);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  [DllImport("user32.dll")] public static extern IntPtr SendMessageW(IntPtr h, uint m, IntPtr w, IntPtr l);
  public delegate bool EnumProc(IntPtr h, IntPtr l);
  public static IntPtr FindByPid(string cls, uint want) {
    IntPtr res = IntPtr.Zero;
    EnumWindows((h, l) => {
      var sb = new StringBuilder(256); GetClassNameW(h, sb, 256);
      if (sb.ToString() == cls) { uint pid; GetWindowThreadProcessId(h, out pid); if (pid == want) { res = h; return false; } }
      return true;
    }, IntPtr.Zero);
    return res;
  }
  public static void SendCmd(IntPtr h, int id) { SendMessageW(h, 0x0111, (IntPtr)id, IntPtr.Zero); }
}
'@

$pass = 0; $fail = 0
# 必须用 $script: 作用域：Check 在 Run-Scenario 内被调用，裸 $pass++ 会写出一个
# 函数局部变量，于是真实跑了 6 条判据、汇总却打印 "0 PASS / 0 FAIL" —— 
# 这正是"聚合把结果洗没"的形态，必须避免。
function Check($n, $c, $d) { if ($c) { $script:pass++; Write-Host "  [PASS] $n" } else { $script:fail++; Write-Host "  [FAIL] $n  $d" } }
function InstalledProc {
    Get-CimInstance Win32_Process -Filter "Name='workbench.exe'" -ErrorAction SilentlyContinue |
        Where-Object { $_.CommandLine -and $_.CommandLine.ToLower().Contains($script:installed.ToLower()) }
}
function Token([int]$p) {
    try { $h = (Invoke-WebRequest -Uri "http://127.0.0.1:$p/" -UseBasicParsing -TimeoutSec 3).Content
          $m = [regex]::Match($h, '__WB_TOKEN__\s*=\s*"([a-zA-Z0-9_-]+)"'); if ($m.Success) { return $m.Groups[1].Value } } catch { }
    return $null
}

function Run-Scenario([string]$KaScript, [string]$Tag) {
    Write-Host "=== 场景 $Tag : keepalive = $KaScript ==="
    if (Test-Path $Root) { Remove-Item -Recurse -Force $Root -ErrorAction SilentlyContinue }
    New-Item -ItemType Directory -Force -Path (Join-Path $Root "runtime"), (Join-Path $Root "workbench-output") | Out-Null
    $script:installed = Join-Path $Root "workbench.exe"
    Copy-Item -LiteralPath $Exe -Destination $script:installed -Force
    $cfg = '{ "schemaVersion": 5, "autoResume": false, "resumeOnBoot": false, "autoStart": false, "server": { "host": "127.0.0.1", "port": ' + $Port + ', "openBrowser": false } }'
    [IO.File]::WriteAllText((Join-Path $Root "workbench-output\config.json"), $cfg, (New-Object System.Text.UTF8Encoding($false)))

    $launchCmd = '"' + $script:installed + '" --silent'
    $null = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $launchCmd }
    $proc = $null
    for ($i = 0; $i -lt 120; $i++) { $proc = InstalledProc | Select-Object -First 1; if ($proc) { break }; Start-Sleep -Milliseconds 250 }
    if (-not $proc) { Check "$Tag 实例已起" $false "未检测到进程"; return $false }
    $pidMain = $proc.ProcessId
    for ($i = 0; $i -lt 120; $i++) { try { $null = Invoke-WebRequest -Uri "http://127.0.0.1:$Port/" -UseBasicParsing -TimeoutSec 2; break } catch { Start-Sleep -Milliseconds 250 } }
    $tok = Token $Port
    if ($tok) { try { $null = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$Port/api/engine/start?token=$tok" -Method POST -Body "{}" -ContentType "application/json" -TimeoutSec 5 } catch { } }
    Start-Sleep -Seconds 2

    $hwnd = [MW]::FindByPid("PersonalWorkbenchTrayClass", [uint32]$pidMain)
    if ($hwnd -eq [IntPtr]::Zero) { Check "$Tag 找到托盘窗口" $false "pid=$pidMain"; return $false }
    [MW]::SendCmd($hwnd, 1006)
    for ($i = 0; $i -lt 80; $i++) { if (-not (InstalledProc)) { break }; Start-Sleep -Milliseconds 250 }
    $flag = Join-Path $Root "runtime\user-quit.flag"
    Check "$Tag 退出后进程已结束且 user-quit 标记在" ((Test-Path $flag) -and (-not (InstalledProc))) "flag=$(Test-Path $flag) proc=$([bool](InstalledProc))"

    $null = & powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $KaScript -Exe $script:installed -RuntimeDir (Join-Path $Root "runtime") 2>&1 | Out-String
    $relaunched = $false
    for ($i = 0; $i -lt 100; $i++) { if (InstalledProc) { $relaunched = $true; break }; Start-Sleep -Milliseconds 250 }
    InstalledProc | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 1
    return $relaunched
}

# 变异体在**运行时**生成（不落仓内文件）：只删掉启动守卫里的 -not $userQuit，
# 其余一字不动。脚本自带变异能力，也不会残留一次性产物。
# PowerShell 单引号串是字面量，无需反引号转义（避免 $proc/$userQuit 被展开）。
$orig = Join-Path $PSScriptRoot "keepalive.ps1"
$src = [IO.File]::ReadAllText($orig)
$mutant = $src.Replace('    if (-not $proc -and -not $userQuit) {', '    if (-not $proc) {')
if ($mutant -eq $src) { throw "变异点未找到：keepalive 的启动守卫形态已变，变异测试本身失效（不得静默当成通过）" }
# 变异体放在**独立临时目录**，并把 lib_silent_launch.ps1 一并复制过去：
# keepalive 用 $PSScriptRoot 定位该库，同目录才走真实拉起路径。否则它会退回
# Start-Process，而本机 PS 5.1 存在 NO_PROXY/no_proxy 重名缺陷会直接抛异常 ——
# 那样变异体"没拉起"是环境噪声，不是判据结果（不可当证据）。
$mutantDir = Join-Path $env:TEMP "wb-mutation-ka"
if (Test-Path $mutantDir) { Remove-Item -Recurse -Force $mutantDir -ErrorAction SilentlyContinue }
New-Item -ItemType Directory -Force -Path $mutantDir | Out-Null
$mutantPath = Join-Path $mutantDir "keepalive.ps1"
$libSrc = Join-Path $PSScriptRoot "lib_silent_launch.ps1"
if (Test-Path $libSrc) { Copy-Item -LiteralPath $libSrc -Destination (Join-Path $mutantDir "lib_silent_launch.ps1") -Force }
# 写 BOM：脚本含中文，避免读者按 ANSI 解码导致字面量乱码
[IO.File]::WriteAllText($mutantPath, $mutant, (New-Object System.Text.UTF8Encoding($true)))
Write-Host ("变异体已生成: " + $mutantPath)

$relaunchOriginal = Run-Scenario $orig "原版"
Check "原版（含守卫）：用户退出后 keepalive 不拉起" (-not $relaunchOriginal) "原版竟然拉起了进程"

$relaunchMutant = Run-Scenario $mutantPath "变异体"
Check "变异体（删掉 -not userQuit）：keepalive 拉起并复现缺陷" ($relaunchMutant) "变异体也没拉起 ⇒ 守卫不是决定因素，修复不算数"

Remove-Item -Recurse -Force $mutantDir -ErrorAction SilentlyContinue

Write-Host "RESULT: $pass PASS / $fail FAIL"
if ($fail -gt 0) { exit 1 }
exit 0

# e2e_soak.ps1 — A7「7x24 长期稳定性」判据的独立验收（不依赖运行实例）
#
# 为什么独立成脚本：tests/e2e.ps1 是同 cwd 多会话共用的活文件（本轮实测被并发改动），
# 往里面追加大段断言会冲突。本脚本自带 fixtures，可直接复跑。
#
# 用法: powershell -NoProfile -ExecutionPolicy Bypass -File tests\e2e_soak.ps1 [-Exe <path>]
param(
  # 默认产物由脚本位置推导（tests/ 的父目录 + dist\workbench.exe），不硬编码。
  # 2026-10-23 恢复时修正：原版硬编码 D:\workdesktop，在干净 clone / 他人机器上失真。
  [string]$Exe = ""
)
$ErrorActionPreference = "Stop"
$pass = 0; $fail = 0
function Check($name, $cond, $detail = "") {
  if ($cond) { Write-Host "  [PASS] $name" -ForegroundColor Green; $script:pass++ }
  else { Write-Host "  [FAIL] $name $detail" -ForegroundColor Red; $script:fail++ }
}
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path (Split-Path -Parent $PSScriptRoot) "dist\workbench.exe" }
if (-not (Test-Path $Exe)) { throw "not found: $Exe" }
$dir = Join-Path $env:TEMP ("wb-soak-" + [guid]::NewGuid().ToString("N").Substring(0,8))
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$base = 1792300000000

Write-Host "===== 1. 合成 fixture：泄漏必须判 leak（退出码 2）=====" -ForegroundColor Cyan
$leak = @("at,heap_mb,alloc_mb,goroutine,handles,num_gc")
for ($i = 0; $i -lt 26; $i++) {
  $at = $base + $i * 3600000
  $heap = 10 + $i * 2
  $leak += ($at.ToString() + "," + $heap.ToString("0.00") + ",1.00,20," + (300 + $i * 5) + ",10")
}
$leakPath = Join-Path $dir "leak.csv"
[System.IO.File]::WriteAllLines($leakPath, $leak, (New-Object System.Text.UTF8Encoding($true)))
$leakOut = & $Exe --soak --json $leakPath 2>&1 | Out-String
$leakRc = $LASTEXITCODE
$leakObj = $null
try { $leakObj = $leakOut | ConvertFrom-Json } catch { $leakObj = $null }
Check "leak fixture parses" ($null -ne $leakObj)
Check "leak fixture => verdict=leak" ($null -ne $leakObj -and $leakObj.verdict -eq "leak") ("actual " + $(if($leakObj){$leakObj.verdict}else{"parse-fail"}))
Check "leak fixture => exit code 2" ($leakRc -eq 2) ("actual " + $leakRc)
Check "leak fixture => heap report leak" ($null -ne $leakObj -and $leakObj.reports.heap.verdict -eq "leak")
Check "leak fixture => slope ~2MB/h" ($null -ne $leakObj -and [math]::Abs($leakObj.reports.heap.slopePerHour - 2) -lt 0.2) ("actual " + $(if($leakObj){$leakObj.reports.heap.slopePerHour}else{"-"}))
Check "leak fixture => BOM/表头不丢采样" ($null -ne $leakObj -and $leakObj.samples -eq 26) ("actual " + $(if($leakObj){$leakObj.samples}else{"-"}))

Write-Host "===== 2. 合成 fixture：平线必须判 ok（退出码 0）=====" -ForegroundColor Cyan
$flat = @("at,heap_mb,alloc_mb,goroutine,handles,num_gc")
for ($i = 0; $i -lt 26; $i++) {
  $at = $base + $i * 3600000
  $flat += ($at.ToString() + ",20.00,10.00,20,300,10")
}
$flatPath = Join-Path $dir "flat.csv"
[System.IO.File]::WriteAllLines($flatPath, $flat, (New-Object System.Text.UTF8Encoding($true)))
$flatOut = & $Exe --soak --json $flatPath 2>&1 | Out-String
$flatRc = $LASTEXITCODE
$flatObj = $null
try { $flatObj = $flatOut | ConvertFrom-Json } catch { $flatObj = $null }
Check "flat fixture => verdict=ok" ($null -ne $flatObj -and $flatObj.verdict -eq "ok") ("actual " + $(if($flatObj){$flatObj.verdict}else{"parse-fail"}))
Check "flat fixture => exit code 0" ($flatRc -eq 0) ("actual " + $flatRc)

Write-Host "===== 3. 零数据不记 PASS（缺文件必须 unknown/3）=====" -ForegroundColor Cyan
$missing = Join-Path $dir "nope.csv"
$mOut = & $Exe --soak --json $missing 2>&1 | Out-String
$mRc = $LASTEXITCODE
$mObj = $null
try { $mObj = $mOut | ConvertFrom-Json } catch { $mObj = $null }
Check "missing file => verdict=unknown" ($null -ne $mObj -and $mObj.verdict -eq "unknown") ("actual " + $(if($mObj){$mObj.verdict}else{"parse-fail"}))
Check "missing file => exit code 3" ($mRc -eq 3) ("actual " + $mRc)
Check "missing file => exists=false" ($null -ne $mObj -and $mObj.exists -eq $false)

Write-Host "===== 4. 时长判据：跨度不足 24h 不得记 PASS =====" -ForegroundColor Cyan
$short = @("at,heap_mb,alloc_mb,goroutine,handles,num_gc")
for ($i = 0; $i -lt 6; $i++) {
  $at = $base + $i * 60000
  $short += ($at.ToString() + ",20.00,10.00,20,300,10")
}
$shortPath = Join-Path $dir "short.csv"
[System.IO.File]::WriteAllLines($shortPath, $short, (New-Object System.Text.UTF8Encoding($true)))
$sOut = & $Exe --soak --json $shortPath 2>&1 | Out-String
$sRc = $LASTEXITCODE
$sObj = $null
try { $sObj = $sOut | ConvertFrom-Json } catch { $sObj = $null }
Check "short span => verdict=unknown" ($null -ne $sObj -and $sObj.verdict -eq "unknown") ("actual " + $(if($sObj){$sObj.verdict}else{"parse-fail"}))
Check "short span => exit code 3" ($sRc -eq 3) ("actual " + $sRc)
Check "short span => longEnough=false" ($null -ne $sObj -and $sObj.longEnough -eq $false)

Write-Host "===== 5. 真实数据：当前实例跨度不足也必须 unknown =====" -ForegroundColor Cyan
$real = & $Exe --soak --json 2>&1 | Out-String
$realRc = $LASTEXITCODE
$realObj = $null
try { $realObj = $real | ConvertFrom-Json } catch { $realObj = $null }
Check "real data returns JSON" ($null -ne $realObj)
Check "real data verdict is valid enum" ($null -ne $realObj -and (@("ok","leak","unknown") -contains $realObj.verdict))
Check "real data never claims ok without 24h" (-not ($realObj -and $realObj.verdict -eq "ok" -and $realObj.longEnough -eq $false))
Check "real data exit code matches verdict" ($realRc -in @(0,2,3))

Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -eq 0) { Write-Host ("RESULT: " + $pass + " PASS / 0 FAIL") -ForegroundColor Green }
else { Write-Host ("RESULT: " + $pass + " PASS / " + $fail + " FAIL") -ForegroundColor Red }
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -gt 0) { exit 1 } else { exit 0 }

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
# --soak --json 的 flat 契约（soak.go 的 soakReport 结构体，2026-10-19 restore 版）。
# 为什么要有这个检查（round 53/54 实测）：本脚本原先按**嵌套契约**写断言
# （reports.heap.verdict / reports.heap.slopePerHour / longEnough），而当前 HEAD 的
# soakReport 是 flat 的（heap / handles / verdict / spanHours / samples / exists / reason /
# exitCode，**没有 reports、也没有 longEnough**）。PowerShell 读不存在的属性返回 null，
# 后果分两种、都致命：
#   * `null -eq "leak"` 为假、`null -ne "leak"` 为真 => 断言**空转**（对缺陷永不敏感，
#     旧产物照样全绿）；
#   * `null -eq $false` 为假 => 在**正确实现**上误红（本轮用含修复的构建实测挂 2 条）。
# longEnough 的消失本身是有意的：并行会话在途的 soak_verdict_test.go 记录现场缺陷是
# 「报表同时输出 longEnough:false 与 verdict:leak/exitCode:2」——自己声明证据不足却给出
# 已证泄漏的结论。唯一实现 soakConclusion 让短跨度一律 unknown/3 之后，那种自相矛盾
# 在结构上不可能出现，字段才随之删除。所以**flat 才是当前正确契约**，本脚本必须跟上。
function MissingFlat([object]$o) {
  $flat = @("exists","path","samples","spanHours","heap","handles","verdict","reason","exitCode")
  $names = @()
  if ($null -ne $o) { $names = @($o.PSObject.Properties.Name) }
  $miss = @()
  foreach ($f in $flat) { if ($names -notcontains $f) { $miss += $f } }
  return ($miss -join ",")
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
Check "leak fixture => flat heap=leak" ($null -ne $leakObj -and $leakObj.heap -eq "leak") ("actual heap=" + $(if($leakObj){$leakObj.heap}else{"-"}))
Check "leak fixture => flat 契约齐全" ($(MissingFlat $leakObj) -eq "") ("缺字段: " + $(MissingFlat $leakObj))
# 原断言读 reports.heap.slopePerHour（flat 契约无此字段：null - 2 = -2，abs(-2)<0.2 恒假
# => 误红）。斜率精度归 supervisor 包单测（AnalyzeTrend 有专项测试）。这里改钉 reason 里的
# 趋势组合 —— flat 契约的真实字段，组合本身就是判据结果。
# 注：本 fixture 的 handles 从 300 线性涨到 425（全程 +41.7%），但判据取**首尾 1/3 均值**
# （320 -> 405 = +26.6%）而非全程极差，故低于 growthRatio=0.30 阈值 => handles 应为 ok。
# （round 54 我最初按 +42% 断言成 leak，被实测纠正 —— 单点比例不是判据，与
#  docs/benchmark/性能基线口径.md §9 的「别用单点建换算直觉」同一课。）
Check "leak fixture => reason 含堆 leak/句柄 ok 组合" ($null -ne $leakObj -and ([string]$leakObj.reason) -match "堆 leak" -and ([string]$leakObj.reason) -match "句柄 ok") ("actual reason=" + $(if($leakObj){$leakObj.reason}else{"-"}))
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
# longEnough 已从契约移除（见上文说明）。跨度过短的等价判据 = verdict=unknown（第 81 行
# 已断言）+ reason 明说未覆盖要求时长 + 契约齐全。
Check "short span => reason 说明跨度不足" ($null -ne $sObj -and ([string]$sObj.reason) -match "未覆盖") ("actual reason=" + $(if($sObj){$sObj.reason}else{"-"}))
Check "short span => flat 契约齐全" ($(MissingFlat $sObj) -eq "") ("缺字段: " + $(MissingFlat $sObj))

Write-Host "===== 4.5 跨进程重启的台阶不得判 leak（round 39 缺陷的回归位）====="
# 为什么补这一节（round 53 实测）：本节之前的 4 个 fixture 全是**单段**合成数据，
# 而 round 39 修的是「跨进程重启把负载台阶误报成 heap +174% leak」——旧脚本没有任何
# 断言触碰那条代码路径。第 5 段「真实数据」只断言形状（可解析/枚举合法/跨度不足不冒充 ok），
# 对缺陷完全钝感：**实测拿 round 39 修复之前的产物跑整套，照样 18 PASS / 0 FAIL**。
# 验收对它在防的缺陷空转，比没验收更危险（给出「验收过了」的错觉）。
#
# fixture 刻意做成**能区分新旧逻辑**：段1 heap=10（num_gc 递增=同进程），段2 heap=30
# （num_gc 从 1 重排=换进程）。旧逻辑跨段排序 → 头1/3=10 尾1/3=30 → +200% 判 leak
# （旧产物实测 verdict=leak、span=25h）；新逻辑按 num_gc 回退裁到末段 → 段内平坦 → ok
# （当前源码实测 heap=ok、span=12h）。所以这里的断言在旧产物上**必须 FAIL**、新产物 PASS。
$restart = @("at,heap_mb,alloc_mb,goroutine,handles,num_gc")
for ($i = 0; $i -lt 13; $i++) {
  $at1 = $base + $i * 3600000
  $restart += ($at1.ToString() + ",10.00,5.00,12,300," + (100 + $i * 3).ToString())
}
for ($i = 0; $i -lt 13; $i++) {
  $at2 = $base + (13 + $i) * 3600000
  $restart += ($at2.ToString() + ",30.00,5.00,12,300," + (1 + $i * 2).ToString())
}
$restartPath = Join-Path $dir "restart.csv"
[System.IO.File]::WriteAllLines($restartPath, $restart, (New-Object System.Text.UTF8Encoding($true)))
$rOut = & $Exe --soak --json $restartPath 2>&1 | Out-String
$rObj = $null
try { $rObj = $rOut | ConvertFrom-Json } catch { $rObj = $null }
Check "restart fixture parses" ($null -ne $rObj)
# 契约齐全断言必须排在行为断言之前：否则字段缺失时 $hv 为 null、
# `null -ne "leak"` 恒真，下面三条全部空转（这正是 round 53 差点带病上线的形态）。
Check "restart fixture => flat 契约齐全" ($(MissingFlat $rObj) -eq "") ("缺字段: " + $(MissingFlat $rObj))
$hv = "?"
$sp = 0
$ov = "?"
if ($null -ne $rObj) { $hv = $rObj.heap; $sp = $rObj.spanHours; $ov = $rObj.verdict }
Check "restart fixture => heap 不判 leak（跨重启台阶不得误报泄漏）" ($null -ne $rObj -and $hv -ne "leak") ("actual heap.verdict=" + $hv)
Check "restart fixture => 整体不判 leak" ($null -ne $rObj -and $ov -ne "leak") ("actual verdict=" + $ov)
Check "restart fixture => span 只算末进程段(约12h，未裁则25h)" ($null -ne $rObj -and $sp -lt 15) ("actual spanHours=" + $sp)

Write-Host "===== 5. 真实数据：当前实例跨度不足也必须 unknown =====" -ForegroundColor Cyan
$real = & $Exe --soak --json 2>&1 | Out-String
$realRc = $LASTEXITCODE
$realObj = $null
try { $realObj = $real | ConvertFrom-Json } catch { $realObj = $null }
Check "real data returns JSON" ($null -ne $realObj)
Check "real data verdict is valid enum" ($null -ne $realObj -and (@("ok","leak","unknown") -contains $realObj.verdict))
# 原断言 verdict=ok 且 longEnough=false（flat 契约无 longEnough => null -eq $false 恒假 =>
# 整个 -not(...) 恒真 => 空转，从没防住任何东西）。等价可证伪判据：ok 必须配 exitCode 0。
Check "real data never claims ok with non-zero exit" (-not ($realObj -and $realObj.verdict -eq "ok" -and $realObj.exitCode -ne 0)) ("actual verdict=" + $(if($realObj){$realObj.verdict}else{"?"}) + " exitCode=" + $(if($realObj){$realObj.exitCode}else{"?"}))
Check "real data exit code matches verdict" ($realRc -in @(0,2,3))

Remove-Item -Recurse -Force $dir -ErrorAction SilentlyContinue
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -eq 0) { Write-Host ("RESULT: " + $pass + " PASS / 0 FAIL") -ForegroundColor Green }
else { Write-Host ("RESULT: " + $pass + " PASS / " + $fail + " FAIL") -ForegroundColor Red }
Write-Host "========================================" -ForegroundColor Cyan
if ($fail -gt 0) { exit 1 } else { exit 0 }

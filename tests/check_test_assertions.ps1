# check_test_assertions.ps1 -- 每个 Test 函数必须至少有一条断言（2026-10-24 round 35 新增）
#
# 为什么需要（round 34 审计实测发现 3 个，round 35 删除并上闸）：
#   workbench 下有 3 个测试文件**零断言** —— 只有 fmt.Println 打印中间状态，
#   没有任何 t.Fatal/t.Error。它们永远 PASS，但对「代码是否正确」什么都没保证。
#   实例：internal/artifact/debug_loose_test.go、debug_strict_test.go、config/zz_debug_test.go
#   （均为 a5cf134「仓库损毁后全量恢复」时进来的调试残留，长期无人维护）。
#
# 危害（不是风格问题，是实质性的）：
#   ① **虚增 PASS 数**：go test 报的 N 个 PASS 里有假成分，让人觉得覆盖比实际好；
#   ② **覆盖漏洞**：看到「这个文件有测试」的人会跳过补测试，而那里的逻辑其实没被验过；
#   ③ 调试脚本本该在编辑器里跑，不该占测试的名位。
#
# 判据（任一命中即 rc=1）：
#   受跟踪 *_test.go 里，形如 `func TestXxx(t *testing.T)` 的函数，
#   其函数体内**没有任何** t.Fatal/t.Fatalf/t.Error/t.Errorf/t.Fail/t.FailNow 调用。
#
# 刻意不判的（避免噪声 —— 本仓铁律：模糊/过宽判据制造噪声，门禁就会被删）：
#   - Example_* / Benchmark_*（非 Test 函数，本就不该有断言要求）；
#   - 断言写在辅助函数里的（少数派，且难以静态判定归属，宁漏不误）。
#
# **两种合法豁免（round 35 首轮运行后精细化，实测 4 个命中里 3 个属此类）**：
#   ① **编译期契约断言**：形如 `var _ SomeInterface = ...` 的赋值。
#      它在编译期就完成了断言（类型不匹配则编译失败），运行期无需再断言。
#      实例：executor/hooks_test.go 的 TestRunnerContractSatisfied。
#   ② **显式声明的不 panic 契约**：注释里写明「不 panic / 不崩溃 / survives nil」等意图，
#      且函数体只有调用（无 fmt.Println 之类的调试输出）。
#      实例：engine_smoke_test.go 的 TestFinishCancelledOnUnknownTask、
#            hooks_attach_test.go 的HooksAttachSurvivesNilChain。
#   判据：这两种形态不算零断言。**其余的才算** ——
#   例如「smoke 测试只调用不验证」是虚的（web/autostart_silent_test.go 的
#   TestAutostartEnabledSmoke 即此类型，已单独处置）。
#
# 头注里保留这份分类，是因为 round 35 首轮一刀切判据报了 4 个、其中 3 个合理 ——
# 若不精细化，门禁第一次运行就会产生 3 个假红，离「被删」就不远了。
#
# 与死代码门禁的分工：check_deadcode 查「没人调用的函数」，
#   本件查「调了但不验证的测试」—— 后者更隐蔽（它在跑，而且绿）。
#
# 用法: powershell -File tests\check_test_assertions.ps1 [-Quiet]
# 退出码: 0=全部有断言 / 1=有零断言 Test / 2=UNVERIFIED
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root 'workbench'

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Host "UNVERIFIED: 找不到 git" -ForegroundColor Red
  exit 2
}

$relFiles = @(& $gitExe -C $root ls-files 'workbench/*_test.go')
if ($LASTEXITCODE -ne 0) {
  Write-Host "UNVERIFIED: git ls-files 失败" -ForegroundColor Red
  exit 2
}
if ($relFiles.Count -eq 0) {
  Write-Host "UNVERIFIED: 未取到任何 _test.go" -ForegroundColor Red
  exit 2
}

$assertRe = '\bt\.(Fatal|Fatalf|Error|Errorf|Fail|FailNow)\s*\('
$funcRe = '^func\s+(Test\w+)\s*\(\s*(\w+)\s+\*testing\.T\s*\)'

$bad = @()
$totalTests = 0
foreach ($rel in $relFiles) {
  $rel = $rel.Trim('"')
  if (-not $rel) { continue }
  $full = Join-Path $root $rel
  if (-not (Test-Path -LiteralPath $full)) { continue }
  $lines = @([IO.File]::ReadAllLines($full))
  # 逐函数扫描：从 func Test... 行到下一个顶层 func/文件尾
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $m = [regex]::Match($lines[$i], $funcRe)
    if (-not $m.Success) { continue }
    $totalTests++
    # 找函数体结束（下一个以 func 开头的行，或文件尾）
    $end = $lines.Count
    for ($j = $i + 1; $j -lt $lines.Count; $j++) {
      if ($lines[$j] -match '^func\s') { $end = $j; break }
    }
    $body = ($lines[$i..($end - 1)] -join "`n")
    # 豁免 ①：编译期契约断言（var _ Iface = ... 在编译期完成断言）
    $compileTimeAssert = ($body -match 'var\s+_\s+\w+\s*=')
    # 豁免 ②：显式声明的不 panic 契约。
    #   检查窗口必须包含 **func 行上方的紧邻注释**（Go 的文档注释就在那里），
    #   2026-10-24 实测：首版只扫函数体，漏掉了写在 doc comment 里的「不得 panic」
    #   （hooks_attach_test.go:58 的 TestHooksAttachSurvivesNilChain 即如此）。
    #   附加条件：函数体不得含 fmt.Print*（那是调试脚本，不给豁免）。
    # 取 func 行上方的**连续注释块**（Go doc comment 可能多行；2026-10-24 实测：
    # 首版只取 1 行，漏掉第 58 行的「不得 panic」—— 它在第 59 行之上一格）。
    $docBlock = ''
    for ($k = $i - 1; $k -ge 0 -and $k -ge ($i - 6); $k--) {
      if ($lines[$k].Trim().StartsWith('//')) { $docBlock = $lines[$k] + " `n" + $docBlock } else { break }
    }
    $panicContract = ((($body + " `n" + $docBlock) -match '不[得应会]?\s*panic|不崩溃|survives?\s*nil|not\s+panic|no\s+panic') -and ($body -notmatch 'fmt\.(Print|Sprint)'))
    if ($body -notmatch $assertRe -and -not $compileTimeAssert -and -not $panicContract) {
      $bad += ($rel.Replace('\', '/') + ':' + ($i + 1) + '  ' + $m.Groups[1].Value + ' 无任何断言')
    }
    $i = $end - 1
  }
}

if ($bad.Count -gt 0) {
  Write-Host ("零断言测试门禁未通过（" + $bad.Count + " 个 Test 无断言，共扫 " + $totalTests + " 个）：") -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object)) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "  危害：虚增 PASS 数 + 让「这里有测试」的错觉掩盖真实覆盖漏洞。" -ForegroundColor Yellow
  Write-Host "  修法：① 补上真实断言；或 ② 若只是调试脚本，删掉（它不该占测试的名位）；" -ForegroundColor Yellow
  Write-Host "        ③ 若已有等价覆盖（round 35 的 3 个都是），直接删 —— 删除后覆盖率不变即为证。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("测试断言合规：" + $totalTests + " 个 Test 函数均有断言") -ForegroundColor Green }
exit 0
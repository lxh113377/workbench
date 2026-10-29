# check_test_assertions.ps1 -- 每个 Test 函数必须至少有一条断言（2026-10-24 round 35 新增，2026-10-29 改由 go/parser 供判定）
#
# 为什么需要（round 34 审计实测发现 3 个，round 35 删除并上闸）：
#   workbench 下有 3 个测试文件**零断言** —— 只有 fmt.Println 打印中间状态，没有任何
#   t.Fatal/t.Error。它们永远 PASS，但对「代码是否正确」什么都没保证。
#   实例：internal/artifact/debug_loose_test.go、debug_strict_test.go、config/zz_debug_test.go
#   （均为 a5cf134「仓库损毁后全量恢复」时进来的调试残留）。
#
# 危害（不是风格问题）：① 虚增 PASS 数；② 看到「这个文件有测试」的人会跳过补测试，
#   而那里逻辑其实没被验过；③ 调试脚本占了测试的名位。
#
# ⚠️ 判定面为什么从「PowerShell 文本扫描」换成「go/parser 子件」（2026-10-29 实测教训）：
#   旧版把「下一个以 func 开头的行」当函数体边界，被 Go **raw string 字面量**骗 ——
#   internal/retry/kind_writeback_contract_test.go 的负控测试体内 fake := <反引号> … func f() { … } <反引号>，
#   那行 func 恰好顶格 ⇒ 函数体被截成两行、真正的 t.Fatalf 不在扫描窗口内 ⇒ 把**有断言的测试**
#   判成零断言（CI run 40889071988 的 repo-gates 假红，红的是判据不是被测件）。
#   第一版补救「按行内反引号奇偶跟踪 raw string 状态」实测**更危险**：双引号串里也能合法含
#   反引号（如 "a`b"），奇偶一旦被带偏，后续**真实** Test 会被当成"在串内"整体跳过 ⇒
#   真零断言测试永不报 = 假绿（本仓口径：假绿比假红严重）。量化证据：奇偶版只数到 1030 个，
#   比旧版少 5 个，而少掉的 5 个全是真测试（artifact/protocol_test.go:42、
#   engine/p3_offline_e2e_test.go:85、engine/repair_test.go:72、engine/replan_boundary_test.go:68、
#   modelpool/protocol_test.go:32），不是假声明 ⇒ 文本层无法可靠判词法状态。
#   正解：词法/语法交给 go/parser（workbench/cmd/assertionprobe），本件只做受理与报红。
#
# 判据（任一命中即 rc=1）：受跟踪 *_test.go 里，形如 func TestXxx(t *testing.T) 的函数，
#   体内没有任何 t.Fatal/Fatalf/Error/Errorf/Fail/FailNow 调用（t.Log/t.Skip 不算 —— 跳过不等于验证）。
#
# 两种合法豁免（round 35 精细化，本轮实现挪进子件、口径不变）：
#   ① compiletime：体内有 `var _ SomeInterface = ...` 型编译期契约断言（类型不符则编译失败），
#      实例 executor/hooks_test.go 的 TestRunnerContractSatisfied。
#   ② panicdoc：doc 注释**或函数区间内任意注释**写明「不得 panic/不崩溃/survives nil」，
#      且体内无 fmt.Print*（有打印说明是调试残留，不给豁免）。
#      实例 engine/engine_smoke_test.go 的 TestFinishCancelledOnUnknownTask —— 注意它的契约注释
#      写在**函数体内**（:278）而非 func 上方；本轮第一版只扫上方注释把它判红，已修。
#   与死代码门禁分工：check_deadcode 查「没人调用的函数」，本件查「调了但不验证的测试」。
#
# 用法: powershell -File tests\check_test_assertions.ps1 [-Quiet]
# 退出码: 0=全部有断言 / 1=有零断言 Test / 2=UNVERIFIED（取不到文件清单、子件跑不起来或解析失败）
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"
function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }
function Unverified([string]$m) { Write-Host ("UNVERIFIED: " + $m) -ForegroundColor Red; exit 2 }

# 文件清单口径与旧版一致：只查受跟踪的 *_test.go（未跟踪件由 check_untracked_go 负责）。
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Unverified "找不到 git" }
$rel = @(& git -C $root ls-files "workbench/*_test.go" 2>$null)
if ($LASTEXITCODE -ne 0 -or $rel.Count -eq 0) { Unverified ("未取到受跟踪 *_test.go 清单（rc=" + $LASTEXITCODE + "）") }
$rel = @($rel | ForEach-Object { "$_".Trim([char]34).Trim() } | Where-Object { $_ })
$rel = @($rel | ForEach-Object { $_ -replace "^workbench/", "" })
if ($rel.Count -eq 0) { Unverified "清单去掉 workbench/ 前缀后为空" }

# 子件在 workbench/ 模块内跑（go run ./cmd/assertionprobe），路径按该模块相对给出。
$out = ""
Push-Location $wb
try {
  $out = (& go run ./cmd/assertionprobe $rel 2>&1 | Out-String)
  $rc = $LASTEXITCODE
} finally { Pop-Location }
if ($rc -ne 0) {
  Unverified ("assertionprobe 退出码 " + $rc + "（解析失败或跑不起来 ⇒ 不能当成「全都有断言」）：" + (($out -split "`n") | Where-Object { "$_".Trim() } | Select-Object -First 2) -join " | ")
}
$lines = @(($out -split "`n") | Where-Object { "$_".Trim() })
$tested = @($lines | Where-Object { $_.StartsWith("TEST|") })
# 解析面为空一律 UNVERIFIED —— 本仓 ㉑二：不得把空集合当「全部合规」。
if ($tested.Count -eq 0) { Unverified ("assertionprobe 没输出任何 TEST 行（共给 " + $rel.Count + " 个文件）⇒ 判据没跑到东西上") }
$parseFail = @($lines | Where-Object { $_.StartsWith("PARSE_FAIL|") })
if ($parseFail.Count -gt 0) { Unverified ("有文件解析失败：" + ($parseFail | Select-Object -First 3) -join " ;; ") }
$bad = @()
$exemptN = 0
foreach ($l in $tested) {
  $p = $l.Split("|")
  if ($p.Count -lt 5) { Unverified ("TEST 行字段不足：" + $l) }
  if ($p[4].Trim() -eq "NONE") { $bad += ($p[1] + ":" + $p[2] + "  " + $p[3] + " 无任何断言") }
  elseif ($p.Count -ge 6 -and "$($p[5])".Trim()) { $exemptN++ }
}
Say ("扫描 Test 函数：" + $tested.Count + " 个（其中编译期/不panic 契约豁免 " + $exemptN + " 个）")
if ($bad.Count -gt 0) {
  Write-Host ("零断言测试门禁未通过（" + $bad.Count + " 个 Test 无断言，共扫 " + $tested.Count + " 个）：") -ForegroundColor Red
  foreach ($b in ($bad | Sort-Object)) { Write-Host ("  - " + $b) -ForegroundColor Red }
  Write-Host "  危害：虚增 PASS 数 + 让「这里有测试」的错觉掩盖真实覆盖漏洞。" -ForegroundColor Yellow
  Write-Host "  修法：① 补上真实断言；② 若只是调试脚本，删掉（它不该占测试的名位）；" -ForegroundColor Yellow
  Write-Host "  ③ 若已有等价覆盖（round 35 的 3 个都是），直接删 —— 删除后覆盖率不变即为证。" -ForegroundColor Yellow
  Write-Host "  判据实现见 workbench/cmd/assertionprobe（用 go/parser，不再做文本扫描）。" -ForegroundColor DarkGray
  exit 1
}
Say ("测试断言合规：" + $tested.Count + " 个 Test 函数均有断言（或属两类契约豁免之一）")
exit 0

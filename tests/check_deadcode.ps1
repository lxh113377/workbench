# check_deadcode.ps1 -- 未导出的零引用函数（死代码）门禁
#
# 为什么需要（2026-10-23 实测发现）：workbench 下 3 个未导出函数零引用 ——
#   cmd/workbench/main.go:562 openPanelOrPrint
#   internal/executor/executor.go:527 (*Runner).environment
#   internal/web/gov_api.go:163 encodeGovJSON
# 三者的共同形态：**函数体完整、注释甚至还在解释「为什么要它」，但没有任何调用点**。
#
# 为什么值得上门禁而不是顺手删掉：
#   1) 本仓的核心范式就是「判据机器化」——发现一类缺陷只删一次，下次同类仍然复发；
#   2) 死代码的危害不是运行时，而是**阅读与维护**：读代码的人会假设它被调用
#      （注释还在承诺行为），于是基于一个假前提做判断；
#   3) 它是「代码规模只增不减」的微观形态，与 memory 治理指标同族。
#
# 判据（任一命中即 rc=1）：
#   受跟踪的非 _test.go 文件里，未导出的函数/方法定义，在整个 workbench 源码树中
#   **除定义行外无任何引用**（词边界匹配）。
#
# 已知的**假阳性豁免**（刻意收窄，不掩盖真死代码）：
#   - 接口实现方法（方法名可能在接口契约里，不在本包出现）
#   - 被 json/reflect/unsafe 间接使用的（无法静态判定）
#   => 本门禁只报「名字在本树零引用」这一可证伪事实，具体是否删由人判；
#      这正是「机器判据给证据、人做决定」的分工。
#
# 用法: powershell -File tests\check_deadcode.ps1 [-Quiet]
# 退出码: 0=无死代码 / 1=有死代码
param([switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"

# 1) 收集所有受跟踪的 .go（排除 _test.go 与 build/）
$goFiles = @(git -C $root ls-files "workbench/*.go")
$goFiles = @($goFiles | ForEach-Object { $_.Trim('"') })
if ($goFiles.Count -eq 0) { Write-Host "UNVERIFIED: 未取到受跟踪的 .go" -ForegroundColor Red; exit 2 }

# 2) 全树语料（用于引用计数）
$corpus = ""
foreach ($rel in $goFiles) { $corpus += [IO.File]::ReadAllText((Join-Path $root $rel)) + "`n" }

# 3) 抽取未导出函数/方法定义
$dead = @()
foreach ($rel in $goFiles) {
  if ($rel -match '_test\.go$') { continue }
  $lines = @([IO.File]::ReadAllLines((Join-Path $root $rel)))
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $l = $lines[$i]
    $name = $null
    if ($l -match '^func ([a-z]\w*)\(') { $name = $Matches[1] }
    elseif ($l -match '^func \([^)]+\) ([a-z]\w*)\(') { $name = $Matches[1] }
    if (-not $name) { continue }
    # 词边界计数（定义行本身算 1 次）
    $n = ([regex]::Matches($corpus, "\b" + [regex]::Escape($name) + "\b")).Count
    if ($n -le 1) {
      $dead += ($rel.Replace('\', '/') + ':' + ($i + 1) + '  ' + $name)
    }
  }
}

if ($dead.Count -gt 0) {
  Write-Host ("死代码（未导出函数，全树零引用）" + $dead.Count + " 处：") -ForegroundColor Red
  foreach ($d in ($dead | Sort-Object)) { Write-Host ("  - " + $d) -ForegroundColor Red }
  Write-Host "修法：删掉，或补调用点；若属接口实现/反射使用，请在定义处注释说明（下一轮可做白名单）。" -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) { Write-Host ("无死代码：检查 " + $goFiles.Count + " 个受跟踪 .go") -ForegroundColor Green }
exit 0

# check_soak_json_contract.ps1 -- --soak --json 的字段契约（断言只能落在真实存在的字段上）
#
# 为什么需要（round 53/54 实测）：
#   tests/e2e_soak.ps1 曾按**嵌套契约**写断言（reports.heap.verdict / reports.heap.slopePerHour /
#   longEnough），而 HEAD 的 soakReport 是 **flat** 的（heap/handles/verdict/spanHours/samples/
#   exists/reason/exitCode，无 reports、无 longEnough）。PowerShell 读不存在的属性返回 null，
#   于是断言静默变质：
#     * `null -ne "leak"` 恒真 => 空转（对缺陷永不敏感）—— round 53 实测：拿 round 39 修复
#       **之前**的产物跑整套 e2e_soak，照样 18 PASS / 0 FAIL，验收对它在防的缺陷完全空转；
#     * `null -eq $false` 恒假 => 在**正确实现**上误红（round 54 实测挂 2 条，差点把验收
#       挂进 CI 就直接红）。
#   longEnough 的移除是有意的：并行会话在途的 soak_verdict_test.go 记录现场缺陷为「报表
#   同时输出 longEnough:false 与 verdict:leak/exitCode:2」；唯一实现 soakConclusion 让短跨度
#   一律 unknown/3 后，那种自相矛盾在结构上不可能，字段才删。所以 flat 是当前正确契约。
#   根因是**契约漂移无人拦**：改 soak.go 输出结构不触发任何门禁，验收脚本就悄悄变成空转。
#
# 判据（纯静态、不依赖构建产物，可在 repo-gates 安全运行）：
#   1) 从 soak.go 的 soakReport 结构体解析 json tag -> 真实字段白名单。
#   2) 扫验收脚本里对 soak 结果对象（变量名含 obj / 名为 j）的属性访问链，链首不在白名单
#      => 判红（读了不存在的字段，断言必然空转或误红）。跳过注释行。
#   3) 结构体解析不到 / 白名单为空 => UNVERIFIED（改名等要显式暴露，不静默放行）。
#
# 刻意只做字段存在性，不判 soak 结论对不对（那是 supervisor 单测与 e2e_soak 的职责）——
#   判据要落在可证伪的窄面，否则模糊语义 => 噪声 => 门禁被删（本仓在册教训）。
#
# 实现注记：regex 用字符码拼装。JS 侧的 String.replace 会展开 dollar-quote、双引号里的
#   $( 会被 PowerShell 当子表达式 —— round 53/54 各踩一次，产物必须回读 + 实跑。
#
# 用法: powershell -File tests/check_soak_json_contract.ps1 [-Quiet] [-E2ePath p] [-SoakPath p]
# 退出码: 0=访问全在契约内 / 1=有访问落在不存在的字段 / 2=UNVERIFIED
param(
  [switch]$Quiet,
  [string]$E2ePath = '',
  [string]$SoakPath = ''
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$bs = [string][char]92
if (-not $SoakPath) { $SoakPath = Join-Path $root ('workbench' + $bs + 'cmd' + $bs + 'workbench' + $bs + 'soak.go') }
if (-not $E2ePath) { $E2ePath = Join-Path $root ('tests' + $bs + 'e2e_soak.ps1') }
foreach ($need in @($SoakPath, $E2ePath)) {
  if (-not (Test-Path -LiteralPath $need)) {
    Write-Host ('UNVERIFIED: 缺文件 ' + $need + '（契约两端都要在才判得了）') -ForegroundColor Red
    exit 2
  }
}

# 1) soakReport 结构体 -> json tag 白名单
$soak = [IO.File]::ReadAllText($SoakPath)
$inStruct = $false
$bodyLines = @()
foreach ($ln in ($soak -split [char]10)) {
  $t = $ln.Trim()
  if (-not $inStruct) {
    if ($t.StartsWith('type soakReport struct')) { $inStruct = $true }
    continue
  }
  if ($t -eq '}') { break }
  $bodyLines += $t
}
if ($bodyLines.Count -eq 0) {
  Write-Host 'UNVERIFIED: 解析不到 soakReport 结构体（改名/拆分了？判据要跟着更新）' -ForegroundColor Red
  exit 2
}
$bt = [string][char]96
$q = [string][char]34
$key = 'json:' + $q
$fields = @()
foreach ($bl in $bodyLines) {
  $i1 = $bl.IndexOf($bt)
  if ($i1 -lt 0) { continue }
  $i2 = $bl.IndexOf($bt, $i1 + 1)
  if ($i2 -lt 0) { continue }
  $tag = $bl.Substring($i1 + 1, $i2 - $i1 - 1)
  $ki = $tag.IndexOf($key)
  if ($ki -lt 0) { continue }
  $rest = $tag.Substring($ki + $key.Length)
  $ci = $rest.IndexOf($q)
  if ($ci -le 0) { continue }
  $fname = $rest.Substring(0, $ci)
  if ($fname -and ($fname -ne '-')) { $fields += $fname }
}
$fields = @($fields | Sort-Object -Unique)
if ($fields.Count -eq 0) {
  Write-Host 'UNVERIFIED: soakReport 无任何 json tag（判据需扩展）' -ForegroundColor Red
  exit 2
}

# 2) 扫验收脚本的属性访问链
$e2e = [IO.File]::ReadAllText($E2ePath)
$patS = $bs + '$' + '([a-zA-Z_][a-zA-Z0-9_]*)(([.][a-zA-Z_][a-zA-Z0-9_]*)+)'
$pat = [regex] $patS
$bad = @()
$hit = @()
$li = 0
foreach ($ln in ($e2e -split [char]10)) {
  $li++
  $t = $ln.Trim()
  if ($t.StartsWith('#')) { continue }
  foreach ($mm in $pat.Matches($t)) {
    $var = $mm.Groups[1].Value
    $chain = $mm.Groups[2].Value
    # 只认 soak 结果对象：变量名含 obj 或就叫 j（leakObj/sObj/rObj/realObj/mObj/j...）
    if (-not (($var -match 'obj') -or ($var -match '^j$'))) { continue }
    # PowerShell 的 -split 参数按**正则**解释：[char]46 就是「任意字符」，会把字段名逐字符
    #   切碎成空串（round 54 实测：15 条属性访问全被判「顶层字段为空」）。必须用转义点。
    $parts = @($chain.Substring(1) -split '\.')
    $top = $parts[0]
    # PowerShell 自身成员不是 JSON 字段（MissingFlat 里的 PSObject 即此类）
    if ($top -match '^(PSObject|Properties|Methods|BaseObject|Length|Count)$') { continue }
    if ($fields -contains $top) { $hit += $top; continue }
    $bad += ('L' + $li + ' ' + $var + $chain + '  顶层字段 ' + $top + ' 不在 soakReport 契约内')
  }
}
$bad = @($bad | Sort-Object -Unique)

if ($bad.Count -gt 0) {
  Write-Host ('soak JSON 契约门禁未通过：验收断言访问了 --soak --json 里不存在的字段（' + $bad.Count + ' 处）') -ForegroundColor Red
  foreach ($x in $bad) { Write-Host ('  - ' + $x) -ForegroundColor Red }
  Write-Host ('  soakReport 真实字段: ' + ($fields -join ' / ')) -ForegroundColor Yellow
  Write-Host '  危害：读不存在的属性得 null —— null 与任何值比较，使断言恒真（空转、对缺陷永不敏感）或恒假（在正确实现上误红）。' -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
  Write-Host ('soak JSON 契约合规：soakReport 导出 ' + $fields.Count + ' 个字段（' + ($fields -join ' ') + '）；验收脚本属性访问均落在契约内（命中 ' + (@($hit) | Sort-Object -Unique).Count + ' 个不同字段）') -ForegroundColor Green
}
exit 0

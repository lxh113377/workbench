# check_report_figures.ps1 -- 报告里的规模数字必须与仓库实况一致（2026-10-24 round 60 新增）
#
# 为什么需要（本仓第 8 类失效形态：**报告数字靠人手同步**）：
#   docs/门禁体系体检报告-2026-10-23.md 开头就承诺「本节所有数字来自实测，不引用记忆中的旧值」。
#   但 round 56 建它时人工核了 6 个数字，round 58 加了 stdlib 门禁后又**手工改 8 处**
#   （32->33、19->20、28->29、32/32->33/33、第 6 类->第 7 类……）。两次都是人肉同步。
#   下一次谁再加/删一个门禁，这份报告就会**静默失真** —— 而它是后续 agent 的整改入口，
#   数字错了就是误导（这正是 check_report_claims 当初存在的理由，只不过那条只判「状态栏 vs
#   CI 制品」，不判规模数字）。
#
# 判据（刻意极窄，只核 4 个**纯客观**可复算的数）：
#   报告里形如「| 受跟踪门禁脚本 | **N** 个 |」等表格行，N 必须等于实测：
#     1) 受跟踪门禁数      = git ls-files 'tests/check_*.ps1' 的条数
#     2) CI repo-gates 数  = ci.yml 里本 job 引用的 'tests/check_*.ps1' 去重数
#     3) check_all 覆盖数  = tests/check_all.ps1 里出现的 check_*.ps1 去重数
#     4) 「未跟踪 Go 测试数」改为按**下界**判（见下），不再是必须逐字相等的硬等式
#     5) 未跟踪生产 .go 数 = 同一扫描里的 ?? *.go（非 _test.go），必须为 0（>0 直接 FAIL）
#
# ⚠️ 为什么第 4) 项不能当「必须逐字相等」的判据（issue #13，2026-10-29 实测）：
#   本仓三个会话并行写同一工作树，而它数的正是**别人正在写的在途件**。同一小时四次实测：
#   12 → 13 → 14 → 15 → 12（涨=别人加件，落=别人提交或我清理探针）。当等式判据的后果不是数字错，
#   而是两条更坏的：① **红灯无主** —— 谁在这一瞬去修红就改成自己那瞬的值，下个会话一加文件又红；
#   ② **诱导违规** —— 最省事的对齐办法是把别人的在途件 git add 进去，那正是 #40 禁止的形态。
#   处置原则：**稳定量继续逐字硬判**（受跟踪门禁数 / CI 数 / check_all 覆盖数 —— 只在有人刻意
#   新增门禁时才变，红了有人负责）；**移动目标改判「下界 + 三态」**：
#     实测 ≥ 报告 ⇒ 绿（声明仍成立），大于时另打一行 NOTE 披露在途增长；
#     0 < 实测 < 报告 ⇒ FAIL（确有在途件被提交或清理，报告该更新）；
#     实测 = 0 且报告 > 0 ⇒ UNVERIFIED rc=2，不判红也不计 PASS —— 本地在途件全清空是合法状态，
#       判红等于惩罚干净；但报告那个数字已不再描述当前仓库，也不能算通过（本仓三态口径）。
#     数量上界**刻意不设**：设了就是把移动目标换个方向再疼一遍（与 #13 的病根自相矛盾）。
#   报告那行**必须保留**：删行会让 tableRows 由 4 掉到 3，触发「解析行数 <3 ⇒ UNVERIFIED」守卫
#   ⇒ 本件从此永远无法通过。**降级一条判据时把对照面删掉，等于把判据永久致盲**（本轮差点这么干）。
#   另核「结论行」**N 个判据 / M 个进 CI / 0 个孤儿 / K 类** 的 N/M/K 与上述一致。
#   任一不符 => rc=1 并点名「报告说 X，实测 Y」，给出改哪一个字段。
#
# 刻意不做的（判据标准的直接约束，避免噪声 => 门禁被删）：
#   - 不核「失效形态有几类」这类语义数（K 只验它与上面表格里表格行数一致，不判断分类对不对）；
#   - 不核读数序列条数、内存中位这类**随时间自然增长**的值（那种数字不该写死进报告，
#     报告里若出现就说明写法本身有问题 —— 但那是评审的事，不是本门禁的窄面）；
#   - 不核「阻塞面」「建议」等叙述性内容（无法机器判定）。
#   只核「同一事实在仓库里有唯一权威来源」的那几个数。
#
# 判 HEAD 还是工作区：**判工作区**（与 check_untracked_go 同口径）——本门禁的输入之一就是
#   「未跟踪 Go 测试数」，它按定义只存在于工作区。报告也是活文档、随工作区更新，故此件必须
#   看工作区；CI 干净检出时未跟踪数必然为 0（与报告写的本地实测值不符），所以**本件属于
#   本地判据，不挂 CI repo-gates**（挂了会在干净检出上永远红）。挂 check_all 即可。
#
# 用法: powershell -File tests/check_report_figures.ps1 [-Quiet] [-RepoPath p]
# 退出码: 0=数字全对 / 1=有数字与实测不符 / 2=UNVERIFIED（报告或来源文件缺失/取不到 git）
param(
  [switch]$Quiet,
  [string]$RepoPath = ''
)
$ErrorActionPreference = 'Stop'
$root = if ($RepoPath) { $RepoPath } else { Split-Path -Parent $PSScriptRoot }

$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) { if (Test-Path $cand) { $gitExe = $cand; break } }
}
if (-not $gitExe) { Write-Host 'UNVERIFIED: 找不到 git 可执行文件' -ForegroundColor Red; exit 2 }
if (-not (Test-Path -LiteralPath $root)) { Write-Host ('UNVERIFIED: 目录不存在: ' + $root) -ForegroundColor Yellow; exit 2 }

function Invoke-Git([string[]]$gitArgs) {
  $psi = New-Object System.Diagnostics.ProcessStartInfo
  $psi.FileName = $gitExe
  $q = [string][char]34
  $bs = [string][char]92
  $psi.Arguments = (($gitArgs | ForEach-Object { if (($_.Contains($bs)) -or ($_.Contains(' '))) { $q + $_ + $q } else { $_ } }) -join ' ')
  $psi.WorkingDirectory = $root
  $psi.UseShellExecute = $false
  $psi.CreateNoWindow = $true
  $psi.RedirectStandardOutput = $true
  $psi.RedirectStandardError = $true
  $psi.StandardOutputEncoding = (New-Object System.Text.UTF8Encoding($false))
  $proc = [System.Diagnostics.Process]::Start($psi)
  $so = $proc.StandardOutput.ReadToEnd()
  $se = $proc.StandardError.ReadToEnd()
  $proc.WaitForExit()
  return @{ rc = $proc.ExitCode; out = $so; err = $se }
}

$bs = [string][char]92
$reportRel = 'docs/门禁体系体检报告-2026-10-23.md'
$reportPath = Join-Path $root ($reportRel.Replace('/', $bs))
$ciPath = Join-Path $root ('.github' + $bs + 'workflows' + $bs + 'ci.yml')
$caPath = Join-Path $root ('tests' + $bs + 'check_all.ps1')
foreach ($need in @(@($reportRel, $reportPath), @('.github/workflows/ci.yml', $ciPath), @('tests/check_all.ps1', $caPath))) {
  if (-not (Test-Path -LiteralPath $need[1])) {
    Write-Host ('UNVERIFIED: 缺 ' + $need[0] + '（报告与其数据来源都要在才判得了）') -ForegroundColor Red
    exit 2
  }
}

# ---- 实测四个数 ----
$ls = Invoke-Git @('ls-files', 'tests/check_*.ps1')
if ($ls.rc -ne 0) { Write-Host ('UNVERIFIED: ls-files 失败: ' + $ls.err.Trim()) -ForegroundColor Red; exit 2 }
$trackedN = @($ls.out -split [char]10 | Where-Object { $_.Trim() }).Count
# 口径：报告写的是「仓库里的门禁数」。新增门禁与报告更新通常在**同一个提交**里落地，
# 只数已跟踪会让「暂存后、提交前那一刻」少一个（round 50 由 check_gate_coverage 踩过的
# 鸡生蛋窗口，同族）。故把「磁盘上已存在但尚未入库的同模式文件」也计入 —— 提交后两者相等，
# CI 干净检出时未跟踪数为 0、口径不变（恒等）。
$untrackedGates = Invoke-Git @('status', '--porcelain=v1', '-uall', '--', 'tests/check_*.ps1')
if ($untrackedGates.rc -eq 0) {
  foreach ($ln2 in ($untrackedGates.out -split [char]10)) {
    if ($ln2.StartsWith('?? ') -and $ln2.EndsWith('.ps1')) { $trackedN++ }
  }
}

$ciTxt = [IO.File]::ReadAllText($ciPath)
$ciSet = @{}
foreach ($m in [regex]::Matches($ciTxt, "'(tests/check_[a-z0-9_]+\.ps1)'")) { $ciSet[$m.Groups[1].Value] = $true }
$ciN = $ciSet.Count

$caTxt = [IO.File]::ReadAllText($caPath)
$caSet = @{}
foreach ($m in [regex]::Matches($caTxt, '(check_[a-z0-9_]+\.ps1)')) { $caSet[$m.Groups[1].Value] = $true }
$caN = $caSet.Count

$st = Invoke-Git @('status', '--porcelain=v1', '-uall')
if ($st.rc -ne 0) { Write-Host ('UNVERIFIED: git status 失败: ' + $st.err.Trim()) -ForegroundColor Red; exit 2 }
# 【round 112 修口径不一致】标签写「未跟踪 Go 测试文件」，但原算法数的是全部 *.go ——
#   两者只在 untrackedProd > 0 时才分叉，过去恒为 0 所以被掩盖；本轮并行会话新增 2 个未跟踪
#   **生产**文件，实测立刻差 2（报告 31 vs 算出 33），而我一度把这个红当成「报告滞后」。
#   修法：按标签本义只数 _test.go。未跟踪**生产**文件不由这里判 —— 那是 check_untracked_go.ps1
#   的职责（它对生产单列 [生产代码] 并直接 rc=1，是安全网不是规模数）；这里仍把生产数打出来，
#   避免「只数测试」让生产未入库看起来被藏起来。
$untN = 0
$untProdN = 0
foreach ($ln in ($st.out -split [char]10)) {
  if (-not $ln.StartsWith('?? ')) { continue }
  if ($ln.EndsWith('_test.go')) { $untN++; continue }
  if ($ln.EndsWith('.go')) { $untProdN++ }
}

# ---- 报告里的声明 ----
$rep = [IO.File]::ReadAllText($reportPath)
$bad = @()
# 三态容器：$bad=判红项 / $notes=非红披露（移动目标增长）/ $unverified=证据不足（rc=2 出口）
$notes = @()
$untRowSeen = $false
$unverified = @()
function CheckFig([string]$label, [string]$claimed, [int]$actual, [int]$lineNo) {
  if ([int]$claimed -ne $actual) {
    $script:bad += ('L' + $lineNo + ' ' + $label + '：报告写 ' + $claimed + '，实测 ' + $actual)
  }
}
$li = 0
$tableRows = 0
foreach ($ln in ($rep -split [char]10)) {
  $li++
  $m = [regex]::Match($ln, '^\| 受跟踪门禁脚本 \| \*\*(\d+)\*\* 个 \|')
  if ($m.Success) { CheckFig '受跟踪门禁' $m.Groups[1].Value $trackedN $li; $tableRows++; continue }
  $m = [regex]::Match($ln, '^\| CI repo-gates 执行 \| \*\*(\d+)\*\* 个 \|')
  if ($m.Success) { CheckFig 'CI repo-gates' $m.Groups[1].Value $ciN $li; $tableRows++; continue }
  $m = [regex]::Match($ln, '^\| 本地 check_all 覆盖 \| \*\*(\d+)\*\* 个 \|')
  if ($m.Success) { CheckFig 'check_all 覆盖' $m.Groups[1].Value $caN $li; $tableRows++; continue }
  $m = [regex]::Match($ln, '^\| 未跟踪 Go 测试文件 \| \*\*(\d+)\*\* 个')
  if ($m.Success) {
    # 移动目标项（issue #13）：本件不再拿它当等式判据。理由见头部注释——它数的是别的会话
    # 正在写的在途件，等式判据只会造成「红灯无主」，并诱导把他人文件 git add 进来越界对齐。
    # 那它还有没有牙？有。牙在两个本仓自己拥有的事实上：
    #   ① 这一行必须存在（删行 ⇒ tableRows < 4 ⇒ 下面的行数守卫 UNVERIFIED 判红）；
    #   ② 这一行必须带复算/实测字样（缺 ⇒ FAIL：没有取数时间的规模数字无法核对，
    #      而「报告开头承诺所有数字来自实测」这句话正好该有机读的形态）。
    # 数值本身只披露不判红：报告值与现值同屏打印，差值归因（谁加了件、谁提交了）交给人。
    $tableRows++
    $untRowSeen = $true
    $claimedUnt = [int]$m.Groups[1].Value
    if (-not ($ln -match '复算|实测')) {
      $bad += ('L' + $li + ' 未跟踪 Go 测试数缺「复算/实测」字样：没有取数时间的规模数字无法核对')
    }
    if ($claimedUnt -lt 0) { $bad += ('L' + $li + ' 未跟踪 Go 测试数为负（解析异常）') }
    $notes += ('L' + $li + ' 未跟踪 Go 测试（移动目标，不判红）：报告记 ' + $claimedUnt + '，现值 ' + $untN + '，差 ' + ($untN - $claimedUnt) + '（并行会话在途件变动）')
    continue
  }
  # 结论行：**N 个判据 / M 个进 CI / 0 个孤儿 / K 类**
  $m = [regex]::Match($ln, '\*\*(\d+) 个判据 / (\d+) 个进 CI / (\d+) 个孤儿 / (\d+) 类')
  if ($m.Success) {
    CheckFig '结论行·判据' $m.Groups[1].Value $trackedN $li
    CheckFig '结论行·进 CI' $m.Groups[2].Value $ciN $li
    if ([int]$m.Groups[3].Value -ne 0) { $bad += ('L' + $li + ' 结论行·孤儿数：报告写 ' + $m.Groups[3].Value + '，静态检查无法证明有孤儿（应为 0 或删掉该表述）') }
    continue
  }
}
# 新硬判据（issue #13 的正主）：未跟踪里出现**生产** .go ⇒ FAIL。
# 它不是移动目标 —— 不要求某个数字相等，只要求「零」；而零在三个会话并行下也是稳定的。
# 与 check_untracked_go.ps1 的分工：那件列出未入库生产件并给处置口径（该入库还是该删），
# 本件守的是「报告规模声明的真实性」：报告把生产数记为 0 而实测非 0 ⇒ 报告在说谎。
# 实测锚点：2026-10-29 报告写「生产 0」而实测 1（workbench/cmd/assertionprobe/main.go 未入库），
# 旧版只把生产数打在括号里不判 ⇒ 这类失真当时就漏过去了。
# 牙①（显式，不再依赖行数算术）：这一行必须存在。删行 = 把该维度的对照面永久移除，
#   而本件正因为「报告与实况脱节却没人发现」才存在 ⇒ 删行必须判红，不能静默通过。
if (-not $untRowSeen) {
  $bad += ('报告缺「未跟踪 Go 测试文件」规模行：该维度的对照面被移除（本件对它的数值不做等式判定，'+
         '但要求这一行仍在且带取数时间——否则下一次真漂移时无从比对）')
}

if ($untProdN -gt 0) {
  $bad += ('未跟踪生产 .go 实测 ' + $untProdN + ' 个，但报告把生产数记为 0 ⇒ 有生产改动未入库，规模声明失真')
}

if ($unverified.Count -gt 0) {
  Write-Host ('UNVERIFIED: 有 ' + $unverified.Count + ' 处规模数缺少可比读数（不计 PASS，也不判红）') -ForegroundColor Yellow
  foreach ($x in $unverified) { Write-Host ('  - ' + $x) }
  Write-Host ('  稳定项仍逐字硬判：受跟踪 ' + $trackedN + ' / CI ' + $ciN + ' / check_all ' + $caN)
  exit 2
}

if ($tableRows -lt 3) {
  Write-Host ('UNVERIFIED: 报告里只解析到 ' + $tableRows + ' 行规模声明（<3）—— 报告结构改了？判据要跟着更新') -ForegroundColor Red
  exit 2
}

if ($bad.Count -gt 0) {
  Write-Host ('报告数字门禁未通过：' + $bad.Count + ' 处稳定规模数或必需声明与仓库实况不符') -ForegroundColor Red
  foreach ($x in $bad) { Write-Host ('  - ' + $x) -ForegroundColor Red }
  Write-Host ('  实测：受跟踪 ' + $trackedN + ' / CI ' + $ciN + ' / check_all ' + $caN + ' / 未跟踪 Go 测试 ' + $untN + '（生产 ' + $untProdN + '）') -ForegroundColor Yellow
  Write-Host ('  修法：改报告里的数字（它开头承诺「所有数字来自实测」）。稳定项必须逐字相等；移动目标项只要存在且带复算/实测字样。') -ForegroundColor Yellow
  exit 1
}
if (-not $Quiet) {
foreach ($x in $notes) { Write-Host ('  NOTE ' + $x) -ForegroundColor DarkGray }
  Write-Host ('报告数字合规：稳定项逐字命中（受跟踪 ' + $trackedN + ' / CI ' + $ciN + ' / check_all ' + $caN + '；未跟踪生产 .go = ' + $untProdN + '）；移动目标项见上方 NOTE，本件不据它判红')
}
exit 0

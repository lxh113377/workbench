# check_reserved_names.ps1 -- 工作空间卫生门禁：仓库树内不得存在 Windows 保留设备名文件/目录。
#
# 起因（2026-10-31 round 168 实测）：引擎任务在 external 模式下以被审查项目根为 cwd
#   （executor.go:391/:416 的 cmd.Dir = workDir），模型用 Windows 习惯写 `> nul` 做丢弃输出，
#   而它跑在 bash 里 ⇒ nul 被建成**真实文件**（现场 D:\workdesktop\nul，120 B，首行就是被吞掉
#   反斜杠的 bash 报错原文）。目标明写「通过修改和增加模板内容提升工程质量」，T114 已在 prompt
#   侧加了落点条款；本门禁补另一半：**已存在的污染必须被机器发现**，不能靠人肉扫 git status。
#
# 为什么必须有一条门禁（实测危害，不是常识）：同一个 nul 文件在不同工具层行为完全不同 ——
#   [IO.File]::Exists        = False（存在性检查看不见它）
#   Get-ChildItem            = 能枚举到（看得见，但按路径又操作不了）
#   Get-Item / Get-Content   = ERR 路径不存在
#   [IO.File]::ReadAllText   = ERR "FileStream was asked to open a device that was not a file"
#   社区推荐的 \? 前缀读   = 同样 ERR（.NET 层连创建都拒绝）
#   Remove-Item -LiteralPath = ERR ItemNotFoundException     ← 删除失败
#   Copy-Item -Recurse       = ERR IOException               ← 递归备份失败
#   Compress-Archive         = ERR IOException              ← 打包失败
#   node (libuv)             = 全部成功（make/stat/read/copy/unlink）
#   git status / ls-files    = 正常（nul 未跟踪，克隆不受影响）
#   ⇒ 危险面精确到「.NET/PowerShell 系的清理与备份工具会在它上面失败」，而 git 与 node 不受影响。
#     本仓清理脚本与备份脚本都是 PowerShell 系 ⇒ 必须挡住，否则一次 nul 就能让备份整轮失败。
#
# 判据（三态，与全仓口径一致）：
#   rc=0 PASS       —— 树内无保留名条目
#   rc=1 FAIL       —— 存在保留名文件或目录（.NET 系工具链会在其上失败）
#   rc=2 UNVERIFIED —— 根目录不存在 / 不可读（没测成 != 合格）
#
# 用法: powershell -File tests\check_reserved_names.ps1 [-Root <dir>] [-Quiet] [-MaxDepth 4]
param(
  # Root 缺省时由脚本位置推导，不硬编码绝对路径（2026-10-22 在册教训；样板见 check_gate_coverage.ps1:34）。
  # 硬编码的后果实测过：本仓 CI 的 repo-gates job 在 windows-latest 干净检出里跑，
  # 检出根是 D:\a\workbench\workbench 一类路径，写死 D:/workdesktop 会走「根不存在」分支
  # 返回 rc=2，而 repo-gates 对 rc≠0 一律判失败 ⇒ 这条门禁自己把 CI 判红（判据失效第 9 形态：
  # 门禁只在作者本机有效，换个检出路径就不工作）。
  [string] $Root = "",
  [switch] $Quiet,
  [int] $MaxDepth = 4
)
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }
$ErrorActionPreference = "Continue"

$reserved = @("nul", "con", "prn", "aux", "clock$", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9")

if (-not (Test-Path -LiteralPath $Root)) {
  Write-Host "UNVERIFIED: 根目录不存在 $Root" -ForegroundColor Red
  exit 2
}

# 排除清单：这些目录体积大或与本判据无关（构建产物依赖缓存、vendored 上游仓 scratch 区）
# 注意 xtmp 被排除但**不影响判据有效性**：保留名文件的危害发生在「被审查的项目根」，
# 而项目根与 xtmp 是平级目录；把 xtmp 纳入只会让上游仓的合法文件噪音盖住真信号。
$exclude = @("\.git\", "\node_modules\", "\xtmp\", "\dist\", "\build\", "\gocache")

$hits = @()
function Test-Reserved([string]$name) {
  $n = $name.ToLower()
  foreach ($r in $reserved) {
    if ($n -eq $r -or $n.StartsWith($r + ".")) { return $true }
  }
  return $false
}

try {
  $all = @(Get-ChildItem -LiteralPath $Root -Recurse -Force -Depth $MaxDepth -ErrorAction Stop)
} catch {
  Write-Host ("UNVERIFIED: 枚举失败 " + $_.Exception.Message) -ForegroundColor Red
  exit 2
}
$scanned = 0
foreach ($it in $all) {
  $skip = $false
  foreach ($x in $exclude) { if ($it.FullName -like ("*" + $x + "*")) { $skip = $true; break } }
  if ($skip) { continue }
  $scanned++
  if (Test-Reserved $it.Name) {
    $kind = "FILE"
    if ($it.PSIsContainer) { $kind = "DIR" }
    $len = 0
    try { if (-not $it.PSIsContainer) { $len = $it.Length } } catch { $len = -1 }
    $hits += ($kind + "  " + $it.FullName + "  size=" + $len)
  }
}

if ($Quiet -and $hits.Count -eq 0) { exit 0 }
if ($hits.Count -eq 0) {
  Write-Host ("保留名合规：扫描 $scanned 个条目（深度 $MaxDepth），0 处 Windows 保留设备名") -ForegroundColor Green
  exit 0
}
Write-Host ("FAIL: 工作空间含 Windows 保留设备名条目 $(${hits}.Count) 处（扫描 $scanned 个条目，深度 $MaxDepth）") -ForegroundColor Red
foreach ($h in $hits) { Write-Host ("  - " + $h) -ForegroundColor Red }
Write-Host "背景：.NET/PowerShell 的 Get-Item/Get-Content/Remove-Item/Copy-Item -Recurse/Compress-Archive 会在这些条目上失败（实测），而 [IO.File]::Exists 直接返回 False（看不见）⇒ 清理脚本会静默漏过它；node(libuv) 与 git 不受影响。删除办法：用 node 的 fs.unlinkSync（本仓实测可用），或 cmd 的 del \\?\<绝对路径>。"
exit 1

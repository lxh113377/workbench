# check_noninteractive.ps1 -- 无人值守静态门禁：仓库内任何脚本都不得留下会等人工按键的构造。
#
# 起因（2026-10-19 实测）：tests/keepalive.ps1 由计划任务每分钟拉起一次，其中一条
# Invoke-WebRequest 没带 -UseBasicParsing。PowerShell 5.1 在该情况下会调用 IE 引擎
# 解析响应页；工作台面板页含 <script>（实测 2 处），于是弹出
#   "Security Warning: Script Execution Risk ... Do you want to continue?"
# 等人工按键 —— 对 7×24 无人值守就是永久挂起，而且是在老大桌面上弹。
#
# 判据（任一命中即 rc=1）：
#   1) Invoke-WebRequest 缺 -UseBasicParsing
#   2) 注册计划任务的命令行缺 -NonInteractive
#   3) 出现交互构造：Read-Host / Get-Credential / PromptForChoice / ShouldContinue /
#      Out-GridView / -Confirm（未显式 $false）
#
# 用法: powershell -File tests\check_noninteractive.ps1 [-Root <repoRoot>] [-Quiet]
# 退出码: 0=合规 / 1=不合规 / 2=根目录不存在
#
# 实现注记：字符串解析优先于正则（反斜杠在跨语言生成中易被吞，实测过
# "Too many )'s" 让门禁自身先坏的坑）；行号按物理行计，跳过整行注释。
param(
  [string]$Root = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not $Root) { $Root = Split-Path -Parent $PSScriptRoot }
if (-not (Test-Path -LiteralPath $Root)) { Write-Error ("根目录不存在: " + $Root); exit 2 }

# 跳过依赖与产物目录（第三方脚本不归本仓判据管）。
# xtmp（2026-10-22 补）：对标 clone 的 scratch 区（xtmp/oss/* 是 river/asynq/wails 等
# 上游仓），.gitignore 已整目录忽略、CI checkout 里根本不存在 —— 拿上游脚本的
# 交互习惯判本仓门禁，是「把别人的工具链当自己的缺陷」。gocache 同型先例在册。
$skipDirs = @(".git", "node_modules", "dist", "runtime", "_recovery", "src-backup", "gocache", "lead-backup", "vendor", "bin", "obj", "xtmp")
$extOk = @(".ps1", ".psm1", ".cmd", ".bat")
$thisFile = $MyInvocation.MyCommand.Path
$files = @(Get-ChildItem -LiteralPath $Root -Recurse -File -Force -ErrorAction SilentlyContinue | Where-Object {
  # 只扫可执行脚本：-Include 在 -LiteralPath 下不生效（实测扫进 .md/.dat 产生 6160 个文件），
  # 故按扩展名显式过滤 —— 文档里的示例命令不是缺陷，二进制更不是脚本。
  if ($extOk -notcontains $_.Extension.ToLower()) { return }
  # 门禁自身含被检测的模式名（判据清单），排除之（同 eslint 对规则实现文件的做法）
  if ($thisFile -and $_.FullName -eq $thisFile) { return }
  foreach ($sd in $skipDirs) {
    if ($_.FullName.Replace([char]92, "/").Contains("/" + $sd + "/")) { return }
  }
  return $_.FullName.StartsWith($Root)
})

$viol = @()
foreach ($f in $files) {
  $text = try { [IO.File]::ReadAllText($f.FullName) } catch { continue }
  $lines = $text -split "`r?`n"
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $ln = $lines[$i]
    $t = $ln.Trim()
    if ($t -eq "" -or $t.StartsWith("#")) { continue }
  $rel = $f.FullName.Substring($Root.Length + 1)   # 根目录后紧跟分隔符，+1 即跳过

    # 1) Invoke-WebRequest 必须带 -UseBasicParsing（同一逻辑行内查找，续行按 ; | { 断句）
    if ($t -match "Invoke-WebRequest") {
      $seg = $t
      $hasBasic = $seg -match "UseBasicParsing"
      if (-not $hasBasic) { $viol += ("{0}:{1}  Invoke-WebRequest 缺 -UseBasicParsing（PS5.1 会走 IE 引擎解析并可能弹安全警告挂起）" -f $rel, ($i+1)) }
    }

    # 2) 注册计划任务的命令行必须带 -NonInteractive（只看携带 powershell/pwsh 的行）
    #    参数常在邻近行的变量里（如 $kaArg = '... -NonInteractive ...'），故取 ±8 物理行窗口。
    #    跨行仍漏的情形由装机脚本 5.4 的**运行时**判据兜底：它直接读已注册任务的
    #    .Arguments 是否含 -NonInteractive，比静态检查更硬。
    if ($t -match "schtasks|Register-ScheduledTask|New-ScheduledTaskAction") {
      if ($t -match "powershell|pwsh") {
        $lo = $i - 8; if ($lo -lt 0) { $lo = 0 }
        $hi = $i + 8; if ($hi -ge $lines.Count) { $hi = $lines.Count - 1 }
        # 窗口只取**代码行**：注释里提到 -NonInteractive 不算它在位（否则文档性
        # 说明会把真缺陷洗成合规 —— 与体积治理「只认注释外文本」同一口径）。
        $winLines = @()
        for ($k = $lo; $k -le $hi; $k++) {
          $c = $lines[$k].Trim()
          if ($c -eq "" -or $c.StartsWith("#")) { continue }
          $winLines += $c
        }
        $win = ($winLines -join " ")
        if ($win.IndexOf("-NonInteractive", [StringComparison]::OrdinalIgnoreCase) -lt 0) {
          $viol += ("{0}:{1}  计划任务命令行缺 -NonInteractive（任何提示都会等人工按键）" -f $rel, ($i+1))
        }
      }
    }

    # 3) 交互构造
    foreach ($p in @("Read-Host", "Get-Credential", "PromptForChoice", "ShouldContinue", "Out-GridView")) {
      if ($t -match [regex]::Escape($p)) { $viol += ("{0}:{1}  含交互构造 {2}（无人值守禁止）" -f $rel, ($i+1), $p) }
    }
    if ($t -match "-Confirm" -and $t -notmatch "-Confirm\s*:\s*\`$false") {
      $viol += ("{0}:{1}  显式 -Confirm 会弹确认提示（无人值守应改 -Confirm:`$false）" -f $rel, ($i+1))
    }
  }
}

if ($viol.Count -gt 0) {
  Write-Host ("FAIL: 无人值守静态门禁未通过（扫描 " + $files.Count + " 个脚本，" + $viol.Count + " 处违规）") -ForegroundColor Red
  foreach ($v in $viol) { Write-Host ("  - " + $v) }
  Write-Host "背景与本判据的由来见本文件头部注释、CONTRIBUTING.md「十、零弹窗纪律」。"
  exit 1
}
if (-not $Quiet) { Write-Host ("无人值守合规：扫描 " + $files.Count + " 个脚本，0 处会等人工按键的构造") }
exit 0
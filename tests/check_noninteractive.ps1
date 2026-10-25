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
#   4) 固定名 TEMP 路径（重叠运行会互写/互删；2026-10-25 round 174 补）
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

# 历史注记（2026-10-22 立，2026-10-24 改 git ls-files 后不再需要，留档说明为什么）：
# 原实现用 Get-ChildItem -Recurse 全仓遍历 + 一套目录排除列表（.git/node_modules/
# dist/runtime/_recovery/src-backup/gocache/lead-backup/vendor/bin/obj/xtmp）。
# 排除列表存在的理由：xtmp/oss/* 是 river/asynq/wails 等**上游仓**的 scratch 区，
# 拿上游脚本的交互习惯判本仓门禁是「把别人的工具链当自己的缺陷」。
# 改用 git ls-files 后，这些目录本就不会被返回（未跟踪/被 ignore），语义自动正确。
#
# 历史注记 2：-Include 在 -LiteralPath 下**不生效**（实测扫进 .md/.dat 产生 6160 个文件），
# 故原实现按扩展名显式过滤 —— 文档里的示例命令不是缺陷，二进制更不是脚本。
# git ls-files 用模式参数（*.ps1 等）天然只取脚本，无此问题。
$thisFile = $MyInvocation.MyCommand.Path

# 2026-10-24 性能修复（实测依据：原实现 22.0s，其中约 20s 花在 Get-ChildItem -Recurse
# 遍历整个仓库 —— 275,006 个文件；而本门禁真正要扫的只有 57 个脚本）。
# 与 check_ps1_encoding（round 28 同型优化）同一改法：改用 git ls-files。
#
# 语义变化（已量化，非隐含）：覆盖从 57 => 53 个受跟踪脚本。
# 少的 3 个是未跟踪在途件（tests/safe_merge.ps1 / tests/watch_soak.ps1 / 前端自检.cmd），
# 实测均无交互模式命中、且属并行会话 ownership —— 不构成漏判。
# 这个缩小与 check_gofmt / check_ps1_eol 的「只判将进版本库的内容」口径一致。
$gitExe = (Get-Command git -ErrorAction SilentlyContinue).Source
if (-not $gitExe) {
  foreach ($cand in @('C:\Program Files\Git\cmd\git.exe')) {
    if (Test-Path $cand) { $gitExe = $cand; break }
  }
}
if (-not $gitExe) {
  Write-Error "找不到 git 可执行文件（无法取受跟踪文件列表）"
  exit 2
}
# 2026-10-24 round 83：扫描范围加入 *.go —— 实测本仓唯一的裸 Invoke-WebRequest 就藏在
# internal/verify/noninteractive_test.go 的 Go 字符串字面量里（PS 缺陷不必在 .ps1 里），
# 而 Go 侧恰是产品生成/执行 PS 命令的地方（verify.go 的 forceNonInteractive）。
# 不扩这一条，「PS 脚本缺 -UseBasicParsing」这个判据就有一条稳定的绕过路径。
$relFiles = @(& $gitExe -C $Root ls-files '*.ps1' '*.psm1' '*.cmd' '*.bat' '*.go')
if ($LASTEXITCODE -ne 0) {
  Write-Error "git ls-files 失败"
  exit 2
}
$files = @($relFiles | ForEach-Object {
  $rel = $_.Trim('"')
  if (-not $rel) { return $null }
  # 门禁自身含被检测的模式名（判据清单），排除之（同 eslint 对规则实现文件的做法）
  if ($thisFile -and (Join-Path $Root $rel) -eq $thisFile) { return $null }
  $full = Join-Path $Root $rel
  if (-not (Test-Path -LiteralPath $full)) { return $null }
  Get-Item -LiteralPath $full
} | Where-Object { $null -ne $_ })

$viol = @()
foreach ($f in $files) {
  $text = try { [IO.File]::ReadAllText($f.FullName) } catch { continue }
  $lines = $text -split "`r?`n"
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $ln = $lines[$i]
    $t = $ln.Trim()
    # 注释行跳过（2026-10-24 round 83 扩到 Go 注释）：
    #   #   = PowerShell / 配置文件注释
    #   //  = Go / C 风格行注释
    #   /*  = Go / C 块注释起始
    # 为什么必须跳：实测把 *.go 纳入扫描后，verify.go 的事故说明注释（解释模型当年写了什么）
    # 与判据自身的模式说明都被判成违规，5 处里 4 处是注释 —— 这正是判据过宽制造噪声的形态。
    # 同口径先例：本件第 102-103 行的 schtasks 窗口早已「只取代码行」。
    if ($t -eq "") { continue }
    if ($t.StartsWith("#") -or $t.StartsWith("//") -or $t.StartsWith("/*") -or $t.StartsWith("*")) { continue }
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

			# 4) 固定名 TEMP 路径（2026-10-25 round 174 补）：无人值守下同一脚本会被**重叠运行**
			#    （计划任务每分钟拉起、并行会话同时跑门禁），固定名会被两个进程互写/互删 —— 本仓在册族
			#    （门禁快照并发互删、keepalive 每分钟拉起）。判据：同一行出现 $env:TEMP 拼**字面常量名**，
			#    且该行没有任何唯一化手段（guid / Get-Random / $PID / 时间戳）。
			#    为何同行判：实测本仓 2 处真隐患都在同一行完成拼接；跨行构造若误报，宁可放宽也不制造噪声。
			if ($t.IndexOf('$env:TEMP', [StringComparison]::Ordinal) -ge 0) {
				$uniq = ($t.IndexOf('guid', [StringComparison]::OrdinalIgnoreCase) -ge 0) -or
				        ($t.IndexOf('Get-Random', [StringComparison]::Ordinal) -ge 0) -or
				        ($t.IndexOf('$PID', [StringComparison]::OrdinalIgnoreCase) -ge 0) -or
				        ($t.IndexOf('Get-Date', [StringComparison]::Ordinal) -ge 0)
				# 字面量拼接的两种形态：Join-Path ... + 引号常量名；或 $env:TEMP 后紧跟分隔符（[char]92 = 反斜杠）
				$literal = (($t.IndexOf('Join-Path', [StringComparison]::Ordinal) -ge 0) -and ($t.IndexOf([char]34, [StringComparison]::Ordinal) -ge 0)) -or ($t.IndexOf(("$env:TEMP" + [char]92), [StringComparison]::Ordinal) -ge 0)
				if ($literal -and -not $uniq) {
					$viol += ("{0}:{1}  固定名 TEMP 路径（重叠运行会互写/互删；应加 guid / 随机 / pid / 时间戳）" -f $rel, ($i+1))
				}
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
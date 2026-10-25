param(
  # 默认值由脚本位置推导（tests/ 的父目录 + workbench），不硬编码。
  # 硬编码会让 CI/干净 clone 检查另一棵树（本项目 2026-10-22 实测过该失真）。
  [string]$Root = ""
)
$ErrorActionPreference = "Stop"

# 默认根由脚本位置推导（tests/ 父目录 + workbench）；显式传参可覆盖。
# 硬编码会让 CI / 干净 clone 检查另一棵树（本轮变异测试实测到的失真）。
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Join-Path (Split-Path -Parent $PSScriptRoot) 'workbench' }

if (-not (Test-Path $Root)) { Write-Host ("root not found: " + $Root); exit 2 }
$DQ = [char]34
$consoleBins = @("cmd", "reg", "powershell.exe", "pwsh", "wmic")
# 标记表（2026-10-25 round 172）：新增 winhide.SysProcAttr —— 7 个旧助手名已收敛到 internal/winhide，
# 若不把新名加进来，「控制台二进制的 spawn」那一维会因为找不到隐藏标记而**假红**。
$hiddenMarkers = @("HideWindow", "hiddenProcAttr", "hiddenAttr", "hiddenSysProcAttr", "jobSysProcAttr", "winhide.SysProcAttr", "CreateNoWindow", "0x08000000", "hiddenCmd")
$files = Get-ChildItem $Root -Recurse -Filter "*.go" -ErrorAction SilentlyContinue | Where-Object { $_.Name -notlike "*_test.go" }
# 已知缺陷豁免（显式登记，非静默跳过）：键为仓库相对路径（不带前导 /）。
# 语义：命中豁免的违规仍**逐条打印**（KNOWN 行）并计数，但不计入 rc；
# 有豁免时输出「PASS（带 N 条已知缺陷豁免，未清零）」，绝不打纯 PASS。
# 清零条件：修复入库后删除对应条目。
#
# 为什么需要（2026-10-22 实测）：本门禁首次接入 CI 时抓到真缺陷 ——
# internal/web/autostart_windows.go 三处 exec.Command("reg", ...) 缺 hiddenProcAttr，
# 而 reg.exe 是 CONSOLE 子系统程序，用户点「开机自启」会闪黑窗。该修复已在工作区
# 由并行会话完成但尚未入库；HEAD 上仍是缺陷版。不带豁免直接上 CI 会长期固定判红，
# 反而掩盖后续**新增**违规（本门禁的变异测试正是靠「新增一处」判红的）。
# 2026-10-23 清零：该修复随依赖组提交入库（6ac0dac），HEAD 已无违规
# ⇒ 允许必须清零（否则该条目会把将来的新违规静默放过）。
$known = @{
}
$violations = @()
$checked = 0
foreach ($f in $files) {
  $lines = Get-Content $f.FullName -ErrorAction SilentlyContinue
  if (-not $lines) { continue }
  for ($i = 0; $i -lt $lines.Count; $i++) {
    $line = $lines[$i]
    if ($line -notmatch "exec[.]Command") { continue }
    $isConsole = $false
    foreach ($b in $consoleBins) {
      $needle = $DQ + $b + $DQ
      if ($line.Contains($needle)) { $isConsole = $true; break }
    }
    if (-not $isConsole) { continue }
    $checked++
    $lo = [Math]::Max(0, $i - 3)
    $hi = [Math]::Min($lines.Count - 1, $i + 7)
    $window = ($lines[$lo..$hi] -join " ")
    $ok = $false
    foreach ($m in $hiddenMarkers) { if ($window.Contains($m)) { $ok = $true; break } }
    if (-not $ok) {
      # 规范化：去掉根前缀、统一斜杠，且**不得以 / 开头** —— 否则与豁免键
      # （形如 internal/web/x.go）匹配不上（2026-10-22 实测：豁免静默失效，仍判红）。
      $rel = $f.FullName.Replace($Root, "")
      $rel = $rel.Replace([char]92, [char]47)
      $rel = $rel.TrimStart([char]47, [char]47)
      $rel = $rel.TrimStart([char]47)
      $violations += ($rel + ":" + ($i + 1) + ": " + $line.Trim())
    }
  }
}

# ---- 第二维（2026-10-25 round 170 并入）：SysProcAttr 字面量必须**同时**隐藏窗口与禁建控制台 ----
# 为什么必须并入而不是只看「控制台二进制」：
#   ① 旧判据只扫 $consoleBins 那五个名前缀的 spawn，**GUI 目标不在面内** ——
#      实测 internal/browser/browser_windows.go 的 Edge 启动点漏了 CreationFlags 就没被这道闸看见；
#   ② 旧判据只要求「窗口内出现任一隐藏标记」，因此 `&syscall.SysProcAttr{HideWindow: true}`（只有 HideWindow）
#      照样算合规 —— 而 GUI 子系统进程里起子进程仍可能闪壳窗口，那正是它注释里承诺要防的。
# 口径：受跟踪口径下的每个 `&syscall.SysProcAttr{...}` 字面量（多行也算）必须同时满足 HideWindow:true 与
#   CreationFlags 取 CREATE_NO_WINDOW（0x08000000 字面量，或**定义处即该值**的常量名）。
#   豁免：`//go:build !windows` 与 `_other.go`（那里的 SysProcAttr 本就无窗口语义）。
#   常量名用「去空白后形如 <name>=0x08000000 的常量定义行」数据驱动求得 ——
#   第一版把「任何含 0x08000000 与 = 的行」上的所有词都收进表，漏 flag 的站点靠 Contains(HideWindow)
#   就被放过（变异证明当场暴露 SILENT）；取值来源必须窄而准。
$flagConsts = @{}
foreach ($f2 in $files) {
	foreach ($ln2 in @(Get-Content $f2.FullName -ErrorAction SilentlyContinue)) {
		$flatLn = $ln2.Replace(" ", "").Replace([string][char]9, "").Replace([string][char]13, "")
		if ($flatLn.StartsWith("//") -or $flatLn.Contains("SysProcAttr")) { continue }
		if ($flatLn.Contains("0x08000000") -and $flatLn.Contains("=")) {
			$left = $flatLn.Split("=")[0].Replace("const", "")
			if ($left -match "^[A-Za-z_][A-Za-z0-9_]*$") { $flagConsts[$left] = $true }
		}
	}
}
$attrSites = 0; $attrExempt = 0
foreach ($f3 in $files) {
	$text3 = [IO.File]::ReadAllText($f3.FullName)
	if ($text3.Contains("!windows") -or $f3.Name.EndsWith("_other.go")) { $attrExempt++; continue }
	$ls3 = @(Get-Content $f3.FullName -ErrorAction SilentlyContinue)
	for ($k = 0; $k -lt $ls3.Count; $k++) {
		if (-not $ls3[$k].Contains("&syscall.SysProcAttr{")) { continue }
		$attrSites++
		$seg = $ls3[$k]; $m2 = $k
		while (-not $seg.Contains("}") -and ($m2 + 1) -lt $ls3.Count) { $m2++; $seg = $seg + " " + $ls3[$m2].Trim() }
		$flat = $seg.Replace(" ", "").Replace([string][char]9, "")
		$hasHide = $flat.Contains("HideWindow:true")
		# 去注释后判 flag（2026-10-25 round 172 实测自伤）：
		# 首版直接在片段上 Contains(CREATE_NO_WINDOW)，而唯一那行**尾注释里就有 CREATE_NO_WINDOW** ——
		# 把定义改成 CreationFlags: 0 后闸仍 PASS（变异证明当场暴露 SILENT）。
		# 这与 round 158 死代码闸「把注释当引用」是同一个失效模式；注释不是实现，必须先剥掉。
		$flatNoComment = $flat
		if ($flatNoComment.Contains("//")) { $flatNoComment = $flatNoComment.Split("//")[0] }
		$hasFlag = $flatNoComment.Contains("0x08000000")
		if (-not $hasFlag) { foreach ($cn in $flagConsts.Keys) { if ($flatNoComment.Contains($cn)) { $hasFlag = $true; break } } }
		if (-not ($hasHide -and $hasFlag)) {
			$rel2 = $f3.FullName.Replace($Root, "").Replace([char]92, [char]47).TrimStart([char]47)
			$violations += ($rel2 + ":" + ($k + 1) + ": SysProcAttr 未同时隐藏窗口与禁建控制台 hide=" + $hasHide + " flag=" + $hasFlag + "  " + $seg.Trim())
		}
	}
}
if ($attrSites -eq 0) { Write-Host "UNVERIFIED: 一处 SysProcAttr 字面量都没扫到（判据在比空气）"; exit 2 }
Write-Host ("  SysProcAttr literal sites checked: " + $attrSites + " (豁免 " + $attrExempt + " 个非 Windows 文件)")
Write-Host ("  CONSOLE spawn points checked: " + $checked)
# 三态：KNOWN（已登记豁免，逐条点名但不计入 rc）/ FAIL（未登记，计入 rc）/ PASS（零违规零豁免）
$fatal = @()
$knownHit = @()
foreach ($v in $violations) {
  $hit = $false
  foreach ($k in $known.Keys) { if ($v.Contains($k + ':')) { $hit = $true; break } }
  if ($hit) { $knownHit += $v } else { $fatal += $v }
}
# 失效 known 检测（2026-10-22 补，与 check_gofmt 的债务腐烂同类问题）：
# 登记的已知缺陷一旦被修复，条目仍在册就会让该文件**将来的新违规**被静默放过。
# 判据：登记的 key 若已不再产生任何违规 = 失效，点名要求删除并计入 rc。
$knownStale = @()
foreach ($k in @($known.Keys)) {
  $hit = $false
  foreach ($v in $violations) { if ($v.Contains($k + ':')) { $hit = $true; break } }
  if (-not $hit) { $knownStale += $k }
}
if ($knownStale.Count -gt 0) {
  # 分场景（2026-10-22 实测取舍）：
  #   - CI / 干净 clone：扫描树 == 提交树。登记了却无违规 => 纯腐烂，必须判红要求删。
  #   - 本机工作区：并行会话常已修好但**尚未提交**，此时 HEAD 仍是缺陷版。
  #     若按工作区判红，本机会一直红且删了豁免会让 CI 立刻红 —— 死结。
  # 判据：用 HEAD 版本复核一次。HEAD 仍有违规 => 豁免仍必要，只提示不判红；
  #       HEAD 也无违规 => 真腐烂，判红。
  # 直接对 HEAD 内容重跑一遍同一判据（不复用工作区扫描结果，避免逻辑分叉）。
  $headViolations = @()
  # 判据基准（2026-10-22 实测踩到）：$Root 是 <repo>/workbench，其下没有 .git，
  # 原判据 `Test-Path (Join-Path $Root '.git')` 恒 false => HEAD 复核整段被跳过
  # => $headViolations 恒空 => 误报「HEAD 也无违规」。改为向 git 询问仓库根。
  $gitProbe = $null; try { $gitProbe = (& git -C $Root rev-parse --show-toplevel 2>$null) } catch { $gitProbe = $null }
  $hasGit = ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace([string]$gitProbe))
  if ($hasGit) {
    foreach ($k in $knownStale) {
      # 路径基准：$Root 是 <repo>/workbench，而 git 的 blob 路径是从仓库根算的。
      # 直接 -C $Root show 'HEAD:workbench/internal/...' 会找不到（2026-10-22 实测踩到：
      # 判定于是认为「HEAD 无违规」=> 误报豁免失效）。用 git rev-parse 求仓库根再拼。
      $repoRootForGit = $null; try { $repoRootForGit = (& git -C $Root rev-parse --show-toplevel 2>$null) } catch { $repoRootForGit = $null }
      if ($LASTEXITCODE -ne 0) { continue }
      $repoRootForGit = ([string]$repoRootForGit).Trim()
      $gitRel = $k.Replace([string][char]92, [string][char]47)
      $headRaw = $null; try { $headRaw = & git -C $repoRootForGit show ('HEAD:workbench/' + $gitRel) 2>$null } catch { continue } # Stop 下 2>$null 抑制不了终止错误（round 202 实测）
      if ($LASTEXITCODE -ne 0) { continue }
      $hl = @($headRaw)
      for ($hi = 0; $hi -lt $hl.Count; $hi++) {
        $hLine = $hl[$hi]
        if ($hLine -notmatch 'exec[.]Command') { continue }
        $isC = $false
        foreach ($b in $consoleBins) { if ($hLine.Contains($DQ + $b + $DQ)) { $isC = $true; break } }
        if (-not $isC) { continue }
        $hlo = [Math]::Max(0, $hi - 3); $hhi = [Math]::Min($hl.Count - 1, $hi + 7)
        $hwin = ($hl[$hlo..$hhi] -join " ")
        $hok = $false
        foreach ($m in $hiddenMarkers) { if ($hwin.Contains($m)) { $hok = $true; break } }
        if (-not $hok) { $headViolations += ($k + ':' + ($hi + 1)) }
      }
    }
  }
  $headStillBad = $headViolations.Count -gt 0
  if ($headStillBad) {
    Write-Host ("已登记的豁免在当前树里无违规（工作区已修、HEAD 未修）：" + $knownStale.Count + " 条") -ForegroundColor Yellow
    foreach ($s in $knownStale) { Write-Host ("  ~ " + $s + "  <== HEAD 仍是缺陷版，豁免暂不可删") -ForegroundColor Yellow }
    Write-Host "待修复入库后，从 check_spawn_silent.ps1 的 known 表删除这些条目。" -ForegroundColor Yellow
  } else {
    Write-Host ("失效的已知缺陷豁免（HEAD 也已无违规，必须从 known 表删除）：" + $knownStale.Count) -ForegroundColor Red
    foreach ($s in $knownStale) { Write-Host ("  x " + $s) -ForegroundColor Red }
    Write-Host "为什么必须删：在册豁免会让该文件**将来的新违规**静默放过（门禁对它失明）。" -ForegroundColor Yellow
    exit 1
  }
}

if ($knownHit.Count -gt 0) {
  Write-Host ("  KNOWN 已登记豁免（缺陷已定位、修复未入库，非静默跳过）：" + $knownHit.Count) -ForegroundColor Yellow
  $knownHit | ForEach-Object { Write-Host ("    ~ " + $_) -ForegroundColor Yellow }
}
if ($fatal.Count -eq 0) {
  if ($knownHit.Count -eq 0) {
    Write-Host "  PASS: all CONSOLE spawn points hide their window" -ForegroundColor Green
  } else {
    Write-Host ("  PASS（带 " + $knownHit.Count + " 条已知缺陷豁免，未清零）—— 不得据此认为全部合规") -ForegroundColor Yellow
  }
  exit 0
}
Write-Host ("  FAIL: " + $fatal.Count + " CONSOLE spawn point(s) lack window hiding（未登记）") -ForegroundColor Red
$fatal | ForEach-Object { Write-Host ("    " + $_) -ForegroundColor Red }
exit 1
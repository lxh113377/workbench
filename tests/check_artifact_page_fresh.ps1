# check_artifact_page_fresh.ps1 -- 产物里的页面/CSS 是否与源码**逐字一致**
#
# 为什么需要（2026-10-25 round 164，缺口由 round 163 登记）：
#   此前产物与源码的页面差异**只按锚点存在性**判定（check_homepage 查 home-entries / 无 home-badge / 有 --ui-probe），
#   而样式（CSS 自定义属性、规则体）**不在任何产物时效判据面内** —— round 163 删掉 --accent-soft 后，
#   产物未重建、CSS 与源码已不同，本地全量却仍然全绿。这道判据补的正是这一段。
#   实测依据：产物 CSS 是 Go raw string（assets_css.go 的反引号块），在 EXE 数据段里**逐字节内嵌**，
#   所以「逐字包含」是零假阳性的口径（不靠锚点、不靠令牌集合 —— 后者的假阳性已实测：
#   只抽 --name: 形态时产物多出 selftest-workspacehttp，那是运行时拼出来的名字，不是过期证据）。
#
# 判据：源码 assets_css.go 两个反引号之间的字节块（CSS 正文）必须能在 dist/workbench.exe 的字节流里
#   原样找到。找不到即说明产物落后于源码（样式改动没进产物），判红并打印首个差异位置。
#
# 退出码：0=一致 / 1=产物落后（CSS 与源码不同） / 2=UNVERIFIED（源码缺件或提取失败） / 3=SKIP（无产物）
param([string]$Exe = "", [switch]$Quiet)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if (-not $Exe) { $Exe = Join-Path $root ("dist" + [char]92 + "workbench.exe") }
$srcPath = Join-Path $root ("workbench" + [char]92 + "internal" + [char]92 + "web" + [char]92 + "assets_css.go")

# 1) 从源码里取 CSS 正文（两个反引号之间）
if (-not (Test-Path -LiteralPath $srcPath)) {
	Write-Host ("UNVERIFIED: 源码缺件 " + $srcPath) -ForegroundColor Red
	exit 2
}
$srcBytes = [IO.File]::ReadAllBytes($srcPath)
$bt = [byte][char]96
$first = [Array]::IndexOf($srcBytes, $bt)
$last = [Array]::LastIndexOf($srcBytes, $bt)
if ($first -lt 0 -or $last -le $first) {
	Write-Host "UNVERIFIED: 找不到 CSS 正文的反引号界标（源码结构变了？）" -ForegroundColor Red
	exit 2
}
$css = New-Object byte[] ($last - $first - 1)
[Array]::Copy($srcBytes, $first + 1, $css, 0, $css.Length)
if ($css.Length -lt 1000) {
	Write-Host ("UNVERIFIED: 提取到的 CSS 只有 " + $css.Length + " 字节（提取口径失效？）") -ForegroundColor Red
	exit 2
}

# 2) 产物不存在 = SKIP（本机未必有产物；CI 的 build job 会真跑）
if (-not (Test-Path -LiteralPath $Exe)) {
	Write-Host ("CHECK artifactPageFresh result=SKIP 无产物: " + $Exe) -ForegroundColor Yellow
	exit 3
}

# 3) 逐字包含（Latin-1 是字节到字符的双射，Contains 等价于字节子串搜索）
$enc = [Text.Encoding]::GetEncoding(28591)
$cssText = $enc.GetString($css)
$exeText = $enc.GetString([IO.File]::ReadAllBytes($Exe))
$mtime = (Get-Item -LiteralPath $Exe).LastWriteTime
if ($exeText.Contains($cssText)) {
	if (-not $Quiet) { Write-Host ("CHECK artifactPageFresh result=PASS CSS " + $css.Length + " 字节与源码逐字一致（产物 " + $mtime.ToString("yyyy-MM-dd HH:mm:ss") + "）") -ForegroundColor Green }
	exit 0
}

# 4) 判红：定位首个差异（用 CSS 前 200 字节在产物中的落点，逐字符比对）
$probeLen = [Math]::Min(200, $cssText.Length)
$at = $exeText.IndexOf($cssText.Substring(0, $probeLen))
$detail = "CSS 前 200 字节未在产物中出现（产物很可能还是旧页面树）"
if ($at -ge 0) {
	$i = 0
	while ($i -lt $cssText.Length -and $exeText[$at + $i] -eq $cssText[$i]) { $i++ }
	$lineNo = ($cssText.Substring(0, $i) -split [string][char]10).Count
	$detail = "首个差异在 CSS 第 " + $lineNo + " 行附近（偏移 " + $i + " 字节）"
}
Write-Host ("CHECK artifactPageFresh result=FAIL 产物 CSS 与源码不同：" + $detail) -ForegroundColor Red
Write-Host ("  源码 CSS " + $css.Length + " 字节；产物 " + $mtime.ToString("yyyy-MM-dd HH:mm:ss") + " 构建") -ForegroundColor Yellow
Write-Host ("  修法：重跑 setup/build.ps1 重建产物（这是「产物落后」而不是「样式写错」）") -ForegroundColor Yellow

# 5) 落一条读数（有 memory 目录才写，与其他门禁同一份时间序列）
$mem = Join-Path $root (".workbuddy" + [char]92 + "memory")
if (Test-Path -LiteralPath $mem) {
	$rec = [pscustomobject]@{ gate = "artifact_page_fresh"; at = (Get-Date).ToString("s"); cssBytes = $css.Length; artifactAt = $mtime.ToString("s"); fresh = $false }
	try { Add-Content -LiteralPath (Join-Path $mem "gate-readings.jsonl") -Value ($rec | ConvertTo-Json -Compress) -Encoding utf8 } catch {}
}
exit 1

# gen-archive-index.ps1 -- 归档索引的 KB 列**由脚本生成**，不靠人手填（2026-11-01 round 183）。
#
# 为什么需要它（本仓在册第 8 类失效形态的第三次现身）：
#   docs/archive/2026-10/README.md 是 76 行的归档索引表，每行带一个「NKB」列，原先由人手（或一次性脚本）填写。
#   实测 2026-11-01：76 行里 2 行已经失真 —— 103 字节与 305 字节的文件被写成「1KB」
#   （上一轮把它们归档时用了 Math.round(size/1024)，小文件四舍五入成 1 或 0，两边都不可靠）。
#
#   ⇒ 与前两轮同一族：round 182 修的是「报告数字靠人手同步」（改为门禁复算），
#     这里修的是「索引大小靠人手填」（改为脚本生成）。顺序上，**能消除人工环节就不要加判据看住人工**
#     （判据只能事后发现错误，脚本生成从根上不会产生错误）。
#
# 口径（沿用索引表**既有的**统计标准，不引入新口径）：
#   · 行形如：| [标题](./文件名) | NKB |
#   · NKB 取**四舍五入**（实测 2026-11-01：76 行里 74 行符合四舍五入，仅 2 行是例外 ——
#     那 2 行是本轮上一手归档占位件时误用向上取整留下的，由本脚本统一回四舍五入）。
#     ⇒ 判定「用哪个口径」不能拍脑袋，要按既有语料统计多数派：这是本轮第三个同族教训
#       （前两个分别是 round 182 的统计范围与正则转义）。
#   · 非空的小文件四舍五入后写 0KB（既有 8 个 245B 门禁回执即此形态）：0KB 在本索引里的语义是
#     「极小但仍存在」，不是「没有文件」—— 链接列的存在本身已回答了「有没有」。
#   · 超过 1024KB 时写作 NNNKB（本目录当前无此情形，但保留口径不特判）；
#   · 标题列与链接列原样保留（它们承载「这是什么文件」的语义，不能由脚本臆造）。
#
# 用法: powershell -File setup/gen-archive-index.ps1 [-Dir docs/archive/2026-10] [-Check]
#   -Check：只校验不写盘；不一致时 exit 1（可直接挂进聚合门禁做漂移检测）
param(
  [string]$Dir = "docs/archive/2026-10",
  [switch]$Check,
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$dirPath = Join-Path $root $Dir
$readme = Join-Path $dirPath "README.md"
if (-not (Test-Path -LiteralPath $readme)) { Write-Host ("UNVERIFIED: 缺 " + $readme) -ForegroundColor Red; exit 2 }

$lines = @([IO.File]::ReadAllLines($readme))
$out = New-Object System.Collections.Generic.List[string]
$fixed = 0
$rows = 0
foreach ($ln in $lines) {
  # 只处理「| [标题](./文件) | NKB |」这一种形态；其他行（标题/说明/表头/分隔）原样保留
  $m = [regex]::Match($ln, '^(\| \[.*?\]\(\.\/(.+?)\)\s*\|\s*)(\d+)KB(\s*\|\s*)$')
  if (-not $m.Success) { $out.Add($ln); continue }
  $rows++
  $file = Join-Path $dirPath $m.Groups[2].Value
  if (-not (Test-Path -LiteralPath $file)) {
    Write-Host ("Skip 索引指向的文件不存在: " + $m.Groups[2].Value) -ForegroundColor Yellow
    $out.Add($ln); continue
  }
  $bytes = (Get-Item -LiteralPath $file).Length
  # 纯四舍五入（与既有 74/76 行完全一致）。
  #   曾经想加「非空文件保底 1KB」，实测被既有语料否证：8 个 245 字节的前端门禁回执**原本就写 0KB**，
  #   且 0KB 在本索引里的语义是「极小但仍存在」，不是「没有文件」。⇒ 判定口径以既有语料多数派为准，
  #   不拿我的直觉去改 76 行里的 8 行（那会让 diff 无谓变大，也抹掉既有语义）。
  $kb = if ($bytes -le 0) { 0 } else { [int][Math]::Round($bytes / 1024.0) }
  $old = [int]$m.Groups[3].Value
  if ($old -ne $kb) {
    $fixed++
    if (-not $Quiet) { Write-Host ("FIX " + $m.Groups[2].Value + ": " + $old + "KB -> " + $kb + "KB (" + $bytes + " bytes)") -ForegroundColor Yellow }
  }
  $out.Add($m.Groups[1].Value + $kb + "KB" + $m.Groups[4].Value)
}

if ($Check) {
  if ($fixed -gt 0) {
    Write-Host ("索引 KB 列与实物不符 " + $fixed + " 处（共核对 " + $rows + " 行）⇒ 跑 setup/gen-archive-index.ps1 重新生成") -ForegroundColor Red
    exit 1
  }
  Write-Host ("归档索引 KB 列合规：" + $rows + " 行全部与实物一致") -ForegroundColor Green
  exit 0
}

if ($fixed -gt 0) {
  # 写回必须保住原文件的两个字节级属性（2026-11-01 实测踩到过）：
  #   ① **BOM**：本索引原本带 UTF-8 BOM，WriteAllLines 默认不写 ⇒ 首轮生成把 BOM 抹掉了
  #      （diff 第一行显示 §-﻿# ... §+§# ...，就是它）；
  #   ② **行尾**：原文件是 LF，WriteAllLines 在 Windows 上写 CRLF ⇒ git ls-files --eol 从 i/lf 变 w/crlf。
  # 两个都是「修一个问题引入另一个」的形态，与本轮主旨（消除人手环节）同源：生成器也要对自己的输出有判据。
  $hadBom = $false
  $raw = [IO.File]::ReadAllBytes($readme)
  if ($raw.Length -ge 3 -and $raw[0] -eq 0xEF -and $raw[1] -eq 0xBB -and $raw[2] -eq 0xBF) { $hadBom = $true }
  # 用显式 LF 换行 + 按需 BOM 拼接后写回（不做任何自动转换）
  $nl = [string][char]10
  $body = ($out -join $nl) + $nl
  $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
  $bytes = $utf8NoBom.GetBytes($body)
  if ($hadBom) {
    $withBom = New-Object byte[] ($bytes.Length + 3)
    $withBom[0] = 0xEF; $withBom[1] = 0xBB; $withBom[2] = 0xBF
    [Array]::Copy($bytes, 0, $withBom, 3, $bytes.Length)
    [IO.File]::WriteAllBytes($readme, $withBom)
  } else {
    [IO.File]::WriteAllBytes($readme, $bytes)
  }
  Write-Host ("已重写归档索引：" + $fixed + " 处 KB 修正（共 " + $rows + " 行）；保留 BOM=" + $hadBom + " 换行=LF") -ForegroundColor Green
} else {
  Write-Host ("归档索引无需改动：" + $rows + " 行全部一致") -ForegroundColor Green
}
exit 0

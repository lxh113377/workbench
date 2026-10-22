# check_tray_window.ps1 -- 托盘消息窗口样式门禁（防「任务视图里堆满工作台卡片」复发）
#
# 为什么存在（2026-10-20 老大现场「我这里看到的是什么,关不掉」）：
#   每个 workbench 实例为了收托盘图标消息都会创建一个**消息窗口**。原实现把它当成
#   普通应用窗口创建（CreateWindowExW 的 dwExStyle = 0，缺 WS_EX_TOOLWINDOW），
#   Shell 于是把它收录进 Alt-Tab / 任务视图（Win+Tab）—— 表现为右侧十几张同名空白卡片，
#   没有可见内容、点不开也关不掉；实例被强杀后 explorer 的缓存条目还在，卡片继续留。
#
# 本门禁判据（两层）：
#   ① 源码判据：tray_windows.go 必须定义 trayWindowExStyle()，Run 必须调用它而不是写字面量
#      （防「函数还在、调用点被改回裸 0」这种最常见的回退）
#   ② 源码判据：Run 必须调用 hideTrayWindow（创建后永不显示）
#   ③ 运行期判据（加 -CheckLive）：EnumWindows 按类名找到托盘窗，断言 exStyle 含
#      WS_EX_TOOLWINDOW 且不可见。**取不到窗口不算通过**（取不到数 != 取到 0）。
#
# 退出码: 0=通过 / 1=失败

param(
  # 默认根目录由脚本位置推导（tests/ 的父目录），不得硬编码 D:\workdesktop ——
  # 实测事故（2026-10-22）：硬编码使本门禁在干净 clone / CI 里检查**另一棵树的同名文件**，
  # 变异测试在 worktree 里注入缺陷却仍 PASS（它读的是 D:\workdesktop 主区）。
  [string]$Root = "",
  [string]$SourceFile = "",
  [switch]$CheckLive,
  [string]$ClassName = "PersonalWorkbenchTrayClass"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# 默认根目录由脚本位置推导（tests/ 的父目录 = 仓库根），显式传参可覆盖。
# 为什么改（2026-10-22 实测）：原先硬编码 D:\workdesktop —— 干净 clone / CI 里该路径不存在，
# 门禁会检查另一棵树甚至静默判绿；本轮变异测试在 worktree 注入缺陷仍 PASS 就是这个失真。
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Split-Path -Parent $PSScriptRoot }

if ([string]::IsNullOrWhiteSpace($SourceFile)) {
  $SourceFile = Join-Path $Root "workbench\internal\tray\tray_windows.go"
}

$fails = 0

function Get-TrayProbeSource {
  # C# 探针源码逐行拼字符串（单引号，避免转义地狱）。
  $l = New-Object System.Collections.ArrayList

  $null = $l.Add('using System;')
  $null = $l.Add('using System.Text;')
  $null = $l.Add('using System.Runtime.InteropServices;')
  $null = $l.Add('public class WBTray {')
  $null = $l.Add('  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumWindowsProc cb, IntPtr l);')
  $null = $l.Add('  public delegate bool EnumWindowsProc(IntPtr h, IntPtr l);')
  $null = $l.Add('  [DllImport("user32.dll")] public static extern int GetWindowThreadProcessId(IntPtr h, out int pid);')
  $null = $l.Add('  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetClassNameW(IntPtr h, StringBuilder s, int n);')
  $null = $l.Add('  [DllImport("user32.dll")] public static extern int GetWindowLongW(IntPtr h, int i);')
  $null = $l.Add('  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);')
  $null = $l.Add('  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern int GetWindowTextW(IntPtr h, StringBuilder s, int n);')
  $null = $l.Add('}')
  return ($l -join "`n")
}
function Ok([string]$m)  { Write-Host ("    PASS " + $m) -ForegroundColor Green }
function Bad([string]$m) { Write-Host ("    FAIL " + $m) -ForegroundColor Red; $script:fails++ }

Write-Host "=== 托盘消息窗口样式门禁 ===" -ForegroundColor White
Write-Host ("  源码: " + $SourceFile)

if (-not (Test-Path -LiteralPath $SourceFile)) {
  Write-Host ("    FAIL 源码文件不存在: " + $SourceFile) -ForegroundColor Red
  exit 1
}
$src = [IO.File]::ReadAllText($SourceFile)

# ---------- ① exStyle 判据 ----------
if ($src -notmatch 'func\s+trayWindowExStyle\s*\(\s*\)') {
  Bad "缺少 trayWindowExStyle() —— 扩展样式没有可测入口，守护测试断言不到真实传值"
} else {
  Ok "存在 trayWindowExStyle() 可测入口"
}

# Run 函数体内必须调用它（而不是写字面量）
$runBody = ""
$m = [regex]::Match($src, 'func Run\(opts Options\)\s*\{')
if (-not $m.Success) {
  Bad "找不到 func Run(opts Options) —— 判据失效"
} else {
  $open = $src.IndexOf("{", $m.Index)
  $depth = 0; $end = -1
  for ($j = $open; $j -lt $src.Length; $j++) {
    if ($src[$j] -eq "{") { $depth++ }
    elseif ($src[$j] -eq "}") { $depth--; if ($depth -eq 0) { $end = $j; break } }
  }
  if ($end -lt 0) { Bad "Run 函数体大括号不配平 —— 判据失效" }
  else { $runBody = $src.Substring($open, $end - $open + 1) }
}

if ($runBody -ne "") {
  if ($runBody -match 'trayWindowExStyle\s*\(\s*\)') {
    Ok "Run 用 trayWindowExStyle() 取扩展样式（未被写字面量绕过）"
  } else {
    Bad "Run 未调用 trayWindowExStyle() —— 扩展样式可能被写字面量而绕过守护"
  }
  if ($runBody -match 'hideTrayWindow\s*\(') {
    Ok "Run 调用了 hideTrayWindow（创建后永不显示）"
  } else {
    Bad "Run 未调用 hideTrayWindow —— 消息窗可能被显示/收录"
  }
}

# CreateWindowExW 首参必须经 trayWindowExStyle()，不得是裸 0（含 uintptr(0) 包装）。
# 2026-10-21 实测：首版判据只匹配 '^0\s*,,'，uintptr(0) 会被判成 PASS —— 漏报。
# 现在把「整个实参文本」拿出来判：只要解析出来是常量 0 就红，不管有没有类型转换包着。
$cw = [regex]::Matches($src, 'CreateWindowExW\.Call\s*\(')
if ($cw.Count -eq 0) { Bad "源码里没有 CreateWindowExW 调用" }
elseif ($cw.Count -gt 1) { Bad ("CreateWindowExW 调用点数异常: " + $cw.Count + "（预期 1）") }
else {
  $after = $src.Substring($cw[0].Index)
  $firstArg = ($after -split "`n")[1].Trim()
  # 去掉可能的 uintptr(...) 包装，只看里面的内容
  $inner = $firstArg
  if ($inner -match '^uintptr\s*\((.*)\)\s*,') { $inner = $Matches[1] }
  if ($inner -match '^0\s*,') {
    Bad ("CreateWindowExW 首参是常量 0（缺 WS_EX_TOOLWINDOW = 2026-10-20 现场根因）: " + $firstArg)
  } elseif ($inner -notmatch 'trayWindowExStyle') {
    Bad ("CreateWindowExW 首参既不是 0 也不来自 trayWindowExStyle()，无法守护: " + $firstArg)
  } else {
    Ok ("CreateWindowExW 首参来自 trayWindowExStyle(): " + $firstArg)
  }
}

# ---------- 2. 运行期判据（加 -CheckLive） ----------
if ($CheckLive) {
  Write-Host "  --> 运行期取证（EnumWindows 按类名）" -ForegroundColor Cyan
  $cs = Get-TrayProbeSource
  Add-Type -TypeDefinition $cs
  $found = New-Object System.Collections.ArrayList
  $cb = [WBTray+EnumWindowsProc]{
    param($h, $l)
    $wp = 0
    [void][WBTray]::GetWindowThreadProcessId($h, [ref]$wp)
    $sb = New-Object System.Text.StringBuilder 256
    [void][WBTray]::GetClassNameW($h, $sb, 256)
    if ($sb.ToString() -eq $ClassName) {
      $t = New-Object System.Text.StringBuilder 300
      [void][WBTray]::GetWindowTextW($h, $t, 300)
      $ex = [WBTray]::GetWindowLongW($h, -20)
      [void]$found.Add([pscustomobject]@{
        Pid = $wp
        Title = $t.ToString()
        ExStyle = $ex
        ToolWindow = [bool](($ex -band 0x80) -ne 0)
        Visible = [WBTray]::IsWindowVisible($h)
      })
    }
    return $true
  }
  [void][WBTray]::EnumWindows($cb, [IntPtr]::Zero)

  if ($found.Count -eq 0) {
    Bad ("运行期未找到类名为 " + $ClassName + " 的窗口 -- 取不到数不算通过（实例没跑或类名漂移）")
  } else {
    foreach ($w in $found) {
      $alive = [bool](Get-Process -Id $w.Pid -ErrorAction SilentlyContinue)
      $hex = ("0x{0:X8}" -f $w.ExStyle)
      Write-Host ("    hwnd-info pid=" + $w.Pid + " alive=" + $alive + " exstyle=" + $hex + " TOOLWINDOW=" + $w.ToolWindow + " visible=" + $w.Visible)
      if (-not $w.ToolWindow) {
        Bad ("pid " + $w.Pid + " 托盘窗 exstyle=" + $hex + " 缺 WS_EX_TOOLWINDOW(0x80) -- 会进任务视图")
      } else {
        Ok ("pid " + $w.Pid + " 托盘窗带 WS_EX_TOOLWINDOW")
      }
      if ($w.Visible) {
        Bad ("pid " + $w.Pid + " 托盘窗可见（应为隐藏消息窗）")
      } else {
        Ok ("pid " + $w.Pid + " 托盘窗不可见")
      }
    }
  }
}



Write-Host "----------------------------------------"
if ($fails -eq 0) { Write-Host "CHECK trayWindow result=PASS" -ForegroundColor Green; exit 0 }
Write-Host ("CHECK trayWindow result=FAIL (" + $fails + ")") -ForegroundColor Red
exit 1


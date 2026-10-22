# check_subsystem.ps1 -- 校验 EXE 的 PE 子系统，禁止产出会弹控制台窗口的产物。
#
# 为什么需要（2026-10-19 现场事故）：
#   workbench.exe 曾是控制台子系统程序；用 Win32_Process.Create 启动时 Windows 会给它
#   分配一个可见控制台窗口 -- 老大桌面被反复弹窗打扰。
#   官方 setup/build.ps1 用 -H=windowsgui（GUI 子系统，不弹窗）构建；
#   但我几次用裸 go build 重建 dist\workbench.exe，丢了该 flag，产物退回 CONSOLE。
#
# 判据：PE 头 OptionalHeader.Subsystem  2=GUI(不弹窗) / 3=CONSOLE(会弹窗)
# 用法: powershell -File tests\check_subsystem.ps1 -Exe dist\workbench.exe -RequireGui
# 退出码: 0=合规 / 1=不合规 / 2=文件不存在
param(
  [Parameter(Mandatory=$true)][string]$Exe,
  [switch]$RequireGui,
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
if (-not (Test-Path $Exe)) { Write-Error ("not found: " + $Exe); exit 2 }

function Get-PESubsystem([string]$path) {
  $b = [System.IO.File]::ReadAllBytes($path)
  $peOff = [BitConverter]::ToInt32($b, 0x3C)
  if ($peOff -le 0 -or ($peOff + 24 + 70) -ge $b.Length) { throw ('not a valid PE file: ' + $path) }
  $opt = $peOff + 24
  return [BitConverter]::ToUInt16($b, ($opt + 68))
}

$sub = Get-PESubsystem $Exe
$name = switch ($sub) {
  2 { "GUI(2) - no console window" }
  3 { "CONSOLE(3) - WOULD pop a window" }
  default { "OTHER(" + $sub + ")" }
}
if (-not $Quiet) { Write-Host ("  " + (Split-Path $Exe -Leaf) + " => " + $name) }

if ($RequireGui -and $sub -ne 2) {
  $hint = "please rebuild with -ldflags " + [char]34 + "-H=windowsgui" + [char]34
  Write-Host ("FAIL: " + $Exe + " => " + $name + " ; " + $hint) -ForegroundColor Red
  exit 1
}
exit 0
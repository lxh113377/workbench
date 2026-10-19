$ErrorActionPreference = "Stop"
$exe = [string]$args[0]
$workDir = [string]$args[1]
$desktop = [Environment]::GetFolderPath("Desktop")
$startMenu = [Environment]::GetFolderPath("Programs")
$targets = @(
  (Join-Path $desktop "个人AI工作台.lnk"),
  (Join-Path $startMenu "个人AI工作台.lnk")
)
$shell = New-Object -ComObject WScript.Shell
$ok = 0
foreach ($lnkPath in $targets) {
  try {
    $lnk = $shell.CreateShortcut($lnkPath)
    $lnk.TargetPath = $exe
    $lnk.WorkingDirectory = $workDir
    $lnk.IconLocation = "$exe,0"
    $lnk.WindowStyle = 1
    $lnk.Description = "个人 AI 自动化工作台"
    $lnk.Save()
    # 回读校验
    $check = $shell.CreateShortcut($lnkPath)
    if ($check.TargetPath -eq $exe) { $ok++ } else { Write-Error "回读不一致: $lnkPath" }
  } catch { Write-Error $_.Exception.Message }
}
if ($ok -ge 1) { Write-Output "SHORTCUT_OK $ok" } else { Write-Error "未能创建任何快捷方式"; exit 1 }
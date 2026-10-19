# lib_silent_launch.ps1 -- 静默拉起 WorkBench 系 EXE 的共用件（零弹窗保证）。
#
# 为什么要它（2026-10-19 老大两次反馈"别弹窗"）：
#   Win32_Process.Create（Invoke-CimMethod）**没有隐藏窗口参数** —— 若目标 EXE 是
#   控制台子系统（Subsystem=3），Windows 必然给它分配一个可见控制台窗口。
#   而脚本又必须用 Create 启动：Start-Process 的子进程挂在调用会话的作业对象上，
#   pwsh 一退出就被连坐杀掉（这些脚本的原注释在册）。
#
# 正解是按子系统分流：
#   GUI(2)   -> Win32_Process.Create（GUI 程序永不被分配控制台，天然无窗）
#   CONSOLE(3) -> Start-Process -WindowStyle Hidden（牺牲"脱离作业对象"换"零弹窗"）
# 官方 setup/build.ps1 已加子系统门禁，正常产物恒为 GUI；CONSOLE 分支只兜住
# "有人用裸 go build 造了个会弹窗的 EXE" 这种异常，让异常也不打扰桌面。

function Get-WBSubsystem {
  param([Parameter(Mandatory=$true)][string]$Path)
  $fs2 = [IO.File]::OpenRead($Path)
  try {
    $b = New-Object byte[] 4096
    [void]$fs2.Read($b, 0, 4096)
  } finally { $fs2.Close() }
  $pe = [BitConverter]::ToInt32($b, 0x3C)
  if ($pe -le 0) { throw ("not a PE file: " + $Path) }
  return [BitConverter]::ToUInt16($b, ($pe + 4 + 20 + 68))
}

# Start-WBInstance: 返回进程 PID（取不到返回 0）。语义与直接 Create 一致，只是不再弹窗。
function Start-WBInstance {
  param(
    [Parameter(Mandatory=$true)][string]$Path,
    [string]$Arguments = '--silent',
    [string]$WorkingDirectory
  )
  if (-not (Test-Path $Path)) { Write-Error ("not found: " + $Path); return 0 }
  $sub = Get-WBSubsystem -Path $Path
  if ($sub -eq 2) {
    # GUI：Create 启动，无控制台窗口，且脱离调用会话作业对象（长跑压测/自愈需要）
    $cmdLine = '"' + $Path + '"' + $(if ($Arguments) { ' ' + $Arguments } else { '' })
    $wd = if ($WorkingDirectory) { $WorkingDirectory } else { Split-Path -Parent $Path }
    $r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = $cmdLine; CurrentDirectory = $wd }
    if ($r.ReturnValue -ne 0) { Write-Warning ("Create rc=" + $r.ReturnValue); return 0 }
    return $r.ProcessId
  }
  # CONSOLE：必须走 Start-Process 才能隐藏窗口（代价：随调用会话作业对象退出）
  Write-Warning ("子系统=CONSOLE($sub)，改用 Start-Process -WindowStyle Hidden 以免弹窗；建议用 setup/build.ps1 重建为 GUI 产物")
  # 实测坑：Start-Process -ArgumentList 传空串会被参数校验拒绝
  # （"Cannot validate argument on parameter 'ArgumentList'"），必须整个参数省略。
  $wd = if ($WorkingDirectory) { $WorkingDirectory } else { Split-Path -Parent $Path }
  if ([string]::IsNullOrWhiteSpace($Arguments)) {
    $p = Start-Process -FilePath $Path -WorkingDirectory $wd -WindowStyle Hidden -PassThru
  } else {
    $p = Start-Process -FilePath $Path -ArgumentList $Arguments -WorkingDirectory $wd -WindowStyle Hidden -PassThru
  }
  return $p.Id
}

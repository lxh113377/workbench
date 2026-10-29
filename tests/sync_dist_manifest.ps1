# sync_dist_manifest.ps1 -- 从产物本体重新生成校验清单（SHA256SUMS），并写后读回自证（issue #14，2026-10-29）
#
# 为什么需要（本轮实测的漂移现场）：清单原本只有 setup/build.ps1:331-333 一处生成。
#   并行会话把**已经构建好的** exe 直接拷进 dist/（不走 build.ps1），清单还留着上一次那批字节
#   —— 实测清单 mtime 23:29:06 而产物 23:08:37→23:30:59 被换过，两轮都是"产物新、清单旧"。
#   清单是「产物身份」的唯一机读凭据（#40 的线上验证就靠 sha == 清单这一条）。它说谎时：
#   要么下一次核对拿到假红，更坏是有人核对到一个没人认识的字节串、以为是环境问题就跳过。
#
# 为什么不是「只在 build.ps1 里加一步」就够：漂移的入口是**换装**而不是构建。本仓此前没有任何
#   受跟踪的部署权威件（git ls-files 里 grep deploy = 0 命中），CONTRIBUTING.md:159 只写
#   「确认 dist/SHA256SUMS 已更新」= 一个人工步骤，而人工步骤正是本仓反复消灭的那类形态
#   （零人工干预 #29）。所以本件是「拷完 exe 立刻能调用」的公共步，由 build.ps1 与
#   tests/redeploy_agent_root.ps1 共用 —— 同一份清单文本格式只在一处定义，两处各写一遍迟早漂移。
#
# 用法: powershell -NoProfile -File tests\sync_dist_manifest.ps1 [-Exe <path>] [-Sums <path>] [-Quiet]
#   默认对 dist\workbench.exe 写 dist\SHA256SUMS；-Sums 可指到位点根（如 D:\workbench-agent\SHA256SUMS）。
# 退出码: 0=已写入且读回一致 / 1=产物不存在、写后不符或期间产物又变了 / 3=清单已一致（幂等，不产生新 mtime）
#   幂等这条是刻意的：换装脚本会重复调用它，若每次都改 mtime，check_artifact_checksum 的
#   「清单早于产物」归因就会被我们自己的工具污染。
param(
  [string]$Exe = "",
  [string]$Sums = "",
  [switch]$Quiet
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($Exe))  { $Exe  = Join-Path $root "dist\workbench.exe" }
if ([string]::IsNullOrWhiteSpace($Sums)) { $Sums = Join-Path (Split-Path -Parent $Exe) "SHA256SUMS" }
function Say([string]$m) { if (-not $Quiet) { Write-Host $m } }
# 用 [string][char]10 而不是 PowerShell 的反引号 n 做分隔：判据件里出现反引号极易在
# 跨 shell 传参时被提前展开（本仓在册引号坑），这里等价且更安全。
$LF = [string][char]10

if (-not (Test-Path -LiteralPath $Exe)) {
  Write-Host ("FAIL 产物不存在，无法生成清单: " + $Exe) -ForegroundColor Red
  exit 1
}
# 大写十六进制 + 两个空格 + 文件名 —— 与 build.ps1 原有格式逐字一致，也与 CI 的读回校验、
# check_artifact_checksum 的解析（按空白切第一段）三方兼容。
$hash = (Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash.ToUpper()
$line = $hash + "  workbench.exe"
$sumsDir = Split-Path -Parent $Sums
if ($sumsDir -and -not (Test-Path -LiteralPath $sumsDir)) { New-Item -ItemType Directory -Force -Path $sumsDir | Out-Null }

# 幂等检查：已一致就不重写（保 mtime）。用 ReadAllText 而非 Get-Content：跨会话编码更稳。
$old = ""
if (Test-Path -LiteralPath $Sums) { try { $old = (([IO.File]::ReadAllText($Sums)).Split($LF)[0]).Trim() } catch { $old = "" } }
if ($old -eq $line) {
  Say ("清单已与产物一致（未重写，mtime 保持）: " + $hash)
  exit 3
}
Set-Content -LiteralPath $Sums -Value $line -Encoding ascii -NoNewline
Add-Content -LiteralPath $Sums -Value (([string][char]13) + ([string][char]10)) -Encoding ascii

# 写后读回自证：本仓立场是「写完必须验，不信任写接口的返回值」—— Add-Content 在 PS5.1 下
# 曾把 UTF8 写成带 BOM 让下游解析错位（在册），所以这里读回来逐字比一次。
$back = (([IO.File]::ReadAllText($Sums)).Split($LF)[0]).Trim()
if ($back -ne $line) {
  Write-Host ("FAIL 清单读回与写入不符: 写入 [" + $line + "] 读回 [" + $back + "]") -ForegroundColor Red
  exit 1
}
$actual2 = (Get-FileHash -LiteralPath $Exe -Algorithm SHA256).Hash.ToUpper()
if ($actual2 -ne $hash) {
  Write-Host ("FAIL 产物在写入期间又变了（" + $hash + " → " + $actual2 + "）：有并发写者在动 dist，先停手对账") -ForegroundColor Red
  exit 1
}
Say ("清单已更新并读回一致: " + $Sums + " = " + $hash)
exit 0

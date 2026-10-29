# round66_live_verify.ps1 -- 受控复现「环境等待封顶判死」，取 kind=pool_wait_capped 的行为面证据。
#
# 为什么入库（而不是留在 xtmp 里用完就丢）：本仓纪律「验收判据跟着代码走」。改造一那条
# 「判死必须具名」的判据在单测里是桩（recordRunner），而**真实引擎 + 真实 HTTP + 真实台账**
# 这一层只有一次性证据；不留一个可复跑的件，下次谁改动 requeueForPoolWait 就得重新摸索一遍。
# 另一条原因：报告 §7.3 写的复算命令若指向 xtmp（被 .gitignore 忽略）就是断链引用。
#
# 安全性（为什么可以放心跑）：
#   - 独立数据根 xtmp/round66-verify（workbench.exe 按所在目录解析布局），不碰 dist 与线上根；
#   - config 里 6 条车道**全部 enabled=false** ⇒ Router.Pick 必失败 ⇒ 必然走封顶判死；
#     全程**不发起任何模型调用**（零额度、零 DSH 子进程）；
#   - 自主供料/链式衍生/方案阶段/池同步全部关闭，端口 20912（避开 17890/17891）；
#   - 生产守护按 CommandLine 匹配 dist / agent 根，不会接管本实例 ⇒ **收尾必须自己做完**
#     （脚本末尾 --quit + 复核；实测遗留过一次，见报告 §6.8）。
#
# 用法: pwsh -NoProfile -File tests/round66_live_verify.ps1
# 退出码: 0=VERDICT_PASS / 1=判红(未具名或意外终态) / 2=环境不满足(取不到 token 或建不了任务)
# 前置: 先构建出与待验代码一致的二进制，并把路径改到 $exe（默认取 xtmp 的构建位）。
$ErrorActionPreference = "Continue"
$base = "D:/workdesktop/xtmp/round66-verify"
$exe = $base + "/workbench.exe"
$port = 20912

. "D:/workdesktop/tests/lib_silent_launch.ps1"
$lp = Start-WBInstance -Path $exe -Arguments "--headless"
Write-Host ("LAUNCHED_PID=" + $lp)
$tok = ""
$deadline = (Get-Date).AddSeconds(50)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 2
  try {
    $h = (Invoke-WebRequest -Uri ("http://127.0.0.1:" + $port + "/") -UseBasicParsing -TimeoutSec 4).Content
    $m = [regex]::Match($h, "__WB_TOKEN__\s*=\s*" + [string][char]34 + "([a-zA-Z0-9_-]+)")
    if ($m.Success) { $tok = $m.Groups[1].Value; break }
  } catch { }
}
Write-Host ("TOKEN_FOUND=" + [bool]$tok + " len=" + $tok.Length)
if (-not $tok) { Write-Host "RESULT=NO_TOKEN"; exit 2 }
Invoke-WebRequest -Uri ("http://127.0.0.1:" + $port + "/api/engine/start?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -UseBasicParsing -TimeoutSec 15 | Out-Null
Start-Sleep -Seconds 2
$r = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $port + "/api/tasks/manual?token=" + $tok) -Method POST -ContentType "application/json" -InFile ($base + "/task.json") -TimeoutSec 20
$taskId = $r.task.id
Write-Host ("TASK=" + $taskId + " state=" + $r.task.state)
if (-not $taskId) { Write-Host "RESULT=ENQUEUE_FAIL"; exit 2 }
$verdict = "TIMEOUT"
$seen = ""
$deadline = (Get-Date).AddSeconds(300)
while ((Get-Date) -lt $deadline) {
  Start-Sleep -Seconds 5
  try {
    $list = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $port + "/api/tasks?token=" + $tok) -TimeoutSec 10
    $me = $list.tasks | Where-Object { $_.id -eq $taskId } | Select-Object -First 1
    if (-not $me) { continue }
    $seen = "state=" + $me.state + " kind=" + $me.errorKind + " poolWaits=" + $me.poolWaits
    if ($me.state -eq "failed") {
      if ($me.errorKind -eq "pool_wait_capped") { $verdict = "PASS" } else { $verdict = "WRONG_KIND" }
      break
    }
    if ($me.state -eq "done" -or $me.state -eq "degraded" -or $me.state -eq "cancelled" -or $me.state -eq "rejected") { $verdict = "UNEXPECTED_" + $me.state; break }
  } catch { }
}
Write-Host ("VERDICT_" + $verdict + " | " + $seen)
try {
  $f = Invoke-RestMethod -Uri ("http://127.0.0.1:" + $port + "/api/failures?token=" + $tok) -TimeoutSec 10
  $bk = ($f.buckets | ForEach-Object { $_.kind + ":" + $_.count }) -join " "
  Write-Host ("FAILURES total=" + $f.total + " buckets=" + $bk)
} catch { Write-Host ("failures_read_fail " + $_.Exception.Message) }
$log = $base + "/workbench-output/logs/workbench.jsonl"
if (Test-Path $log) {
  $needle = "pool_wait_" + "capped"
  $hit = @(Get-Content $log | Where-Object { $_ -match $needle })
  Write-Host ("LOG_HITS=" + $hit.Count)
  $hit | Select-Object -Last 2 | ForEach-Object { Write-Host ("LOG " + $_.Substring(0, [Math]::Min(240, $_.Length))) }
} else { Write-Host "LOG_MISSING" }
try { Invoke-WebRequest -Uri ("http://127.0.0.1:" + $port + "/api/engine/stop?token=" + $tok) -Method POST -Body "{}" -ContentType "application/json" -UseBasicParsing -TimeoutSec 10 | Out-Null } catch { }
Start-Sleep -Seconds 2
$qq = Start-Process -FilePath $exe -ArgumentList "--quit" -WindowStyle Hidden -PassThru
Start-Sleep -Seconds 8
Write-Host "VERIFY_DONE"

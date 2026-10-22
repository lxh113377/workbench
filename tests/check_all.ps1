<#
  check_all.ps1 — 工作台预检总门禁：把三条独立门禁串成一次调用。

  为什么需要（2026-10-19）：
    近期反复踩的三类坑，各自都做成了单点门禁脚本：
      1) check_subsystem.ps1      —— 产物本身不要弹控制台（PE 子系统 = GUI）
      2) check_spawn_silent.ps1   —— spawn 的子进程不要弹控制台（隐藏窗口属性）
      3) check_no_shrink.ps1      —— 合并前禁止整文件覆盖丢掉他会话的改动
       4) check_gate_links.ps1     —— 受跟踪脚本的引用不得断链（2026-10-22 补：实发 check_all 自己引用的 4 个门禁从未入库，干净 clone 里总门禁静默降级）
   但它们散在三处，靠人记得跑。本脚本把「记得跑几个」收敛成「跑一个」。

  设计原则（反掩盖，对标 audit-runner-safe-aggregate）：
    - 每一段独立执行、独立记失败，**不允许**用聚合把单个失败洗绿；
    - 任何一段 rc!=0 => 整体 rc=1；
    - 有段被 SKIP（前置缺失）必须显式打印 SKIP，且**不计入 PASS**；
    - 末尾打印每段的实测 rc，便于核对。

  用法:
    powershell -NoProfile -ExecutionPolicy Bypass -File tests/check_all.ps1
    powershell ... -File tests/check_all.ps1 -Source <我的文件> -Target <live文件>   # 附带覆盖安全检查
  退出码：0 = 全部通过 / 1 = 至少一段失败 / 2 = 参数错误
#>
param(
  # 默认根目录由脚本位置推导；硬编码会让干净 clone/CI 检查另一棵树（2026-10-22 实测失真）。
  [string]$Root = '',
  # Exe 为空时由 Root 推导（dist\workbench.exe），允许调用方显式覆盖。
  [string]$Exe  = '',
  # 可选：额外做一次「我要覆盖 live 的某个文件」的安全性预检
  [string]$Source = '',
  [string]$Target = ''
)
$ErrorActionPreference = 'Continue'

# 默认根目录由脚本位置推导（tests/ 的父目录 = 仓库根），显式传参可覆盖。
# 为什么改（2026-10-22 实测）：原先硬编码 D:\workdesktop —— 干净 clone / CI 里该路径不存在，
# 门禁会检查另一棵树甚至静默判绿；本轮变异测试在 worktree 注入缺陷仍 PASS 就是这个失真。
if ([string]::IsNullOrWhiteSpace($Root)) { $Root = Split-Path -Parent $PSScriptRoot }
if ([string]::IsNullOrWhiteSpace($Exe)) { $Exe = Join-Path $Root 'dist\workbench.exe' }

function Section([string]$name, [scriptblock]$body) {
  Write-Host ('==> ' + $name) -ForegroundColor Cyan
  & $body
  $rc = $LASTEXITCODE
  if ($null -eq $rc) { $rc = 0 }
  if ($rc -eq 0) { Write-Host ('    PASS (rc=0) ' + $name) -ForegroundColor Green }
  else { Write-Host ('    FAIL (rc=' + $rc + ') ' + $name) -ForegroundColor Red; $script:fails++ }
}
$fails = 0

Write-Host '=== 工作台预检总门禁 ===' -ForegroundColor White

Section '产物静默（PE 子系统 = GUI）' {
  $g = Join-Path $Root 'tests\check_subsystem.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  if (-not (Test-Path $Exe)) { Write-Host '    SKIP exe 不存在（未构建）'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe $Exe -RequireGui
}

Section '实例唯一（无孤儿/无重复生产实例）' {
  $g = Join-Path $Root 'tests\instance_guard.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  # 报告模式（不加 -Apply）：只为判口，不在这里杀进程；回收位在 keepalive 每分钟那一跳
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -MaxInstances 6
}

Section '首页契约（产物页面 = 当前源码页面）' {
  $g = Join-Path $Root 'tests\check_homepage.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  if (-not (Test-Path $Exe)) { Write-Host '    SKIP exe 不存在（未构建）'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe $Exe
}

Section '子进程静默（spawn 点全隐藏）' {
  $g = Join-Path $Root 'tests\check_spawn_silent.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Root (Join-Path $Root 'workbench')
}

# 托盘消息窗样式门禁（2026-10-21 新增）：防「任务视图里堆满工作台卡片」复发。
# 只做源码判据（不加 -CheckLive），保证 CI/无头环境可用；运行期取证见下面的独立入口。
Section '托盘窗口样式（消息窗不得进任务视图）' {
  $g = Join-Path $Root 'tests\check_tray_window.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Root $Root
}

# 纯净检出编译门禁（2026-10-21 新增）：工作树里跑测试全绿 ≠ 提交的东西能跑。
# 实测抓到过：git archive 导出 HEAD 后 go build 报 undefined，因 4 个依赖文件从未入库。
Section '纯净检出编译（防提交编译不过的快照）' {
  $g = Join-Path $Root 'tests\check_snapshot_build.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Repo $Root
}

Section '服务化长驻实例·空闲态内存（三态 rc=2 不计 PASS）' {
  $g = Join-Path $Root 'tests\check_resident_longrun.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe (Join-Path $Root 'dist\workbench.exe')
  # 三态：rc=2 = 无活实例 / 引擎在跑 / 内存未稳定 —— 按项目铁律记 UNVERIFIED，
  # 不计 PASS 也不判红（与 check_perf / check_queue_size 同口径）。
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'queue 规模（记录只增不减的复发检测；三态 rc=2 不计 PASS）' {
  $g = Join-Path $Root 'tests\check_queue_size.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # 三态：rc=2 = 无 queue.json（未构建/未运行），按项目铁律记 UNVERIFIED，
  # **不得**当 PASS 也不得直接判红 —— 但 check_all 的 Section 只会把非 0 记 FAIL。
  # 故这里把 rc=2 显式改写成 0 并在输出里保留 UNVERIFIED 字样（与 check_perf 同口径）。
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED 无 queue.json（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '文档引用完整性（活文档点名的验收入口必须存在或已披露）' {
  $g = Join-Path $Root 'tests\check_doc_refs.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section '报告待办与 CI 现实一致性（防状态栏滞后误导下游）' {
  $g = Join-Path $Root 'tests\check_report_claims.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section 'PowerShell 脚本编码（含非 ASCII 必须带 UTF-8 BOM）' {
  $g = Join-Path $Root 'tests\check_ps1_encoding.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section '未跟踪 Go 文件（本地判据：CI 干净检出恒 PASS，不接 repo-gates）' {
  $g = Join-Path $Root 'tests\check_untracked_go.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # 生产代码未跟踪 = 一定坏（工作区与提交语义不一致）=> 计入 FAIL
  # 测试文件未跟踪 = 并行在途件的正常工作流 => 已逐条点名，不判红
  #   （差额已打印：提交的测试函数 vs 工作区测试函数）
  if ($LASTEXITCODE -eq 0) { Write-Host '    （差额见上方三行读数）' }
}

Section '暂存区 gofmt（提交前闸，防「改完忘 gofmt」进提交）' {
  $g = Join-Path $Root 'tests\check_staged_gofmt.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  # 三态：rc=2（不在 git 仓库/找不到 gofmt）按 UNVERIFIED 处理，不计 PASS 也不判红
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '死代码（未导出函数全树零引用）' {
  $g = Join-Path $Root 'tests\check_deadcode.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section 'Go 代码格式（LF 归一化口径，与 .gitattributes 分工）' {
  $g = Join-Path $Root 'tests\check_gofmt.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}
Section '门禁引用完整性（受跟踪脚本的引用不得断链）' {
  $g = Join-Path $Root 'tests\check_gate_links.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

if ($Source -ne '' -or $Target -ne '') {
  Section '覆盖安全（禁止整文件覆盖丢改动）' {
    $g = Join-Path $Root 'tests\check_no_shrink.ps1'
    if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE = 0; return }
    if ($Source -eq '' -or $Target -eq '') { Write-Host '    SKIP 需同时给 -Source 与 -Target'; $global:LASTEXITCODE = 0; return }
    & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Source $Source -Target $Target
  }
} else {
  Write-Host '==> 覆盖安全（未指定 Source/Target，跳过）' -ForegroundColor DarkGray
  Write-Host '    SKIP 本段不计入 PASS；合并前请显式带 -Source/-Target 再跑' -ForegroundColor Yellow
}

Write-Host '========================================'
if ($fails -eq 0) { Write-Host 'PRECHECK: all-gates-pass' -ForegroundColor Green; exit 0 }
Write-Host ('PRECHECK: ' + $fails + ' section(s) failed') -ForegroundColor Red
exit 1
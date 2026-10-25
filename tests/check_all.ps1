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
  退出码（2026-10-24 round 63 起，段级三态）：
    0 = 全部段 PASS；1 = 至少一段 FAIL；2 = 参数错误
    段级判定三态：PASS (rc=0) / FAIL (rc≠0,3) / SKIP (rc=3，门禁未执行，不计 PASS 也不判红)
    段内还可能有 UNVERIFIED（门禁跑了但证据不足，rc=2 由各段自行降级并打印）
    SKIP≠PASS：原先 SKIP 分支设 LASTEXITCODE=0 被 Section 判成 PASS（round 61 实测），已修。
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

# SKIP 分支用 rc=3（约定常量，见头部「退出码口径」）返回，Section 据此区分三态。
function Section([string]$name, [scriptblock]$body) {
  Write-Host ('==> ' + $name) -ForegroundColor Cyan
  & $body
  $rc = $LASTEXITCODE
  if ($null -eq $rc) { $rc = 0 }
  if ($rc -eq 0) { Write-Host ('    PASS (rc=0) ' + $name) -ForegroundColor Green; return }
  # 三态（2026-10-24 round 63 实测修复）：原先 SKIP 分支设 $global:LASTEXITCODE = 0，
  # 函数读完照样判 PASS —— 门禁**根本没跑却报通过**（round 61 实测：-Exe 指向不存在的路径时，
  # 「SKIP exe 不存在（未构建）」下一行就是「PASS (rc=0) 产物静默」）。这是聚合洗绿的变体：
  # 与「聚合把单个失败洗绿」不同，这里是**把没执行洗成执行成功**，危害更大（没人会怀疑）。
  # 修复只升 SKIP 一态：rc=3 显示 SKIP 且不计 PASS 也不判红（跑不了 != 失败）。
  # **刻意不把 rc=2(UNVERIFIED) 升成 FAIL**：28 个被引门禁里 26 个会返回 rc=2，
  # 而在途文件/无产物/无活实例都会触发它 —— 判红会让本地总门禁天天红，然后被整体绕过，
  # 那才是真的洗绿。rc=2 仍由各段自己的「UNVERIFIED（不计 PASS）」分支处理（保持原状）。
  if ($rc -eq 3) { Write-Host ('    SKIP (rc=3) ' + $name + ' —— 门禁未执行，不计 PASS 也不判红') -ForegroundColor Yellow; $script:skips++; return }
  Write-Host ('    FAIL (rc=' + $rc + ') ' + $name) -ForegroundColor Red; $script:fails++
}
$fails = 0
$skips = 0

Write-Host '=== 工作台预检总门禁 ===' -ForegroundColor White

Section '产物静默（PE 子系统 = GUI）' {
  $g = Join-Path $Root 'tests\check_subsystem.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  if (-not (Test-Path $Exe)) { Write-Host '    SKIP exe 不存在（未构建）'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe $Exe -RequireGui
}

Section '实例唯一（无孤儿/无重复生产实例）' {
  $g = Join-Path $Root 'tests\instance_guard.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  # 报告模式（不加 -Apply）：只为判口，不在这里杀进程；回收位在 keepalive 每分钟那一跳
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -MaxInstances 6
}

Section '首页契约（产物页面 = 当前源码页面）' {
  $g = Join-Path $Root 'tests\check_homepage.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  if (-not (Test-Path $Exe)) { Write-Host '    SKIP exe 不存在（未构建）'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe $Exe
}

Section '子进程静默（spawn 点全隐藏）' {
  $g = Join-Path $Root 'tests\check_spawn_silent.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Root (Join-Path $Root 'workbench')
}

# 托盘消息窗样式门禁（2026-10-21 新增）：防「任务视图里堆满工作台卡片」复发。
# 只做源码判据（不加 -CheckLive），保证 CI/无头环境可用；运行期取证见下面的独立入口。
Section '托盘窗口样式（消息窗不得进任务视图）' {
  $g = Join-Path $Root 'tests\check_tray_window.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Root $Root
}

# 纯净检出编译门禁（2026-10-21 新增）：工作树里跑测试全绿 ≠ 提交的东西能跑。
# 实测抓到过：git archive 导出 HEAD 后 go build 报 undefined，因 4 个依赖文件从未入库。
Section '纯净检出编译（防提交编译不过的快照）' {
  $g = Join-Path $Root 'tests\check_snapshot_build.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Repo $Root
}

Section '服务化长驻实例·空闲态内存（三态 rc=2 不计 PASS）' {
  $g = Join-Path $Root 'tests\check_resident_longrun.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe (Join-Path $Root 'dist\workbench.exe')
  # 三态：rc=2 = 无活实例 / 引擎在跑 / 内存未稳定 —— 按项目铁律记 UNVERIFIED，
  # 不计 PASS 也不判红（与 check_perf / check_queue_size 同口径）。
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'queue 规模（记录只增不减的复发检测；三态 rc=2 不计 PASS）' {
  $g = Join-Path $Root 'tests\check_queue_size.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # 三态：rc=2 = 无 queue.json（未构建/未运行），按项目铁律记 UNVERIFIED，
  # **不得**当 PASS 也不得直接判红 —— 但 check_all 的 Section 只会把非 0 记 FAIL。
  # 故这里把 rc=2 显式改写成 0 并在输出里保留 UNVERIFIED 字样（与 check_perf 同口径）。
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED 无 queue.json（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '文档引用完整性（活文档点名的验收入口必须存在或已披露）' {
  $g = Join-Path $Root 'tests\check_doc_refs.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section '报告待办与 CI 现实一致性（防状态栏滞后误导下游）' {
  $g = Join-Path $Root 'tests\check_report_claims.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

# 以下 4 段是「本地补齐 CI 侧判据」（2026-10-25 round 152）：实测这 4 个门禁此前**只挂 CI**，
# 本地 check_all 从不跑它们 ⇒ 本地全绿后推送仍可能被 CI 判红（本地绿 / CI 红 陷阱）。
# 它们本身与暂存区/在途文件无关，属本地可判，故补进本套件。
# 三态口径与 check_perf / check_queue_size 一致：rc=2 = 没测成，改写成 0 并保留 UNVERIFIED 字样。
Section 'CHANGELOG 结构（锚点与条目形态；本地补齐 CI 侧判据）' {
  $g = Join-Path $Root 'tests\check_changelog.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED 取不到 CHANGELOG（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'issue 模板 config（表单开关与必填项；本地补齐 CI 侧判据）' {
  $g = Join-Path $Root 'tests\check_issue_forms.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED 取不到 ISSUE_TEMPLATE（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '非交互约束（不得要求人工交互；本地补齐 CI 侧判据）' {
  $g = Join-Path $Root 'tests\check_noninteractive.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED 取不到被检文件（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '性能基线（本地补齐 CI 侧判据；无产物时 SKIP 而非判红）' {
  $g = Join-Path $Root 'tests\check_perf.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  if (-not (Test-Path $Exe)) { Write-Host ('    SKIP 产物不存在（未构建）: ' + $Exe); $global:LASTEXITCODE = 3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Exe $Exe
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'PowerShell 脚本编码（含非 ASCII 必须带 UTF-8 BOM）' {
  $g = Join-Path $Root 'tests\check_ps1_encoding.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section '门禁读数趋势（informational：有数据就报，无数据 UNVERIFIED）' {
  $g = Join-Path $Root 'tests/check_readings_trend.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # 本件只报趋势、不设红线（rc 只会是 0 或 2）；rc=2 = 还没有读数落盘，按 UNVERIFIED 处理
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（尚无读数落盘）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '未跟踪 Go 文件（本地判据：CI 干净检出恒 PASS，不接 repo-gates）' {
  $g = Join-Path $Root 'tests\check_untracked_go.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # 生产代码未跟踪 = 一定坏（工作区与提交语义不一致）=> 计入 FAIL
  # 测试文件未跟踪 = 并行在途件的正常工作流 => 已逐条点名，不判红
  #   （差额已打印：提交的测试函数 vs 工作区测试函数）
  if ($LASTEXITCODE -eq 0) { Write-Host '    （差额见上方三行读数）' }
}

Section '注释时效（现状型数字断言必须带日期，round 31/32 同型教训）' {
  $g = Join-Path $Root 'tests\check_comment_staleness.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '.ps1 语法可解析（字节层之上的第三层：能解析才能跑）' {
  $g = Join-Path $Root 'tests\check_ps1_syntax.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '多 BOM（受跟踪文本文件不得有多个前导 BOM）' {
  $g = Join-Path $Root 'tests\check_multi_bom.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'gitignore 泄漏（可再生产副产物不得漏出 ignore）' {
  $g = Join-Path $Root 'tests\check_gitignore_leak.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '内存口径隔离（RSS 与 Go heap 不得混进同一比较）' {

  $g = Join-Path $Root 'tests\check_caliber_isolation.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '报告数字一致性（体检报告的规模数字必须与仓库实况相符）' {

  $g = Join-Path $Root 'tests\check_report_figures.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '纯标准库约束（go.mod 无 require、无 go.sum/vendor、模块内无外部 import）' {

  $g = Join-Path $Root 'tests\check_stdlib_only.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'soak JSON 契约（验收断言必须落在 soakReport 的真实字段上）' {

  $g = Join-Path $Root 'tests\check_soak_json_contract.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '产物时效（soak/A7 判定逻辑必须在产物里；无产物则 UNVERIFIED）' {

  $g = Join-Path $Root 'tests\check_artifact_soak_fresh.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '受理面覆盖（每个 check_*.ps1 必须挂某个受理面，防判据没挂受理面）' {

  $g = Join-Path $Root 'tests\check_gate_coverage.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '全仓入库 EOL（已入库的 CRLF/混合残留，CI 兜底层）' {


  $g = Join-Path $Root 'tests\check_repo_eol.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '.ps1 入库 EOL（防 CRLF 混入把 diff 放大成全行改动）' {
  $g = Join-Path $Root 'tests\check_ps1_eol.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'git hooks 启用状态（提交前闸是否真装着）' {
  $g = Join-Path $Root 'tests\check_hooks_enabled.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  # rc=2 = 不在 git 仓库（CI 干净检出本就不装 hook）=> UNVERIFIED，不计 PASS 不判红
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '暂存区 gofmt（提交前闸，防「改完忘 gofmt」进提交）' {
  $g = Join-Path $Root 'tests\check_staged_gofmt.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  # 三态：rc=2（不在 git 仓库/找不到 gofmt）按 UNVERIFIED 处理，不计 PASS 也不判红
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '暂存区 BOM（即将入库的文件不得有多重 BOM）' {
  $g = Join-Path $Root 'tests\check_staged_bom.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'CSS 颜色令牌（rgb 通道不得漂移、:root 外不得绕过）' {
  $g = Join-Path $Root 'tests\check_css_rgb_pair.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'CSS 死令牌（设计令牌不得定义了却没人用）' {


  $g = Join-Path $Root 'tests\check_dead_css_tokens.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '忽略错误（同一操作的两半不得区别对待）' {

  $g = Join-Path $Root 'tests\check_ignored_error.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section 'skip 披露（t.Skip 必须带理由；总数可见）' {

  $g = Join-Path $Root 'tests\check_skip_disclosure.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '测试必须有断言（零断言测试 = 假绿）' {

  $g = Join-Path $Root 'tests\check_test_assertions.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
  if ($LASTEXITCODE -eq 2) { Write-Host '    UNVERIFIED（不计 PASS）' -ForegroundColor Yellow; $global:LASTEXITCODE = 0 }
}

Section '死代码（未导出函数全树零引用）' {
  $g = Join-Path $Root 'tests\check_deadcode.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

Section 'Go 代码格式（LF 归一化口径，与 .gitattributes 分工）' {
  $g = Join-Path $Root 'tests\check_gofmt.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}
Section '门禁引用完整性（受跟踪脚本的引用不得断链）' {
  $g = Join-Path $Root 'tests\check_gate_links.ps1'
  if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
  & powershell -NoProfile -ExecutionPolicy Bypass -File $g
}

if ($Source -ne '' -or $Target -ne '') {
  Section '覆盖安全（禁止整文件覆盖丢改动）' {
    $g = Join-Path $Root 'tests\check_no_shrink.ps1'
    if (-not (Test-Path $g)) { Write-Host '    SKIP 门禁脚本不存在'; $global:LASTEXITCODE =  3; return }
    if ($Source -eq '' -or $Target -eq '') { Write-Host '    SKIP 需同时给 -Source 与 -Target'; $global:LASTEXITCODE =  3; return }
    & powershell -NoProfile -ExecutionPolicy Bypass -File $g -Source $Source -Target $Target
  }
} else {
  Write-Host '==> 覆盖安全（未指定 Source/Target，跳过）' -ForegroundColor DarkGray
  Write-Host '    SKIP 本段不计入 PASS；合并前请显式带 -Source/-Target 再跑' -ForegroundColor Yellow
}

Write-Host '========================================'
# 汇总措辞（round 63）：有 SKIP 时不许再说 all-gates-pass —— 「全过」这个词必须名实相符，
# 否则又回到 round 61 那个缺陷的语言版（门禁没跑却报通过）。SKIP 只说明「跑不了」，
# 不判红，但要让看结果的人一眼知道有几段没验。
if ($fails -gt 0) {
  Write-Host ('PRECHECK: ' + $fails + ' section(s) failed' + $(if ($skips -gt 0) { '（另有 ' + $skips + ' 段 SKIP 未执行）' } else { '' })) -ForegroundColor Red
  exit 1
}
if ($skips -gt 0) {
  Write-Host ('PRECHECK: pass-with-skips (' + $skips + ' 段未执行，不计 PASS)') -ForegroundColor Yellow
  exit 0
}
Write-Host 'PRECHECK: all-gates-pass' -ForegroundColor Green
exit 0
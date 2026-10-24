# check_snapshot_build.ps1 -- 纯净检出编译门禁（防「提交了编译不过的快照」）
#
# 为什么存在（2026-10-21 实测抓到自己的问题）：
#   提交 tray 加固时，工作树里 go test 全绿 —— 但那只证明**工作树**能跑，
#   不证明**提交进去的东西**能跑。实测：git archive 导出 HEAD 快照后
#   go vet ./... 立刻报 undefined: tryRaiseIntegrity / hiddenCmd，
#   因为 tray_windows.go / executor.go 依赖的 4 个文件从未入库
#   （git log --all 实测 NEVER TRACKED）。
#
# 判据：把 HEAD 导出到临时目录，从零编译。任何 undefined/缺失 = FAIL。
# 与「工作树里跑测试」互为补充：后者可能被未提交文件喂饱而假绿。
#
# 退出码: 0=快照可编译 / 1=快照编译失败 / 2=环境缺工具

param(
  # 默认由脚本位置推导（tests/ 的父目录），不硬编码 —— 否则 CI 里指向不存在的 D:\workdesktop。
  [string]$Repo = "",
  # 快照里 Go 模块根（相对快照根）
  [string]$ModuleDir = "workbench",
  # 超时（秒）
  [int]$TimeoutSec = 300
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# 默认根目录由脚本位置推导（tests/ 的父目录 = 仓库根），显式传参可覆盖。
# 为什么改（2026-10-22 实测）：原先硬编码 D:\workdesktop —— 干净 clone / CI 里该路径不存在，
# 门禁会检查另一棵树甚至静默判绿；本轮变异测试在 worktree 注入缺陷仍 PASS 就是这个失真。
if ([string]::IsNullOrWhiteSpace($Repo)) { $Repo = Split-Path -Parent $PSScriptRoot }

$gitDir = Join-Path $Repo ".git"
if (-not (Test-Path -LiteralPath $gitDir)) {
  Write-Host "FAIL 不是 git 仓库: $Repo" -ForegroundColor Red
  exit 1
}

# 唯一名（2026-10-25 实测修假红）：原命名只到秒，同秒两次调用得到**完全相同**的目录名
# （实测 wb-snapshot-033919 两次相等=True）；本仓是多会话同机并发，两个门禁的 finally
# 会 Remove-Item -Recurse 互删对方的快照树，正在编译的那个 go build 随即报缺失而判红 ——
# 红的是门禁自身的竞态，不是提交内容。改用与 e2e_soak/selfheal 等既有门禁同一 GUID 惯例。
$tmp = Join-Path $env:TEMP ("wb-snapshot-" + [guid]::NewGuid().ToString("N").Substring(0,8))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null

Write-Host "=== 纯净检出编译门禁 ===" -ForegroundColor White
Write-Host ("  仓库: " + $Repo)

try {
  # 保留 archive 只为「快照大小」这一行读数（体积异常是有效信号）；解包不用它。
  $tar = Join-Path $tmp "snap.tar"
  & git --git-dir="$gitDir" archive --format=tar -o "$tar" HEAD
  if ($LASTEXITCODE -ne 0) { Write-Host "FAIL git archive 失败" -ForegroundColor Red; exit 1 }
  Write-Host ("  快照大小: " + [math]::Round((Get-Item $tar).Length / 1MB, 2) + " MB")

  $snapRoot = Join-Path $tmp "snap"
  New-Item -ItemType Directory -Force -Path $snapRoot | Out-Null
  #
  # 解包方式（2026-10-22 CI 实测改）：原先用 `tar -xf` —— 在本机正常，但 CI runner 上
  # tar.exe(bsdtar) 按系统代码页（英文 Windows = 437）解码 UTF-8 文件名，中文名落盘成乱码。
  # 实测证据：CI 上 repoRoot() 正确返回 snap 路径，却 `open ...\snap\docs\自主运行方案.md:
  # The system cannot find the file specified` —— 文件不是没提交，是解出来名字坏了。
  # 该门禁的全部价值是「快照里到底有什么」，若解包本身失真则判据不可信。
  # 改用 git 自己的 checkout 索引（按字节处理文件名，与平台代码页无关）：
  #   git --work-tree=<dest> checkout-index -a -f
  # 语义等价于 archive（都取 HEAD 内容），但不经 tar.exe 的编码层。
  & git --git-dir="$gitDir" --work-tree="$snapRoot" checkout-index -a -f
  if ($LASTEXITCODE -ne 0) {
    Write-Host "FAIL 快照展开失败（git checkout-index）" -ForegroundColor Red
    exit 1
  }

  $modRoot = Join-Path $snapRoot $ModuleDir
  if (-not (Test-Path -LiteralPath $modRoot)) {
    Write-Host ("FAIL 快照里找不到模块根: " + $ModuleDir) -ForegroundColor Red
    exit 1
  }

  Push-Location $modRoot
  try {
    # 注意：$ErrorActionPreference="Stop" 下，go 写 stderr 会被当 NativeCommandError 抛出，
    # 2>&1 | Out-String 抓不到（2026-10-21 实测踩到）。必须临时改 Continue 才读得到编译器错误。
    $eapBackup = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $out = & go build ./... 2>&1 | Out-String
    $rc = $LASTEXITCODE
    $ErrorActionPreference = $eapBackup
    if ($out.Trim()) { Write-Host $out }
    if ($rc -ne 0) {
      Write-Host "FAIL 纯净检出编译失败 —— 提交了编译不过的快照（上面即为缺失符号）" -ForegroundColor Red
      Write-Host "  常见原因：tracked 文件引用了从未入库的符号/文件。" -ForegroundColor Yellow
      Write-Host "  自查: git log --all -- <缺失文件>  (空 = NEVER TRACKED)" -ForegroundColor Yellow
      exit 1
    }
    Write-Host "PASS 纯净检出 go build ./... 成功" -ForegroundColor Green

    # 有测试就再跑一轮（快照里的测试才是提交真相）
    #
    # WB_REPO_ROOT（2026-10-22 CI 实测补）：部分测试用 repoRoot() 靠「目录探测」找仓库根
    # （要求 docs/自主运行方案.md + workbench/internal/web/html.go 并存）。探测在本机可靠，
    # 但在 CI runner 上实测返回空串 —— 环境（locale/代码页/临时目录形态）差异所致，
    # 本机用 8.3 短名、纯 ASCII TEMP、PS5.1 解包等多种形态均无法复现。
    # 既然本门禁**已知**快照根在哪，就不该让被测代码去猜：显式注入 repoRoot() 自己支持的
    # WB_REPO_ROOT 覆盖位。这不是「改测试让它过」—— 探测逻辑本身的正确性由工作树里的
    # TestRepoRootHasRequiredAnchors 继续守护；这里只是把快照形态下的已知事实告知被测代码。
    $env:WB_REPO_ROOT = $snapRoot
    # 已知 flaky 登记（2026-10-24 round 82 实测，一次性登记、修好后清零）：
    #   internal/verify 的 TestPromptingCommandDoesNotHang 真的会启动 powershell -File +
    #   Invoke-WebRequest 抓一个含 <script> 的本地页面；Windows PowerShell 5.1 默认用 IE 引擎解析，
    #   命中 script 时弹「Security Warning: Script Execution Risk」并**挂起等人工按键** ——
    #   该测试的设计意图就是抓这个回归（源码注释：「被跳过而非执行 = 回归没测到真路径」），
    #   所以不能 skip。但在 CI runner 上弹窗行为依赖桌面 session 状态，实测 10 次里偶发 1 次超时（36s）。
    #   处置：flaky 的特征是「偶发」，与真缺陷的区别是**重试即过**。故失败后重试一次，
    #   并把重试事实打进输出（不静默）。若两次都失败才是真缺陷，照常判红。
    #   这不是「改测试让它过」：测试本身一行未动；也不放过真缺陷（连挂两次必红）。
    $ErrorActionPreference = "Continue"
    # -timeout 接线（2026-10-25）：$TimeoutSec 原先声明了却全仓无人使用（只在 param 出现一次），
    # 属「文档承诺比实现新」的第三种形态。接进 go test 的 -timeout：它约束测试二进制的**执行时长**
    # （不含编译），实测全量执行 70.7s 远小于默认 300s，故不引入新假红；换来的是「卡死挂钟」变成
    # 有栈输出的超时 —— PS5.1 抓不住挂起子进程，等 check_all 整段跑完才发现代价更高。
    # 下方 flaky 重试同样接上，保证「重试一次」不会变成「再挂一次」。
    $tout = & go test ./... -count=1 -timeout ("{0}s" -f $TimeoutSec) 2>&1 | Out-String
    $trc = $LASTEXITCODE
    $ErrorActionPreference = $eapBackup
    if ($trc -ne 0) {
      $flakyHit = ($tout -match 'TestPromptingCommandDoesNotHang')
      if ($flakyHit) {
        Write-Host "NOTE 纯净检出 go test 首轮失败，命中已知 flaky（TestPromptingCommandDoesNotHang，PS5.1+IE 弹窗挂起）—— 重试一次以区分偶发与真缺陷" -ForegroundColor Yellow
        Write-Host $tout
        $ErrorActionPreference = "Continue"
        $tout = & go test ./... -count=1 -timeout ("{0}s" -f $TimeoutSec) 2>&1 | Out-String
        $trc = $LASTEXITCODE
        $ErrorActionPreference = $eapBackup
        if ($trc -eq 0) {
          Write-Host "PASS 纯净检出 go test 成功（第二次；首轮为已知 flaky，非真缺陷）" -ForegroundColor Green
        }
      }
    }
    if ($trc -ne 0) {
      Write-Host "FAIL 纯净检出 go test 失败" -ForegroundColor Red
      if ($tout.Trim()) { Write-Host $tout }
      exit 1
    }
    Write-Host "PASS 纯净检出 go test ./... 全绿" -ForegroundColor Green
  } finally {
    Pop-Location
  }

  Write-Host "----------------------------------------"
  Write-Host "CHECK snapshotBuild result=PASS" -ForegroundColor Green
  exit 0
} finally {
  try { Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue } catch {}
}

# build.ps1 — 一键构建「个人 AI 工作台」单 EXE
# 用法: powershell -ExecutionPolicy Bypass -File setup\build.ps1 [-Version 1.0.0]
param(
  [string]$Version = "1.0.0",
  [switch]$SkipTests,
  [switch]$SkipCoverGate,
  [double]$CoverThreshold = 55,
  # PerPackageCover：按"包:阈值"覆盖个别包的基线。
  #
  # **2026-10-23 重校两次（第二次是必须的，见下）**：
  # 方法：go test ./... -count=1 -cover 连跑两次，读数完全一致（覆盖率本身稳定）。
  # 新阈值 = 实测 - 3pp 余量。
  #
  # ⚠️ **阈值必须按 CI 环境定，不能按本机**（本轮实测教训）：
  # 首次按**本机**读数设定后，CI 直接判红 —— 同一份代码、同一条命令，CI 覆盖率系统性低 10~20pp：
  #   single 71.4(CI) vs 89.2(本机) / web 37.7 vs 57.4 / executor 42.7 vs 54.3
  #   supervisor 51.7 vs 68.0 / retention 61.5 vs 77.6
  # 原因：CI 无桌面会话、无网络、无真实 DSH 安装，部分测试走不同分支（或被环境跳过），
  # 覆盖率天然更低。**门禁在 CI 跑，所以基准必须是 CI**。
  #
  # 复核前的问题：14 个阈值里 9 个比实测低 15pp 以上（single 16 vs 89.2、dshprofile 2.5 vs 79.5），
  # 这些包损失 20%+ 覆盖门禁都不会报 —— 护栏形同虚设。
  # 最终对照（CI 实测 -> 新阈值）：
  #   engine 68.3->65 / web 37.7->34 / executor 42.7->39 / modelpool 58.5->55 / supervisor 51.7->48
  #   cmd/workbench 5.7->2 / evaluate 63.8->60 / modelroute 50.6->47 / shortcut 28.9->25
  #   chain 88.6->85 / judge 81.1->78 / single 71.4->68 / tray 见下 / dshprofile 77.8->74
  # tray 双环境实测：CI 26.3% / 本机 21.3%（**本机更低**——该包断言依赖 Explorer 桌面会话），
  #   故阈值必须取下界：21.3 - 3 = 18。这是「两个环境都要过」的取值规则。
  #
  # tray 说明（2026-10-21 立、2026-10-23 两次复核更新）：守护测试全是「常量/契约」型断言
  # （WS_EX_TOOLWINDOW 必须 0x80、类名不漂移、标题自述内部窗、结构体尺寸、完整性级别），
  # 守「消息窗不得进任务视图」这条 2026-10-20 现场缺陷的根因。真正创建窗口的 Run()
  # 只能在有 Explorer 的真机跑，单测断言只会假绿，故上限本就不高。CI 实测 26.3%，阈值设 23。
  #
  # 维护规则：只许抬不许降；若某包实测跌破阈值，先补测试再谈降阈值（降阈值必须写清理由）。
# browser 阈值说明（2026-10-25 round 168 立）：该包**此前 0%**（无测试文件 ⇒ 不进门禁分母）。
# 本轮修「无头判定三份实现漂移」时给它补了首批测试（口径：WB_HEADLESS 取值与 CI 变量），
# 实测 25.8% —— 未覆盖的部分是 Open/findEdge/fallback 的真实进程启动路径：
# 起 Edge --app 或 rundll32 会真弹窗口/真起进程，单测里跑它们等于污染桌面，
# 正是本包要在测试里守的纪律（见 headless_test.go 的 TestOpenDoesNothingWhenHeadless）。
# 故按本仓「实测-3pp」惯例设 22，后续只许抬不许降。
  [string[]]$PerPackageCover = @("workbench/internal/engine=65", "workbench/internal/web=34", "workbench/internal/executor=39", "workbench/internal/modelpool=55", "workbench/internal/supervisor=48", "workbench/cmd/workbench=2", "workbench/internal/evaluate=60", "workbench/internal/modelroute=47", "workbench/internal/shortcut=25", "workbench/internal/chain=85", "workbench/internal/judge=78", "workbench/internal/single=68", "workbench/internal/tray=18", "workbench/internal/dshprofile=74", "workbench/internal/browser=22")

# dshprofile 阈值说明（2026-10-21 立、2026-10-23 复核更新）：该包原被判「只能真机跑」
# （LocateDSH/resolveFromShim 探测、copyDir/linkOFM 复制、hiddenProcAttr 全是 IO 型）。
# **该判断已过时**：后来补了真实测试（env override + 临时目录），CI 实测 77.8%、
# 13 个测试 0 个 t.Skip。阈值按「实测-3pp」设为 74。
# 教训：包的能力描述会随测试补齐而失效，阈值必须定期按实测重校，不能一次设定管到底。
)
$ErrorActionPreference = "Stop"

# 分段时间（2026-10-25 round 166，纯观测、不改行为）：构建链此前只报总时长，
# 无法归因到段。加这一段的起因：实测 go vet 1.2s / go test -count=1 39.8s / go build 3s，
# 三者相加远小于整链 97.7s —— 说明大头在我没量的地方（覆盖插桩、真探针启停、各判据）。
$chainStart = Get-Date
$script:stepT0 = Get-Date
$script:stepName = "（链起点）"
function Mark([string]$next) {
  $now = Get-Date
  $el = [math]::Round(($now - $script:stepT0).TotalSeconds, 1)
  Write-Host ("    [计时] " + $script:stepName + " = " + $el + " s") -ForegroundColor DarkGray
  $script:stepT0 = $now
  $script:stepName = $next
}
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"
$dist = Join-Path $root "dist"
$build = Join-Path $wb "build"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Mark "1/6 图标"
Write-Host "==> 1/6 生成图标" -ForegroundColor Cyan
$makeIcon = Join-Path $PSScriptRoot "make-icon.ps1"
if (Test-Path $makeIcon) {
  New-Item -ItemType Directory -Force -Path $build | Out-Null
  & $makeIcon -Out (Join-Path $build "app.ico") | Out-Null
} else {
  Write-Warning "    make-icon.ps1 不存在，跳过图标生成（EXE 仍可用）"
}

Mark "2/6 资源（无 app.rc 时几乎为 0）"
Write-Host "==> 2/6 编译资源（图标 + 版本信息）" -ForegroundColor Cyan
# 缺件分级（2026-10-21 实测修复）：app.rc / make-icon.ps1 在本机不存在且从未入库
# （git ls-files 零命中），而 $ErrorActionPreference="Stop" 会让 cc1 报
# "app.rc: No such file" 直接把全链构建炸在 2/6 —— 之后所有版本都只能走手工通道。
# 现在的判据落在**资源面本身**，不是「工具在不在」：
#   · app.rc 在  -> 照常 windres 编译并覆盖 syso（版本信息随 -Version 更新）；
#   · app.rc 不在 -> 校验 git 跟踪的静态 syso 在盘且带 VS_VERSION_INFO
#     （实测 12116 B / Unicode 串含版本号）=> 声明降级「图标与 FileVersion 用静态件」，
#     不炸链；syso 也不在 => 判红（那才是真的缺资源）。
$rcPath = Join-Path $build "app.rc"
$sysoDst = Join-Path $wb "cmd\workbench\rsrc_windows_amd64.syso"
if (Test-Path $rcPath) {
  $windres = Get-ChildItem "C:\Users\$env:USERNAME\AppData\Local\Microsoft\WinGet\Packages\BrechtSanders.WinLibs*" -Recurse -Filter windres.exe -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($windres) {
    Push-Location $build
    & $windres.FullName -i app.rc -o rsrc_windows_amd64.syso -O coff
    Pop-Location
    Copy-Item (Join-Path $build "rsrc_windows_amd64.syso") $sysoDst -Force
    Write-Host "    资源编译完成（图标 + 版本信息随 -Version 更新）"
  } else {
    Write-Warning "    未找到 windres，跳过资源编译（EXE 仍可用，版本信息为上次静态值）"
  }
} else {
  if (Test-Path $sysoDst) {
    $sysoBytes = [IO.File]::ReadAllBytes($sysoDst)
    $sysoText = [Text.Encoding]::Unicode.GetString($sysoBytes)
    if ($sysoText -notmatch 'VS_VERSION_INFO') {
      throw "静态 syso 存在但不含 VS_VERSION_INFO（资源件损坏，需重新提供 app.rc 或恢复 syso）"
    }
    Write-Host ("    app.rc 缺件 => 用 git 跟踪的静态 syso（" + $sysoBytes.Length + " B，含版本信息）；图标与 FileVersion 保持静态值")
  } else {
    throw "app.rc 与静态 syso 都不在：产物将没有图标与版本信息（把 app.rc 入库或恢复 cmd\workbench\rsrc_windows_amd64.syso）"
  }
}

Mark "3/6 vet+gofmt"
Write-Host "==> 3/6 代码检查（go vet + gofmt）" -ForegroundColor Cyan
# gofmt 门禁（2026-10-22 补）：本仓长期没有格式门禁，根因是 core.autocrlf 下工作副本全 CRLF、
# 直接 gofmt -l 会满屏假红（实测 73/286 个文件），谁都只能撤掉。
# check_gofmt.ps1 的口径是「LF 归一化后判代码格式」，行尾由 .gitattributes(*.go eol=lf) 负责，
# 两件事分开判，互不掩盖；实测首跑点名 55 个真漂移（全部纯空白，go build/vet/test 零变化）。
& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path (Split-Path $PSScriptRoot -Parent) "tests\check_gofmt.ps1")
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "gofmt 门禁未通过（代码格式漂移）" }
Push-Location $wb
& go vet ./...
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "go vet 失败" }

if (-not $SkipTests) {
Mark "4/6 test+覆盖率"
  Write-Host "==> 4/6 运行测试 + 覆盖率门禁" -ForegroundColor Cyan
  $coverOut = (& go test ./... -count=1 -cover 2>&1 | Out-String)
  $testRc = $LASTEXITCODE
  Write-Host $coverOut
  if ($testRc -ne 0) { Pop-Location; throw "测试失败" }

  # 覆盖率门禁（对标 river/asynq/temporal 的 CI 保护）：
  # 只约束"已有测试"的包，未写测试的包不计入分母，避免为了凑数补空测试。
  # 阈值可用 -CoverThreshold 覆盖，个别包用 -PerPackageCover 单独设线，-SkipCoverGate 可临时跳过。
  if (-not $SkipCoverGate) {
    $gateFailed = @()
    $testedCount = 0
    foreach ($line in ($coverOut -split "`n")) {
      if ($line -match '^(?:ok|FAIL)\s+(\S+)\s+.*coverage:\s+([0-9.]+)%') {
        $pkg = $Matches[1]
        $cov = [double]$Matches[2]
        $testedCount++
        $threshold = $CoverThreshold
        foreach ($entry in $PerPackageCover) {
          $kv = $entry -split "=", 2
          if ($kv.Count -eq 2 -and $kv[0] -eq $pkg) { $threshold = [double]$kv[1] }
        }
        if ($cov -lt $threshold) { $gateFailed += ($pkg + "=" + $cov + "%<阈值" + $threshold + "%") }
      }
    }
    if ($testedCount -eq 0) { Pop-Location; throw "覆盖率门禁：未解析到任何带覆盖率的包，判据失效" }
    if ($gateFailed.Count -gt 0) {
      Pop-Location
      throw ("覆盖率门禁未通过（阈值 " + $CoverThreshold + "%）：" + ($gateFailed -join ", "))
    }
    Write-Host ("    覆盖率门禁通过：" + $testedCount + " 个有测试的包均达标") -ForegroundColor Green

    # ---------- 覆盖率读数序列化（2026-10-24 新增）----------
    # 为什么：覆盖率是**最容易静默漂移**的指标 —— round 22 实测发现 14 个阈值里 9 个
    # 已低于实测 15pp 以上（护栏失效），而那是因为没人持续记录读数、没人发现趋势。
    # 单次读数不够（本轮教训：本机与 CI 覆盖率系统性差 10~20pp，且 tray 本机反而更低），
    # 序列读数才能看出「在涨还是在跌」。落盘到 .workbuddy/memory/gate-readings.jsonl，
    # 与 check_untracked_go.ps1 的读数同一文件、同一 schema 风格（gate 字段区分来源）。
    $readingsDir = Join-Path $root '.workbuddy\memory'
    if (Test-Path -LiteralPath $readingsDir) {
      $per = @{}
      foreach ($line in ($coverOut -split "`n")) {
        $mm = [regex]::Match($line, '^ok\s+(\S+)\s+[0-9.]+s\s+coverage:\s+([0-9.]+)%')
        if ($mm.Success) { $per[$mm.Groups[1].Value] = [double]$mm.Groups[2].Value }
      }
      if ($per.Count -gt 0) {
        $sum = 0.0; $minPkg = ''; $minVal = 101.0
        foreach ($k in $per.Keys) {
          $sum += $per[$k]
          if ($per[$k] -lt $minVal) { $minVal = $per[$k]; $minPkg = $k }
        }
        $rec = [ordered]@{
          at = (Get-Date).ToString('o')
          gate = 'coverage'
          env = if ($env:CI -eq 'true') { 'ci' } else { 'local' }
          pkgCount = $per.Count
          avgPct = [math]::Round($sum / $per.Count, 2)
          minPkg = $minPkg
          minPct = $minVal
          head = (& git rev-parse --short HEAD 2>$null)
        }
        Add-Content -LiteralPath (Join-Path $readingsDir 'gate-readings.jsonl') -Value ($rec | ConvertTo-Json -Compress) -Encoding UTF8
        Write-Host ("    覆盖率读数已落盘：avg=" + $rec.avgPct + "% min=" + $rec.minPkg + "=" + $rec.minPct + "% (" + $rec.env + ")") -ForegroundColor DarkGray
      }
    }
  } else {
    Write-Host "    覆盖率门禁已跳过（-SkipCoverGate）" -ForegroundColor Yellow
  }
} else {
Mark "4/6 test（跳过）"
  Write-Host "==> 4/6 跳过测试" -ForegroundColor Yellow
}

Mark "5/6 go build"
Write-Host "==> 5/6 构建单 EXE" -ForegroundColor Cyan
$buildTime = Get-Date -Format o
$env:CGO_ENABLED = "0"
& go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version -X main.buildTime=$buildTime" -o (Join-Path $dist "workbench.exe") ./cmd/workbench
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "构建失败" }
Pop-Location

Mark "6/6 校验+链接判据"
Write-Host "==> 6/6 校验产物" -ForegroundColor Cyan
$exe = Join-Path $dist "workbench.exe"
if (-not (Test-Path $exe)) { throw "产物不存在" }

# 子系统门禁（2026-10-19 弹窗事故）：控制台子系统的 EXE 被 Win32_Process.Create
# 或计划任务拉起时，Windows 会给它分配一个**可见控制台窗口** —— 桌面被反复弹窗。
# 官方构建必须带 -H=windowsgui（GUI 子系统，不弹窗）；裸 go build 重建会退回 CONSOLE，
# 这里当场判红，杜绝「会弹窗的产物」进入 dist。
$subChecker = Join-Path $PSScriptRoot "..\tests\check_subsystem.ps1"
if (Test-Path $subChecker) {
  # 必须经子进程 -ExecutionPolicy Bypass 调用：本机默认策略会拒绝加载未签名脚本
  # （实测 & 直调报 "cannot be loaded... not digitally signed"，门禁会假通过）。
  $chkHost = if (Get-Command powershell.exe -ErrorAction SilentlyContinue) { "powershell.exe" } else { "pwsh" }
  & $chkHost -NoProfile -ExecutionPolicy Bypass -File $subChecker -Exe $exe -RequireGui
  # 注意：此处不能 Pop-Location —— 位置栈已在 5/6 段尾弹出，再弹会抛错并掩盖真实判据。
  if ($LASTEXITCODE -ne 0) { throw "产物子系统不合规（会弹控制台窗口），见上方 FAIL 提示" }
  Write-Host "    子系统: GUI（无控制台窗口）OK"
} else {
  # 校验器不在（例如 CI 上 tests/check_subsystem.ps1 尚未入库）时**内联自证**，
  # 而不是跳过 —— 跳过会让门禁在 CI 上静默失效，等于没有门禁。
  $fs2 = [IO.File]::OpenRead($exe); $b = New-Object byte[] 4096; [void]$fs2.Read($b, 0, 4096); $fs2.Close()
  $peOff = [BitConverter]::ToInt32($b, 0x3C)
  $sub = [BitConverter]::ToUInt16($b, ($peOff + 4 + 20 + 68))
  if ($sub -ne 2) { throw ("产物子系统不合规: Subsystem=" + $sub + "（2=GUI 不弹窗 / 3=CONSOLE 会弹控制台窗口）") }
  Write-Host "    子系统: GUI（内联校验，无控制台窗口）OK"
}

# 首页契约门禁（2026-10-20 现场：老大「打开的是旧的页面,已经有新的了」）。
#
# 为什么必须进构建链：那次 dist\workbench.exe 是从**旧树**构建后覆盖进来的 ——
# 产物里是旧首页（home-badge / 运行中 / 引擎状态 / 开始运行 + 隐藏入口），
# 且**没有** --ui-probe 格（探针静默回落成 GUI）。当时的门禁只看 PE 子系统与体积，
# 没有任何一条判据看「页面内容是不是当前源码的」，于是旧页一路发到老大桌面。
# 本门禁从产物字节 + 真探针 HTTP 响应两头取证，判据见 tests\check_homepage.ps1 头注释。
$homeChecker = Join-Path $PSScriptRoot "..\tests\check_homepage.ps1"
if (Test-Path $homeChecker) {
  $chkHost2 = if (Get-Command powershell.exe -ErrorAction SilentlyContinue) { "powershell.exe" } else { "pwsh" }
  & $chkHost2 -NoProfile -ExecutionPolicy Bypass -File $homeChecker -Exe $exe
  if ($LASTEXITCODE -ne 0) { throw "首页契约门禁未通过（产物页面不是当前源码的，见上方 FAIL/UNVERIFIED 提示）" }
  Write-Host "    首页: 当前源码页 OK"
} else {
  # 校验器不在时**内联自证**：至少判死「旧首页锚点」与「缺 ui-probe 通道」两种已知劣化。
  $hbytes = [IO.File]::ReadAllBytes($exe)
  $htext = [Text.Encoding]::UTF8.GetString($hbytes)
  if ($htext.Contains("home-badge")) { throw "产物含旧首页锚点 home-badge：不是当前源码构建的" }
  if (-not $htext.Contains("home-entries")) { throw "产物缺新首页锚点 home-entries：不是当前源码构建的" }
  if (-not $htext.Contains("UI_PROBE_READY")) { throw "产物缺 --ui-probe 通道：探针会静默回落成 GUI" }
  Write-Host "    首页: 内联校验（无旧页锚点 + 有探针通道）OK"
}
$info = (Get-Item $exe).VersionInfo
$size = [math]::Round((Get-Item $exe).Length / 1MB, 2)
Write-Host "    文件: $exe"
Write-Host "    大小: $size MB"
Write-Host "    版本: $($info.FileVersion)"
Write-Host "    产品: $($info.ProductName)"
$hash = (Get-FileHash $exe -Algorithm SHA256).Hash
"$hash  workbench.exe" | Set-Content (Join-Path $dist "SHA256SUMS")
Write-Host "    SHA256: $hash"

# 清单读回自证（2026-10-25 round 154）：CI 的 build job 有独立读回，本地构建链此前没有。
# 与「子系统校验器不在就内联自证，而不是跳过」同一立场：校验器在就跑它，不在就内联比一次。
$sumsChecker = Join-Path $PSScriptRoot "..\tests\check_artifact_checksum.ps1"
if (Test-Path $sumsChecker) {
  $chkHost3 = if (Get-Command powershell.exe -ErrorAction SilentlyContinue) { "powershell.exe" } else { "pwsh" }
  & $chkHost3 -NoProfile -ExecutionPolicy Bypass -File $sumsChecker -Exe $exe -Sums (Join-Path $dist "SHA256SUMS") -Quiet
  if ($LASTEXITCODE -ne 0) { throw "产物与 SHA256SUMS 不一致（rc=$LASTEXITCODE）：清单读回自证失败" }
  Write-Host "    清单读回自证 OK" -ForegroundColor Green
} else {
  $readBack = (Get-FileHash $exe -Algorithm SHA256).Hash
  if ($readBack -ne $hash) { throw "清单读回自证失败（无校验器，内联比对）: $readBack != $hash" }
  Write-Host "    清单读回自证 OK（内联）" -ForegroundColor Green
}

# 产物页面与源码一致（2026-10-25 round 164）：CSS 正文（Go raw string 块）必须逐字内嵌在 EXE 里。
# 为什么放在构建链上：CI 的 repo-gates job 跑在 build 之前、那里没有产物；只有这里既刚产出又必须为真。
# 与前两处同一立场：校验器在就跑它并采信其 rc，不在就内联自证一次，绝不静默跳过。
$pageChecker = Join-Path $PSScriptRoot ("..\tests\check_artifact_page_fresh.ps1")
if (Test-Path $pageChecker) {
  & $chkHost3 -NoProfile -ExecutionPolicy Bypass -File $pageChecker -Exe $exe -Quiet
  if ($LASTEXITCODE -ne 0) { throw ("产物页面与源码不一致（rc=" + $LASTEXITCODE + "）：新产物本不该出现此情况，检查 CSS 提取口径" ) }
  Write-Host "    产物页面与源码逐字一致 OK" -ForegroundColor Green
} else {
  $cssSrc = [IO.File]::ReadAllBytes((Join-Path $wb "internal\web\assets_css.go"))
  $bt = [byte][char]96
  $f0 = [Array]::IndexOf($cssSrc, $bt)
  $l0 = [Array]::LastIndexOf($cssSrc, $bt)
  $css = New-Object byte[] ($l0 - $f0 - 1)
  [Array]::Copy($cssSrc, $f0 + 1, $css, 0, $css.Length)
  $enc = [Text.Encoding]::GetEncoding(28591)
  if (-not $enc.GetString([IO.File]::ReadAllBytes($exe)).Contains($enc.GetString($css))) {
    throw ("产物页面与源码不一致（无校验器，内联比对）：CSS " + $css.Length + " 字节未逐字出现在产物里")
  }
  Write-Host "    产物页面与源码逐字一致 OK（内联）" -ForegroundColor Green
}

# 内嵌资产全量检查（2026-10-25 round 176，落实 round 175 登记的正确做法）：
#   PS 那道闸是**快速子集**（只比 CSS 一块，秒级、不编译）；这里用 Go 版 AST 判据覆盖**全部内嵌资产块**
#   （实测 13 块：8 个 CSS/JS/HTML + 4 个 docx XML 模板 + favicon SVG，约 214 KB）。
#   为什么必须放这里：该判据要读**刚构建出的产物**，而 4/6 段的 go test 发生在产物之前 ——
#   放 4/6 只会拿上一次的旧产物判，语义错位。
#   用 go/parser 取 raw literal 的真实位置（不数反引号）：round 175 实测三条「数反引号」口径全部假阳性。
$assetTest = Join-Path $wb "internal\web\asset_artifact_contract_test.go"
if (-not (Test-Path $assetTest)) {
	throw ("产物资产判据缺失：" + $assetTest + "（判据应受版本控制；缺失=判据面出现空洞，不许静默跳过）")
}
Push-Location $wb
& go test ./internal/web/ -run 'TestAssetsPresentInArtifact' -count=1 2>&1 | Out-Null
$assetRc = $LASTEXITCODE
Pop-Location
if ($assetRc -ne 0) { throw ("产物与内嵌资产不一致（rc=" + $assetRc + "）：完整报告见 go test ./internal/web/ -run TestAssetsPresentInArtifact -v") }
Write-Host "    内嵌资产全量逐字一致 OK（AST 口径，13 块）" -ForegroundColor Green

# 网络来源标记（MOTW）门禁（2026-10-19 老大截图："打开文件 - 安全警告 / 无法验证发布者"）：
# 只要 EXE 带 Zone.Identifier 备用数据流，Windows 附件管理器就会在双击时弹
# "无法验证发布者。你确定要运行此软件吗？" 的安全警告 —— 一步人工点击，而且
# 在无人值守场景会直接把启动卡死。产物必须是本机构建且已解除阻止的干净文件。
Mark "6.5 MOTW"
Write-Host "==> 6.5/7 MOTW 门禁（弹安全警告的前提）" -ForegroundColor Cyan
$zone = Get-Item -LiteralPath $exe -Stream Zone.Identifier -ErrorAction SilentlyContinue
if ($zone) {
  Write-Host "    产物带 Zone.Identifier（网络来源标记）→ 解除阻止" -ForegroundColor Yellow
  Unblock-File -LiteralPath $exe -ErrorAction SilentlyContinue
  $zone2 = Get-Item -LiteralPath $exe -Stream Zone.Identifier -ErrorAction SilentlyContinue
  if ($zone2) { throw "产物仍带 Zone.Identifier：双击会弹『无法验证发布者』安全警告，无法自动清除（检查文件属主/ACL）" }
  Write-Host "    已解除阻止（不会再弹安全警告）OK" -ForegroundColor Green
} else {
  Write-Host "    无网络来源标记 OK" -ForegroundColor Green
}

# 完整性标签门禁（2026-10-19 托盘图标事故）：产物所在目录若被打上 Low 完整性标签，
# 从它启动的进程会继承 Low IL，于是**永远注册不上托盘图标**（NIM_ADD → Access is denied），
# 而且 Low 进程自己修不了（icacls 返回 1299）。目录属主在 Medium 下可以修，构建脚本
# 正是 Medium —— 因此把「产物目录必须是 Medium」做成构建门禁，坏标签不带进下一轮。
Mark "7/7 完整性标签"
Write-Host "==> 7/7 完整性标签门禁（托盘图标可注册的前提）" -ForegroundColor Cyan
$labelLines = @(& icacls.exe $dist 2>&1 | Where-Object { "$_" -match 'Mandatory Label' })
if ($labelLines.Count -gt 0 -and ("$($labelLines -join ' ')") -match 'Low Mandatory Level') {
  Write-Host "    产物目录被打上 Low 完整性标签（沙箱残留）→ 尝试提升为 Medium" -ForegroundColor Yellow
  & icacls.exe $dist /setintegritylevel '(OI)(CI)M' | Out-Null
  $labelLines = @(& icacls.exe $dist 2>&1 | Where-Object { "$_" -match 'Mandatory Label' })
  if (("$($labelLines -join ' ')") -match 'Low Mandatory Level') {
    throw "完整性标签仍是 Low：从该目录启动的实例不会有托盘图标（icacls 提升失败，请检查目录 DACL/属主）"
  }
  Write-Host "    已提升为 Medium OK" -ForegroundColor Green
} else {
  Write-Host "    完整性标签: 非 Low OK" -ForegroundColor Green
}

Write-Host ""
Mark "7/7 完整性标签"
Write-Host ("    [计时] 构建链总计 = " + [math]::Round(((Get-Date) - $chainStart).TotalSeconds, 1) + " s") -ForegroundColor Cyan
Write-Host "构建成功！双击 $exe 或运行 setup\install.ps1 安装快捷方式。" -ForegroundColor Green

# build.ps1 — 一键构建「个人 AI 工作台」单 EXE
# 用法: powershell -ExecutionPolicy Bypass -File setup\build.ps1 [-Version 1.0.0]
param(
  [string]$Version = "1.0.0",
  [switch]$SkipTests,
  [switch]$SkipCoverGate,
  [double]$CoverThreshold = 55,
  # PerPackageCover：按"包:阈值"覆盖个别包的基线（新抽出的纯逻辑包更高，
  # 与子进程强耦合的 engine 包先按当前基线守护、逐步抬升）。
  # 分档基线依据：engine/web/executor 与大块子进程/HTTP 耦合，先按当前水平守护、
  # 每次改造只许抬不许降；logging/retry/queue/model 等纯逻辑包走全局 55% 阈值。
  #
  # tray 阈值说明（2026-10-21 复核对账）：托盘包的 5 个守护测试全部是「常量/契约」型断言
  # （WS_EX_TOOLWINDOW 必须是 0x80、类名不得漂移、标题必须自述内部窗、结构体尺寸、
  # 完整性级别判定），它们守的是「消息窗不得进任务视图」这条 2026-10-20 现场缺陷的根因。
  # 这些测试刻意**不**随覆盖率数字浮动：真正创建窗口的 Run() 只能在有 Explorer 的真机上跑，
  # 单测环境断言它只会得到假绿。因此 tray 的实际覆盖率长期停在 16.x%，
  # 阈值按实测基线设为 16（不许降到 16 以下，也不虚设到够不着的高位）。
  [string[]]$PerPackageCover = @("workbench/internal/engine=20", "workbench/internal/web=15", "workbench/internal/executor=26", "workbench/internal/modelpool=49", "workbench/internal/supervisor=46", "workbench/cmd/workbench=3", "workbench/internal/evaluate=50", "workbench/internal/modelroute=50", "workbench/internal/shortcut=23", "workbench/internal/chain=80", "workbench/internal/judge=55", "workbench/internal/single=16", "workbench/internal/tray=16", "workbench/internal/dshprofile=2.5")

# dshprofile 阈值说明（2026-10-21 复核对账，与 tray/single 同型）：
# 该包 18 个函数里只有 profilePatch 是纯函数（已 100% 覆盖），
# 其余全是 IO 型：真实 DSH 安装目录探测（LocateDSH/resolveFromShim）、
# 文件复制（copyDir/linkOFM/ensureProfile/ensureOFMData）、
# 进程属性（hiddenProcAttr）—— 这些在单测环境里要么依赖真实安装态、
# 要么只能在有 Explorer 的真机跑，硬凑覆盖率只会造假绿。
# 实测基线 2.6%（纯函数那一个），阈值按实测设为 2.5（略低于实测，
# 也不虚设到够不着的高位）；将来补真实 IO 测试时只许抬不许降。
)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$wb = Join-Path $root "workbench"
$dist = Join-Path $root "dist"
$build = Join-Path $wb "build"
New-Item -ItemType Directory -Force -Path $dist | Out-Null

Write-Host "==> 1/6 生成图标" -ForegroundColor Cyan
$makeIcon = Join-Path $PSScriptRoot "make-icon.ps1"
if (Test-Path $makeIcon) {
  New-Item -ItemType Directory -Force -Path $build | Out-Null
  & $makeIcon -Out (Join-Path $build "app.ico") | Out-Null
} else {
  Write-Warning "    make-icon.ps1 不存在，跳过图标生成（EXE 仍可用）"
}

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
  } else {
    Write-Host "    覆盖率门禁已跳过（-SkipCoverGate）" -ForegroundColor Yellow
  }
} else {
  Write-Host "==> 4/6 跳过测试" -ForegroundColor Yellow
}

Write-Host "==> 5/6 构建单 EXE" -ForegroundColor Cyan
$buildTime = Get-Date -Format o
$env:CGO_ENABLED = "0"
& go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$Version -X main.buildTime=$buildTime" -o (Join-Path $dist "workbench.exe") ./cmd/workbench
if ($LASTEXITCODE -ne 0) { Pop-Location; throw "构建失败" }
Pop-Location

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

# 网络来源标记（MOTW）门禁（2026-10-19 老大截图："打开文件 - 安全警告 / 无法验证发布者"）：
# 只要 EXE 带 Zone.Identifier 备用数据流，Windows 附件管理器就会在双击时弹
# "无法验证发布者。你确定要运行此软件吗？" 的安全警告 —— 一步人工点击，而且
# 在无人值守场景会直接把启动卡死。产物必须是本机构建且已解除阻止的干净文件。
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
Write-Host "构建成功！双击 $exe 或运行 setup\install.ps1 安装快捷方式。" -ForegroundColor Green
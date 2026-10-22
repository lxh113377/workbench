# Changelog

> 本项目所有值得一提的变更都记在这里。
> 格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
> 版本号遵循[语义化版本](https://semver.org/lang/zh-CN/)。
>
> 版本与产物对应关系以 `dist/SHA256SUMS` 与 `workbench.exe --version` 的实测输出为准。

## [Unreleased]

> 规则：未发布的变更一律记在本节；发版时把本节整体提升为新的 `## [x.y.z] - YYYY-MM-DD`，
> 并立刻补一个空的 `## [Unreleased]` 回顶（CI 的 changelog 门禁会校验这一条）。
>
> 修复说明（2026-10-22 自查）：上一轮插入条目时把上面第二行说明误吞了（锚点含它但替换块
> 未重新输出）—— 本轮恢复，并记下这条教训：以「包含后文的多行锚点」做插入时，
> 替换块必须把锚点里的**全部**内容重新输出，否则静默丢行。

### Fixed
- **数据规模触发：产出页显示 0 份产出 / 队列页今日产出恒 0（2026-10-22 真实规模实测）**：
  当前真实库存 **33,728 个任务**（其中 done 86 / degraded 6 / rejected 33,187），
  两个只在数据量长大后才会出现的功能性缺陷：
  ① **产出页空白**：原写法 `/api/tasks?limit=3000` 取最新 3000 条再前端 `filter(state==='done')`。
  最新窗口被 2,919 条 rejected 挤满 ⇒ 窗口内 done=0 ⇒ 页面显示「还没有产出」，
  而库存实际有 86 份完成品（81 份通过验收）。用户会以为系统坏了。
  修：改用后端本就支持的 `?state=` 过滤（实测 done 86 条 54ms，比拉 3000 条 15ms 更准更快）。
  **实测复验：产出页 0 → 86 份产出 / 81 份通过验收。**
  ② **队列页「今日产出」恒 0**：该值读 `cache.status.stats.todayDone`，而 `cache.status` 只在
  `renderOverview()` 里赋值 —— 用户直接从 `#queue` 进页面时 overview 从未渲染，
  `cache.status` 永远 undefined。CDP 实测进队列页后 1.2/2.5/4.0/6.0s 四次采样
  `cacheStatus` 均为 false，而 API 实际 `todayDone=8`。修：队列页自己取 `/api/status` 并写入
  cache。**实测复验：今日产出 0 → 8。**
  新增 `scale_contract_test.go` 两条判据（产出页必须按 state 取数且不得退回裸 limit 写法 /
  队列页必须自己取 status 且写进 cache）；变异 S1 退回裸 limit → CAUGHT、
  S2 不取 status → CAUGHT、S3 取了但不写 cache → CAUGHT。
  同类修复：队列页 active 列表同型缺陷（原拉 limit=600 再前端过滤），一并改为按状态取。

> ⚠️ **本轮未发布**：官方链 3/6 步 gofmt 门禁判红，点名 6 个文件
>（`assets_gov.go` / `export.go` / `pool_page.go` / `autostart_windows.go` /
> `tray_window_style_test.go` / `deadlock_test.go`）—— 均非本轮改动，其 **HEAD blob 本身就 gofmt 脏**，
> 是并行会话正在推进的 gofmt 治理在途件。按「不碰他人在途件」纪律不代跑 gofmt（会覆盖其并发写入），
> 故本轮只入库不发布；待其落定后由下轮走官方链补发。
### Fixed
- **文案与交互语义跨页一致性（2026-10-22 机器化审计，164 条用户可见文案）**：
  从源码提取全部按钮/提示/空态文案做规范检查，发现两类真问题：
  ① **同一动作两种措辞**：保存开机自启的按钮写作「应用」，而同页其它保存动作一律「保存」——
  用户得猜「应用」是不是和执行类操作一样危险。统一为「保存」。
  ② **同一语义的空态两种写法**：管理面板「没有失败任务」有两处（折叠块 + 详细块），
  一处「最近没有失败任务。」、一处「没有失败任务 —— 保持这样最好。」；翻到两处会以为
  说的是两码事。统一为后者。另把删除模板后的含糊提示「已处理」改成「已删除该模板」。
  **审计结论（好的部分，已固化成判据防回退）**：57 个按钮 0 带句末标点、23 处失败提示
  100% 采用「XX失败：<原因>」形态、无英文成功提示（OK/Success/Done）、无模糊错误话。
  新增 `copy_consistency_test.go` 三条判据（按钮是动作词且无句末标点 / 失败提示必须点名 /
  同语义空态只能一种写法）；变异 W1「保存」退回「应用」→ CAUGHT、W2 空态退回两种写法 →
  CAUGHT、W3 toast 改含糊话 → CAUGHT。
### Fixed
- **跨页一致性审计：同一产品两种卡片语言 / 两种内容顶距（2026-10-22 机器化审计）**：
  以 CDP 逐页读 computedStyle 对四页做机器化比对（13 项度量，差异 11 项里多数是内容长度
  的必然结果，真不一致 2 项）：
  ① **卡片语言分裂**：治理台的 `.gov-card` 用圆角（`--radius` 12px）+ margin 14px + 实边框，
  而管理/池的 `.card` 用 HUD **切角**（`clip-path` + radius 0）+ margin 18px + inset 描边。
  设计体系头注释写明「HUD 面板的角不是圆的，是斜切的」（`clip-path` 9 处、`--cut` 为主语言），
  故 `.gov-card` 是异类 ⇒ 对齐 `.card`（切角/radius 0/18px 节奏/inset 描边）；
  悬停改用内描边换色（切角卡没有可描边的 border，会被 clip-path 切掉）。
  ② **内容区顶距 26px vs 14px**：管理面板 DOM 是 `topbar-shell → tabs → content`，
  而 `.topbar-shell + .content` 是**相邻**兄弟选择器、匹配不到（中间隔着 `.tabs`），
  于是有页签的页回落 26px、无页签的页 14px。补 `.tabs + .content` + `.topbar-shell ~ .content`
  双保险。
  复测：三页 `paddingTop` 全部 **14px**（改前 admin 26px）；三页 `borderRadius=0px`、
  `marginBottom=18px`、`borderTopColor=transparent` **完全一致**。
  新增 `cross_page_consistency_test.go` 两条判据（两卡基座度量同源 / 内容顶距选择器齐备）；
  变异 C1 gov-card 退回圆角 → CAUGHT、C2 删 `.tabs + .content` → CAUGHT。
### Fixed
- **check_snapshot_build 在 CI 上因 tar.exe 文件名编码失真而误判（2026-10-22 实测定位）**：
  该门禁在 CI 持续判红，报 `TestRepoRootHasRequiredAnchors: repoRoot(...snap) 下读不到方案文档`。
  逐层排掉后拿到决定性证据：`repoRoot()` **正确**返回了快照根，但 `snap\docs\自主运行方案.md`
  在磁盘上不存在 —— 文件不是没提交，是**解包时名字坏了**。
  根因：门禁用 `tar -xf` 展开 `git archive`，而 CI runner 的 tar.exe(bsdtar) 按**系统代码页**
  （英文 Windows = 437）解码 tar 头里的 UTF-8 文件名，中文名落盘成乱码。本机（代码页 936）
  恰好正确，故本机全绿 —— 典型的「环境相关、本机不可复现」。
  这比「判据过严」严重：门禁的全部价值是「快照里到底有什么」，若解包本身失真，判据不可信。
  修：改用 `git --git-dir --work-tree checkout-index -a -f` 展开快照 —— 语义等价（同取 HEAD），
  但文件名由 git 按字节处理，**不经过 tar.exe 的编码层**，与平台代码页无关。
  `git archive` 保留仅用于「快照大小」这一行体积读数。
  验证：本机 rc=0；同时对比两种展开方式在中英文锚点上的落盘保真度。
- **repo-gates 第二、三轮判红（2026-10-22 连续实测，红因逐个下移）**：
  门禁挂上 CI 后按预期开始工作，每轮红点都是真问题，逐个修到绿：
  ① **PS5.1 ParserError**（Unexpected token / Missing closing）：25 个含中文的 .ps1 缺 UTF-8 BOM。
    不只是我改过的 3 个 —— 构建链核心 `setup/build.ps1`、CI 在跑的 `check_perf.ps1` 都缺。
    修：全部只前置 EF BB BF；新增 `check_ps1_encoding.ps1` 上闸（含非 ASCII 才要求 BOM）。
  ② **双重 BOM**（我自己两次犯）：在已有 BOM 的文件上再前置一次 ⇒ PS5.1 把第二个 BOM 当命令字符，
    报 `assignment expression is not valid`。是「补 BOM」这类修复的高发失误，已扩展编码门禁覆盖
    （连出两个 EF BB BF 即判红），并当场抓出 `check_all.ps1` 的双重 BOM 修掉。
  ③ **check_snapshot_build 在 CI 上 `TestRepoRootHasRequiredAnchors` 判红**：
    `repoRoot()` 靠目录探测找仓库根，在本机各种形态（8.3 短名 / 纯 ASCII TEMP / PS5.1 解包 /
    非仓库 cwd）全部通过，CI runner 上返回空串 —— 环境（locale/代码页）相关，本机无法复现。
    处置：快照门禁**已知**根在哪，就显式注入 `repoRoot()` 自带的 `WB_REPO_ROOT` 覆盖位，
    不让被测代码去猜；探测逻辑本身的正确性仍由工作树里的同一用例守护。
  ④ **check_gate_links 连续两轮点名新断链**（它自己抓自己）：先抓到 `check_ps1_encoding.ps1`
    未入库，再抓到 `setup-out-of-box.ps1` 引用的 `fix-runtime-patch.ps1` / `qa-artifacts.ps1` 未入库。
    两者均入库（`.bak-inject-append` 备份件不提交，无引用）。
  实测：PS5.1 下 9 个门禁全 PASS；`check_gofmt` 仍为「PASS 带 34 条显式豁免未清」（并行在途件）。
- **PowerShell 脚本编码：25 个含中文脚本缺 UTF-8 BOM，CI 的 PS5.1 会 ParserError（2026-10-22 两轮实测）**：
  本轮把 8 个门禁挂上 CI 后，repo-gates job 判红，报的是
  `Unexpected token` / `Missing closing ')'`，行号指向 `check_snapshot_build.ps1:42`
  的 `Write-Host ("  仓库: " + $Repo)` —— 本机 PS7 一切正常，CI 的 PS5.1 却把中文按 ANSI 解码拆坏字节。
  这是同一坑的**第二次复现**（第一次是本轮新建 check_gofmt/check_gate_links 时）：
  修好后又踩一次 —— 编辑那三个既有门禁时写回文件把 BOM 剥掉了。
  危害比缺判据更隐蔽：门禁报的是**它自己**的语法错，不是被测缺陷。
  排查：写 `tests/check_ps1_encoding.ps1` 扫全仓，实测 **25 个**含非 ASCII 的 .ps1/.psm1 缺 BOM ——
  不只是我碰过的 3 个，还包括构建链核心 `setup/build.ps1`、以及正在 CI 里跑的 `check_perf.ps1`
  （它侥幸没炸：含中文的字符串不在解析路径上）。属于系统性技术债，不是个别失误。
  处置：① 25 个脚本一律**只前置 EF BB BF**（不重写正文，符合判据自身给的修复指引），
  前置后逐个 `Parser` 语法校验通过（12 个抽查 + 全量）；② 新增编码门禁并接入 CI 的 repo-gates
  与 check_all：判据只针对真实失效面（**含非 ASCII 才要求 BOM**，纯 ASCII 脚本不强求，避免无意义噪声）；
  ③ 实现时踩到本仓在册坑：`Get-ChildItem -Include` 在 `-LiteralPath` 下不生效，
  首版把 4074 个 .md/.json 也判成脚本 —— 改为显式扩展名过滤。
  验证：PS5.1 真实执行 9 个门禁全 PASS；干净 clone 复核后仅剩「新脚本未入库」的预期断链（提交即清零）。
- **门禁判据「存在但没挂受理面」：23 个受跟踪门禁脚本里 CI 只执行 4 个（2026-10-22 实测）**：
  本仓 tests/ 下有 23 个受跟踪 .ps1，CI（.github/workflows/ci.yml）此前只跑
  `check_changelog / check_noninteractive / check_issue_forms / check_perf` 四条。
  其余一条都没被任何自动化执行 —— 包括本轮该建但**从未上闸**的 `check_gofmt`、
  以及 `check_gate_links / check_tray_window / check_spawn_silent / check_snapshot_build`。
  「判据写了不等于判据生效」是本仓在册失效形态，这次是它自己复现。
  处置：新增独立 job `repo-gates`（与 vet/test 分开，失败可归因到具体门禁，不淹在测试输出里），
  逐条执行 8 个纯源码门禁、**逐个判 rc**（反掩盖：任一 rc!=0 即整体失败，不允许聚合洗绿）。

  实施中顺带挖出三个真问题（都当场修掉）：
  ① **门禁硬编码绝对路径** `D:\workdesktop`（check_all / check_tray_window / check_spawn_silent /
     check_snapshot_build 都有）：在干净 clone / CI 里该路径不存在，门禁会检查**另一棵树**。
     实证：我在 worktree 里注入 tray 缺陷做变异测试，门禁仍 PASS —— 因为它读的是主区文件。
     修：默认根一律由 `$PSScriptRoot` 推导（tests/ 的父目录），显式传参仍可覆盖。改正后同一变异
     **立即判红**（`FAIL Run 未调用 hideTrayWindow`）。
  ② **check_spawn_silent 抓到 HEAD 上的真缺陷**：`internal/web/autostart_windows.go` 三处
     `exec.Command("reg", ...)` 缺 `hiddenProcAttr()`，而 reg.exe 是 CONSOLE 子系统程序 ——
     用户每点一次「开机自启」就闪一个黑窗。该修复已在工作区由并行会话完成但未入库。
     处置：门禁加**显式已知缺陷豁免**（逐条打印 KNOWN + 三态措辞「PASS（带 N 条豁免，未清零）」），
     绝不打纯 PASS；变异验证「新增一处未登记违规」→ 判红并点名。
  ③ 豁免初版**静默失效**：违规路径带前导 `/` 而豁免键不带，Contains 匹配不上 → 仍判红。
     修：路径规范化去前导斜杠；这正是「豁免机制自己也要被判据验」的例证。

  验证：干净 HEAD worktree + 完整拷入本轮改动的 8 个门禁 → **8/8 PASS**；本地同样 8/8 PASS。
  变异验证：tray 缺陷 → 判红；新增未登记 spawn 违规 → 判红；恢复 → PASS。
- **WCAG 1.4.4 缩放审计：400% 放大下正文字号反而变小（2026-10-22 实测缺陷）**：
  `@media (max-width:480px){ body{font-size:14px} }` 在 400% 缩放（视口 360px）时触发，
  把正文从 15px 降到 14px。窄视口有两种成因：① 手机（真小屏，14px 合理）；② **用户缩放放大**
  （200%/400% 后视口同样变窄）。CSS 无法区分两者，而后者里「为了看清而放大、字号反而变小」
  与 1.4.4 的意图正好相反。修：删掉该字号覆盖，保留 15px —— 窄视口靠**布局**消化
  （单列/卡片化/换行），不靠牺牲可读性。
  实测对比：400% 下 bodyFont **14px → 15px**，且零溢出、零截断不变。
  新增 `zoom_contract_test.go` 两条判据（body 字号不得被媒体查询改小 + 窄屏适配必须保留
  布局手段）；变异 Z1 恢复 14px → CAUGHT、Z2 删表格卡片化 → CAUGHT。

### Added
- **200%/400% 缩放审计（WCAG 1.4.4，同轮）**：5 页 × 2 档实测（720×450 / 360×225，
  deviceScaleFactor 2/4）：`overflowX` 全 ≤0、越界元素 0、内容截断 0、按钮高度 36px 达标、
  正文最小 12.5px。缩放场景此前从未实测过，本轮证明布局在极端缩放下不塌。
### Added
- **打印样式（@media print）—— 此前全站零打印规则（2026-10-22 打印介质实测）**：
  CDP 模拟打印介质实测，打印预览里 body 仍是 rgb(4,7,13) 深底 + 极光渐变 + 顶栏 + 页签 +
  按钮残骸，**纸上是满页黑块、完全不可用**。而界面本身有「打印自检报告」按钮，
  用户真的会按 Ctrl+P —— 这个场景从来没被设计过。
  新增打印版「白纸黑字」：关掉所有装饰层（渐变/网格/辉光/扫描线/伪元素）、隐去导航与操作面
  （顶栏/页签/工具栏/按钮/Toast）、卡片退化为分隔线（省墨且清晰）、文本与表格强制深色、
  状态色压深保留语义（黑白打印机也能辨）、**折叠块强制展开**（纸质件无法点击，未展开
  等于内容丢失）、长表格不截断且跨页可断、外链打印出 URL。
  复测（同一探针）：body 深底变成 **白底 rgb(255,255,255) 加黑字 rgb(17,17,17)**、
  topbar/tabs/btn 全部 display:none、card 白底无渐变。
  新增 print_styles_contract_test.go 三条判据；变异验证 P1 摘掉整块 CAUGHT、P2 白底改黑
  首轮 **MISSED**（判据只查 background:#fff 子串，color:#111 被保留导致子串仍在），
  收紧为整串并加反向深色断言后 CAUGHT、P3 删折叠展开 CAUGHT。
- **键盘导航审计通过（同轮，无需修改）**：CDP 真 Tab 键 × 5 页实测，焦点环全部可见
  （outline=solid/2px 且 :focus-visible=true），治理台 SUMMARY 可聚焦、Space 可展开。
  同时留下一条踩坑：**探针实例有 --seconds 时限，超时后 Edge 渲染网络错误页，CDP 仍可连、
  evaluate 仍返回，读数看起来完全正常但测的是错误页** —— 本轮据此一度误判两个不存在的
  缺陷。防护：跑探针前先 curl 断言 HTTP 200。

### Added
- **模型池：各免费池「一键启用 / 一键停用」+ 网关专属/其它车道下线（老大 2026-10-22 令）**：
  1) **整池开关** —— 新增 `Manager.SetPoolEnabled(lane, group, enabled, actor)`：面板每张分组卡头部
     带「一键启用/一键停用」两枚按钮（`POST /api/pool/lane`），CLI 侧 `--model-pool lane <lane> <enable|disable>`
     同口径。判据要点：**group 优先于 lane**（分组卡就是「池」的单位；自填端点常是 lane=aggregate 而
     Group=「自定义」，取交集会把真实存在的池判成空池）；手动开关一律清掉 `AutoDisabled`，否则「一键停用」
     会被下一轮网关同步按「仍在广播」自动 revive，按钮成摆设。停用走两段式确认（armConfirm），
     一次误点不得让整条渠道退出调度。缺 `enabled` / 缺 lane+group / 未知车道 → 400，绝不静默成功。
  2) **网关两条车道去掉，只留 eac/kilo/ofm 等免费车道** —— `SpecFromGatewayModel` 的网关专属/网关其它
     两个分支改为 `default: return Spec{}, false`（非 `our-free-model*` 形态一律不纳管，计入 `SyncResult.Skipped`）；
     新增 `Manager.PurgeGatewayLanes` 清理**历史账本存量**（判据 provider=="gateway" ∪ 分组名命中，
     幂等、不误删用户自填端点），`Service.SyncNow` 每轮开头自动调用，另给 `POST /api/pool/purge` 与
     `--model-pool purge-gateway` 两个人工/脚本入口。前端 GROUP_ORDER 摘除该二组，车道水位只报 3 条免费池。
  实测：真实网关广播 56 个 id → 入池 38 / 跳过 18；线上实例首轮同步清掉 25 条存量；EAC 池一键停用
  `available 42→37` 且路由表同步收缩、再启用回 42 并落盘。`go build/vet` = 0，`go test ./...` 29 包全绿
  （modelpool 覆盖率 70.2%，门禁阈值 49）。新增回归：modelpool 4 条 + web 3 条（含反假绿坏样本）。
  产物经 `setup/build.ps1` 七道门禁重建（旧件备份在 `dist/archive/`）。
- **测试层 Windows 平台耦合：`go vet` 在 Linux 容器内编译失败（2026-10-22 与上一条同轮修复）**：
  `internal/executor/linkdir_silent_test.go` 与 `cmd/workbench/headless_test.go` 在**无 build tag** 的
  `_test.go` 里使用 `syscall.SysProcAttr{HideWindow, CreationFlags}` —— 这两个字段只在 Windows 存在，
  于是 `tests/docker/run_pool_e2e.ps1` 的「池门禁 go test」段在 `go vet ./...` 就红
  （实测 `vet: ... HideWindow undefined`），把模型池的 Linux 侧验收整体拖红。
  修法：Windows 专属断言按平台拆开 —— `linkdir_silent_test.go` 加 `//go:build windows`；
  `hiddenTestCmd` 从无 tag 文件抽出，拆成 `headless_testcmd_windows_test.go`（真设窗口标志）与
  `headless_testcmd_other_test.go`（非 Windows 同签名直通，保住 4 个调用方文件的编译）。
  验证：Windows `go vet ./...`=0、Linux 容器内 `go vet ./...`=0；
  `DOCKER_GATE_PASS`（debian dash + alpine busybox **双腿各 102/102**、池门禁 go test PASS）。
  产物重建 9.80 MB / SHA256 `1D64BDCB…`。

### Fixed
- **仓库全库 Go 格式漂移 + 上闸（2026-10-22 实测：73/286 个假红根因是 CRLF，真漂移 52 个）**：
  本仓长期「CI 无 gofmt 门禁」，此前只记过一句「所以仓库存在长期格式漂移」。本轮把它挖到底：
  ① **直接原因是假红淹没** —— 仓库无 `.gitattributes` 且 `core.autocrlf=true`，工作副本全是 CRLF，
  而 gofmt 只接受 LF：对工作副本跑 `gofmt -l .` 得 **73 个**，干净 clone 上得 **286 个**（整文件重写）。
  实测反证：chain.go 归一化成 LF 后立刻为 0。② **但里面确实混着真漂移** —— 归一化后仍有 54 个文件
  需要格式化（config.go / main.go / html.go 等），全是 struct tag 与 composite literal 的对齐空格。
  两类混在一起，谁上闸谁被假红劝退，这就是门禁至今缺位的机制性原因。
  修：① 新增 `.gitattributes`：`*.go text eol=lf`（gofmt 口径）+ 脚本/yml/md `text=auto` + 二进制 binary；
  ② 新增 `tests/check_gofmt.ps1`：**把每个受跟踪 .go 归一化成 LF 再判**，只判代码格式、不判行尾
  （两者职责分开、互不掩盖），首跑点名 55 个真漂移；③ 批量 `gofmt -w` 修复 **52 个**（纯空白，
  已机器证明：253 个文件的「剥离全部空白后逐字节比对」0 处内容变化）；④ 接入 `build.ps1 3/6` 与
  `check_all` 独立一段，CI 侧待 `.github/workflows/ci.yml`（并行会话在途件）入库后再接。
  豁免机制（非静默跳过）：`tests/gofmt-debt.txt` 显式登记 2 个**并行会话在途**的 shortcut 文件
  （gofmt 会重排其文档注释结构 = 代改他人代码，本轮不碰）；门禁仍点名+计数，措辞用三态
  「PASS 但 N 个豁免未清」，绝不在有豁免时打「合规」。清零条件写进 debt 文件。
  变异验证：清空 debt → 判红并点名 2 个；注入未登记漂移（queue.go struct 对齐）→ 判红并点名该文件；
  恢复 → PASS。回归：`go build`/`go vet`/`go test ./...` 全 rc=0（52 个纯空白改动零行为变化），
  `check_all` 九段全 PASS/合规 SKIP。
- **预检总门禁断链：check_all 引用的 4 个门禁从未入库，干净 clone 里静默降级（2026-10-22 盘点实测）**：
  `tests/check_all.ps1`（受跟踪）把散落的单点门禁串成一次调用，但它 Join-Path 引用的
  `check_subsystem / check_spawn_silent / check_no_shrink / check_homepage` 全部 untracked ——
  而各段的前置检查是「脚本不存在 ⇒ SKIP 且 rc=0」，于是 CI/干净 clone 里总门禁**绿色地**只跑
  入库的那几条；本机全绿掩盖了「别人拿到的门禁更弱」这个事实。同族事故本仓在册：
  `check_snapshot_build.ps1`（git archive 导出 HEAD 后 go build 报 undefined，因 4 个依赖从未入库）
  ——那是 Go 侧，本次是脚本侧复现。
  修：① 四个门禁脚本 + `keepalive.ps1`（计划任务实际拉起的守护件，10-19/10-20 定稿，非在途件）入库；
  ② 新增机器判据 `tests/check_gate_links.ps1`：**受跟踪**脚本以代码行引用的每个 `tests\x.ps1`
  必须存在**且受跟踪**（注释行不算声明，与 check_noninteractive「窗口只取代码行」同口径；
  untracked 之间的引用不查，不误伤并行在途件），并已接入 check_all 作为独立一段 —— 断链从
  「静默 SKIP」变成「显式 FAIL」；首跑即点名 5 条（含它自己），入库后全绿。
  ③ 顺带修 check_noninteractive 一处误报：它把 `xtmp/oss/*`（对标 clone 的上游仓 scratch，
  .gitignore 已忽略、CI 里不存在）里上游脚本的 Read-Host 判成**本仓**违规（7 处）；skipDirs
  补 xtmp 后复扫 42 个在册脚本 0 违规。实测：check_all 八段全 PASS/合规 SKIP（rc=0）、
  check_gate_links 双向验证（先判红 5 条、入库后 rc=0）、check_snapshot_build 纯净检出编译仍绿。
- **newTestEngine 收尾等待仍失效（一修的反例，2026-10-22 本地 -cpu=1 复现）**：
  上一条（1852ac2）把等待写成「进入 newTestEngine 就 `go inflight.Wait()`、cleanup 时 `<-fin`」——
  **结构上不成立**：WaitGroup 计数为 0 时 Wait 立即返回，那个 fin 通道只反映「构造瞬间没有在途」，
  runOne 之后才 `Add(1)` 的 postProcess（engine.go:1243）永远等不到；CI 上 1852ac2 自己的 run
  （39733004935）随即同判据再红即为证据。本机普通跑不复现，用 `-cpu=1` 放大调度窗口后
  20 连跑 7 红（三个 plan 用例同根因）。修法：把 Wait 挪进 cleanup 闭包**内**才启动 go Wait()，
  等的是「测试跑完时真正在途的工作」；5s 有界兜底不变。修复后 20 连跑全绿、整包 -cpu=1 ×3 全绿。
  教训（与本仓「判据要直接测真实现」同型）：**修并发时序问题，必须先在能稳定复现的口径下
  证伪旧修法**（-cpu=1 / 高 count），否则「本机绿 + 结构错」的修法会上线再红一轮。
- **engine 包 CI 独有假红（第二次同型）：newTestEngine 收尾不等在途 postProcess，RemoveAll 被并发新增击穿（2026-10-22 实测）**：
  docs-only 提交 609135a 的 CI run 判红 `TestPlanModelDiffersFromExecutorRole: TempDir RemoveAll
  cleanup: ... workbench-output: The directory is not empty` —— 与「契约测试连红 8 次」同族：
  红与被推送内容无关，本机 20 连跑不复现（窗口关着），慢 runner 上才张开。
  根因链：runOne 成功路径把 postProcess（自评/摘要/链式后继）丢进 goroutine（engine.go:1244，
  e.inflight 记账），测试函数返回时它还在往 layout.TaskDir 写；而 newTestEngine 的收尾
  只关了 store 句柄，没排在途工作 —— RemoveAll 删到一半被新增文件击穿，报「目录非空」。
  修：t.Cleanup 增加一条**有界 5s** 的 inflight 排空（注册在 st.Close 之后 ⇒ 后进先出，
  先排空再关 store 再删目录，与生产 stop() 的两段 drain 同型）；等待不许挂死测试。
  实测：go test ./internal/engine -count=1 -race rc=0、-count=5 -race rc=0；
  诚实记录：本机 -count=20 下「摘掉排空」的变异体也不判红（窗口关着），
  判红证据在 CI 侧（docs-only 提交不可能改出功能回归，红点只能来自收尾竞态）。
  与上一条 dshprofile 修复同理：**这类红一律修测试收尾，绝不降判据、绝不重跑掩盖**。
- **CI 覆盖率门禁判红（build 5/6 步）：dshprofile 只有 patch 文案判据、环境构建链零覆盖（2026-10-22 实测）**：
  上一条修复把 test job 变绿后，红点下移到 **build + coverage gate**：`覆盖率门禁未通过：
  workbench/internal/dshprofile=1.4%<阈值2.5%`。根因不是阈值设错，是该包此前 3 条判据全在
  验 cordis.patch.yml 的文案，而 LocateDSH / ensureOFMData / ensureProfile / Bootstrap 这条
  「环境准备主链」没有任何测试。**处置是补真判据而不是降阈值**。
  新增 `bootstrap_env_test.go` 十条（外部依赖全走环境变量注入，CI runner 无 DSH Desktop 也能跑）：
  LocateDSH 优先 DSH_DESKTOP_EXE、CLI 文件缺失必须点名报错、resolveFromShim 双向、
  ofmSourceDir 认 index.js 不认空目录、ensureOFMData 落「caveman/ponytail=off + 32768」质量配置
  且已存在不重写、writeIfChanged 内容未动不碰 mtime、copyDir 嵌套、Bootstrap 端到端 + 二次幂等。
  **顺带抓到并修掉一个真 bug**：`resolveFromShim` 按 `"\\n"` 字面量（反斜杠+n）切分 shim，
  永远切不开 —— 整文件被当一行，首个引号落在哪全看 shim 写法；已装的真实 shim 恰好写法配合才正确，
  前置有多条带引号路径时**解析出错误 exe**。改为按真换行切分，并加反向判据
  `TestResolveFromShimSkipsEarlierQuotedPaths`（旧实现下必红：实测抓到它返回 "Junk First.exe"）。
  变异验证：把修复回退成字面量切分 → 判据 CAUGHT；恢复 → dshprofile 覆盖 1.4% → **79.5%**。
  实测：`go test ./... -cover` 29 包对照 PerPackageCover 全表零未达标、`go vet ./...` rc=0、
  `go test -race -count=1 ./...` rc=0。
- **远端 CI 连续 8 次假红：契约测试的收尾方式在 Windows 上不成立（2026-10-22 实测）**：
  远端 CI 自 08:46(+08) 起连续 8 次 red，失败点全部是同型的一句 ——
  `TempDir RemoveAll cleanup: unlinkat ...\TestSilentDoesNotOpenPanel*\002: The process cannot access
  the file because it is being used by another process`。其中**一次判红的提交是纯文档提交**
  （docs/changelog），说明红与被推送内容无关：`cmd/workbench` 契约测试把 t.TempDir() 当子进程的
  工作目录用，而 Windows 上目录里只要有进程还活着或句柄未释放就删不净 —— t.TempDir() 把「删不净」
  记成**测试 FAIL**，于是契约判据全绿、CI 却连红（门禁形同虚设，且没人发现）。
  处置（`headless_test.go` 新增三条可复用能力，`silent_contract_test.go` 改用它们）：
  ① `killTree` 收整棵进程树（taskkill /T /F）—— 旧实现只 Kill 直接子进程，收不回脱离进程树的后代；
  ② `reapSpawnedExe` 按**可执行文件绝对路径**精确回收残留实例（路径经环境变量传入，避免命令转义；
  只可能命中本测试自建目录下的实例，不误伤用户正在跑的实例）；
  ③ `newSpawnDir` + `removeSpawnDirWithin`：自管目录、重试删除、**删不净只留证不判红**
  （契约判据在收尾前已跑完入库，环境残留不该伪装成契约被破坏），并打印「CommandLine 命中该目录」
  的进程快照供定位。新增 `spawn_cleanup_test.go` 两条判据 + **变异验证**：
  M-A 去掉 taskkill → 孙进程判据 CAUGHT（实测报出 9.83s 失败）、M-B 把留证改回判红 → 容忍判据 CAUGHT。
  实测：`go test -race -count=1 ./...` rc=0（CI 同命令）、`go vet ./...` rc=0；顺带把本包两个被改文件的
  gofmt 漂移归零。
- **retry 策略非法值只静默钳制、无痕迹（2026-10-22 对标 river `job_executor.go:563-571`）**：
  `retry.Policy.nextDelay` 对「算出来的等待非正」一律钳到 **1ms** —— 那是热循环配方
  （高频失败时每秒重试一次，既打上游又刷台账）；且配置写坏与「值本来就小」在日志里完全同形。
  修：构造期跨字段矛盾（`MaxDelayMs < BaseDelayMs`、抖动越界、退避列表非正项）登记 `Policy.Fixes`；
  运行期算出非正等待时回落可信的默认指数退避并登记 `Policy.Warnings`（去重）；
  引擎在 3 处装配点（New / Recover / ReloadConfig）读 `Fixes` 写 WARN 日志。
  判据 18 条（含反向：合法配置零 Fixes/Warnings）；变异两条：删抖动塌陷兜底 → 判红（实测抓到 692ms 热循环值）、
  删 Fixes 登记 → 判红。retry 覆盖 89.1% → **95.7%**。
- **`config.Normalize()` 零跨字段校验（31 处钳制全是单字段）+ v3 迁移判据被版本号污染（2026-10-22 对标 river `client.go:528-534`）**：
  ① 新增 `normalizeCrossField`，三条**功能不可达**判据：`StallSeconds >= DefaultTimeout` 时看门狗
  永不可能先于硬超时触发（stalled 成死配置）→ 钳到超时一半；`ReplanRounds >= RepairRounds` 推翻
  「先用便宜手段」的设计顺序 → 钳到返工-1；`PlannerMaxMinutes >= DefaultTimeout` 使方案阶段挤占全部执行预算 → 钳到超时一半。
  每一条登记 `Config.Warnings`（不入盘，`canonicalizeForCompare` 里抹平，防无谓重写）。
  ② **实测发现既有缺陷**：`Normalize()` 开头把零值配置的 `SchemaVersion` 直接抬到 6，
  而 v2→v3 迁移分支的判据用的是这个**被改过的值** ⇒ 零值/半截配置永远不命中迁移，
  `appendProtocol` 停在 false、`repairRounds` 停在 0（P3 产物协议形同虚设）。
  这正是本文件记过两次的「版本号被提前抬升」的**第三处复现**；改为用项目自己文档里写的 `fromSchema`（进入时的版本）。
  ③ 反向判据：跨字段段**不得触碰自洽配置**（默认 420s 不得被改；一轮实测写反方向把默认抬到 1860s 被既有测试抓出）。
  ④ 一条判据写了又撤（`DailyCap <= Concurrency`）：它把「一天只产 1 条」这种**刻意收紧**当矛盾配置抬到 200，
  被两条既有测试当场判红 —— 跨字段校验只修「功能不可达」，不替用户调参。留作反例记录。
  判据 7 条；变异两条：摘掉跨字段调用 → 三条判据同时点名、v3 判据退回污染值 → 判红。config 覆盖 69.1% → **70.0%**。
- **失败与取消路径「状态已迁、台账未写」（2026-10-22 对标 asynq `processor.go:336-338`）**：
  ① `failTask` 改造前顺序是 `Update`(迁状态) → `Audit` ⇒ 存在窗口「状态已是 failed，台账查不到为什么」；
  更糟的是 `Update` 返回错误时只 emit 一条**瞬时事件（不落盘）**，任务停在 running、既不重试也不统计。
  ② `rescueInterrupted` 的判死分支**改造前零审计** —— 判死是不可逆终态决策，却没有任何可追溯台账。
  ③ `finishCancelled` 只在迁移**失败**时才写审计，正常取消只有瞬时事件（事后无法区分「用户取消」与「任务失败」）。
  修：新增 `writeLedgerThenTransition` seam（沿用本仓 `autonomy.writeLedgerThenActivate` 同型范式），
  三处全部改为「先留痕后生效」；`Store.Audit` 由 `void` 改为**返回错误**（改造前 `_ = s.audit.Append` 把失败全吞），
  审计写失败时退到任务事件流留痕（两级留痕面写在不同目录，同时不可写的概率极低），保证「至少一处留痕」是结构保证。
  判据 6 条；变异两条：seam 反序 → 判红（实测抓到 `[state ledger]`）、
  摘掉判死专用审计 → 判红（**关键**：`Transition` 自己也会写一条 kind=transition 的审计且文案重合，
  第一版判据只搜文案被一条变异放过，加严为**按 kind=error 过滤**后才真正抓住）。engine 覆盖 74.2% → **75.3%**。
- **卡死看门狗只认 stdout 事件，不认模型写盘（2026-10-22 对标 river `watchStuck` 后新增，会直接丢产出）**：
  `res.Touch()` 只在 `consume()` 解析到 stdout 事件时刷新活跃度。而 artifact 产出协议下模型常常
  「先静默写文件、最后才吐 final 事件」，长文写作类任务的静默写盘期可达数分钟 ——
  只认事件时，这类任务会在 `StallSeconds`（默认 420s）到期被判卡死并杀进程树，**已写出的产物一并丢失**（不可逆）。
  修：活跃度改双信号取较新者 —— ① stdout 事件 ② workspace 产出文件 mtime（`LatestOutputMtimeMs`，
  扩展名白名单与 `hasWorkspaceOutput` 同口径，目录不可读时退化为「无信号」= 改造前行为，不误判）；
  判定逻辑收敛到纯函数 `IsStalled`（可单测、可变异），看门狗 goroutine 只负责周期调用与善后。
  判据 5 条；变异一条：`IsStalled` 回退成单信号 → 判红。executor 覆盖 49.2% → **52.9%**。
- **审计/留痕顺序判据第一版测重了对象（2026-10-22 自查，防重犯）**：
  第一版在测试里**重写了一遍判据逻辑**（把「取较新者」在测试内实现一次），
  变异把实现回退成单信号时照样全绿 —— 判据根本没碰到被测代码。
  同一轮还踩到第二次：看门狗判据写成测试内联逻辑、变异无效；
  以及 `config` 变异「摘掉跨字段调用」第一版没真摘掉（编辑锚点里调用行仍在注释之后）→ 假绿。
  教训：**变异测试必须先在实现上制造真实回退、并确认回退后的行为差异**；
  判据要直接调用被测的纯函数/seam，不重写逻辑。已按此重写全部新判据并复跑变异。
- **假 running 无人认领：卡死救援只有启动时一次扫，无持续轮询（2026-10-22 第五项，对标 river `internal/maintenance/job_rescuer.go`）**：
  L4（stuck_rescue.go）覆盖「进程崩溃后重启」；但**进程活着而任务失去归属**
  （goroutine 未起/早退未收尾/状态与内存表分叉）时，面板显示「正在执行 N」而实际零在跑，
  这类任务既不重试也不统计、还永久占着 running 计数与其链（queue.Pop 会静默跳过 busy 链上的
  queued 任务，queue.go:107 ⇒ 回收了但取不出＝假入队）。
  修：新增 `internal/engine/runtime_rescue.go` —— 调度 tick 内每 30 秒扫台账（river 同款周期），
  连续 max(StallSeconds×2, 600s) 处于「台账 running 但本进程无 goroutine」才动手；
  **两阶段确认**（首扫只登记候选，二扫达阈值才回收）。中途改用任务 UpdatedAt 判静默的方案
  被实测否决：`store.Update` 在 mutator 后无条件覆写 UpdatedAt，无关写入会重置计时且判据不可测。
  四条安全边界各钉反向判据：在飞(goroutine 存在)不碰、取消中不碰、阈值非法不碰、登记表随任务
  离开 running 自动清理。回收动作释放链占用 + 与 L4 共用同一份 CrashRescues 额度（防双预算翻倍）；
  留痕一律 kind=error 审计先于状态生效（沿用本轮 ledger seam）。
  判据 8 条；变异三条全部判红：摘在飞门 →「误杀在飞」点名、摘两阶段 → 三条点名、
  摘 ReleaseChain →「busy=1 want 0」点名（第三首版断言写成 busy>1 曾放过变异，已加严 ——
  「占用没恶化」≠「被释放」，queue.Pop 的静默跳过会让弱断言假绿）。engine 覆盖 75.3% → **75.9%**。
### Added
- **慢后端加载反馈：骨架占位 + 采样按钮 busy（2026-10-22 CDP 限速实测缺陷）**：
  ① 切页签首帧整片空白 —— 实测（CDP 限速 1.2s）模板页 `pane.innerHTML` 长度 = **0**，
  1600ms 后才有内容，用户无从判断「正在读」还是「坏了」；② 治理台「立即采样」按钮
  **零反馈** —— +0/+700/+3700ms 三帧读数完全一致（文案与 disabled 全程不变），而采样要跑
  一次真实量测、可能数秒，用户只会当成「没反应」再点一次。
  修：新增 `loadingPane`（切页签时若面板仍空则先给骨架）与 `failPane`（**失败必须替换占位**，
  否则「读失败」会显示成「一直在读」—— 判据卷「Loading 兜底不得永驻」的另一半）；
  采样按钮补 `disabled + 采样中…` 并在 `finally` 复原；采样失败的 `alert()` 换成 `toast()`
  （原生弹窗阻塞界面且不属于本设计体系）。新增 `.skeleton` 骨架样式（合成层友好的背景位移）。
  新增 `loading_feedback_contract_test.go` 两条判据（断言**调用点**而非标识符；含「禁原生 alert」），
  变异验证 M1 占位调用 / M2 失败态替换 / M3 采样 disabled / M4 恢复 alert 全部 CAUGHT。

### Fixed
- **治理台 `toast is not a function` 运行时回归（2026-10-22，门禁当场判红）**：把采样失败的
  `alert()` 换成 `toast()` 时，漏了 gov.js **没有** toast 定义 —— 三张页面（app.js / pool.js /
  gov.js）是各自独立的脚本包，某一页的 helper 对另一页不可见。前端门禁 `interact-governance`
  实测报错：`errors=["reject: toast is not a function"]`、`runtimeErrors=1`，判定 **FAIL**
  （ok=22 fail=1）—— 这是本轮唯一一次真回归，且被门禁在发布前抓住。
  修：给 gov.js 补同款 `toast` helper；新增 `toast_helper_contract_test.go`
  （govJS 一旦调用 `toast(` 就必须有 `const toast =`；另两页同规则反向断言），
  变异验证「摘掉 gov 的 toast 定义」→ CAUGHT。
  复测：门禁回到 **ok=23 fail=0**（`interact-governance errors=0`），发布 1.5.8。

### Added
- **慢后端加载反馈**（同日实测）：切页签首帧整片空白（`pane.innerHTML`=0）→ 骨架占位；
  治理台「立即采样」零反馈（+0/+700/+3700ms 三帧读数一致）→ `disabled + 采样中…` 并在
  `finally` 复原；`alert()` → `toast()`。端到端复测：采样按钮 `采样中…/true` → 复原；
  模板页首帧由 0 → 126 字符「读取中」骨架 → 1600ms 后真实内容。

> ⚠️ 本轮**换件已完成**（1.5.8，SHA256 AB9DAD18…DAED），但记录一条过程事实：
> 本轮 build.ps1 先后被并行会话在途件阻断 **3 次**（retry.go copylocks → stuck_rescue.go
> 未使用 import → stuck_rescue.go 语法错，均为写入中途的瞬时态），另有 1 次全量测试
> 撞上写入窗口误报失败；最后一次官方链 7/7 通过。
> ⚠️ 发布历史（前轮欠账）：树曾被并行会话在途件连续阻断 —— (a) `internal/retry/retry.go` 的
> `Policy` 含 `sync.Mutex` 仍按值传参（`go vet` copylocks，官方链 3/6 步判红）；
> (b) `internal/engine/stuck_rescue.go` 未使用 import（`go build` 失败，实测 11:02 仍在编辑）。
> 本端改动自身 `go vet ./internal/web` rc=0、包内测试全绿、CHANGELOG 门禁合规；
> 端到端 CDP 复测待树可编译后补。

### Added
- **内联编辑器键盘可达性：Escape 关闭 + 焦点回退（2026-10-22 走查实测缺口）**：
  池内联编辑器（新增/编辑模型）与模板内联编辑器此前都只能靠鼠标离开 —— 池编辑器有「取消」
  按钮但**没有 Escape 绑定**，模板编辑器连「取消」入口都没有；键盘用户进得去出不来。
  两处同型补齐：① Escape 关闭（文档级 keydown，只挂一次，不随重渲染累积监听）；
  ② 关闭后焦点**精确退回触发按钮**（打开时记 `document.activeElement`，关闭时还回去）；
  ③ 模板编辑器补可见「取消」按钮（与池编辑器对齐）。
  端到端实证（CDP 真按键）：池 → 打开时 focus=BUTTON:edit，Escape 后 editorOpen=false 且
  focus 仍是那个 edit 按钮；模板 → 按钮列表 [保存模板, 取消]，Escape 后内容清空、
  焦点回到「新建模板」。
  新增 `keyboard_dismiss_contract_test.go` 两条判据（三构件齐备：Escape 绑定/回焦记录/取消入口；
  keydown 绑定不得随渲染累积）。变异验证：M-A 摘掉池 Escape → CAUGHT、M-B 摘掉模板取消 →
  CAUGHT、**M-C 摘掉回焦记录首轮 MISSED**（判据只断言标识符 `editorReturnFocus`，而声明行
  与关闭函数里都有它 ⇒ 收窄为断言赋值语句本身后才 CAUGHT，与 round 10 的 M2 同源教训）。

> ⚠️ 本轮**未换件**：官方 build.ps1 在 3/6 `go vet` 步失败，根因是**并行会话在途改动**
> `internal/retry/retry.go`（HEAD 无 sync.Mutex、工作区有，10:30 改）—— `Policy` 含 Mutex
> 仍按值传参，vet copylocks 判红。不代改他人在途件，也不把已知 vet 缺陷打进 24/7 活实例。
> 本端改动自身 `go vet ./internal/web ./internal/gov` rc=0、包内测试全绿。

### Fixed
- **`retry.Policy` 运行期状态迁出结构体 —— 解掉 go vet copylocks（官方链 3/6 步判红；2026-10-22，清掉上方 ⚠️ 的成因 (a)）**：
  在途改动给 `Policy` 内嵌了 `warnMu sync.Mutex`（告警去重表 + 抖动随机源），而 `Policy`
  在本仓是**按值传递**的（`engine/helpers.go` 构造字面量后交给 `New`）—— vet 判死三处：
  `retry.go:138 New passes lock by value`、`retry.go:179 assignment copies lock value`、
  `engine/helpers.go:37 call of retry.New copies lock value`。后果不止静态告警：
  每复制一份策略就多出一份独立告警表，`Warnings()` 读数随副本而异。
  处置：运行期状态收进 `runtime *policyRuntime`（指针，锁只此一份），配置字段仍可按值传；
  `New()` 一律重建运行期状态（构造器语义）。新增 `runtime_state_test.go` 三条判据 ——
  `New` 不得继承入参告警（旧实现在此条判红）、按值复制必须共享同一份告警表、零值策略不 panic。
  实测：`go vet ./...` rc=0、`go test ./...` rc=0、`-race` 绿；并把本次改动镜像到 HEAD 的干净
  worktree 复跑全量 vet+test rc=0（等价 CI 判据）。
- **治理台考核矩阵三态失真（2026-10-21 前端走查实测）**：`CheckItem` 只有 bool `Pass`，
  unverified 项被序列化成 `"pass":false`，而前端去读根本不存在的 `it.level` ⇒ 六格全渲染
  FAIL，与同一页头部「3 PASS / 2 FAIL / 1 未验证」自相矛盾。修复：Go 侧补显式
  `state`（ok/fail/unverified）字段，前端优先读 state 并保留旧形态回落；
  新增 `checklist_state_test.go` 两条判据（三态分布 ⇄ 计数自洽、序列化必须带
  state=unverified），并对真实源码注入缺陷实测 rc=1（判据可证伪，非恒真）。
- **模型池组内序号从 2 开始（2026-10-21 走查实测）**：调用方传 `i+1`、`poolRow` 内又
  `(i+1)` 双重加一。改为调用方传 0 基索引，序号列恢复 1,2,3。
- **总览 KPI 随事件流反复淡入（走查截图抓到半透明读数）**：SSE 每来一条事件即重建
  overview DOM，`kpiIn` 入场动画照播 ⇒ 读数闪烁。新增 `overviewAnimated` 门：入场动画
  只属于首帧，后续刷新走 `.grid.no-anim .kpi{animation:none}`。
- **成功率把「还没跑过」读成 0.0%（假读数）**：完成数为 0 时该格显示「—」，有分母才显百分比。
- **「运行中但今日产出 0」四重缺陷叠加（2026-10-21 老大实报）**：面板显示
  `引擎 运行中 ｜ 排队 0 ｜ 今日产出 0`，而实例确实活着。取证（逐条可复算）：
  当日 00:00:02–01:39:59 产出 2000 条自主草案，**标题与 Prompt 去重后都只有 1 个**，
  全部被评估器拒绝（综合分 48/54 < 阈值 55），01:39:59 之后当天再无任何新任务。
  四层根因：① `DeriveNextGoal` 在**上一代标题**上继续追加「 · 深化：X」，代际复利把目标标题
  顶到 31 rune / 78 字节；② `model.Truncate` 的 n 是**字节**而标题上限常量是 **rune**（80），
  中文 3 字节/rune ⇒ 89 字节截到 80 后回退合法边界正好停在 78 字节，**刚追加的阶段后缀被整体吃掉**，
  标题退化成目标标题本身、每轮逐字相同；③ 评估器 `novelty()` 对同目标不做区分，而自主 Prompt 固定注入
  「项目目标 + 已完成阶段」，同目标相邻阶段相似度天然 0.9+ ⇒ novelty=0 ⇒ 价值 41 / 综合 48 **恒不通过**
  （`gates()` 里早有同目标豁免，novelty 没跟上，同一语义两处不一致 = 豁免被抵消）；
  ④ `tasksToday()` 把 **rejected 草案也算进 dailyCap**，2000 条从未执行过的草案把配额吃满后
  两条供货链同时停机一整天。修复：新增 `composeTitle()`（按 rune 限长、焦点段整体保留）与
  `rootGoalTitle()`（根标题重拼，掐断复利）；`novelty()` 同目标只认精确指纹、
  `gates()` 把「精确重复（任何目标都拦）」与「模糊相似（同目标豁免）」分开判；
  `tasksToday()` 不计 rejected 且模板链统一走它；新增 `rejectionPressure()` 拒绝熔断（阈值 5，
  连续被拒即停手并留痕）。**变异验证四条**：恢复旧码后对应用例逐条判红、复原后判绿。
- **静默停摆自证 `StallReason`（2026-10-21）**：`internal/engine/stall_reason.go` 把
  「为什么此刻没有产出」归纳成 8 个稳定原因码 + 人话文案（结构性阻塞 > 限流 > 配额耗尽 >
  规划熔断 > 目标阻塞 > 池冷却 > 无来源），经 `/api/status` 的 `stallReason` 输出，
  面板总览渲染 `stall-banner`、顶栏把原因顶到读数位置。原则：有产出时**不渲染**（不制造噪音）。
  根因是 24/7 最坏的失效形态——进程活着、phase=running、没有任何错误，只有产出是 0。

### Fixed
- **390px 窄屏管理面板表格列宽塌陷（2026-10-21 窄屏走查实测）**：`table-layout:fixed`
  把 6-8 列均分成每列 ~48px，「价值先验」折成 4 行、「状态」列跑位。修法：管理面板全部
  表格补 `data-label`（排队/执行中/产出/模板/模型健康度/失败原因六张表），≤480px 切
  卡片模式（thead 隐藏、tr 变卡、td 前置字段名，复用池表已验证范式）。
  取证：隔离实例 390px `theadVisible=0 / labeledTd=48`、队列 4 行转卡（row display:block）。
- **治理台「风险预警」文案读数不可扫 + round2 负数舍入方向错（2026-10-22 接续实测）**：
  上一轮只修了「量化指标」卡（展示层 govNum），但「风险预警」卡的 Detail 文案由
  `gov.VolumeAlerts` 在**数据源头**用 fmt.Sprintf 直接拼数字，展示层修不到它，
  于是同一页两处读数格式不一致：`任务失败率 当前 0.51比例`（单位粘连 + 量级读不出）、
  `日吞吐量 …（占用率 400%）`（低方向指标越小越坏，报 400% 会让人以为超载，
  真实含义是「只达红线要求的 25%」）。修：新增 `gov/metric_text.go`
  （numberText 千分位 / percentText 按量级取精度 / metricValueText 比例换算并吞掉
  单位词 / usagePhrase 按方向说人话），Detail 与所有消费面同源格式化；并去掉 Detail
  里重复的指标名前缀（面板 Title 与 Detail 并列渲染，「并发执行数 并发执行数…」念两遍）。
  顺带抓到 `round2` 既有 bug：`float64(int64(v*100+0.5))/100` 是向正无穷舍入，
  负数算错方向（-1234.5 → -1234.49）；本包 pearson() 相关系数取值域 [-1,1]，负相关
  是真正常态，故非理论问题。改用 math.Round。由 metric_text_test.go 千分位用例抓到。
  实测：预警三条 = 「当前 51%，警示线 7.0%，红线 10%（达红线 510%）」
  「…仅为红线要求的 38%」，页内浮点噪声/「比例」粘连/旧「占用率」措辞扫描均为 0。
  新增 5 条判据（千分位/百分数量级/比例吞单位/占用率方向语义/Detail 不重复 Title），
  变异验证 M-A 退回带前缀 → CAUGHT、M-B 比例退回裸小数 → CAUGHT。
- **治理台「量化指标」读数不可扫（2026-10-22 展开态走查实测）**：三类问题同源——
  数值原样拼进 HTML。① 阈值带 IEEE754 噪声：`Warn`/`Red` 未 round（阈值多为
  `max*0.8` 算式得出），面板显示「开发执行数 警示 4.800000000000001」
  「任务失败率 警示 0.06999999999999999」共 20 位裸浮点；② 比例类指标显示裸小数
  （`0.51` / 红线 `0.1`），读不出「51% 失败率」的量级；③ 大数无分组（`4000000 元`）。
  修：Go 侧 `Warn`/`Red` 与 `Value` 同精度 round2（源头修，不在展示层掩盖）；前端新增
  `govNum`（比例→百分数且阈值同步换算、≥1000 千分位分组）。实测：页内浮点噪声扫描
  0 处，「任务失败率 51% / 警示 7.0% / 红线 10%」、「单契约金额 警示 4,000,000」。
  新增 `eval_readability_test.go` 两条判据（13 项 × Value/Warn/Red/Ratio 小数位 ≤ 2 +
  占用率方向语义）；变异验证 M1 退回不 round → CAUGHT，M2「Value 也不 round」首轮
  **MISSED**（样本字段经 round2 前后无差别 = 覆盖面不足），补长小数 MemMB 后 CAUGHT。
- **已完成任务的产出在面板显示「（暂无产出）」、批量导出静默跳过大半任务
  （2026-10-22 交互面走查实测，功能级缺陷）**：详情接口与导出都硬编码
  `ReadTaskArtifact(id, "final.md")`，但产出文件名由 `engine.choosePrimary`
  按 outputFormat 与体积决定（实测样本 `article.md`），`final.md` 只是
  「模型一个文件都没写」时的兜底名 ⇒ 绝大多数任务读不到内容。
  实测读数：outputBytes=5935 的任务，面板 `.doc` 只有 6 个字符（就是那句占位文案）。
  修复：新增共用解析器 `readTaskArtifact`（三级回落：① `task.OutputFile` 绝对路径
  ② 目录 final.md ③ 目录内最大 .md/.txt，且拒绝 0 字节文件），详情与导出同源调用；
  接口另回 `contentFrom` 标明实际命中的文件。修复后面板 `.doc` 91px→620px、
  6 字符→2040 字符（7 个 h2 + 5 个 li 真渲染、无原始 markdown 泄漏），
  导出 79 篇 / 311KB / 零空壳。新增 `task_artifact_test.go` 8 例（三级回落 +
  目录不存在 / nil task / 0 字节三个反向），并对真实源码注入 3 个变异体实测判红：
  M1 退回硬编码 final.md、M2 空文件算命中、M3 扫描不过滤扩展名（会读到 events.jsonl）。
- **导出文档的领域标签与面板漂移（md / txt / docx 三条路径）**：面板表格已中文化
  （app.js 的 DOMAIN_TEXT），但导出在 Go 侧拼元信息行、仍写裸枚举 ⇒ 同一份数据
  面板显示「内容创作」、导出文档里是「content」。新增 Go 侧 `domainLabels` +
  `domainLabel()`（未知枚举原样返回以便发现缺项，不吞数据），三处导出接线；
  `domain_label_test.go` 把「两张表同源」做成构建期判据（正则抠 app.js 的
  DOMAIN_TEXT 与 Go 表逐键比对，双向差集点名），注入「值漂移」与「缺键」
  两个变异体均实测判红。修复后导出 79 篇领域行 100% 中文、英文泄漏 0。
- **首页主体尺度不随画布呼吸（2026-10-22 超宽屏走查实测）**：主按钮与入口条固定 320px，
  在 2560/3440 宽下取景框与雷达环都在放大、唯独主体不变，主体占比塌到 13%/9% ⇒
  「精密仪器」变成「仪器里的小按钮」。改为 `clamp(320px,26vw,460px)` + 高度
  `clamp(120px,11vw,164px)` + 入口条字号/行高同步。CDP 逐档实测阶梯：
  390→320(82%) / 1024→320(31%) / 1440→374(26%) / 1920→460(24%) / 2560→460(18%)，
  六档（含 740×360 矮屏，高度自动收到 90px）零溢出、主体全部在视口内。
  系数取 26 而非 16 的原因：16vw 在 1440 只有 230px、被 320 下限兜住，
  会让 1440-1920 整段成为「占比从 22% 掉到 17% 却一点没放大」的死区。

### Added
- **首页入场序列「镜头推进」（2026-10-22 动效篇补强）**：获奖级第一屏的标准是叙事性入场，
  此前首页只有常驻呼吸、打开即终态。新增三段式 1.5s 时间轴：环境光从景深亮起（blur 6px→0）
  → 主按钮推近 + 点亮（translateY 14px + scale .965 → 落定，.28s 起）→ 入口条落位（.62s 起）。
  两条实现纪律：① 全部驱动 transform/opacity/filter（合成层属性，不触发 layout）；
  ② **动画顺序即优先级** —— `animation-play-state:paused`（hover）只暂停列表第一个动画，
  故常驻 respire 放第一位、入场放最后，用户在入场途中 hover 不会把推近卡死。
  实证（CDP 逐帧）：80ms 暗场仅环境网格、300ms 主体落定 + 入口 op 0.84、700ms 全落位；
  `prefers-reduced-motion` 下降级为静态完整态（animDur=1e-06s 且 opacity=1，不卡透明）。
- **修复「同规则块两条 animation 声明互相覆盖」**：`body.home::before` 的入场与常驻 bloom
  原为两条独立声明，后者整体覆盖前者 ⇒ 入场被静默关掉（走查实测才发现 bloom 仍在、
  入场没跑）。合并为同一 animation 列表（`homeStageEnv …, bloom 12s … 1.5s infinite`）。

### Fixed
- **官方 build.ps1 全链首次跑通（2026-10-22）**：此前 2/6 资源步必失败（app.rc 缺件 +
  `$ErrorActionPreference="Stop"`），且 6/6 后有一个孤儿 `} else {`（首页门禁块误植了
  子系统的内联兜底，PowerShell 解析报错）——两个缺陷都因为「从没跑过 2/6 之后」而潜伏。
  修：2/6 改为「有 rc 才 windres，无 rc 则校验 git 跟踪的静态 syso 含 VS_VERSION_INFO」；
  孤儿 else 归位到子系统 if 块；dshprofile 覆盖率按实测基线设分档阈值 2.5
  （18 函数里只有 profilePatch 是纯函数已 100%，其余为真实 IO 型，与 tray/single 同型）。

### Added
- **获奖级前端走查第一轮（2026-10-21，八维自检）**：以 Awwwards/Webby/FWA 标准逐页目检四页
  （首页/管理台/模型池/治理台）× 三视口，落地：① 治理台 `.gov-card` 补基座样式（此前只有
  反光没有盒体，卡片呈裸文字漂浮）；② 首页第一屏补品牌铭牌（伪元素承载，不破「极简首页」
  DOM 契约，宽屏走取景框层、窄屏保留铭牌）；③ 表格领域中文化（code→代码生成 等六域，
  搜索同时认中文）；④ 数字列右对齐 + 操作列 `td.ops` 原语（替代 4 处把按钮拉成列的内联
  `td.row`）；⑤ 微交互补齐：页签悬停下划线预览、折叠标题悬停指示点变信号色、治理校验格
  悬停微抬、菜单/折叠/toast 存在感动效、输入 focus 光晕统一；⑥ 卡顶巡游光降亮（.95→.55）
  放慢（9s→11s）、KPI 角标阴影收进格内；⑦ 池页悬停残留旧信号橙 → 电青（全站唯一强调色）；
  ⑧ theme-color 三页统一 #04070d（与冷底一致）；⑨ 空态卡壳治理：模板页空 editor 卡不再
  常驻，编辑器展开时自带卡壳；⑩ Escape 关闭「更多」菜单 + toast 补 role=status 播报。
  验证：go vet 0、go test ./... 29 包全绿、frontend-gate 23/23 PASS、design-audit 15 页
  低对比 0 / 焦点缺失 0 / 溢出 0（最低对比 5.29:1）。
- **双角色闭环：思考模型只出方案，agent 模型负责执行（2026-10-21 老大立规）**：
  立规原话「思考模型只出方案，agent 模型负责执行，不然只出方案没有执行落地的模型无法闭环」。
  改造前每个任务只调一次模型、task.Prompt 原样喂过去：有推理优势的模型被当写稿机，
  擅长执行的 agent 模型拿不到任何结构化指令，验收失败只能同一模型重做（方案层面错了永远修不掉）。
  新增方案层 `internal/blueprint`（提示词 → 解析含截取降级 → 渲染执行指令 + 路径/解释器白名单校验）
  与引擎编排 `engine/plan_stage.go`（选模型、调用、落盘、退化）；`runOne` 改为
  **方案阶段 → 渲染执行指令 → 执行阶段** 两次独立调用，方案模型与执行模型可分别指定
  （`plan.plannerModels` / `plan.plannerTier`，默认 quality）。
  闭环分三级、顺序写死在 `completeTask`：返工有余量 → repair（改实现，便宜先用）；
  返工用尽且重规划有余量 → **replan**（`engine/replan.go`，回到方案层改前提）；
  两者用尽 → degraded。方案层自身失败（429 等）**不判任务死刑**：退化直执行 +
  `PlanDegraded=true` 落盘，面板可区分「方案跑过」与「方案挂了」。
  配置面新增 `plan` 段（enabled/plannerModels/plannerTier/plannerMaxMinutes/replanRounds/
  appendPlanToPrompt），`schemaVersion 5 → 6`，老安装零值由 `NormalizePlan` 按开启补齐。
  证据：新增 `--plan-selftest` 离线自检（桩执行器跑全链，12/12 PASS，不耗额度）+
  21 条单测 + 黑盒分发契约测试。详见 `docs/双角色闭环交付报告-2026-10-21.md`。
- **产物体积归因 + 双向体积护栏 + 清单/趋势双收口（issue #8 四判据收口，2026-10-21）**：
  ① **体积归因**：两天 +83.3%（7,747,072 增至 14,199,296 B）主因 = **在未提交合并工作区上构建**
  （模型池分组 UI、assets 重写等在途改动全部进了旧 EXE），非依赖/代码膨胀；提交态
  （61decb5）构建为 9,650,176 B，对照 10-18 基准真实增量 +24.6%（四项改造 + Prometheus 导出 + 前端改动），
  归类**预期**。② **exeMinBytes 双向判**（判据 3）：基线加 exeMinBytes=7,747,072（=10-18 基准原值，
  锚定上一代完整功能），check_perf.ps1 增判定 1b，ci.yml 的 3MB 硬编码下界改引基线；
  跌破下界=产物缺重大组件（资产丢失/误裁包/半成品），与膨胀同为回归、只是方向相反。
  ③ **SHA256SUMS 读回校验**（判据 2 后半）：CI Verify artifact 步独立比对清单 hash 与实测 hash，
  漂移判红不允许跳过（setup/build.ps1 构建后强制重写清单已在 HEAD 就位）。
  ④ **perf 趋势时间序列**（判据 4）：每次 CI 把 verdict+体积读数落盘 perf-trend/perf-verdict.jsonl
  并 upload 成 artifact（命名含 run_id 防覆盖、retention 90 天）——上界拦翻倍级膨胀，
  趋势拦每次涨 5% 的缓慢贴边。
- **实例回收 + 托盘窗样式修复（2026-10-20 现场两连）**：
  `tests/instance_guard.ps1`（机器级实例盘点 + 有界回收，六条裁决：生产实例只留 1 个 / 用户显式退出后残留 /
  探针超预算 / 探针无预算超 TTL / 全局硬上限 MaxInstances / 探针父死兜底），接入 `tests/keepalive.ps1` 每分钟一跳，
  每次真清理留痕 `runtime/instance-guard.log`。根因：四条拉起链（Run 键 / 计划任务 / SelfHeal / 探针）
  + 单实例锁按 runtime 目录 + 无任何回收者，且 Win32_Process.Create 让「父死子退」失效 —— 现场实测 24 个孤儿实例。
  同时修 `internal/tray/tray_windows.go`：托盘消息窗补 `WS_EX_TOOLWINDOW` + `WS_POPUP` +
  `SWP_HIDEWINDOW`（原先 dwExStyle=0，导致每个实例在任务视图/Alt-Tab 留一张关不掉的空白卡片，
  实测 `ex=0x00000100 TOOLWINDOW=False`），标题改为「内部消息窗口，请忽略」，类名提常量并加 4 个守护测试。
- **首页契约门禁**：`tests/check_homepage.ps1`（产物必须含新页锚点 `home-entries`、不得含旧页锚点 `home-badge`、
  必须有 `UI_PROBE_READY` 通道，且真探针抓 HTTP 首页同为新页），已进 `setup/build.ps1` 构建链与
  `tests/check_all.ps1` 总预检。根因：21:52 的 dist 产物是从旧树构建后覆盖进来的，服务的是旧首页且丢了
  `--ui-probe` 分支，而当时的门禁只看 PE 子系统 + 体积，没有一条判据看页面内容。三态自证 rc=0/1/2。
- **Prometheus 文本格式指标导出（P1-3）**：`internal/web/metrics_prom.go` 提供 `GET /metrics`
  （exposition v0.0.4，纯标准库），把任务状态分布、尝试总数、**按 ErrorKind 的失败分类**、
  任务耗时 sum/count 与进程侧 goroutine/堆/GC 暂停投影成监控系统可直接抓取的形式。
  三条硬约束写在结构里而非靠自觉：样本只能挂在已登记族上（`# HELP`/`# TYPE` 成对写出，
  重复登记复用同族）；浮点经 `promSanitizeFloat` 兜底，绝不产 NaN/±Inf；标签名走白名单、
  标签值按规范转义且非法 UTF-8 先修正、超长按 UTF-8 边界截断。零任务时每个合法状态仍输出
  0 值序列（采集端可预先建面板，不必等序列凭空出现）。新增 8 条判据含逐行 exposition 校验
  与「同 (name,labels) 重复序列」检测（变异测试：删 `# TYPE` 与改回两条同名样本均判红）。
- **性能基线护栏（B8 / L8）**：`docs/benchmark/perf-baseline.json`（数据件，含 provenance、
  上界推导与三个 null 字段的原因）、`docs/benchmark/性能基线口径.md`（逐字段取证命令与三轮
  原始读数）、`tests/check_perf.ps1`（229 行三态门禁：0=PASS / 1=FAIL / 2=UNVERIFIED，
  「没测成」绝不当成「测得过」），并把 `ci.yml` 的产物体积上界从硬编码 40MB 改为引用基线、
  新增 `Perf baseline gate` 步骤。实测三态齐验（真实产物 rc=0、缺参/缺文件 rc=2、
  变异上界 rc=1，基线按 SHA256 逐字节还原）。
- **开源对标报告入主树**：`docs/benchmark/WorkBench-开源对标报告.md`（8 个 peer：
  n8n / dify / AutoGPT / autogen / temporal / river / asynq / windmill，八维对比 +
  P0/P1/P2 清单 + M0-M4 路径），此前只存在于受 `.gitignore` 屏蔽的 `src-backup/`。
- **知识盲区与学习计划**：`docs/知识盲区与学习计划.md`，B1-B8 盲区绑容器实测读数，
  L1-L9 学习路径含精读对象与机器判据。
- **开源化三件套**：`ARCHITECTURE.md`（单页定位/进程模型/包结构/五条架构取舍）、
  `.github/workflows/ci.yml`（vet + `go test -race` + 覆盖率门禁 + 产物校验）、
  `autonomy-e2e.yml`（容器内确定性通道跑完整自主闭环）。
- **issue/PR 协作模板**：`.github/ISSUE_TEMPLATE/`（缺陷报告 / 功能请求 / config.yml）、
  `.github/pull_request_template.md`（判据必须贴命令与真实输出）、
  `.github/labels.json` + `setup/labels.ps1`（12 个正交标签）。
- **零弹窗拉起共用件** `tests/lib_silent_launch.ps1`：按 PE 子系统分流，
  GUI 走 `Win32_Process.Create`（保持脱离作业对象），CONSOLE 走
  `Start-Process -WindowStyle Hidden`。
- **崩溃救援额度（L4）**：`model.Task.CrashRescues` 记录跨崩溃的救援轮次，
  `internal/engine/stuck_rescue.go` 的 `decideRescue` 分流「重新入队 / 判死」，
  状态机新增 `interrupted → failed` 出口。修的是**无界崩溃循环**：
  原先 `Recover()` 把 interrupted 无条件转回 queued，而 `Attempts` 是单次 `runOne`
  内的序号（每轮被覆盖），不能当崩溃预算 —— 于是"一跑就弄死进程"的任务
  配合 KeepAlive 每分钟拉起会无限重投，看起来"一直在干活"实际零产出。
  判死写 `ErrorKind=crash_loop`，面板失败原因分布可区分。
- **社区协作面（L7）**：`.github/ISSUE_TEMPLATE/`（缺陷报告 8 字段 / 功能请求 7 字段 /
  config.yml）、`.github/pull_request_template.md`、`.github/labels.json` +
  `setup/labels.ps1`（12 个正交标签，含本仓特有的 `flaky` / `platform-coupling` /
  `external-constraint`）。模板把本项目的判据文化写成必填项：
  「取不到数写 UNVERIFIED 不要填 0」「必须贴命令与真实输出」「外部约束另打标签」。
- **三条仓库级门禁（已挂 CI 的 Repo hygiene gates 步骤）**：
  `tests/check_changelog.ps1`（Unreleased 必须居首 / 日期必须 YYYY-MM-DD / 版本降序）、
  `tests/check_noninteractive.ps1`（禁 `Invoke-WebRequest` 缺 `-UseBasicParsing`、
  计划任务缺 `-NonInteractive`、以及 Read-Host / Get-Credential / -Confirm 等交互构造）、
  `tests/check_issue_forms.ps1`（Issue Forms 结构：顶层三键、每块 attributes.label、
  dropdown/checkboxes 的 options 必须位于 attributes 之内、contact_links 必须用 about）。
  三条判据均经**变异测试**验证能抓回对应缺陷（不是"跑过了"就算数）。
- **CONTRIBUTING 新增三节**：九 协作流程与判据文化、十 零弹窗纪律、十一 延伸文档。

### Fixed
- **无头执行：测试不再弹窗、测试不再被实例守卫杀掉（2026-10-21 老大指令「不要给我跳出来窗口，无头执行」）**：
  ① **产品层**：`internal/browser` 新增 `WB_HEADLESS`（或 `CI`）判定，命中即**不启动任何浏览器**；
  Edge/rundll32 回落路径补 HideWindow + CREATE_NO_WINDOW；`cmd/workbench` 新增 `--headless`
  （与 `--silent` 同为"不弹面板"，保留旧名是因为既有计划任务已用它）与 `headlessEnv()`
  环境层兜底（`openPanel` 先判 env —— 某处分支漏判 silent 也不会污染桌面）。
  实测：`WB_HEADLESS=1` 起实例，msedge 窗口数增量 **0**。
  ② **测试层**：`hiddenTestCmd`（HideWindow + CREATE_NO_WINDOW + 注入 `WB_HEADLESS=1`）
  接管 `cmd/workbench` 全部子进程 spawn。
  ③ **找出并修掉"测试随机红"的真因**：`go test -c` 编出的测试二进制同样带
  `ProductName=个人AI工作台`（rsrc 被链接进去），命令行又不含 `--ui-probe`/`--soak`
  ⇒ `tests/instance_guard.ps1` 判成**"生产实例重复"** ⇒ 每分钟的 keepalive(`-Apply`)
  **直接 kill 它**。现场表现：`go test ./cmd/workbench` 隔几次整包
  `exit status 0xffffffff`、日志无线索（"测试假红"会让真缺陷被当噪声）。
  修法：Go 测试二进制特征（`-test.` 参数 / `*.test.exe`）纳入"非生产"判据，
  按预算/TTL 回收，绝不按"生产重复"杀。**实测：修复前 2/5 随机红 → 修复后 6/6 全绿**。
- **限速闸门补构造期接线（D9 自查补漏）**：上一批写的是「`New`/恢复/`ReloadConfig`/`SetRunner` 四接线」，
  实测当时只有 3 处调用点（`New()` 里没有）。查的过程发现真实缺口：
  `cmd/workbench/main.go:419` 是第二个引擎入口（一次性 CLI 自检，只 `engine.New` 后直接
  `SelfTestArtifactPipeline`，全程不调 `ReloadConfig`），那条路径跑到真模型时不受任何速率约束。
  补 `New()` 内的 `applyRateLimit`（幂等：主路径随后 `ReloadConfig` 再装一次只是替换令牌桶，
  且 `New`→`SetLogger`→`ReloadConfig` 之间不发请求）⇒ 调用点 3 → 4，表述名实相符。
  新增 `newBareEngine`（复刻该 CLI 路径）+ `TestRateLimiterWiredAtConstruction`。
  **变异验证**：撤掉 `New()` 接线后只有这一条判红、其余 4 条限速用例仍绿 ⇒ 证明它不是
  `ReloadConfig` 用例的重复劳动，而是唯一覆盖 CLI 路径的那条（`0ae60ff`）。
- **语义层绕过执行器（缺陷 D7）**：`judge.run()` 原先每次自己 `executor.New(...)`，于是
  ① 单元测试注入的假执行器（`engine.SetRunner`）管不到语义子任务 —— `go test` 会真的拉起
  DSH Desktop 子进程（取证：`t.TempDir` 残留 `tasks/summary-task-done-e2e` 与
  `tasks/critique-task-done-e2e`，本机同时堆积 20+ 个 DSH 进程；真实单任务 65-148s 而测试
  只 `WaitInflight(5s)` ⇒ 后处理仍在写 `events.jsonl` 时用例结束，Windows 下 TempDir 的
  RemoveAll 报 "used by another process"，engine 包判红、容器与 CI 却绿）；
  ② 生产路径绕过模型池 —— `main.go` 装在 `e.runner` 上的 FailoverRunner（故障转移 + 车道
  健康度）与全局限速闸门对语义子任务全部失效，每个任务额外 2-3 次裸请求不受任何约束；
  ③ 执行钩子的尝试日志对语义子任务失效。修复：`judge.SetRunner` + `engine.SetSemanticRunner`，
  引擎构造与 `SetRunner` 时同步注入。
- **限速闸门生产从未接线（缺陷 D9）**：`internal/ratelimit` + `executor.SetLimiter` + 面板
  `RateLimiterSnapshot` 三件套 2026-10-19 就写好了，但实测**生产没有任何调用点**
  （`ratelimit.New` 全仓仅出现在测试里），`requestsPerMinute=20` 是装饰品 —— 面板显示限速、
  实际不限速，正是 08:00-08:04 每分钟 45-70 次请求打爆免费车道的结构性原因。修复：新增
  `internal/engine/runner_wiring.go` 的 `applyRateLimit`，沿装饰器链下探装配，`New`/恢复路径/
  `ReloadConfig`/`SetRunner` 四处接线；配置热更新会真的改变发出速率。
- **事件文件句柄生命周期（缺陷 D8）**：`executor.run()` 用 `defer eventsFile.Close()`，而
  `consume` 协程可能在 `Run` 返回后仍在写（`wg.Wait` 有 15 秒超时兜底）—— 对已关闭文件继续
  Write 的错误被 `_` 吞掉，事件尾部静默丢失。改为 `sync.Once` 包裹的 `closeEvents`：所有早退
  路径（pipe 接管失败 / 隔离环境准备失败 / `cmd.Start` 失败）由 run 的 defer 释放，正常路径由
  consume 自己收尾，超时兜底路径主动关闭两个读端解阻塞并再给一次有界等待。
  新增 `events_lifecycle_test.go`（含变异测试：撤掉 defer 后改名判红）。
- **自评解析接受无关 JSON（缺陷 D10）**：`extractJSON` 抓「第一个平衡 JSON」，模型回答里若含
  产物协议 manifest（`{"files":[...],"verify":[...]}`）等其它 JSON，`Unmarshal` 到 `Score`
  会全字段零值却返回 nil error ⇒ 自评质量被误判为 0 且 `degraded=false`，上层以为是模型的
  诚实评分。后果链：质量 0 → `ShouldExtend` 判否 → 链不延伸；自主验收按 quality 门槛判失败
  → 闭环不推进（`TestClosedLoopThroughRunOne` 实测红 5.57s）。修复用 key 存在性判据（不是数值
  范围），`quality=0` 仍是合法评分；反向用例守护收紧不误伤。
- **构造期告警永久静默（缺陷 D12）**：`engine.New()` 里 `e.log` 先是 Nop，而 `main.go` 在
  `New()` **返回之后**才 `SetLogger` —— 于是构造期所有「失败只告警不阻断」的告警（工作目录
  越权审计打不开、自主子系统初始化失败）写进 `io.Discard`。现场表现为自主链路没起来、
  `workspace-audit.jsonl` 始终为空，但日志与面板里一条原因都查不到。修复：`warnDeferred`
  缓冲 + `SetLogger` 冲刷（清空后重复 SetLogger 不重写）；同时删掉 `New()` 里那行自杀式的
  `e.log = logging.Nop()` 重设。
- **面板日志拿不到 component（缺陷 D11）**：`logging.At()`/`With()` 只把固定字段加到 slog 侧，
  handler 通道（面板 `/api/logs` 走这条）的 `Record` 另起一路、仅从本次 kv 构造 —— 落盘 JSONL
  有 `component`、面板读数却没有，`ARCHITECTURE.md` 承诺的「component 字段可按来源过滤」在
  面板链路上是假的。修复：`Logger.attrs` 自存一份并在 emit 时并入（本次 kv 覆盖同名键，与
  slog 语义一致）；`copyAttrs` 返回独立副本，防兄弟子日志器串字段。logging 覆盖 82.8% → 93.9%。
- **无人值守弹窗挂死**（老大 2026-10-19 17:29 反馈"这个东西后台静默不要弹出窗口"）：
  守护计划任务每分钟拉起 `tests/keepalive.ps1`，其中一条 `Invoke-WebRequest` 缺
  `-UseBasicParsing` —— PowerShell 5.1 因此调用 IE 引擎解析响应页，而面板页含 `<script>`
  （实测 2 处），触发 "Security Warning: Script Execution Risk … Do you want to continue?"
  等人工按键；同时任务注册命令行缺 `-NonInteractive`，两者叠加 = 桌面弹窗 + 任务永久挂起。
  修复：`-UseBasicParsing` + `$ConfirmPreference="None"` + `$ProgressPreference` +
  注册补 `-NonInteractive` 与 `Settings.Hidden`；新增 `tests/check_noninteractive.ps1`
  静态门禁（已挂 CI，变异测试验证可抓「只在注释里提 flag」的洗绿形态）。
- **自主闭环决策留痕顺序**（issue #1）：`autonomy.advanceLocked` 与 `bootstrapLocked`
  的恢复路径原先「先切活跃目标、后写 nextGoal 决策」，而两者写不同文件
  （goals.json / decisions.jsonl，非原子），留下一个可观测窗口 —— 面板与
  `--selftest` 判据 7b 会看到「目标已切换但台账无推导记录」，CI 上
  `TestRunOneDrivesAutonomyClosedLoop` 的偶发红即源于此。现收敛为
  `writeLedgerThenActivate`（先留痕后生效），并由顺序守卫测试钉死。
- **非 Windows 删除无安全阀**（issue #2）：`retention.Trash` 的 `!windows` 分支此前是裸
  `os.RemoveAll`，危险路径判定只存在于 Windows 分支 —— 实测 `Trash("/")` 在 Linux
  会递归删掉文件系统根。现 `refuseDangerousPath` 上移为平台无关实现，两条删除路径同过闸。
- **产物子系统门禁**：`setup/build.ps1` 校验 EXE 的 PE `Subsystem` 必须为 2(GUI)，
  非 GUI 直接判红。控制台子系统 EXE 被计划任务/`Win32_Process.Create` 拉起时
  Windows 必分配可见控制台窗口（桌面反复弹窗的根因）。
- **测试硬编码 Windows 工具链**（issue #2）：engine / verify 的 10 个用例改为解释器探针
  （pwsh→python3→python→node）；Windows 上全缺时 `t.Fatal`，禁止 `t.Skip` 洗绿。
  Linux 容器 `go test ./...` 从「3 包 10 用例 FAIL」转为全绿。
- **失败判据被偷换**：`TestMaterializeArtifactsFail` 原先只断言 `Pass == false`，
  而解释器缺失导致的「启动失败」同样 Pass=false —— 测试绿但什么都没验。
  现补断 `FailReason` 必须含「退出码 2」。

### Changed
- **仓库卫生：可再生产物入 .gitignore（`bce8e53`）**：`git ls-files --others` 实测 **53,751 个
  untracked**（xtmp 22,397 / dist 13,400 / `.workbuddy/gocache` 13,353 / runtime 3,792 /
  `tests/docker-autonomy/evidence` 507+）⇒ `git status` 一屏滚不完，「目录卫生」类判据读数全是噪音。
  只忽略可再生的缓存与跑出来的产物；**不忽略整个 `.workbuddy`**（`git ls-files` 实测 `memory/`
  下 5 个 `reply-to-lead*.md` 受跟踪，一把梭会把项目记忆藏起来）。
  安全断言（防「忽略=删除」事故重演）：把全部 1,838 个已跟踪文件喂给 `git check-ignore --stdin`
  ⇒ 被新规则命中 **0 个**；`git status` 里 'D' 条目 **0**。结果 untracked **53,751 → 210**。
  如实登记边界：`/dist/` 比原有条目更宽，将来若要在 dist 下**新增**交付物需 `git add -f`。
- **执行层抽象收敛（L5）**：`engine.SetLogger` 原先断言 `e.runner.(*executor.Runner)` ——
  接口里带具体类型等于没脱离结构体，一旦执行器被 `FailoverRunner` 包住（`main.go` 的做法）
  断言就不匹配，尝试日志对模型池路径静默失效。改为沿 `Inner()` 链找 `AttachHooks` 能力接口
  （`executor.Runner.AttachHooks` + `modelpool.FailoverRunner.AttachHooks`），新增执行通道只要
  实现 `RunnerContract + AttachHooks` 就自动获得尝试日志。三条守护测试（含 nil 链不 panic、
  BeforeRun 中止归 `boot_failed`），变异测试验证「回退成具体类型断言」必判红。
- **`limiter_integration_test.go` 判据改相对量**：原断言「单次 Run < 2 秒」在全量并行
  （`go test ./...`）下实测 4.12 秒而判红 —— 那 4 秒是 `linkDir` spawn cmd.exe + 建目录 +
  写覆盖层的宿主排队开销，与「限流器是否等待」无关，属判据测错对象。改为测**两次连续 Run 的
  相对差**（满桶首次立即通过、第二次才被 rate 拖住），宿主整体变慢时两次一起变慢不受影响。
- **`limiter_snapshot_test.go` 判据方向纠正**：改造前断言「未装闸门 = 快照零值」，把 D9 那个
  「生产从未接线」的缺陷当成了正确行为。现在要求装配后读数非零（零值即回归）。
- **覆盖率提升并固化基线**（issue #3）：`internal/chain` 0% → 88.6%、
  `internal/judge` 0% → 63.7%（含空输入 / 非法 JSON / 越界钳位 / 40 万字符超长负面用例）；
  `build.ps1 -PerPackageCover` 新增 `chain=80`、`judge=55` 诚实基线（只许抬不许降）。
- CI 触发分支补 `master`（建仓默认分支实为 master，此前 workflow 只写 main 导致 push 不触发）。
- Windows 8.3 短路径（`RUNNER~1`）导致的 CI 假失败：路径比较统一经 `evalExisting` 归一。
- `store.Update` 由原地写改为换指针，消除与 `runOne` defer 的真 data race（`-race` 哨兵抓出）。

### Changed
- **模型池清单改为按渠道分组管理**（老大 2026-10-20 反馈：「控制台模型池页面乱七八糟的，
  做成像 Our Free Model 一样统一分类管理的」）。改造前 37 个模型平铺成一张 9 列表，
  不分渠道、不看健康度，一屏全是行 —— 「乱七八糟」的具体来源是：OFM 车道 13 个里有 10 个
  是「上游判定不可用」的自动停用项，与真正能用的 3 个混在一起，把可用的埋掉了。
  现在按 `Spec.Group` 分四张卡片（OFM 免费 / Kilo 免费池 / EAC 协付 / 网关专属车道），
  每张卡片头部是该组聚合健康度（`pill` 定色 + 可用/总数 + 下线 + 停用 + 成功/失败 + 均延迟），
  形态对齐 Our Free Model 设置页的渠道卡片。组内再分两层：**能调度的默认展开**，
  **停用/下线的折进「另 N 个停用/下线」**。序号改组内独立编号（原跨组连续编号会让第 2 组
  第一行显示「14」，读起来像「全池第 14 个」）。分组维度由服务端 `Spec.Group` 给出，
  前端不自己推断，避免两处口径漂移。
  实测（37 条真实池数据、`--ui-probe` 隔离实例）：4 分组卡片、4 个默认展开、2 个默认折叠，
  组头读数 `OFM 免费 可用3/13 停用10 成功/失败3/1`、`Kilo 免费池 可用17/17 均延迟86.9s`、
  `EAC 协付 可用5/5`、`网关专属车道 可用0/2 停用2`。`go build`/`go vet` rc=0、
  `go test ./...` **27 包全 ok**（含 `TestPoolTouchContract` 窄屏 data-label 契约未破）。
  改动文件：`internal/web/assets_pool.go`（`renderList` 重写 + `poolRow` 抽函数 + `poolFold` 默认展开）、
  `internal/web/pool_page.go`（新增 `.pool-group*` 页内样式，未污染全局 `assets_css.go`）。

### Fixed
- **「任务/产出/模板/设置 子页面被挡住」**（老大 2026-10-20 二轮反馈：「除了总览界面没问题，
  任务、产出、模板、设施子页面都被挡住了」）。**这不是错觉也不是缓存**：老大窗口是 1040×613
  物理像素，而本机屏幕 175% 缩放（`devicePixelRatio=1.75`），页面真实 CSS 视口只有 **594×350**；
  在这么矮的视口里，顶栏+页签条吃掉 **239px = 68% 屏高**，子页只剩 **99px** 可见。
  总览首屏是 4 个矮小的 KPI 数字，能露出来；其余四页首屏是卡片标题区，几乎全被推出视口 ⇒
  体感「被挡住」。（先按 z-index/遮挡查了两轮全绿，因为 `elementFromPoint` 全命中正确元素；
  真正的判据是**内容可见高度占视口比例**。）
  修复：`@media (max-width:720px)` 重做窄屏排版 —— 顶栏由「三行堆叠」改单行（品牌只留 9px 色块、
  状态与按钮同排）、页面标题 30px→15px、页签 44~46px→38px；导航总高 **239px → 78px（-67%）**，
  内容可见 **99px → 222px（+124%）**。同时修两个连带缺陷：① `@media (pointer:coarse), (max-width:720px)`
  把「窗口窄」误当「手指触屏」，桌面窄窗口页签被强制 44px，已拆成只有真触屏才 44px；
  ② 模型池顶栏 4 个按钮在 390px 下把页面撑宽 18px（`scrollWidth=408 > clientWidth=390`），
  `.topactions` 改 `flex-wrap:wrap + min-width:0`，实测 `overflowX=0`。
  新增 `pane` 前端契约（逐页签量：内容是否钻进导航区 `overlap≤1px`、页签条是否被压扁
  `tabsHeight≥30`、**导航占视口 `navRatio≤45%`**），门禁用例 **20 → 23**，新增
  `pane-geometry`(1440×900) / `pane-geometry-narrow`(1040×613) / **`pane-geometry-realwindow`(594×350 = 问题现场)**。
  双向反证：修复前 v1.3.2 在新判据下 **FAIL**（`navRatio=55.1`、1040 档模板页 `paneTextLen=0`），
  修复后 **PASS 23/23**（`navRatio=22.3%`、五页 `overlap=0`、`visible=222px`）。
  `go vet` rc=0、`go test ./...` **27 包全 ok**；发布件 v1.3.3（SHA256 `C92E074F…62CC3`）。
  详见 `docs/子页面被挡住-根因与修复-2026-10-20.md`。
- **页签条被 flex 压成 0 高度（真渲染 bug）**（老大 2026-10-20 二轮截图反馈「页签像被切掉半截」）：
  CDP 实测 `.tabs` 的 `height = 0`，而内部 `.tab` 高 46px，页签溢出父容器叠到下方卡片上。
  根因是 flexbox 规范细节：`body.admin` 为 `flex-direction:column`，而 `.tabs` 带 `overflow-x:auto`，
  **overflow 非 visible 的 flex item 自动最小尺寸为 0**，于是被内容挤扁（滚动时 z-index 4 < 吸顶栏 20 更明显）。
  修复：`.tabs` 显式 `flex:0 0 auto` + `min-height:46px`。实测改前 `tabs.h=0` → 改后 `tabs.h=46`
  （同一探针、同一 1040×613 窗口）。新增 `TestTabsCannotBeSquashedByFlex`（只取 `.tabs{}` 规则块断言 +
  剥离反证防恒真）。同类风险已排查：`.content` 无 overflow、`.table-scroll`/`.logbox` 在 block 卡片内，
  只有 `.tabs` 同时是 flex item 且自带 overflow。
- **任务页空态摆了两张空卡**（同一张截图）：三张卡里两张只写「队列是空的」「当前没有执行中」，
  而此刻唯一能做的事（加任务）还藏在折叠里。改为全空时只留一张卡 = 状态摘要 + 一句解释
  「引擎会自动从模板生成，不需要你手动加」+ **直接摊开**的填表；有任务时才回到表格 + 折叠表单。
  新增 `TestQueuePageHasActionWhenEmpty`。发布件 v1.3.2（SHA256 `9286B468…1CF3`）。

### Changed
- **界面简化：管理面板 10 个页签收敛为 5 个，次级信息全部折叠**（老大 2026-10-20 反馈
  「页面乱七八糟的东西太多了，不够简洁，对用户不友好」）。这不是换皮，是信息架构手术：
  旧版把「系统能吐出的所有数字」平铺在一屏 —— 总览 10 张 KPI 卡 + 5 张明细卡顺排（1414px）、
  模型池 9 列表格 + 常显 12 字段添加表单（1228px）、治理台 hero 结论带下面再念一遍同内容的
  banner，用户要的信息反而被淹没。
  改法五条：① 页签 **10 → 5**（总览/任务/产出/模板/设置；自主+治理并入总览，评估+工作目录+
  审计日志并入设置），旧深链 `#logs`/`#evaluate`/`#workspace` 经 `LEGACY_TABS` 映射、
  hash 原样保留，老书签不失效；② 首屏只留 **4 个真关心的读数**（今日产出/排队/成功率/累计完成），
  其余降级进折叠块；③ 新增 `<details class="fold">` 折叠件，模型健康度/运行时资源/失败原因/
  评估权重/换工作目录/访问审计/操作流水/失败任务一律收起；④ 空态改成一句话人话
  （「队列是空的 —— 引擎会自动从模板生成任务」），不再是整块空白；⑤ 顶栏 6 按钮 + 3 链接
  收敛为 3 按钮 + 1「更多」菜单。
  治理台顶栏同步统一为「刷新 + 立即采样 + 更多」，跨页入口收进菜单。
  模型池同步瘦身：9 列 → 6 列（删「延迟」「最近错误/备注」两个半数行为空的列，合并成模型名
  下的小字），行内操作 6 → 4 高频 + 「⋯」收纳恢复/删除，添加表单从常显改折叠（高级参数再折一层），
  车道表并入总览、状态日志折叠。治理台删除与 hero 完全重复的 banner，首启无数据时给
  「点立即采样」的说明而非铺 7 张全 0 卡，四个取证明细卡折叠。
  判据同步（简化不许回退，也不许把功能删掉）：新增 `TestAdminTabsStaySimplified`
  （页签清单双向钉死 + 旧深链映射必须存在）、`TestTopbarHasSingleMoreMenu`、
  `TestAdminPageHasWorkspaceSection`（工作目录从独立页签变设置页一块，但 `fillWorkspace`
  能力必须在）；前端 `ADMIN_TABS` 常量 + tabs 契约数量/清单双向校验，`assertions_selftest`
  新增 4 条反证（页签被加回 / 丢主入口 / 旧书签成死路 / 旧书签丢 hash），cdp_probe 新增
  `legacy-hash-admin` 用例，门禁用例 19 → **20**。
  顺手修掉门禁自身的一处假绿隐患：探针已增用例但 `frontend-gate.mjs` 的 `EXPECTED_CASES`
  仍停在 19（回执显示「20/19」），该常量是「结果集完整」的唯一判据，写小即允许漏跑仍判 PASS；
  已改 20 并加启动前自检（从探针源码数用例，不符即 exit 3 硬停），双向反证通过。
  非技术用户视角补做（v1.3.1）：把模型池/设置页的行话改成白话并附代价说明 ——
  线协议→「接口类型（OpenAI 标准对话 / Responses / Anthropic 格式）」、Tier→「用途偏好
  （质量优先（慢一点，写得好）/ 均衡 / 速度优先）」、车道→「线路来源」、BaseURL→「接口地址」、
  DSH profile→「运行配置（一般不用改）」；模板库从 24 行全平铺改为**默认只显示价值先验最高的
  8 个 + 「还有 N 个，全部展开」**并加搜索框（按名/领域过滤，250ms 去抖）。
  新增 `TestTemplatesLibraryIsSearchableAndFolded`、`TestFormLabelsAvoidJargon`（后者反向禁止
  ">Tier<"/"线协议<select"/"DSH profile</td>" 回潮）。
  实测：`go vet ./...` rc=0；`go test ./... -count=1` **27 包全 ok**；候选件与发布件前端门禁
  均 **PASS ok=20 fail=0 unverified=0**；量化设计审计 15 页低对比 **0 处**（最低 5.29:1）、
  焦点缺失 0、横向溢出 0；admin 总览页高 1414px → **471px**。发布件 `dist\workbench.exe` v1.3.0
  （换件前备份 `workbench.exe.bak-pre-simple-*`）。详见 `docs/界面简化-信息架构收敛-2026-10-20.md`。


### Fixed
- **托盘「退出工作台」关不掉**（老大 2026-10-19 反馈「程序怎么我点退出工作台关不掉了」）：
  点退出后进程确实退了，但**≤60 秒就被外部守护重新拉起并自动开跑**，用户观感＝关不掉。
  根因是「生产者与消费者各缺一半」：产品侧早有表达能力（`engine.MarkUserQuit` 落
  `state.json` 的 `userQuit` + `runtime/user-quit.flag`），但**只有 `--quit`/面板
  `/api/engine/quit` 这条 HTTP 路径在用**，托盘这条不经 HTTP 的入口漏了；
  而守护 `tests/keepalive.ps1`（计划任务 `WorkBenchKeepAlive`，每分钟一次）创建至今
  **从未读取过该标记**，把 `stopped`/`idle` 一律当「该拉起来」。
  实测复现（旧二进制 + 真实托盘命令 `WM_COMMAND 1006`）：退出后 60s 被拉起开跑、120s 又拉一次。
  修复：① `main.go` 新增 `quitByUser`（先 `MarkUserQuit` 再优雅退出），托盘 `OnQuit` 接入；
  **signal(SIGTERM) 路径刻意不落标记**——被动中断重启后理应接续，落标记会破坏 7x24。
  ② `tests/keepalive.ps1` 新增 `Test-UserQuit`（flag 主判据 + `state.json` 回退），
  **两处**拉起判定都加 `-not $userQuit` 守卫；脚本**绝不自己清标记**（清除点只在
  `Engine.Start`，守护替用户清标记等于替用户改主意）。
  验证：隔离实例端到端 `tests/quit_acceptance.ps1` **13 PASS / 0 FAIL**（含负向对照：
  清标记后守护必须拉起，证明是「尊重意愿」而非「关掉守护」）；变异测试
  `tests/quit_mutation_check.ps1` 证明去掉守卫即复现缺陷；真实安装盘上托盘退出后
  `userQuit=true` **连续 105 秒（≥2 个守护周期）未被拉起**（标记存活比「没看到进程」更强：
  只有 `Engine.Start` 会清它，它还在就说明守护没动手）。
  详见 `docs/缺陷-托盘退出关不掉-2026-10-19.md`。
- **首页入口「启动中点它白点」**（2026-10-19 目标轮：添加首页进入子页面的入口）：首页唯一按钮就是子界面入口，  但实测抓到两类会让入口失效的时序缺陷 —— ① busy 态被 `lock:true` 禁用，disabled 的按钮连 `click` 都不派发；  ② 开屏 900ms 自动启动在途时，用户这一击被 `if (inflight) { return; }` 直接吞掉，页面停在首页。  取证：CDP 直连把 `/api/engine/start` 拖慢 6s 造出在途窗口，点击后 9s 内 `location.href` 从未离开 `/`（`landed-not-admin`）。  修复：busy 改 `lock:false`（防重入交给 inflight），在途点击记 `pendingEnter`，启动收敛后补一次 `enterPanel()`——吞请求不吞意图。  判据：新增 `home_entry_contract_test.go`（正则断言，抗视觉重做的空格漂移）+ 前端探针 `home-enter` 用例  （`tests/ui/probes/enter.js` 真实点击 → 轮询落地 URL），门禁用例 18→19。实测：`go vet ./...` rc=0、  `go test ./internal/web ./cmd/workbench` 全绿、`frontend-gate` 19/19 PASS、busy 窗口点击端到端落到 `/admin?token=`。
- **快捷方式「点了打不开」**（老大 2026-10-19 反馈）：桌面快捷方式写死了 `--silent`，
  而该参数语义是"服务化实例：不弹面板"，于是双击后进程与端口都正常，
  **桌面上却什么都不出现**。实测对照：`--silent` 新起可见窗口 0 个；
  不带则 1.7s 内出现面板窗口。修复：快捷方式 `Arguments=""` + `WindowStyle=1`，
  回读校验从"只看 TargetPath"升级为 TargetPath+Arguments 双校验；
  `ensureShortcuts` 从"跑过一次永远跳过"改为每次启动校验并修复（否则旧快捷方式永不被纠正）。
  详见 `docs/缺陷-点击打不开与托盘无图标-2026-10-19.md`。
- **托盘图标永不出现**（老大 2026-10-19 反馈「状态栏也没有图标」）：安装目录
  `D:\workdesktop` 被文件系统沙箱打上 **Low 完整性标签**，从其启动的进程继承 Low IL，
  被 UIPI 拒绝向 Medium 的 Explorer 注册托盘图标（`NIM_ADD` → `Access is denied`）。
  取证为排除法：同一二进制在 `C:\Temp`/`D:\`/`D:\app-cache` 全部 `ret=1 OK`，
  只有 `D:\workdesktop\**` 失败；且 Low 进程自己修不了（`icacls` 返回 1299）。
  四层处置：① 目录标签提升为 Medium（实测图标立即出现）；② `tests/keepalive.ps1`
  每分钟自愈（Medium 上下文）；③ `internal/tray/integrity_windows.go` 进程内自检 +
  失败原因经 `tray.Options.OnWarn` 上报日志与事件流（杜绝"静默少一个图标"）；
  ④ `setup/build.ps1` 新增完整性标签门禁。顺带：托盘图标改为取 EXE 自带图标
  （原为 `IDI_APPLICATION` 通用白板图），并接 `TaskbarCreated` 广播在 Explorer
  重启后重新注册。
- **`--silent` 未覆盖「已在运行」分支**：守护/压测/自愈用 `--silent` 撞上既有实例时，
  原实现仍会往桌面弹面板（老大反复反馈"一直在跳"的来源之一）。现两处面板调用点
  统一经 `shouldOpenPanel(silent)` 判断，并新增 `已打开面板：` 输出标记，
  使该行为可被黑盒判据抓住（双向判据 + 变异测试验证可证伪）。
- **桌面快捷方式被临时目录副本劫持（复发性「打不开」根因）**：`isCanonicalExe` 原判据
  只判「文件名 + 同目录数据根」，而隔离 e2e 为了模拟真实实例，**正是**在
  `t.TempDir()` 里复制二进制并建出 `runtime/`、`workbench-output/` —— 形状完全一致。
  于是只要机器上跑过一次端到端测试，`ensureShortcuts` 就把桌面快捷方式改写到
  `D:\app-cache\temp\Test* 002\workbench.exe`（该目录随后被测试框架清掉），
  老大双击就是"打不开"。本轮实测抓获：快捷方式 TargetPath 指向已不存在的临时路径。
  修复：判据加第三条「不落在系统临时目录内」，并拆成 `hasInstallShape` + `underTempDir`
  两条正交判据分别可测（合成判据在测试里恒假）。变异测试验证：去掉临时目录排除即判红；
  连跑两轮全量测试后快捷方式始终指向 `dist\workbench.exe`。
  这也解释了该现象为何此前"修好了又复发"——不是同一 bug 反复，而是两个 bug 轮流生效。
- **产物 MOTW 安全警告**：带 `Zone.Identifier` 的 EXE 双击必弹「无法验证发布者」，
  属"必须人工点一下"的步骤，无人值守下直接卡死启动（实测 ShellExecute 报
  `The operation was canceled by the user`）。`setup/build.ps1` 新增 MOTW 门禁：
  带标记即 `Unblock-File`，清不掉则构建判红。

### Added
- **首页子页面入口条**（老大 2026-10-19 当轮指令：「其它子页面的入口在哪？出现在这个页面上」）：
  首页按钮下方新增三个可见入口「管理面板 / 模型池 / 治理」（`.home-entries`，href 服务端注入 token）。
  旧「隐藏后门 ·」契约随之作废——`TestIndexPageMinimalContract`/`TestHiddenBackdoorIsDotOnly` 改写为
  `TestIndexPageEntryContract`（1 按钮 + 3 可见入口 + 禁 aria-hidden/tabindex=-1 + 禁表单），
  占位符 6→8、探针 HOME_DEFAULTS anchors 1→3、textVisibleLen 阈值改走期望常量（曾硬编码 6 致自测假红）。
  实测：`go test ./internal/web` 全绿；前端门禁 19/19 PASS；三个入口逐一真实点击均带 token 落地对应子页；
  发布件换件重启后 17890 活实例复验（入口可见 + 点「模型池」→ `/modelpool?token=`）。
- **开源对标报告入主树**：`docs/benchmark/WorkBench-开源对标报告.md`（8 个 peer：
  n8n / dify / AutoGPT / autogen / temporal / river / asynq / windmill，八维对比 +
  P0/P1/P2 清单 + M0-M4 路径），此前只存在于受 `.gitignore` 屏蔽的 `src-backup/`。
- **知识盲区与学习计划**：`docs/知识盲区与学习计划.md`，B1-B8 盲区绑容器实测读数，
  L1-L9 学习路径含精读对象与机器判据。
- **开源化三件套**：`ARCHITECTURE.md`（单页定位/进程模型/包结构/五条架构取舍）、
  `.github/workflows/ci.yml`（vet + `go test -race` + 覆盖率门禁 + 产物校验）、
  `autonomy-e2e.yml`（容器内确定性通道跑完整自主闭环）。
- **issue/PR 协作模板**：`.github/ISSUE_TEMPLATE/`（缺陷报告 / 功能请求 / config.yml）、
  `.github/pull_request_template.md`（判据必须贴命令与真实输出）、
  `.github/labels.json` + `setup/labels.ps1`（12 个正交标签）。
- **零弹窗拉起共用件** `tests/lib_silent_launch.ps1`：按 PE 子系统分流，
  GUI 走 `Win32_Process.Create`（保持脱离作业对象），CONSOLE 走
  `Start-Process -WindowStyle Hidden`。
- **崩溃救援额度（L4）**：`model.Task.CrashRescues` 记录跨崩溃的救援轮次，
  `internal/engine/stuck_rescue.go` 的 `decideRescue` 分流「重新入队 / 判死」，
  状态机新增 `interrupted → failed` 出口。修的是**无界崩溃循环**：
  原先 `Recover()` 把 interrupted 无条件转回 queued，而 `Attempts` 是单次 `runOne`
  内的序号（每轮被覆盖），不能当崩溃预算 —— 于是"一跑就弄死进程"的任务
  配合 KeepAlive 每分钟拉起会无限重投，看起来"一直在干活"实际零产出。
  判死写 `ErrorKind=crash_loop`，面板失败原因分布可区分。
- **社区协作面（L7）**：`.github/ISSUE_TEMPLATE/`（缺陷报告 8 字段 / 功能请求 7 字段 /
  config.yml）、`.github/pull_request_template.md`、`.github/labels.json` +
  `setup/labels.ps1`（12 个正交标签，含本仓特有的 `flaky` / `platform-coupling` /
  `external-constraint`）。模板把本项目的判据文化写成必填项：
  「取不到数写 UNVERIFIED 不要填 0」「必须贴命令与真实输出」「外部约束另打标签」。
- **三条仓库级门禁（已挂 CI 的 Repo hygiene gates 步骤）**：
  `tests/check_changelog.ps1`（Unreleased 必须居首 / 日期必须 YYYY-MM-DD / 版本降序）、
  `tests/check_noninteractive.ps1`（禁 `Invoke-WebRequest` 缺 `-UseBasicParsing`、
  计划任务缺 `-NonInteractive`、以及 Read-Host / Get-Credential / -Confirm 等交互构造）、
  `tests/check_issue_forms.ps1`（Issue Forms 结构：顶层三键、每块 attributes.label、
  dropdown/checkboxes 的 options 必须位于 attributes 之内、contact_links 必须用 about）。
  三条判据均经**变异测试**验证能抓回对应缺陷（不是"跑过了"就算数）。
- **CONTRIBUTING 新增三节**：九 协作流程与判据文化、十 零弹窗纪律、十一 延伸文档。

### Fixed
- **无人值守弹窗挂死**（老大 2026-10-19 17:29 反馈"这个东西后台静默不要弹出窗口"）：
  守护计划任务每分钟拉起 `tests/keepalive.ps1`，其中一条 `Invoke-WebRequest` 缺
  `-UseBasicParsing` —— PowerShell 5.1 因此调用 IE 引擎解析响应页，而面板页含 `<script>`
  （实测 2 处），触发 "Security Warning: Script Execution Risk … Do you want to continue?"
  等人工按键；同时任务注册命令行缺 `-NonInteractive`，两者叠加 = 桌面弹窗 + 任务永久挂起。
  修复：`-UseBasicParsing` + `$ConfirmPreference="None"` + `$ProgressPreference` +
  注册补 `-NonInteractive` 与 `Settings.Hidden`；新增 `tests/check_noninteractive.ps1`
  静态门禁（已挂 CI，变异测试验证可抓「只在注释里提 flag」的洗绿形态）。
- **自主闭环决策留痕顺序**（issue #1）：`autonomy.advanceLocked` 与 `bootstrapLocked`
  的恢复路径原先「先切活跃目标、后写 nextGoal 决策」，而两者写不同文件
  （goals.json / decisions.jsonl，非原子），留下一个可观测窗口 —— 面板与
  `--selftest` 判据 7b 会看到「目标已切换但台账无推导记录」，CI 上
  `TestRunOneDrivesAutonomyClosedLoop` 的偶发红即源于此。现收敛为
  `writeLedgerThenActivate`（先留痕后生效），并由顺序守卫测试钉死。
- **非 Windows 删除无安全阀**（issue #2）：`retention.Trash` 的 `!windows` 分支此前是裸
  `os.RemoveAll`，危险路径判定只存在于 Windows 分支 —— 实测 `Trash("/")` 在 Linux
  会递归删掉文件系统根。现 `refuseDangerousPath` 上移为平台无关实现，两条删除路径同过闸。
- **产物子系统门禁**：`setup/build.ps1` 校验 EXE 的 PE `Subsystem` 必须为 2(GUI)，
  非 GUI 直接判红。控制台子系统 EXE 被计划任务/`Win32_Process.Create` 拉起时
  Windows 必分配可见控制台窗口（桌面反复弹窗的根因）。
- **测试硬编码 Windows 工具链**（issue #2）：engine / verify 的 10 个用例改为解释器探针
  （pwsh→python3→python→node）；Windows 上全缺时 `t.Fatal`，禁止 `t.Skip` 洗绿。
  Linux 容器 `go test ./...` 从「3 包 10 用例 FAIL」转为全绿。
- **失败判据被偷换**：`TestMaterializeArtifactsFail` 原先只断言 `Pass == false`，
  而解释器缺失导致的「启动失败」同样 Pass=false —— 测试绿但什么都没验。
  现补断 `FailReason` 必须含「退出码 2」。

### Changed
- **覆盖率提升并固化基线**（issue #3）：`internal/chain` 0% → 88.6%、
  `internal/judge` 0% → 63.7%（含空输入 / 非法 JSON / 越界钳位 / 40 万字符超长负面用例）；
  `build.ps1 -PerPackageCover` 新增 `chain=80`、`judge=55` 诚实基线（只许抬不许降）。
- CI 触发分支补 `master`（建仓默认分支实为 master，此前 workflow 只写 main 导致 push 不触发）。
- Windows 8.3 短路径（`RUNNER~1`）导致的 CI 假失败：路径比较统一经 `evalExisting` 归一。
- `store.Update` 由原地写改为换指针，消除与 `runOne` defer 的真 data race（`-race` 哨兵抓出）。

## [1.3.0] - 2026-10-19

### Added
- **WAL 事件重建（P2-3）**：队列写路径从"每次迁移全量重写 queue.json"改为
  "追加 `queue.wal.jsonl` + 定期压缩成快照"。启动时按「快照 ⊕ WAL 回放」重建内存状态，
  崩溃后只丢未落盘的最后半行（回放时保守丢弃坏行及其后续）。
  - 新增 `store.Savepoint()` 语义：写快照 → fsync → 截断 WAL（任一步崩溃不丢数据）
  - 压缩阈值默认 2000 条，可在 `wal.threshold` 调整
  - 实测：100 次迁移 WAL 均 759 字节/条，而全量快照 100,786 字节 ⇒ 旧实现每次写放大 ~133×
- **CONTRIBUTING.md**：硬约束、本地环境、常用命令、提交前检查清单、代码组织约定、提交信息规范、发布流程、排障入口。
- **CHANGELOG.md**：按 Keep a Changelog 回填 v1.0.0 / v1.1.0 / v1.2.0 三个版本。

### Changed
- **executor 纯逻辑抽取（P1-2 收尾）**：把 `Run` 里的参数拼装、环境变量构造、超时解析、
  stderr 摘要、结果判定抽成纯函数（`BuildArgs`/`BuildArgsWithPatches`/`BuildEnv`/`ResolveTimeout`/
  `FinalizeResult`/`stderrSummary`/`effectiveMaxBytes`/`normalizeConcurrency`），`Run` 只留编排。
  executor 覆盖率 **14.4% → 27.4%**。
- `setup/build.ps1` 覆盖率基线更新：executor 14→26、新增 modelpool=49。
- `tests/e2e.ps1` 加固（应对同机并发会话互相杀进程）：
  1. 进程清理只针对「本验收目标 Exe 路径」，不再按进程名全杀；
  2. 新增 `Invoke-Api`/`Refresh-Session`：连接被拒时重新解析 panel.url 与 token 后重试；
  3. 非空产出率改为只看「本轮新产出」任务，历史遗留短件不计入（避免判据被历史数据绑架）。

### Fixed
- `store` 在 Windows 上长期持有 WAL 文件句柄会导致测试临时目录清理失败（`unlinkat: file in use`）——
  改为每次追加 open→write→sync→close，不持长期句柄。

## [1.2.0] - 2026-10-19

### Added
- **结构化日志（P1-1）**：新增 `internal/logging` 包，基于标准库 `log/slog` 输出单行 JSON 到
  `<DataRoot>/logs/workbench.jsonl`；提供 `At(component)` 子日志器、`Redact` 凭据打码、级别过滤。
- **执行钩子与执行器接口（P1-2）**：`executor.Hooks`（`BeforeRun`/`AfterRun`，钩子 panic 被 recover 且不影响主流程）；
  新增 `executor.RunnerContract` 接口与 `Engine.SetRunner` 注入点，引擎不再依赖具体执行器结构体。
- **指标接口（P1-3）**：`GET /api/metrics` 返回堆内存/Alloc/goroutine/句柄/GC、任务聚合、
  最近 30 条尝试台账与资源阈值上限；面板概览页新增「运行时指标」卡片。
- **失败原因分布（P1-4）**：`Engine.FailureBreakdown()` + `GET /api/failures`，
  按 `ErrorKind` 聚合失败任务（count/lastAt/samples，空态返回 `empty: true`）；面板新增「失败原因分布」卡片。
- **CI（P2-2）**：`.github/workflows/ci.yml`（windows-latest）：`go vet` → `go test -race` →
  `setup/build.ps1`（含覆盖率门禁）→ 产物校验（存在性/大小区间/`--version`/`--diagnose`）。
- 验收脚本新增第 9 段 OBSERVABILITY（12 项断言）。

### Changed
- `setup/build.ps1` 覆盖率门禁支持 `-PerPackageCover` 分档基线；默认阈值 55%。
- 引擎日志统一带 `component=engine` 字段，可按来源过滤。

### Removed
- 下线三处各自为政的裸日志写入：`engine-debug.log`（engine.debugf）、
  `supervisor.log`（自愈记录）、`crash.log`（panic 兜底）；内容并入 `workbench.jsonl`。

### Fixed
- 编辑/日志字段口径不一致导致的排障困难（同一事件分散在三个文件、格式各异）。

## [1.1.0] - 2026-10-18

### Added
- **错误分类重试策略**：新增 `internal/retry`（11 类失败原因 → retry/fail/cancel 三分判定，
  指数退避 + 向下抖动 + 可重试白名单；旧 `retryBackoffMs` 列表配置完全兼容）。
- **执行尝试台账**：`tasks/<id>/attempts.jsonl` 追加写，逐次记录 attempt/model/kind/action/delay/elapsed。
- **优雅停机两段 drain**：新增 `executor.shutdownGraceSeconds`（默认 20s），宽限期后强制取消在跑任务。
- **覆盖率门禁**：`setup/build.ps1` 新增 `-CoverThreshold`（默认 55）与 `-CoverGate` 相关开关。
- 对标报告：`docs/benchmark/WorkBench-开源对标报告.md`（8 个同类开源项目八维对比 + 证据附录）。

### Changed
- `Engine.CancelTask` 对运行中任务从"只写审计"改为**真正取消执行上下文**。
- `failTask` 不再把 `ErrorKind` 硬编码成 `execution`，改为写入执行器给出的真实分类。

### Fixed
- 任务完成后 `events.jsonl` 被空内容覆盖为 0 字节（现场 29 个任务受影响）。
- 被拒任务无法重试：状态机缺少 `rejected → queued` 通道，与 `RetryTask` 允许集矛盾。

## [1.0.0] - 2026-10-18

### Added
- 首个可用版本：Windows 单 EXE 桌面工作台（Go 纯标准库，7.4 MB，无外部运行时依赖）。
- 19 个内部包：配置/存储/队列/引擎/执行器/评估/链生成/语义判分/模型路由/模板/隔离 profile/面板/托盘/单实例/自愈/快捷方式/浏览器拉起。
- 24 条内置任务模板（6 领域 × 4），模板 CRUD、导入导出、恢复默认。
- 三维评估（价值/成本/可行性）+ 硬门禁（重复/高相似/冷却/日上限）+ 语义判分（异步降级安全）。
- 链式迭代：后继模板、自评 nextIdeas、前序产出摘要注入、链深度与扇出限制。
- 全自动闭环：生成 → 评估 → 自动批准 → 队列 → 执行 → 归档 → 自评 → 后继。
- 面板（7 页签 + SSE 实时推送）、MD/TXT/DOCX 导出、开机自启、系统托盘、崩溃恢复。
- 资源自愈：内存/句柄/goroutine 采样，连续越阈自动重启。
- 验收：e2e 20 项全过，单文件 EXE 净目录可跑。

### Known issues
- 7×24 长时压测未跑满 24h（已完成 3 轮长时 + 3 次崩溃注入）。